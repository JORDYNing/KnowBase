CREATE TABLE IF NOT EXISTS tenant_join_requests (
 id INTEGER PRIMARY KEY AUTOINCREMENT,
 tenant_id INTEGER NOT NULL REFERENCES tenants(id),
 user_id VARCHAR(36) NOT NULL REFERENCES users(id),
 source VARCHAR(32) NOT NULL CHECK (source IN ('feishu', 'feishu_bootstrap')),
 status VARCHAR(20) NOT NULL CHECK (status IN ('pending', 'approved', 'rejected')),
 role VARCHAR(20) NOT NULL DEFAULT '',
 review_message VARCHAR(500) NOT NULL DEFAULT '',
 reviewed_by VARCHAR(36),
 reviewed_at DATETIME,
 version INTEGER NOT NULL DEFAULT 1 CHECK (version > 0),
 created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
 updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_join_requests_user_tenant ON tenant_join_requests(tenant_id, user_id);
CREATE UNIQUE INDEX IF NOT EXISTS idx_tenant_join_requests_bootstrap ON tenant_join_requests(tenant_id) WHERE source = 'feishu_bootstrap';
CREATE INDEX IF NOT EXISTS idx_tenant_join_requests_status ON tenant_join_requests(tenant_id, status, id);
