-- +goose Up
-- Thêm column deleted_at để đồng bộ với BaseModel (soft delete của GORM).
-- DLQMessage embed BaseModel có DeletedAt *time.Time, nhưng migration gốc thiếu column này
-- dẫn đến lỗi SQLSTATE 42703 khi GORM INSERT.
ALTER TABLE dlq_messages
    ADD COLUMN deleted_at TIMESTAMPTZ;

CREATE INDEX idx_dlq_messages_deleted_at ON dlq_messages (deleted_at);

-- +goose Down
DROP INDEX IF EXISTS idx_dlq_messages_deleted_at;
ALTER TABLE dlq_messages
    DROP COLUMN IF EXISTS deleted_at;
