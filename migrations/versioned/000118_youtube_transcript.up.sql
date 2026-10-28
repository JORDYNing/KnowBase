ALTER TABLE tenants ADD COLUMN IF NOT EXISTS youtube_transcript_config JSONB;
CREATE TABLE IF NOT EXISTS youtube_import_locks (
    id VARCHAR(64) PRIMARY KEY,
    owner VARCHAR(36) NOT NULL,
    expires_at BIGINT NOT NULL
);
