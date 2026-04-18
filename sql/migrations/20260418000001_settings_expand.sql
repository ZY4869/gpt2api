-- +goose Up
-- +goose StatementBegin

-- ============================================================
-- 扩充 system_settings 的 key 种子:
--   general(通用)、security(安全与认证)、defaults(用户默认值)、
--   gateway(网关与调度)、billing(计费与充值)、mail(邮件)
-- 采用"已存在则保留"语义,不覆盖用户已修改的值。
-- ============================================================

INSERT INTO `system_settings` (`k`, `v`, `description`) VALUES
    -- ---------- 通用 ----------
    ('site.docs_url',         '',       '对外文档链接;留空则前端隐藏"文档"入口'),
    ('site.api_base_url',     '',       '展示给用户的 /v1 入口地址(支持反代;留空=当前站点地址)'),
    ('ui.default_page_size',  '20',     '管理端表格默认分页大小(5~100)'),

    -- ---------- 安全与认证 ----------
    ('auth.require_email_verify',   'false', '新用户注册时是否必须验证邮箱(预留开关,实装后生效)'),
    ('auth.email_domain_whitelist', '',      '允许注册的邮箱域名,逗号分隔(如 qq.com,gmail.com),留空=不限'),
    ('auth.password_min_length',    '6',     '密码最小长度(6~64)'),
    ('auth.invite_code_required',   'false', '是否必须使用邀请码注册(预留)'),
    ('auth.2fa_enabled',            'false', '是否启用二次验证(预留)'),
    ('auth.jwt_access_ttl_sec',     '7200',  'Access Token TTL(秒);修改后新发 token 生效'),
    ('auth.jwt_refresh_ttl_sec',    '604800','Refresh Token TTL(秒);修改后新发 token 生效'),

    -- ---------- 用户默认值(KEY 侧) ----------
    ('key.default_daily_quota_credits', '0',  '新建 API Key 时默认每日配额(积分·厘);0=不限'),
    ('key.max_per_user',                '20', '单用户最多可创建的 API Key 数(0=不限)'),

    -- ---------- 网关与调度 ----------
    ('gateway.upstream_timeout_sec', '60',   '上游非流式请求超时(秒)'),
    ('gateway.sse_read_timeout_sec', '120',  '上游 SSE 流式读超时(秒)'),
    ('gateway.cooldown_429_sec',     '300',  '账号 429 冷却(秒)'),
    ('gateway.warned_pause_hours',   '24',   '风险警告账号暂停小时数'),
    ('gateway.daily_usage_ratio',    '0.8',  '账号日用比例(0.0~1.0);超过后降低优先级'),
    ('gateway.retry_on_failure',     'true', '失败时是否自动切换账号重试一次'),
    ('gateway.retry_max',            '1',    '单次请求的最大重试次数(0~3)'),

    -- ---------- 计费与充值 ----------
    ('billing.credit_per_cny',       '10000','人民币/积分换算展示;默认 1 元 = 10000 积分·厘'),
    ('billing.notify_admin_on_adjust','false','管理员调账时邮件通知超管(预留)'),
    ('recharge.enabled',             'true', '充值入口总开关;关闭后前端隐藏充值菜单,下单接口返回 403'),
    ('recharge.min_cny',             '100',  '单笔最低金额(分);如 100 = 1 元'),
    ('recharge.max_cny',             '0',    '单笔最高金额(分);0=不限'),
    ('recharge.daily_limit_cny',     '0',    '单用户每日累计上限(分);0=不限'),
    ('recharge.order_expire_minutes','30',   '订单有效期(分钟);到期自动置为 canceled')
ON DUPLICATE KEY UPDATE `k` = VALUES(`k`);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
-- 不删除数据:这些条目即便删除对业务也无害,但保留可追溯。
-- +goose StatementEnd
