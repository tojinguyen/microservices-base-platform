-- +goose Up
-- +goose StatementBegin
ALTER TABLE notifications
    ADD COLUMN scheduled_at TIMESTAMPTZ NOT NULL DEFAULT NOW();

CREATE INDEX idx_notifications_scheduled_pending
    ON notifications (scheduled_at, status)
    WHERE status = 'pending';
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_notifications_scheduled_pending;
ALTER TABLE notifications DROP COLUMN scheduled_at;
-- +goose StatementEnd
