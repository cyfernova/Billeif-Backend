CREATE TABLE IF NOT EXISTS a2a_task_events (
    sequence BIGSERIAL PRIMARY KEY,
    task_id UUID NOT NULL,
    event_id VARCHAR(64) NOT NULL UNIQUE,
    event_type VARCHAR(32) NOT NULL,
    data JSONB NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_a2a_task_events_task_sequence
    ON a2a_task_events (task_id, sequence);

CREATE INDEX IF NOT EXISTS idx_a2a_task_events_created_at
    ON a2a_task_events (created_at);
