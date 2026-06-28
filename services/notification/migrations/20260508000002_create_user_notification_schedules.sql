-- +goose Up
-- +goose StatementBegin
CREATE TABLE user_notification_schedules (
    id           UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT CURRENT_TIMESTAMP,
    deleted_at   TIMESTAMPTZ,
    user_id      VARCHAR(100) NOT NULL,
    event_type   VARCHAR(100) NOT NULL,
    send_time    VARCHAR(5)   NOT NULL,
    timezone     VARCHAR(100) NOT NULL DEFAULT 'UTC',
    enabled      BOOLEAN      NOT NULL DEFAULT TRUE,
    payload      TEXT,
    last_sent_at TIMESTAMPTZ,
    CONSTRAINT uq_schedule_user_event UNIQUE (user_id, event_type)
);

CREATE INDEX idx_schedule_user_id ON user_notification_schedules (user_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_schedule_due ON user_notification_schedules (enabled, last_sent_at) WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP TABLE IF EXISTS user_notification_schedules;
-- +goose StatementEnd
