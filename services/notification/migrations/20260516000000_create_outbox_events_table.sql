-- +goose Up

CREATE TABLE outbox_events (
    id             UUID         PRIMARY KEY DEFAULT gen_random_uuid(),
    aggregate_id   UUID         NOT NULL,
    aggregate_type VARCHAR(100) NOT NULL,
    event_type     VARCHAR(100) NOT NULL,
    payload        JSONB        NOT NULL,
    routing_key    VARCHAR(100) NOT NULL,
    status         VARCHAR(20)  NOT NULL DEFAULT 'pending',
    retry_count    INT          NOT NULL DEFAULT 0,
    last_error     TEXT,
    published_at   TIMESTAMPTZ,
    created_at     TIMESTAMPTZ  NOT NULL DEFAULT NOW()
);

-- Partial index: worker only scans unpublished pending rows
CREATE INDEX idx_outbox_events_pending
    ON outbox_events (created_at ASC)
    WHERE published_at IS NULL AND status = 'pending';

-- Trace: find all outbox events for a given notification
CREATE INDEX idx_outbox_events_aggregate
    ON outbox_events (aggregate_id, aggregate_type);

-- +goose Down
DROP INDEX IF EXISTS idx_outbox_events_aggregate;
DROP INDEX IF EXISTS idx_outbox_events_pending;
DROP TABLE IF EXISTS outbox_events;
