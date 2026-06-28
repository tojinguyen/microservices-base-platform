-- +goose Up

CREATE TABLE upload_sessions (
    id               UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_id         UUID NOT NULL REFERENCES videos(id) ON DELETE CASCADE,
    s3_upload_id     VARCHAR(512) NOT NULL,
    object_key       VARCHAR(512) NOT NULL,
    bucket           VARCHAR(128) NOT NULL,
    mime_type        VARCHAR(64),
    total_parts      INT NOT NULL,
    part_size_bytes  BIGINT NOT NULL,
    total_size_bytes BIGINT NOT NULL,
    status           VARCHAR(32) NOT NULL DEFAULT 'initiated',
    expires_at       TIMESTAMPTZ NOT NULL,
    completed_at     TIMESTAMPTZ,
    created_at       TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at       TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- status values: initiated | in_progress | completing | completed | aborted | expired

CREATE INDEX idx_upload_sessions_video_id ON upload_sessions(video_id);
CREATE INDEX idx_upload_sessions_status   ON upload_sessions(status);
CREATE INDEX idx_upload_sessions_active_expiry
    ON upload_sessions(expires_at) WHERE status IN ('initiated', 'in_progress');

CREATE TABLE upload_parts (
    id                 UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id         UUID NOT NULL REFERENCES upload_sessions(id) ON DELETE CASCADE,
    part_number        INT NOT NULL,
    etag               VARCHAR(128),
    size_bytes         BIGINT,
    status             VARCHAR(16) NOT NULL DEFAULT 'pending',
    presign_url        TEXT,
    presign_expires_at TIMESTAMPTZ,
    uploaded_at        TIMESTAMPTZ,
    created_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (session_id, part_number)
);

-- status values: pending | uploaded

CREATE INDEX idx_upload_parts_session_id ON upload_parts(session_id);
CREATE INDEX idx_upload_parts_status     ON upload_parts(session_id, status);

-- +goose Down
DROP TABLE upload_parts;
DROP TABLE upload_sessions;
