-- +goose Up
-- +goose StatementBegin
CREATE INDEX idx_notifications_user_created_id
    ON notifications (user_id, created_at DESC, id DESC)
    WHERE deleted_at IS NULL;
-- +goose StatementEnd

-- +goose Down
-- +goose StatementBegin
DROP INDEX IF EXISTS idx_notifications_user_created_id;
-- +goose StatementEnd
