-- SQLite: Feishu login identity binding on users.
ALTER TABLE users ADD COLUMN feishu_open_id VARCHAR(64);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_feishu_open_id
    ON users (feishu_open_id)
    WHERE feishu_open_id IS NOT NULL AND feishu_open_id <> '';
