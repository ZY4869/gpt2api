-- +goose Up
-- +goose StatementBegin

-- ============================================================
-- 2026-04-18 扩展模型目录
--
-- 背景:账号池当前可用的上游模型(来自 /backend-api/models)远多于初始种子,
--       把它们全部登记进 `models` 表,便于 billing / 路由 / UI 下拉统一使用。
--
-- 价格策略(积分-厘,1 积分 = 0.0001 元;下表均为每 1M token 价):
--
--   level          | input  | output | 备注
--   ---------------+--------+--------+------------------------------------
--   旗舰 5.x       |  25000 |  75000 | gpt-5 / gpt-5-x(非 mini / 非 thinking)
--   Thinking       |  40000 | 120000 | 深度推理,计费更高
--   o3             |  15000 |  60000 | reasoning 中档
--   Mini / Instant |   5000 |  15000 | 轻量 / 快速
--   研究 & Agent   |  30000 |  90000 | Deep Research / Agent
--
-- 所有价格都可以在「模型配置」页面实时调整,这里只是合理默认值。
-- 已存在的同名 slug 会被 ON DUPLICATE KEY UPDATE 覆盖 upstream / description,
-- 但保留原有价格和 enabled 状态,避免覆盖你之前的人工调整。
-- ============================================================

INSERT INTO `models`
  (`slug`, `type`, `upstream_model_slug`,
   `input_price_per_1m`, `output_price_per_1m`,
   `cache_read_price_per_1m`, `image_price_per_call`,
   `description`, `enabled`)
VALUES
  -- 旗舰 5.x
  ('gpt-5-1',          'chat', 'gpt-5-1',          25000,  75000,  5000, 0, 'GPT-5.1',          1),
  ('gpt-5-2',          'chat', 'gpt-5-2',          25000,  75000,  5000, 0, 'GPT-5.2',          1),
  ('gpt-5-2-instant',  'chat', 'gpt-5-2-instant',  25000,  75000,  5000, 0, 'GPT-5.2 Instant',  1),
  ('gpt-5-3',          'chat', 'gpt-5-3',          25000,  75000,  5000, 0, 'GPT-5.3',          1),
  ('gpt-5-3-instant',  'chat', 'gpt-5-3-instant',  25000,  75000,  5000, 0, 'GPT-5.3 Instant',  1),

  -- Thinking(深度推理档)
  ('gpt-5-2-thinking', 'chat', 'gpt-5-2-thinking', 40000, 120000,  8000, 0, 'GPT-5.2 Thinking', 1),
  ('gpt-5-4-thinking', 'chat', 'gpt-5-4-thinking', 40000, 120000,  8000, 0, 'GPT-5.4 Thinking', 1),

  -- Mini / Instant
  ('gpt-5-3-mini',     'chat', 'gpt-5-3-mini',      5000,  15000,  1000, 0, 'GPT-5.3 Mini',     1),
  ('gpt-5-t-mini',     'chat', 'gpt-5-t-mini',      5000,  15000,  1000, 0, 'GPT-5 Thinking Mini',   1),
  ('gpt-5-4-t-mini',   'chat', 'gpt-5-4-t-mini',    5000,  15000,  1000, 0, 'GPT-5.4 Thinking Mini', 1),

  -- reasoning 中档
  ('o3',               'chat', 'o3',               15000,  60000,  3000, 0, 'o3',               1),

  -- 研究 / Agent
  ('research',         'chat', 'research',         30000,  90000,  6000, 0, 'Deep Research',    1),
  ('agent-mode',       'chat', 'agent-mode',       30000,  90000,  6000, 0, 'Agent',            1)
ON DUPLICATE KEY UPDATE
  `type`                = VALUES(`type`),
  `upstream_model_slug` = VALUES(`upstream_model_slug`),
  `description`         = VALUES(`description`)
  -- 价格 / enabled 故意不覆盖,尊重人工配置
  ;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

-- Down 只下架本次新增的模型(不动 gpt-5 / gpt-5-mini / gpt-image-* 等老种子)
UPDATE `models`
   SET `enabled` = 0
 WHERE `slug` IN (
   'gpt-5-1','gpt-5-2','gpt-5-2-instant','gpt-5-3','gpt-5-3-instant',
   'gpt-5-2-thinking','gpt-5-4-thinking',
   'gpt-5-3-mini','gpt-5-t-mini','gpt-5-4-t-mini',
   'o3','research','agent-mode'
 );

-- +goose StatementEnd
