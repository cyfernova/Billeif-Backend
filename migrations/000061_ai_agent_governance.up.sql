CREATE UNIQUE INDEX uq_agents_id_business ON agents (id, business_id);

CREATE TABLE ai_governance_controls (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scope_kind VARCHAR(16) NOT NULL CHECK (scope_kind IN ('global', 'business', 'agent')),
    business_id UUID REFERENCES business_profiles(id) ON DELETE RESTRICT,
    agent_id UUID,
    execution_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    reason_code VARCHAR(100) NOT NULL DEFAULT 'disabled_by_default',
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    updated_by_user_id UUID REFERENCES users(id) ON DELETE RESTRICT,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_ai_governance_control_agent_scope FOREIGN KEY (agent_id, business_id)
        REFERENCES agents(id, business_id) ON DELETE RESTRICT,
    CONSTRAINT ck_ai_governance_control_scope CHECK (
        (scope_kind = 'global' AND business_id IS NULL AND agent_id IS NULL) OR
        (scope_kind = 'business' AND business_id IS NOT NULL AND agent_id IS NULL) OR
        (scope_kind = 'agent' AND business_id IS NOT NULL AND agent_id IS NOT NULL)
    )
);
CREATE UNIQUE INDEX uq_ai_governance_global ON ai_governance_controls ((scope_kind)) WHERE scope_kind = 'global';
CREATE UNIQUE INDEX uq_ai_governance_business ON ai_governance_controls (business_id) WHERE scope_kind = 'business';
CREATE UNIQUE INDEX uq_ai_governance_agent ON ai_governance_controls (business_id, agent_id) WHERE scope_kind = 'agent';
INSERT INTO ai_governance_controls (scope_kind, execution_enabled, reason_code) VALUES ('global', FALSE, 'disabled_by_default');

CREATE TABLE ai_tool_approvals (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE RESTRICT,
    agent_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    tool_key VARCHAR(100) NOT NULL,
    risk_class VARCHAR(64) NOT NULL CHECK (risk_class IN ('read-only', 'internal draft', 'reversible write', 'external communication', 'financial commitment', 'tax or compliance', 'credential or security', 'irreversible or legally significant')),
    arguments_hash CHAR(64) NOT NULL CHECK (arguments_hash ~ '^[0-9a-f]{64}$'),
    sanitized_arguments JSONB NOT NULL CHECK (jsonb_typeof(sanitized_arguments) = 'object' AND pg_column_size(sanitized_arguments) <= 4096),
    resource_type VARCHAR(100) NOT NULL DEFAULT '',
    resource_id VARCHAR(255) NOT NULL DEFAULT '',
    monetary_amount_minor BIGINT CHECK (monetary_amount_minor >= 0),
    monetary_currency CHAR(3),
    tax_amount_minor BIGINT CHECK (tax_amount_minor >= 0),
    tax_currency CHAR(3),
    execution_idempotency_key VARCHAR(255) NOT NULL,
    request_hash CHAR(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_ai_tool_approval_agent_scope FOREIGN KEY (agent_id, business_id) REFERENCES agents(id, business_id) ON DELETE RESTRICT,
    CONSTRAINT ck_ai_tool_approval_monetary_pair CHECK ((monetary_amount_minor IS NULL) = (monetary_currency IS NULL)),
    CONSTRAINT ck_ai_tool_approval_tax_pair CHECK ((tax_amount_minor IS NULL) = (tax_currency IS NULL)),
    CONSTRAINT ck_ai_tool_approval_times CHECK (expires_at > issued_at AND (consumed_at IS NULL OR (consumed_at >= issued_at AND consumed_at < expires_at))),
    UNIQUE (business_id, agent_id, user_id, tool_key, execution_idempotency_key)
);
CREATE INDEX idx_ai_tool_approvals_unconsumed ON ai_tool_approvals (business_id, agent_id, user_id, tool_key, arguments_hash, expires_at) WHERE consumed_at IS NULL;

CREATE TABLE ai_agent_runs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE RESTRICT,
    agent_id UUID NOT NULL,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE RESTRICT,
    idempotency_key VARCHAR(255) NOT NULL,
    request_hash CHAR(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    provider_key VARCHAR(100) NOT NULL,
    model_key VARCHAR(100) NOT NULL,
    model_config JSONB NOT NULL CHECK (jsonb_typeof(model_config) = 'object' AND pg_column_size(model_config) <= 4096),
    prompt_template_version VARCHAR(100) NOT NULL,
    token_budget BIGINT NOT NULL CHECK (token_budget > 0),
    token_reserved BIGINT NOT NULL DEFAULT 0 CHECK (token_reserved >= 0),
    token_used BIGINT NOT NULL DEFAULT 0 CHECK (token_used >= 0),
    max_steps INTEGER NOT NULL CHECK (max_steps > 0),
    steps_used INTEGER NOT NULL DEFAULT 0 CHECK (steps_used >= 0),
    max_tool_calls INTEGER NOT NULL CHECK (max_tool_calls >= 0),
    tool_calls_used INTEGER NOT NULL DEFAULT 0 CHECK (tool_calls_used >= 0),
    max_retries INTEGER NOT NULL CHECK (max_retries >= 0),
    retries_used INTEGER NOT NULL DEFAULT 0 CHECK (retries_used >= 0),
    deadline_at TIMESTAMPTZ NOT NULL,
    cancel_requested_at TIMESTAMPTZ,
    status VARCHAR(32) NOT NULL CHECK (status IN ('initializing', 'running', 'completed', 'failed', 'cancelled', 'timed_out', 'blocked', 'reconciliation_required')),
    input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cost_micros BIGINT NOT NULL DEFAULT 0 CHECK (cost_micros >= 0),
    cost_currency CHAR(3) NOT NULL,
    failure_code VARCHAR(100) NOT NULL DEFAULT '',
    final_disposition VARCHAR(100) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT fk_ai_agent_run_agent_scope FOREIGN KEY (agent_id, business_id) REFERENCES agents(id, business_id) ON DELETE RESTRICT,
    CONSTRAINT uq_ai_agent_run_scope UNIQUE (id, business_id, agent_id, user_id),
    CONSTRAINT uq_ai_agent_run_idempotency UNIQUE (business_id, agent_id, user_id, idempotency_key),
    CONSTRAINT ck_ai_agent_run_limits CHECK (token_reserved + token_used <= token_budget AND steps_used <= max_steps AND tool_calls_used <= max_tool_calls AND retries_used <= max_retries),
    CONSTRAINT ck_ai_agent_run_times CHECK (deadline_at > created_at AND ((status IN ('initializing', 'running')) = (completed_at IS NULL)))
);
CREATE INDEX idx_ai_agent_runs_business ON ai_agent_runs (business_id, created_at DESC, id DESC);
CREATE INDEX idx_ai_agent_runs_active ON ai_agent_runs (business_id, agent_id, deadline_at) WHERE status IN ('initializing', 'running');

CREATE TABLE ai_budget_periods (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    scope_kind VARCHAR(16) NOT NULL CHECK (scope_kind IN ('business', 'agent')),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE RESTRICT,
    agent_id UUID,
    period_start TIMESTAMPTZ NOT NULL,
    period_end TIMESTAMPTZ NOT NULL,
    spend_currency CHAR(3) NOT NULL,
    limit_micros BIGINT NOT NULL CHECK (limit_micros > 0),
    reserved_micros BIGINT NOT NULL DEFAULT 0 CHECK (reserved_micros >= 0),
    settled_micros BIGINT NOT NULL DEFAULT 0 CHECK (settled_micros >= 0),
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CONSTRAINT fk_ai_budget_agent_scope FOREIGN KEY (agent_id, business_id) REFERENCES agents(id, business_id) ON DELETE RESTRICT,
    CONSTRAINT ck_ai_budget_scope CHECK ((scope_kind = 'business' AND agent_id IS NULL) OR (scope_kind = 'agent' AND agent_id IS NOT NULL)),
    CONSTRAINT ck_ai_budget_period CHECK (period_end > period_start AND reserved_micros + settled_micros <= limit_micros)
);
CREATE UNIQUE INDEX uq_ai_budget_business_period ON ai_budget_periods (business_id, period_start, period_end, spend_currency) WHERE scope_kind = 'business';
CREATE UNIQUE INDEX uq_ai_budget_agent_period ON ai_budget_periods (business_id, agent_id, period_start, period_end, spend_currency) WHERE scope_kind = 'agent';
CREATE INDEX idx_ai_budget_period_end ON ai_budget_periods (scope_kind, period_end);

CREATE TABLE ai_tool_executions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL,
    business_id UUID NOT NULL,
    agent_id UUID NOT NULL,
    user_id UUID NOT NULL,
    sequence INTEGER NOT NULL CHECK (sequence > 0),
    tool_key VARCHAR(100) NOT NULL,
    risk_class VARCHAR(64) NOT NULL CHECK (risk_class IN ('read-only', 'internal draft', 'reversible write', 'external communication', 'financial commitment', 'tax or compliance', 'credential or security', 'irreversible or legally significant')),
    approval_required BOOLEAN NOT NULL,
    approval_id UUID REFERENCES ai_tool_approvals(id) ON DELETE RESTRICT,
    arguments_hash CHAR(64) NOT NULL CHECK (arguments_hash ~ '^[0-9a-f]{64}$'),
    sanitized_arguments JSONB NOT NULL CHECK (jsonb_typeof(sanitized_arguments) = 'object' AND pg_column_size(sanitized_arguments) <= 4096),
    resource_type VARCHAR(100) NOT NULL DEFAULT '',
    resource_id VARCHAR(255) NOT NULL DEFAULT '',
    idempotency_key VARCHAR(255) NOT NULL,
    request_hash CHAR(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    status VARCHAR(32) NOT NULL CHECK (status IN ('requested', 'authorized', 'executing', 'succeeded', 'failed', 'denied', 'cancelled', 'timed_out', 'reconciliation_required')),
    effect_disposition VARCHAR(16) NOT NULL DEFAULT 'none' CHECK (effect_disposition IN ('none', 'confirmed', 'unknown')),
    attempt_count INTEGER NOT NULL DEFAULT 0 CHECK (attempt_count >= 0),
    result_type VARCHAR(100) NOT NULL DEFAULT '',
    result_id VARCHAR(255) NOT NULL DEFAULT '',
    failure_code VARCHAR(100) NOT NULL DEFAULT '',
    final_disposition VARCHAR(100) NOT NULL DEFAULT '',
    input_tokens BIGINT NOT NULL DEFAULT 0 CHECK (input_tokens >= 0),
    output_tokens BIGINT NOT NULL DEFAULT 0 CHECK (output_tokens >= 0),
    cost_micros BIGINT NOT NULL DEFAULT 0 CHECK (cost_micros >= 0),
    requested_at TIMESTAMPTZ NOT NULL,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT fk_ai_tool_execution_run_scope FOREIGN KEY (run_id, business_id, agent_id, user_id) REFERENCES ai_agent_runs(id, business_id, agent_id, user_id) ON DELETE RESTRICT,
    CONSTRAINT ck_ai_tool_execution_approval CHECK ((NOT approval_required) OR approval_id IS NOT NULL),
    CONSTRAINT ck_ai_tool_execution_result CHECK ((result_type = '') = (result_id = '')),
    UNIQUE (run_id, sequence),
    UNIQUE (run_id, idempotency_key)
);
CREATE UNIQUE INDEX uq_ai_tool_execution_approval ON ai_tool_executions (approval_id) WHERE approval_id IS NOT NULL;
CREATE INDEX idx_ai_tool_executions_business ON ai_tool_executions (business_id, requested_at DESC, id DESC);

CREATE TABLE ai_spend_reservations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    run_id UUID NOT NULL REFERENCES ai_agent_runs(id) ON DELETE RESTRICT,
    tool_execution_id UUID REFERENCES ai_tool_executions(id) ON DELETE RESTRICT,
    business_budget_period_id UUID NOT NULL REFERENCES ai_budget_periods(id) ON DELETE RESTRICT,
    agent_budget_period_id UUID NOT NULL REFERENCES ai_budget_periods(id) ON DELETE RESTRICT,
    provider_key VARCHAR(100) NOT NULL,
    model_key VARCHAR(100) NOT NULL,
    spend_currency CHAR(3) NOT NULL,
    reserved_micros BIGINT NOT NULL CHECK (reserved_micros > 0),
    settled_micros BIGINT NOT NULL DEFAULT 0 CHECK (settled_micros >= 0 AND settled_micros <= reserved_micros),
    status VARCHAR(32) NOT NULL CHECK (status IN ('reserved', 'settled', 'released', 'reconciliation_required')),
    idempotency_key VARCHAR(255) NOT NULL,
    request_hash CHAR(64) NOT NULL CHECK (request_hash ~ '^[0-9a-f]{64}$'),
    reserved_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    settled_at TIMESTAMPTZ,
    released_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT ck_ai_spend_reservation_times CHECK (expires_at > reserved_at),
    CONSTRAINT ck_ai_spend_reservation_terminal CHECK (
        (status = 'reserved' AND settled_at IS NULL AND released_at IS NULL) OR
        (status = 'settled' AND settled_at IS NOT NULL AND released_at IS NULL) OR
        (status = 'released' AND settled_at IS NULL AND released_at IS NOT NULL) OR
        (status = 'reconciliation_required' AND released_at IS NULL)
    ),
    UNIQUE (run_id, idempotency_key)
);
CREATE UNIQUE INDEX uq_ai_spend_reservation_execution ON ai_spend_reservations (tool_execution_id) WHERE tool_execution_id IS NOT NULL;
CREATE INDEX idx_ai_spend_reservations_run ON ai_spend_reservations (run_id, status);
CREATE INDEX idx_ai_spend_reservations_expiry ON ai_spend_reservations (expires_at) WHERE status IN ('reserved', 'reconciliation_required');

CREATE TABLE ai_provider_circuit_breakers (
    provider_key VARCHAR(100) PRIMARY KEY,
    state VARCHAR(16) NOT NULL CHECK (state IN ('closed', 'open', 'half_open')),
    consecutive_failures INTEGER NOT NULL DEFAULT 0 CHECK (consecutive_failures >= 0),
    opened_at TIMESTAMPTZ,
    retry_at TIMESTAMPTZ,
    probe_token VARCHAR(255),
    probe_expires_at TIMESTAMPTZ,
    last_failure_code VARCHAR(100) NOT NULL DEFAULT '',
    version BIGINT NOT NULL DEFAULT 1 CHECK (version >= 1),
    updated_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT ck_ai_provider_breaker_state CHECK (
        (state = 'closed' AND opened_at IS NULL AND retry_at IS NULL AND probe_token IS NULL AND probe_expires_at IS NULL) OR
        (state = 'open' AND opened_at IS NOT NULL AND retry_at IS NOT NULL AND probe_token IS NULL AND probe_expires_at IS NULL) OR
        (state = 'half_open' AND opened_at IS NOT NULL AND retry_at IS NOT NULL AND probe_token IS NOT NULL AND probe_expires_at IS NOT NULL)
    )
);
CREATE INDEX idx_ai_provider_breakers_state ON ai_provider_circuit_breakers (state, retry_at);
INSERT INTO ai_provider_circuit_breakers (provider_key, state, updated_at)
VALUES ('sarvam', 'closed', NOW());
