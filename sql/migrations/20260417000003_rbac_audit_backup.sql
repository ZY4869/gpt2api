-- +goose Up
-- +goose StatementBegin

-- ============================================================
-- 审计日志表
-- ============================================================
CREATE TABLE IF NOT EXISTS `admin_audit_logs` (
    `id`           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `actor_id`     BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT '操作者 user_id',
    `actor_email`  VARCHAR(128)    NOT NULL DEFAULT '',
    `action`       VARCHAR(128)    NOT NULL COMMENT '语义化 action,如 accounts.create',
    `method`       VARCHAR(8)      NOT NULL,
    `path`         VARCHAR(255)    NOT NULL,
    `status_code`  INT             NOT NULL DEFAULT 0,
    `ip`           VARCHAR(64)     NOT NULL DEFAULT '',
    `ua`           VARCHAR(255)    NOT NULL DEFAULT '',
    `target`       VARCHAR(128)    NOT NULL DEFAULT '' COMMENT '业务对象 id/slug',
    `meta`         JSON            NULL,
    `created_at`   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (`id`),
    KEY `idx_actor_time` (`actor_id`, `created_at`),
    KEY `idx_action_time` (`action`, `created_at`),
    KEY `idx_created_at` (`created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='管理员操作审计日志';

-- ============================================================
-- 数据库备份元数据
--   实际 .sql.gz 文件落盘在 ${BACKUP_DIR},这里只存元数据。
-- ============================================================
CREATE TABLE IF NOT EXISTS `backup_files` (
    `id`           BIGINT UNSIGNED NOT NULL AUTO_INCREMENT,
    `backup_id`    VARCHAR(64)     NOT NULL COMMENT '对外 ID,形如 bk_20260417_120000',
    `file_name`    VARCHAR(255)    NOT NULL,
    `size_bytes`   BIGINT          NOT NULL DEFAULT 0,
    `sha256`       CHAR(64)        NOT NULL DEFAULT '',
    `trigger`      VARCHAR(16)     NOT NULL DEFAULT 'manual' COMMENT 'manual | cron | upload',
    `status`       VARCHAR(16)     NOT NULL DEFAULT 'running' COMMENT 'running | ready | failed',
    `error`        VARCHAR(500)    NOT NULL DEFAULT '',
    `include_data` TINYINT(1)      NOT NULL DEFAULT 1 COMMENT '0=仅结构',
    `created_by`   BIGINT UNSIGNED NOT NULL DEFAULT 0 COMMENT 'actor user_id',
    `created_at`   DATETIME        NOT NULL DEFAULT CURRENT_TIMESTAMP,
    `finished_at`  DATETIME        NULL,
    PRIMARY KEY (`id`),
    UNIQUE KEY `uk_backup_id` (`backup_id`),
    KEY `idx_status_time` (`status`, `created_at`),
    KEY `idx_trigger_time` (`trigger`, `created_at`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='数据库备份索引';

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

DROP TABLE IF EXISTS `backup_files`;
DROP TABLE IF EXISTS `admin_audit_logs`;

-- +goose StatementEnd
