-- +goose Up
-- +goose StatementBegin

-- ============================================================
-- GPT-Image-2 上线(上游 chatgpt.com 灰度模型 gpt-5-3,system_hints=picture_v2)
--
-- 协议参考 legacy/gen_image.py:init → prepare → f/conversation → poll → download
-- 灰度命中判据:conversation.mapping 里 IMG2 tool 消息数 ≥ 2
-- 价格:每次成功出图 50 积分(即 0.5 元,按 1 积分 = 0.01 元 + 100 倍放大存厘)
-- ============================================================

-- 旧的 gpt-image-1 占位记录禁用,不删除(可能有历史 usage_logs 外键软引用)
UPDATE `models`
   SET `enabled` = 0,
       `description` = '[deprecated] 旧占位,请用 gpt-image-2'
 WHERE `slug` = 'gpt-image-1';

-- 新增 gpt-image-2(核心图像模型)
INSERT INTO `models`
    (`slug`, `type`, `upstream_model_slug`, `input_price_per_1m`, `output_price_per_1m`, `image_price_per_call`, `description`)
VALUES
    ('gpt-image-2', 'image', 'gpt-5-3', 0, 0, 500000, 'GPT-Image-2 高清生图(picture_v2 灰度)')
ON DUPLICATE KEY UPDATE
    `type`                 = VALUES(`type`),
    `upstream_model_slug`  = VALUES(`upstream_model_slug`),
    `image_price_per_call` = VALUES(`image_price_per_call`),
    `description`          = VALUES(`description`),
    `enabled`              = 1;

-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin

UPDATE `models`
   SET `enabled` = 0
 WHERE `slug` = 'gpt-image-2';

UPDATE `models`
   SET `enabled` = 1,
       `description` = '生图(每张 50 积分=5角)'
 WHERE `slug` = 'gpt-image-1';

-- +goose StatementEnd
