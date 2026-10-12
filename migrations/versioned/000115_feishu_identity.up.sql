-- Migration 000115: Feishu login identity binding on users.
-- Nullable unique open_id so existing password/OIDC accounts can be linked
-- on first Feishu login without colliding on empty values.

ALTER TABLE users
    ADD COLUMN IF NOT EXISTS feishu_open_id VARCHAR(64);

CREATE UNIQUE INDEX IF NOT EXISTS idx_users_feishu_open_id
    ON users (feishu_open_id)
    WHERE feishu_open_id IS NOT NULL AND feishu_open_id <> '';

COMMENT ON COLUMN users.feishu_open_id IS 'Feishu open_id for SSO login binding';
