-- 游戏封面（外链）、定价（分）、标签（JSON 数组）
ALTER TABLE games ADD COLUMN cover_url VARCHAR(512) NOT NULL DEFAULT '' COMMENT '封面图 HTTPS 外链';
ALTER TABLE games ADD COLUMN price_cents BIGINT NOT NULL DEFAULT -1 COMMENT '价格（分），-1 未填，0 免费';
ALTER TABLE games ADD COLUMN tags JSON NULL COMMENT '标签 JSON 数组';
