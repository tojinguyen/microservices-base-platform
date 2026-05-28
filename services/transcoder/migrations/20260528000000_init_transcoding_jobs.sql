-- +goose Up
CREATE TABLE transcoding_jobs (
    id                UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    video_id          UUID NOT NULL UNIQUE,
    status            VARCHAR(32) NOT NULL DEFAULT 'pending',
    retry_count       INT NOT NULL DEFAULT 0,
    max_retries       INT NOT NULL DEFAULT 3,

    object_key        VARCHAR(512) NOT NULL,
    bucket            VARCHAR(128) NOT NULL,
    mime_type         VARCHAR(64),
    size_bytes        BIGINT,

    hls_base_path     VARCHAR(512),
    master_playlist   VARCHAR(512),
    renditions        JSONB,
    duration_seconds  NUMERIC(10,3),
    output_size_bytes BIGINT,

    error_message     TEXT,
    started_at        TIMESTAMPTZ,
    completed_at      TIMESTAMPTZ,
    created_at        TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at        TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_transcoding_jobs_video_id ON transcoding_jobs(video_id);
CREATE INDEX idx_transcoding_jobs_status   ON transcoding_jobs(status);
CREATE INDEX idx_transcoding_jobs_pending  ON transcoding_jobs(created_at) WHERE status = 'pending';

-- +goose Down
DROP TABLE transcoding_jobs;
