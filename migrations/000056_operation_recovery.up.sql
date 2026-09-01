CREATE TABLE operation_recovery_commands (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    operation_type VARCHAR(64) NOT NULL,
    operation_id UUID NOT NULL,
    actor_subject VARCHAR(255) NOT NULL,
    principal_kind VARCHAR(16) NOT NULL CHECK (principal_kind IN ('business', 'operator')),
    action VARCHAR(80) NOT NULL,
    reason VARCHAR(500) NOT NULL,
    idempotency_key VARCHAR(180) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    operation_version VARCHAR(64) NOT NULL,
    correlation_id UUID NOT NULL,
    status VARCHAR(16) NOT NULL CHECK (status IN ('completed', 'rejected')),
    result_code VARCHAR(80) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    completed_at TIMESTAMPTZ NOT NULL,
    UNIQUE (business_id, actor_subject, action, idempotency_key)
);

CREATE INDEX idx_operation_recovery_business_operation
    ON operation_recovery_commands (business_id, operation_type, operation_id, created_at DESC);

CREATE UNIQUE INDEX ux_operation_recovery_effect
    ON operation_recovery_commands (business_id, operation_type, operation_id, action, operation_version)
    WHERE result_code = 'accepted';
