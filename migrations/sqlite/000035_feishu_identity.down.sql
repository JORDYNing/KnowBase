DROP INDEX IF EXISTS idx_users_feishu_open_id;
-- SQLite cannot DROP COLUMN on older versions reliably; leave the column.
