-- A2A v0.3 Tables Migration
-- Adds support for Google's Agent2Agent Protocol v0.3 and workflow automation

-- =============================================================================
-- A2A Tasks Table
-- =============================================================================
-- Stores A2A task state and messages per v0.3 specification
CREATE TABLE IF NOT EXISTS a2a_tasks (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    session_id UUID,
    state VARCHAR(20) NOT NULL DEFAULT 'working',
    messages JSONB NOT NULL DEFAULT '[]',
    artifacts JSONB DEFAULT '[]',
    metadata JSONB DEFAULT '{}',
    history JSONB DEFAULT '[]',
    target_agent_id UUID,
    source_agent_id UUID,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    -- Constraints
    CONSTRAINT a2a_tasks_state_check CHECK (
        state IN ('working', 'completed', 'failed', 'cancelled', 'input-required', 'rejected')
    )
);

-- Indexes for a2a_tasks
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_session_id ON a2a_tasks(session_id);
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_state ON a2a_tasks(state);
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_target_agent_id ON a2a_tasks(target_agent_id);
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_source_agent_id ON a2a_tasks(source_agent_id);
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_created_at ON a2a_tasks(created_at DESC);

-- GIN index for JSONB queries on metadata
CREATE INDEX IF NOT EXISTS idx_a2a_tasks_metadata ON a2a_tasks USING GIN (metadata);

-- =============================================================================
-- A2A Push Notification Configs Table
-- =============================================================================
-- Stores webhook configurations for push notifications
CREATE TABLE IF NOT EXISTS a2a_push_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID NOT NULL,
    webhook_url VARCHAR(500) NOT NULL,
    secret VARCHAR(255),
    headers JSONB DEFAULT '{}',
    events TEXT[] DEFAULT '{}',
    authentication JSONB DEFAULT '{}',
    is_active BOOLEAN DEFAULT true,
    failure_count INT DEFAULT 0,
    last_failure_at TIMESTAMP WITH TIME ZONE,
    last_success_at TIMESTAMP WITH TIME ZONE,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    -- Foreign key constraint
    CONSTRAINT fk_a2a_push_configs_agent FOREIGN KEY (agent_id)
        REFERENCES agents(id) ON DELETE CASCADE
);

-- Indexes for a2a_push_configs
CREATE INDEX IF NOT EXISTS idx_a2a_push_configs_agent_id ON a2a_push_configs(agent_id);
CREATE INDEX IF NOT EXISTS idx_a2a_push_configs_is_active ON a2a_push_configs(is_active);

-- GIN index for events array queries
CREATE INDEX IF NOT EXISTS idx_a2a_push_configs_events ON a2a_push_configs USING GIN (events);

-- =============================================================================
-- Workflows Table
-- =============================================================================
-- Stores automated workflow definitions for buy/sell operations
CREATE TABLE IF NOT EXISTS workflows (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    name VARCHAR(255) NOT NULL,
    description TEXT,
    user_id VARCHAR(255) NOT NULL,
    agent_id UUID,
    trigger JSONB NOT NULL,
    action JSONB NOT NULL,
    status VARCHAR(20) DEFAULT 'active',
    is_enabled BOOLEAN DEFAULT true,
    last_run TIMESTAMP WITH TIME ZONE,
    next_run TIMESTAMP WITH TIME ZONE,
    run_count INT DEFAULT 0,
    success_count INT DEFAULT 0,
    failure_count INT DEFAULT 0,
    last_error TEXT,
    notification_settings JSONB DEFAULT '{}',
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    deleted_at TIMESTAMP WITH TIME ZONE,

    -- Constraints
    CONSTRAINT workflows_status_check CHECK (
        status IN ('active', 'paused', 'completed', 'failed', 'disabled')
    ),

    -- Foreign key constraint (optional agent)
    CONSTRAINT fk_workflows_agent FOREIGN KEY (agent_id)
        REFERENCES agents(id) ON DELETE SET NULL
);

-- Indexes for workflows
CREATE INDEX IF NOT EXISTS idx_workflows_user_id ON workflows(user_id);
CREATE INDEX IF NOT EXISTS idx_workflows_agent_id ON workflows(agent_id);
CREATE INDEX IF NOT EXISTS idx_workflows_status ON workflows(status);
CREATE INDEX IF NOT EXISTS idx_workflows_is_enabled ON workflows(is_enabled);
CREATE INDEX IF NOT EXISTS idx_workflows_next_run ON workflows(next_run) WHERE next_run IS NOT NULL AND is_enabled = true;
CREATE INDEX IF NOT EXISTS idx_workflows_deleted_at ON workflows(deleted_at);

-- GIN indexes for JSONB trigger and action queries
CREATE INDEX IF NOT EXISTS idx_workflows_trigger ON workflows USING GIN (trigger);
CREATE INDEX IF NOT EXISTS idx_workflows_action ON workflows USING GIN (action);

-- =============================================================================
-- Workflow Runs Table
-- =============================================================================
-- Stores execution history for workflows
CREATE TABLE IF NOT EXISTS workflow_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL,
    trigger_type VARCHAR(50) NOT NULL,
    trigger_data JSONB DEFAULT '{}',
    status VARCHAR(20) NOT NULL DEFAULT 'pending',
    result JSONB DEFAULT '{}',
    error_message TEXT,
    started_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    completed_at TIMESTAMP WITH TIME ZONE,
    duration_ms INT,

    -- Constraints
    CONSTRAINT workflow_runs_status_check CHECK (
        status IN ('pending', 'running', 'completed', 'failed', 'cancelled')
    ),

    -- Foreign key constraint
    CONSTRAINT fk_workflow_runs_workflow FOREIGN KEY (workflow_id)
        REFERENCES workflows(id) ON DELETE CASCADE
);

-- Indexes for workflow_runs
CREATE INDEX IF NOT EXISTS idx_workflow_runs_workflow_id ON workflow_runs(workflow_id);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_status ON workflow_runs(status);
CREATE INDEX IF NOT EXISTS idx_workflow_runs_started_at ON workflow_runs(started_at DESC);

-- =============================================================================
-- Price Alerts Table
-- =============================================================================
-- Stores price monitoring configurations for workflow triggers
CREATE TABLE IF NOT EXISTS price_alerts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    workflow_id UUID NOT NULL,
    product_id UUID NOT NULL,
    condition VARCHAR(20) NOT NULL,
    target_price DECIMAL(15, 2) NOT NULL,
    current_price DECIMAL(15, 2),
    is_triggered BOOLEAN DEFAULT false,
    triggered_at TIMESTAMP WITH TIME ZONE,
    is_active BOOLEAN DEFAULT true,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT NOW(),

    -- Constraints
    CONSTRAINT price_alerts_condition_check CHECK (
        condition IN ('above', 'below', 'equals', 'change_pct')
    ),

    -- Foreign key constraint
    CONSTRAINT fk_price_alerts_workflow FOREIGN KEY (workflow_id)
        REFERENCES workflows(id) ON DELETE CASCADE
);

-- Indexes for price_alerts
CREATE INDEX IF NOT EXISTS idx_price_alerts_workflow_id ON price_alerts(workflow_id);
CREATE INDEX IF NOT EXISTS idx_price_alerts_product_id ON price_alerts(product_id);
CREATE INDEX IF NOT EXISTS idx_price_alerts_is_active ON price_alerts(is_active) WHERE is_active = true;
CREATE INDEX IF NOT EXISTS idx_price_alerts_is_triggered ON price_alerts(is_triggered) WHERE is_triggered = false;

-- =============================================================================
-- Triggers for updated_at
-- =============================================================================

-- Function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_updated_at_column()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ language 'plpgsql';

-- Trigger for a2a_tasks
DROP TRIGGER IF EXISTS update_a2a_tasks_updated_at ON a2a_tasks;
CREATE TRIGGER update_a2a_tasks_updated_at
    BEFORE UPDATE ON a2a_tasks
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Trigger for a2a_push_configs
DROP TRIGGER IF EXISTS update_a2a_push_configs_updated_at ON a2a_push_configs;
CREATE TRIGGER update_a2a_push_configs_updated_at
    BEFORE UPDATE ON a2a_push_configs
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Trigger for workflows
DROP TRIGGER IF EXISTS update_workflows_updated_at ON workflows;
CREATE TRIGGER update_workflows_updated_at
    BEFORE UPDATE ON workflows
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- Trigger for price_alerts
DROP TRIGGER IF EXISTS update_price_alerts_updated_at ON price_alerts;
CREATE TRIGGER update_price_alerts_updated_at
    BEFORE UPDATE ON price_alerts
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();

-- =============================================================================
-- Views
-- =============================================================================

-- View for active workflows with their next scheduled run
CREATE OR REPLACE VIEW active_workflows_view AS
SELECT
    w.id,
    w.name,
    w.user_id,
    w.agent_id,
    w.trigger,
    w.action,
    w.status,
    w.next_run,
    w.run_count,
    w.success_count,
    w.failure_count,
    a.name as agent_name
FROM workflows w
LEFT JOIN agents a ON w.agent_id = a.id
WHERE w.is_enabled = true
  AND w.deleted_at IS NULL
  AND w.status IN ('active', 'paused');

-- View for pending A2A tasks
CREATE OR REPLACE VIEW pending_a2a_tasks_view AS
SELECT
    t.id,
    t.session_id,
    t.state,
    t.target_agent_id,
    t.source_agent_id,
    t.created_at,
    ta.name as target_agent_name,
    sa.name as source_agent_name
FROM a2a_tasks t
LEFT JOIN agents ta ON t.target_agent_id = ta.id
LEFT JOIN agents sa ON t.source_agent_id = sa.id
WHERE t.state IN ('working', 'input-required');
