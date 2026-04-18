package recharge

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/jmoiron/sqlx"
)

var ErrNotFound = errors.New("recharge: not found")

type DAO struct{ db *sqlx.DB }

func NewDAO(db *sqlx.DB) *DAO { return &DAO{db: db} }

// DB 暴露 *sqlx.DB 以便 Service 自己开事务入账。
func (d *DAO) DB() *sqlx.DB { return d.db }

// ---------- Packages ----------

func (d *DAO) ListPackages(ctx context.Context, onlyEnabled bool) ([]Package, error) {
	where := ""
	if onlyEnabled {
		where = "WHERE enabled = 1"
	}
	rows := make([]Package, 0, 8)
	err := d.db.SelectContext(ctx, &rows,
		fmt.Sprintf(`SELECT * FROM recharge_packages %s ORDER BY sort ASC, id ASC`, where))
	return rows, err
}

func (d *DAO) GetPackage(ctx context.Context, id uint64) (*Package, error) {
	var p Package
	err := d.db.GetContext(ctx, &p,
		`SELECT * FROM recharge_packages WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &p, err
}

func (d *DAO) CreatePackage(ctx context.Context, p *Package) (uint64, error) {
	res, err := d.db.ExecContext(ctx,
		`INSERT INTO recharge_packages (name, price_cny, credits, bonus, description, sort, enabled)
         VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.Name, p.PriceCNY, p.Credits, p.Bonus, p.Description, p.Sort, p.Enabled)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	return uint64(id), nil
}

func (d *DAO) UpdatePackage(ctx context.Context, p *Package) error {
	_, err := d.db.ExecContext(ctx,
		`UPDATE recharge_packages SET name=?, price_cny=?, credits=?, bonus=?,
                description=?, sort=?, enabled=? WHERE id=?`,
		p.Name, p.PriceCNY, p.Credits, p.Bonus, p.Description, p.Sort, p.Enabled, p.ID)
	return err
}

func (d *DAO) DeletePackage(ctx context.Context, id uint64) error {
	_, err := d.db.ExecContext(ctx,
		`DELETE FROM recharge_packages WHERE id = ?`, id)
	return err
}

// ---------- Orders ----------

func (d *DAO) CreateOrder(ctx context.Context, o *Order) (uint64, error) {
	res, err := d.db.ExecContext(ctx,
		`INSERT INTO recharge_orders
           (out_trade_no, user_id, package_id, price_cny, credits, bonus,
            channel, pay_method, status, trade_no, pay_url, client_ip, remark)
         VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		o.OutTradeNo, o.UserID, o.PackageID, o.PriceCNY, o.Credits, o.Bonus,
		o.Channel, o.PayMethod, o.Status, o.TradeNo, o.PayURL, o.ClientIP, o.Remark)
	if err != nil {
		return 0, err
	}
	id, _ := res.LastInsertId()
	o.ID = uint64(id)
	return o.ID, nil
}

func (d *DAO) GetByOutTradeNo(ctx context.Context, outTradeNo string) (*Order, error) {
	var o Order
	err := d.db.GetContext(ctx, &o,
		`SELECT * FROM recharge_orders WHERE out_trade_no = ?`, outTradeNo)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &o, err
}

func (d *DAO) GetByID(ctx context.Context, id uint64) (*Order, error) {
	var o Order
	err := d.db.GetContext(ctx, &o,
		`SELECT * FROM recharge_orders WHERE id = ?`, id)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return &o, err
}

// ListFilter 用于 admin / user 端订单列表过滤。
type ListFilter struct {
	UserID uint64
	Status string
	Since  time.Time
	Until  time.Time
}

func (d *DAO) List(ctx context.Context, f ListFilter, offset, limit int) ([]Order, int64, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	if offset < 0 {
		offset = 0
	}
	where := []string{"1=1"}
	args := []any{}
	if f.UserID > 0 {
		where = append(where, "user_id = ?")
		args = append(args, f.UserID)
	}
	if f.Status != "" {
		where = append(where, "status = ?")
		args = append(args, f.Status)
	}
	if !f.Since.IsZero() {
		where = append(where, "created_at >= ?")
		args = append(args, f.Since)
	}
	if !f.Until.IsZero() {
		where = append(where, "created_at < ?")
		args = append(args, f.Until)
	}
	ws := strings.Join(where, " AND ")

	rows := make([]Order, 0, limit)
	if err := d.db.SelectContext(ctx, &rows,
		fmt.Sprintf(`SELECT * FROM recharge_orders WHERE %s ORDER BY id DESC LIMIT ? OFFSET ?`, ws),
		append(args, limit, offset)...); err != nil {
		return nil, 0, err
	}
	var total int64
	if err := d.db.GetContext(ctx, &total,
		fmt.Sprintf(`SELECT COUNT(*) FROM recharge_orders WHERE %s`, ws), args...); err != nil {
		return nil, 0, err
	}
	return rows, total, nil
}

// SumPaidTodayCNY 统计当前用户"今日(DB 服务器时区)"已支付订单总金额(分)。
// 用于每日充值上限判定。
func (d *DAO) SumPaidTodayCNY(ctx context.Context, userID uint64) (int64, error) {
	var sum sql.NullInt64
	err := d.db.GetContext(ctx, &sum,
		`SELECT COALESCE(SUM(price_cny), 0) FROM recharge_orders
           WHERE user_id = ? AND status = 'paid' AND paid_at >= CURDATE()`, userID)
	if err != nil {
		return 0, err
	}
	return sum.Int64, nil
}

// ExpirePending 把所有创建超过 minutes 分钟的 pending 订单置为 expired。
// 返回被更新的行数。用于 cleanup 定时任务 / 管理员手工触发。
func (d *DAO) ExpirePending(ctx context.Context, minutes int) (int64, error) {
	if minutes <= 0 {
		minutes = 30
	}
	res, err := d.db.ExecContext(ctx,
		`UPDATE recharge_orders
           SET status = 'expired'
         WHERE status = 'pending' AND created_at < (NOW() - INTERVAL ? MINUTE)`, minutes)
	if err != nil {
		return 0, err
	}
	n, _ := res.RowsAffected()
	return n, nil
}
