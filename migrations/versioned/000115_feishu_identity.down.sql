DROP INDEX IF EXISTS idx_users_feishu_open_id;
ALTER TABLE users DROP COLUMN IF EXISTS feishu_open_id;
