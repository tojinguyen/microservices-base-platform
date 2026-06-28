-- +goose Up
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS last_dispatched_cursor VARCHAR(36) NOT NULL DEFAULT '',
    DROP COLUMN IF EXISTS last_dispatched_offset;

-- +goose Down
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS last_dispatched_offset INT NOT NULL DEFAULT 0,
    DROP COLUMN IF EXISTS last_dispatched_cursor;
