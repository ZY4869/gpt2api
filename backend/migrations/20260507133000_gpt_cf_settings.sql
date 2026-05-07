-- +goose Up
-- +goose StatementBegin
INSERT INTO `system_config` (`key`, `value`, `remark`) VALUES
  ('gpt.cf.enabled', 'false', 'GPT 图片链路是否启用 FlareSolverr 预解题'),
  ('gpt.cf.flaresolverr_url', '"http://flaresolverr:8191"', 'GPT 图片链路 FlareSolverr 地址'),
  ('gpt.cf.timeout_seconds', '90', 'GPT 图片链路 FlareSolverr 单次解题超时'),
  ('gpt.cf.last_error', '""', '最近一次 GPT 图片链路 FlareSolverr 错误'),
  ('gpt.cf.last_refresh_at', '0', '最近一次 GPT 图片链路 FlareSolverr 成功时间')
ON DUPLICATE KEY UPDATE `value`=VALUES(`value`);
-- +goose StatementEnd

-- +goose Down
DELETE FROM `system_config` WHERE `key` IN (
  'gpt.cf.enabled',
  'gpt.cf.flaresolverr_url',
  'gpt.cf.timeout_seconds',
  'gpt.cf.last_error',
  'gpt.cf.last_refresh_at'
);
