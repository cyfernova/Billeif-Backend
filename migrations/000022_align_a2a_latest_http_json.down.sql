DROP TRIGGER IF EXISTS update_a2a_task_push_configs_updated_at ON a2a_task_push_configs;
DROP INDEX IF EXISTS idx_a2a_task_push_configs_business_id;
DROP INDEX IF EXISTS idx_a2a_task_push_configs_user_id;
DROP INDEX IF EXISTS idx_a2a_task_push_configs_task_id;
DROP TABLE IF EXISTS a2a_task_push_configs;

DROP INDEX IF EXISTS idx_a2a_tasks_business_id;
DROP INDEX IF EXISTS idx_a2a_tasks_user_id;

ALTER TABLE a2a_tasks
    DROP CONSTRAINT IF EXISTS a2a_tasks_state_check;

UPDATE a2a_tasks
SET state = CASE state
    WHEN 'TASK_STATE_WORKING' THEN 'working'
    WHEN 'TASK_STATE_COMPLETED' THEN 'completed'
    WHEN 'TASK_STATE_FAILED' THEN 'failed'
    WHEN 'TASK_STATE_CANCELLED' THEN 'cancelled'
    WHEN 'TASK_STATE_INPUT_REQUIRED' THEN 'input-required'
    WHEN 'TASK_STATE_REJECTED' THEN 'rejected'
    WHEN 'TASK_STATE_SUBMITTED' THEN 'working'
    WHEN 'TASK_STATE_AUTH_REQUIRED' THEN 'input-required'
    ELSE state
END;

ALTER TABLE a2a_tasks
    ADD CONSTRAINT a2a_tasks_state_check CHECK (
        state IN ('working', 'completed', 'failed', 'cancelled', 'input-required', 'rejected')
    );

ALTER TABLE a2a_tasks
    ALTER COLUMN state TYPE VARCHAR(20);

ALTER TABLE a2a_tasks
    DROP COLUMN IF EXISTS business_id,
    DROP COLUMN IF EXISTS user_id;

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
WHERE t.state IN ('working', 'input-required');
