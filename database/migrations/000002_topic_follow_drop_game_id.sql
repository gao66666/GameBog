-- 话题关注仅保留 user_id + topic_id；游戏映射在 game_topic_maps 表。
-- 若库中仍有 topic_follows.game_id 列，执行本脚本后重启应用（GORM 模型已无该字段）。
-- +goose Up
ALTER TABLE topic_follows DROP COLUMN game_id;

-- +goose Down
-- ALTER TABLE topic_follows ADD COLUMN game_id BIGINT UNSIGNED NOT NULL DEFAULT 0 AFTER topic_id;
