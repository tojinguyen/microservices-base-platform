-- +goose Up
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS filter_criteria JSONB       NOT NULL DEFAULT '{}',
    ADD COLUMN IF NOT EXISTS target_audience VARCHAR(50) NOT NULL DEFAULT 'all_users';

-- +goose Down
ALTER TABLE campaigns
    DROP COLUMN IF EXISTS filter_criteria,
    DROP COLUMN IF EXISTS target_audience;
