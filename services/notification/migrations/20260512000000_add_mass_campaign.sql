-- +goose Up

CREATE TABLE campaigns (
    id                     UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    title                  VARCHAR(255) NOT NULL,
    subject                VARCHAR(255),
    content                TEXT         NOT NULL,
    channel                VARCHAR(20)  NOT NULL,
    event_type             VARCHAR(100) NOT NULL,
    status                 VARCHAR(20)  NOT NULL DEFAULT 'pending',
    scheduled_at           TIMESTAMPTZ  NOT NULL,
    total_recipients       INT          NOT NULL DEFAULT 0,
    last_dispatched_offset INT          NOT NULL DEFAULT 0,
    dispatched_count       INT          NOT NULL DEFAULT 0,
    sent_count             INT          NOT NULL DEFAULT 0,
    failed_count           INT          NOT NULL DEFAULT 0,
    created_at             TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at             TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    deleted_at             TIMESTAMPTZ
);

CREATE INDEX idx_campaigns_dispatch
    ON campaigns (status, scheduled_at)
    WHERE status = 'pending' AND deleted_at IS NULL;

CREATE TABLE campaign_recipients (
    id          UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    campaign_id UUID         NOT NULL REFERENCES campaigns(id),
    user_id     VARCHAR(100) NOT NULL,
    recipient   VARCHAR(255) NOT NULL,
    status      VARCHAR(20)  NOT NULL DEFAULT 'pending',
    created_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at  TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_campaign_recipients_pagination
    ON campaign_recipients (campaign_id, created_at ASC, id ASC);

CREATE INDEX idx_campaign_recipients_campaign
    ON campaign_recipients (campaign_id, status);

-- +goose Down
DROP INDEX IF EXISTS idx_campaign_recipients_campaign;
DROP INDEX IF EXISTS idx_campaign_recipients_pagination;
DROP TABLE IF EXISTS campaign_recipients;
DROP INDEX IF EXISTS idx_campaigns_dispatch;
DROP TABLE IF EXISTS campaigns;
