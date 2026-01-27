-- Rollback A2A v0.3 Tables Migration

-- Drop views
DROP VIEW IF EXISTS pending_a2a_tasks_view;
DROP VIEW IF EXISTS active_workflows_view;

-- Drop triggers
DROP TRIGGER IF EXISTS update_price_alerts_updated_at ON price_alerts;
DROP TRIGGER IF EXISTS update_workflows_updated_at ON workflows;
DROP TRIGGER IF EXISTS update_a2a_push_configs_updated_at ON a2a_push_configs;
DROP TRIGGER IF EXISTS update_a2a_tasks_updated_at ON a2a_tasks;

-- Note: We don't drop the update_updated_at_column function as it may be used by other tables

-- Drop tables in reverse order (due to foreign key constraints)
DROP TABLE IF EXISTS price_alerts;
DROP TABLE IF EXISTS workflow_runs;
DROP TABLE IF EXISTS workflows;
DROP TABLE IF EXISTS a2a_push_configs;
DROP TABLE IF EXISTS a2a_tasks;
