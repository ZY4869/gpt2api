-- +goose Up
-- +goose StatementBegin

-- ============================================================
-- 充值套餐(管理员配置,用户充值时从中选取)
-- ============================================================
CREATE TABLE IF NOT EXISTS `recharge_packages` (
    `id`          BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `name`        VARCHAR(64)     NOT NULL COMMENT '套餐名,如 "100元套餐"',
    `price_cny`   INT             NOT NULL COMMENT '售价,单位:分(人民币)',
    `credits`     BIGINT          NOT NULL COMMENT '到账积分(厘)',
    `bonus`       BIGINT          NOT NULL DEFAULT 0 COMMENT '赠送积分(厘)',
    `description` VARCHAR(255)    NOT NULL DEFAULT '',
    `sort`        INT             NOT NULL DEFAULT 0,
    `enabled`     TINYINT(1)      NOT NULL DEFAULT 1,
    `created_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`  DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_enabled_sort` (`enabled`, `sort`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='充值套餐';

INSERT INTO `recharge_packages` (`name`, `price_cny`, `credits`, `bonus`, `description`, `sort`) VALUES
    ('¥ 10 体验包',   1000,   10000000,         0, '1000 积分,约 500 次 GPT-5-mini 对话', 10),
    ('¥ 50 小礼包',   5000,   50000000,   5000000, '5000 积分 + 500 额外', 20),
    ('¥ 100 标准包', 10000,  100000000,  15000000, '10000 积分 + 1500 额外(热销)', 30),
    ('¥ 500 专业包', 50000,  500000000, 100000000, '50000 积分 + 10000 额外', 40)
ON DUPLICATE KEY UPDATE `name` = VALUES(`name`);

-- ============================================================
-- 充值订单
--   流程:pending(已创建,待用户跳转支付) ->
--        paid(支付回调成功,积分已入账) /
--        cancelled(用户手动取消) /
--        failed(回调校验失败等)
--   超时逻辑:30 分钟未支付自动标记为 expired(由后台 cleanup 任务扫)。
-- ============================================================
CREATE TABLE IF NOT EXISTS `recharge_orders` (
    `id`           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `out_trade_no` CHAR(32)        NOT NULL COMMENT '本地订单号(UUID 去横线)',
    `user_id`      BIGINT UNSIGNED NOT NULL,
    `package_id`   BIGINT UNSIGNED NOT NULL DEFAULT 0,
    `price_cny`    INT             NOT NULL COMMENT '实付金额(分)',
    `credits`      BIGINT          NOT NULL COMMENT '基础到账积分(厘)',
    `bonus`        BIGINT          NOT NULL DEFAULT 0,
    `channel`      VARCHAR(16)     NOT NULL DEFAULT 'epay' COMMENT 'epay | manual | other',
    `pay_method`   VARCHAR(16)     NOT NULL DEFAULT '' COMMENT 'alipay | wxpay | 等,下发回调填入',
    `status`       VARCHAR(16)     NOT NULL DEFAULT 'pending' COMMENT 'pending | paid | expired | cancelled | failed',
    `trade_no`     VARCHAR(64)     NOT NULL DEFAULT '' COMMENT '上游交易号,回调填入',
    `paid_at`      DATETIME        NULL,
    `pay_url`      VARCHAR(512)    NOT NULL DEFAULT '',
    `client_ip`    VARCHAR(64)     NOT NULL DEFAULT '',
    `notify_raw`   TEXT            NULL COMMENT '最近一次回调原文,方便排查',
    `remark`       VARCHAR(255)    NOT NULL DEFAULT '',
    `created_at`   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `updated_at`   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_out_trade_no` (`out_trade_no`),
    KEY `idx_user_time` (`user_id`, `created_at`),
    KEY `idx_status_time` (`status`, `created_at`),
    KEY `idx_trade_no` (`trade_no`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='充值订单';

-- +goose StatementEnd


-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `recharge_orders`;
DROP TABLE IF EXISTS `recharge_packages`;
-- +goose StatementEnd
