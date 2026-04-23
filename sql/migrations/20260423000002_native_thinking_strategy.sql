-- +goose Up
-- +goose StatementBegin

INSERT INTO `system_settings` (`k`, `v`, `description`) VALUES
  (
    'gateway.chat_image_thinking_strategy',
    'native_thinking',
    'native_thinking=默认/官方对齐; picture_v2_thinking=兼容回滚'
  )
ON DUPLICATE KEY UPDATE
  `v` = CASE
    WHEN TRIM(COALESCE(`v`, '')) = '' THEN 'native_thinking'
    WHEN TRIM(COALESCE(`v`, '')) = 'picture_v2_thinking' THEN 'native_thinking'
    ELSE `v`
  END,
  `description` = VALUES(`description`);

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

UPDATE `system_settings`
   SET `v` = CASE
         WHEN TRIM(COALESCE(`v`, '')) = 'native_thinking' THEN 'picture_v2_thinking'
         ELSE `v`
       END,
       `description` = 'picture_v2_thinking=稳定模式; native_thinking=抓包实验模式'
 WHERE `k` = 'gateway.chat_image_thinking_strategy';

-- +goose StatementEnd
