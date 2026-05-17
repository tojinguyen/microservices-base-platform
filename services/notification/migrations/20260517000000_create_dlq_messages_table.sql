-- +goose Up
CREATE TABLE dlq_messages (
    id              UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    notification_id UUID         REFERENCES notifications(id) ON DELETE SET NULL,
    queue_name      VARCHAR(100) NOT NULL,
    payload         JSONB        NOT NULL,
    error_message   TEXT         NOT NULL,
    status          VARCHAR(20)  NOT NULL DEFAULT 'pending',
    created_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at      TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_dlq_messages_status ON dlq_messages(status, queue_name);

-- +goose Down
DROP INDEX IF EXISTS idx_dlq_messages_status;
DROP TABLE IF EXISTS dlq_messages;
