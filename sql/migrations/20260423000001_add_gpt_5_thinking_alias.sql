-- +goose Up
-- +goose StatementBegin

INSERT INTO `models`
  (`slug`, `type`, `upstream_model_slug`,
   `input_price_per_1m`, `output_price_per_1m`, `image_price_per_call`,
   `description`, `enabled`)
VALUES
  ('gpt-5-thinking', 'chat', 'gpt-5-4-thinking',
   40000, 120000, 0,
   'GPT-5 Thinking (stable alias)', 1)
ON DUPLICATE KEY UPDATE
  `type` = VALUES(`type`),
  `upstream_model_slug` = VALUES(`upstream_model_slug`),
  `description` = VALUES(`description`),
  `enabled` = 1;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

UPDATE `models`
   SET `enabled` = 0
 WHERE `slug` = 'gpt-5-thinking';

-- +goose StatementEnd
