-- 用户账户余额（仅持久化 MySQL，不作 Redis 缓存）；新建用户默认 500
ALTER TABLE users ADD COLUMN account_balance BIGINT NOT NULL DEFAULT 500 COMMENT '账户余额(整数，默认500)';
