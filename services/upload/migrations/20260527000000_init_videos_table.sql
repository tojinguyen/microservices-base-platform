-- +goose Up
CREATE TABLE videos (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    title             VARCHAR(255) NOT NULL,
    description       TEXT,
    owner_id          UUID,
    object_key        VARCHAR(512) NOT NULL UNIQUE,
    bucket            VARCHAR(128) NOT NULL,
    mime_type         VARCHAR(64),
    size_bytes        BIGINT,
    etag              VARCHAR(128),
    status            VARCHAR(32) NOT NULL DEFAULT 'pending_upload',
    storage_url       VARCHAR(1024),
    upload_expires_at TIMESTAMPTZ,
    uploaded_at       TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_videos_status ON videos(status);
CREATE INDEX idx_videos_owner ON videos(owner_id);
CREATE INDEX idx_videos_pending_expiry
    ON videos(upload_expires_at)
    WHERE status = 'pending_upload';

-- +goose Down
DROP TABLE videos;
