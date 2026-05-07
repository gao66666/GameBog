-- 文章封面图（外链 URL，非本地上传）
ALTER TABLE articles ADD COLUMN cover_url VARCHAR(512) NOT NULL DEFAULT '' COMMENT '封面图外链';
