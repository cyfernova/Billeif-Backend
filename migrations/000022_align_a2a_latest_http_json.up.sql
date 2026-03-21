-- Align persisted A2A storage with the latest HTTP+JSON task model.

ALTER TABLE a2a_tasks
    ALTER COLUMN state TYPE VARCHAR(32);

ALTER TABLE a2a_tasks
    ADD COLUMN IF NOT EXISTS user_id VARCHAR(255),
    ADD COLUMN IF NOT EXISTS business_id VARCHAR(255);

UPDATE a2a_tasks
SET state = CASE state
    WHEN 'working' THEN 'TASK_STATE_WORKING'
    WHEN 'completed' THEN 'TASK_STATE_COMPLETED'
    WHEN 'failed' THEN 'TASK_STATE_FAILED'
    WHEN 'cancelled' THEN 'TASK_STATE_CANCELLED'
    WHEN 'input-required' THEN 'TASK_STATE_INPUT_REQUIRED'
    WHEN 'rejected' THEN 'TASK_STATE_REJECTED'
    ELSE state
END
WHERE state IN ('working', 'completed', 'failed', 'cancelled', 'input-required', 'rejected');

ALTER TABLE a2a_tasks
    DROP CONSTRAINT IF EXISTS a2a_tasks_state_check;

ALTER TABLE a2a_tasks
    ADD CONSTRAINT a2a_tasks_state_check CHECK (
        state IN (
            'TASK_STATE_SUBMITTED',
            'TASK_STATE_WORKING',
            'TASK_STATE_COMPLETED',
            'TASK_STATE_FAILED',
            'TASK_STATE_CANCELLED',
            'TASK_STATE_INPUT_REQUIRED',
            'TASK_STATE_REJECTED',
            'TASK_STATE_AUTH_REQUIRED'
        )
    );

CREATE INDEX IF NOT EXISTS idx_a2a_tasks_user_id ON a2a_tasks(user_id);
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_business_id ON a2a_tasks(business_id);

CREATE TABLE IF NOT EXISTS a2a_task_push_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    task_id UUID NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    business_id VARCHAR(255),
    url VARCHAR(500) NOT NULL,
    token VARCHAR(255),
    authentication JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ,
    CONSTRAINT fk_a2a_task_push_configs_task FOREIGN KEY (task_id)
        REFERENCES a2a_tasks(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_a2a_task_push_configs_task_id
    ON a2a_task_push_configs(task_id, created_at, id);

CREATE INDEX IF NOT EXISTS idx_a2a_task_push_configs_user_id
    ON a2a_task_push_configs(user_id);

CREATE INDEX IF NOT EXISTS idx_a2a_task_push_configs_business_id
    ON a2a_task_push_configs(business_id);

DROP TRIGGER IF EXISTS update_a2a_task_push_configs_updated_at ON a2a_task_push_configs;
CREATE TRIGGER update_a2a_task_push_configs_updated_at
    BEFORE UPDATE ON a2a_task_push_configs
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

CREATE OR REPLACE VIEW pending_a2a_tasks_view AS
SELECT
    t.id,
    t.session_id,
    t.state,
    t.target_agent_id,
    t.source_agent_id,
    t.created_at,
    ta.name AS target_agent_name,
    sa.name AS source_agent_name
FROM a2a_tasks t
LEFT JOIN agents ta ON t.target_agent_id = ta.id
LEFT JOIN agents sa ON t.source_agent_id = sa.id
WHERE t.state IN ('TASK_STATE_SUBMITTED', 'TASK_STATE_WORKING', 'TASK_STATE_INPUT_REQUIRED', 'TASK_STATE_AUTH_REQUIRED');
