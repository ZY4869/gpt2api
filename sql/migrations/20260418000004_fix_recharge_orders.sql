-- +goose Up
-- +goose StatementBegin

-- ============================================================
-- 修复 recharge_orders 表结构:
--   init_schema (20260417000001) 里用的是旧列名(order_no/amount/epay_trade_no/callback_raw),
--   后续 migration 20260417000004 用 CREATE TABLE IF NOT EXISTS,表已存在导致新结构未生效。
--   结果就是 Go DAO 里 SELECT * 时报 `missing destination name order_no in *[]recharge.Order`。
--
-- 本迁移策略(幂等):
--   1. 若当前 recharge_orders 不含新列 `out_trade_no`,视为"还是旧结构",
--      把它 RENAME 成 recharge_orders_legacy_bak 保留数据(若备份表已存在则先删旧备份)。
--   2. 然后 CREATE TABLE IF NOT EXISTS 建出新结构。
--   3. 如果已经是新结构,上面两步都会是 no-op。
-- ============================================================

SET @has_new_col := (
    SELECT COUNT(*)
      FROM information_schema.COLUMNS
     WHERE TABLE_SCHEMA = DATABASE()
       AND TABLE_NAME   = 'recharge_orders'
       AND COLUMN_NAME  = 'out_trade_no'
);

-- 旧结构:清理旧备份
SET @sql1 := IF(@has_new_col = 0,
    'DROP TABLE IF EXISTS `recharge_orders_legacy_bak`',
    'DO 0');
PREPARE s1 FROM @sql1; EXECUTE s1; DEALLOCATE PREPARE s1;

-- 旧结构:重命名老表作为备份
SET @sql2 := IF(@has_new_col = 0,
    'RENAME TABLE `recharge_orders` TO `recharge_orders_legacy_bak`',
    'DO 0');
PREPARE s2 FROM @sql2; EXECUTE s2; DEALLOCATE PREPARE s2;

-- 无论如何:确保新结构存在
CREATE TABLE IF NOT EXISTS `recharge_orders` (
    `id`           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `out_trade_no` CHAR(32)        NOT NULL COMMENT '本地订单号(UUID 去横线)',
    `user_id`      BIGINT UNSIGNED NOT NULL,
    `package_id`   BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `price_cny`    INT             NOT NULL COMMENT '实付金额(分)',
    `credits`      BIGINT          NOT NULL COMMENT '基础到账积分(厘)',
    `bonus`        BIGINT          NOT NULL DEFAULT 0,
    `channel`      VARCHAR(16)     NOT NULL DEFAULT 'epay',
    `pay_method`   VARCHAR(16)     NOT NULL DEFAULT '',
    `status`       VARCHAR(16)     NOT NULL DEFAULT 'pending',
    `trade_no`     VARCHAR(64)     NOT NULL DEFAULT '',
    `paid_at`      DATETIME        NULL,
    `pay_url`      VARCHAR(512)    NOT NULL DEFAULT '',
    `client_ip`    VARCHAR(64)     NOT NULL DEFAULT '',
    `notify_raw`   TEXT            NULL,
    `remark`       VARCHAR(255)    NOT NULL DEFAULT '',
    `created_at`   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_out_trade_no` (`out_trade_no`),
    KEY `idx_user_time`   (`user_id`, `created_at`),
    KEY `idx_status_time` (`status`, `created_at`),
    KEY `idx_trade_no`    (`trade_no`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='充值订单';

-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
-- 回滚不尝试恢复旧结构,只是删新表。旧数据仍在 recharge_orders_legacy_bak。
DROP TABLE IF EXISTS `recharge_orders`;
-- +goose StatementEnd
