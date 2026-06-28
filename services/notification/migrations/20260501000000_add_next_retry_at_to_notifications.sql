-- +goose Up
-- +goose StatementBegin
ALTER TABLE notifications ADD COLUMN next_retry_at TIMESTAMP WITH TIME ZONE;
CREATE INDEX idx_notifications_next_retry_at ON notifications(next_retry_at);
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_notifications_next_retry_at;
ALTER TABLE notifications DROP COLUMN IF EXISTS next_retry_at;
-- +goose StatementEnd
