-- +goose Up
-- +goose StatementBegin

-- ============================================================
-- 系统设置(KV 存储)
--   由 internal/settings 维护,启动时加载到内存缓存,Set 时写 DB + 更新缓存。
--   命名约定:snake_case + 分类前缀(site. / auth. / mail. / limit.)。
--   所有值统一存为字符串;解析由 Go 侧按 schema 负责(bool / int / duration 等)。
-- ============================================================
CREATE TABLE IF NOT EXISTS `system_settings` (
    `k`           VARCHAR(64)  NOT NULL COMMENT '配置项 key,例 site.name',
    `v`           TEXT         NULL     COMMENT '配置项值(字符串序列化)',
    `description` VARCHAR(255) NOT NULL DEFAULT '',
    `updated_at`  DATETIME     NOT NULL DEFAULT CURRENT_TIMESTAMP ON UPDATE CURRENT_TIMESTAMP,
    PRIMARY KEY (`k`)
) ENGINE=InnoDB DEFAULT CHARSET=utf8mb4 COMMENT='系统设置 KV';

-- 默认值种子(已存在则保留原值不覆盖)
INSERT INTO `system_settings` (`k`, `v`, `description`) VALUES
    ('site.name',           'GPT2API',                '站点名称,展示在登录页/顶栏'),
    ('site.description',    '企业级 OpenAI 兼容网关',  '登录页副标题'),
    ('site.logo_url',       '',                        '站点 Logo URL,空则显示默认图标'),
    ('site.footer',         '',                        '页脚版权/备案文本(支持纯文本)'),
    ('site.contact_email',  '',                        '站点对外联系邮箱'),

    ('auth.allow_register',        'true',  '是否开放用户自助注册'),
    ('auth.default_group_id',      '1',     '新用户默认分组 ID(user_groups.id)'),
    ('auth.signup_bonus_credits',  '0',     '新用户注册赠送积分(单位:厘;10000=1 积分)'),

    ('limit.default_rpm',          '60',    '用户默认 RPM(未在 group/key 覆盖时生效)'),
    ('limit.default_tpm',          '60000', '用户默认 TPM'),

    ('mail.enabled_display',       'auto',  '邮件开关展示(auto/true/false),实际是否可用由 SMTP 配置决定')
ON DUPLICATE KEY UPDATE `k` = VALUES(`k`);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS `system_settings`;
-- +goose StatementEnd
