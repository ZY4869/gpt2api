package billing

import (
	"context"
	"errors"
	"fmt"

	"github.com/jmoiron/sqlx"
)

// AdminAdjust 管理员手工调账。
//
//	delta > 0  加积分(例如补偿/赠送)
//	delta < 0  扣积分(例如反作弊回收),允许把余额扣到 >=0(扣到负数会返回错误)
//
// 同时写一条 type=admin_adjust 的流水,ref_id 建议填 admin 的 user_id 字符串,
// remark 由调用方传入人类可读原因。actorID 是发起者 user_id,仅写入 remark 前缀,
// 方便审计时快速定位。
//
// 幂等性:调用方需自己保证(比如前端按钮 debounce);
// 服务端只做原子执行,不去重。
func (e *Engine) AdminAdjust(ctx context.Context, targetUserID, actorID uint64, delta int64, refID, remark string) (balanceAfter int64, err error) {
	if delta == 0 {
		return 0, errors.New("delta must not be zero")
	}
	err = e.runTx(ctx, func(tx *sqlx.Tx) error {
		// 扣款时 WHERE 子句保证不会扣成负数
		var res sqlResult
		if delta > 0 {
			res, err = execR(tx, ctx,
				`UPDATE users
                    SET credit_balance = credit_balance + ?, version = version + 1
                  WHERE id = ? AND deleted_at IS NULL`, delta, targetUserID)
		} else {
			neg := -delta
			res, err = execR(tx, ctx,
				`UPDATE users
                    SET credit_balance = credit_balance - ?, version = version + 1
                  WHERE id = ? AND credit_balance >= ? AND deleted_at IS NULL`,
				neg, targetUserID, neg)
		}
		if err != nil {
			return err
		}
		if res.RowsAffected == 0 {
			if delta < 0 {
				return ErrInsufficient
			}
			return fmt.Errorf("user %d not found", targetUserID)
		}
		if err := tx.QueryRowxContext(ctx,
			`SELECT credit_balance FROM users WHERE id = ?`, targetUserID).Scan(&balanceAfter); err != nil {
			return err
		}
		fullRemark := remark
		if actorID > 0 {
			fullRemark = fmt.Sprintf("[by admin=%d] %s", actorID, remark)
		}
		_, err = tx.ExecContext(ctx,
			`INSERT INTO credit_transactions
              (user_id, key_id, type, amount, balance_after, ref_id, remark)
             VALUES (?, ?, ?, ?, ?, ?, ?)`,
			targetUserID, 0, KindAdjust, delta, balanceAfter, refID, fullRemark)
		return err
	})
	return
}

// sqlResult 对 sql.Result 的简化,只保留 RowsAffected,避免在 runTx 里多次判断错误。
type sqlResult struct {
	RowsAffected int64
}

func execR(tx *sqlx.Tx, ctx context.Context, q string, args ...interface{}) (sqlResult, error) {
	res, err := tx.ExecContext(ctx, q, args...)
	if err != nil {
		return sqlResult{}, err
	}
	n, _ := res.RowsAffected()
	return sqlResult{RowsAffected: n}, nil
}
