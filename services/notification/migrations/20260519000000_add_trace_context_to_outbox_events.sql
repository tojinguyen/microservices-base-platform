-- +goose Up
-- Tách trace_context ra column riêng thay vì nhúng trong payload JSON.
-- Giúp payload chỉ chứa NotificationTask thuần, tránh lỗi unmarshal ở email worker.
ALTER TABLE outbox_events
    ADD COLUMN trace_context JSONB;

-- +goose Down
ALTER TABLE outbox_events
    DROP COLUMN IF EXISTS trace_context;
