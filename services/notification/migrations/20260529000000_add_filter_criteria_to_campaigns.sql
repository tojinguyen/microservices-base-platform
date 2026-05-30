-- +goose Up
ALTER TABLE campaigns
    ADD COLUMN IF NOT EXISTS filter_criteria JSONB NOT NULL DEFAULT '{}',
    ALTER COLUMN target_audience SET DEFAULT 'all_users';

-- +goose Down
ALTER TABLE campaigns
    DROP COLUMN IF EXISTS filter_criteria,
    ALTER COLUMN target_audience SET DEFAULT 'specific';
