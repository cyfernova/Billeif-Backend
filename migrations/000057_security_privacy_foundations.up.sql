CREATE TABLE security_step_up_grants (
    id UUID PRIMARY KEY,
    subject VARCHAR(255) NOT NULL,
    business_id UUID NOT NULL REFERENCES business_profiles(id),
    action VARCHAR(80) NOT NULL,
    resource VARCHAR(255) NOT NULL,
    command_hash CHAR(64) NOT NULL,
    assurance VARCHAR(32) NOT NULL CHECK (assurance IN ('totp')),
    token_hash CHAR(64) NOT NULL UNIQUE,
    authenticated_at TIMESTAMPTZ NOT NULL,
    issued_at TIMESTAMPTZ NOT NULL,
    expires_at TIMESTAMPTZ NOT NULL,
    consumed_at TIMESTAMPTZ,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    CHECK (expires_at > issued_at),
    CHECK (authenticated_at <= issued_at)
);

CREATE INDEX idx_security_step_up_scope
    ON security_step_up_grants (subject, business_id, action, resource, command_hash, expires_at DESC);

CREATE TABLE security_audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID REFERENCES business_profiles(id),
    subject VARCHAR(255) NOT NULL,
    event_type VARCHAR(80) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id VARCHAR(255) NOT NULL,
    outcome VARCHAR(32) NOT NULL CHECK (outcome IN ('accepted', 'completed', 'rejected', 'failed', 'reconciliation_required')),
    reason_code VARCHAR(80) NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX idx_security_audit_business_time
    ON security_audit_events (business_id, occurred_at DESC);

CREATE TABLE security_pending_uploads (
    id UUID PRIMARY KEY,
    business_id UUID NOT NULL REFERENCES business_profiles(id),
    uploader_id VARCHAR(255) NOT NULL,
    kind VARCHAR(64) NOT NULL,
    bucket VARCHAR(128) NOT NULL,
    object_key VARCHAR(1024) NOT NULL UNIQUE,
    content_type VARCHAR(128) NOT NULL,
    size_bytes BIGINT NOT NULL CHECK (size_bytes > 0 AND size_bytes <= 26214400),
    checksum_sha256 VARCHAR(64) NOT NULL,
    status VARCHAR(24) NOT NULL CHECK (status IN ('pending', 'quarantined', 'clean', 'rejected', 'deleted')),
    scan_code VARCHAR(80) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    expires_at TIMESTAMPTZ NOT NULL,
    completed_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    CHECK (expires_at > created_at)
);

CREATE INDEX idx_security_pending_upload_scope
    ON security_pending_uploads (business_id, uploader_id, created_at DESC);
CREATE INDEX idx_security_pending_upload_cleanup
    ON security_pending_uploads (status, expires_at)
    WHERE deleted_at IS NULL;

CREATE TABLE privacy_requests (
    id UUID PRIMARY KEY,
    business_id UUID NOT NULL REFERENCES business_profiles(id),
    subject VARCHAR(255) NOT NULL,
    kind VARCHAR(16) NOT NULL CHECK (kind IN ('export', 'delete')),
    status VARCHAR(32) NOT NULL CHECK (status IN ('pending', 'retention_hold', 'processing', 'completed', 'reconciliation_required')),
    idempotency_key VARCHAR(180) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    artifact_key VARCHAR(1024) NOT NULL DEFAULT '',
    artifact_hash VARCHAR(64) NOT NULL DEFAULT '',
    error_code VARCHAR(80) NOT NULL DEFAULT '',
    requested_at TIMESTAMPTZ NOT NULL,
    purge_after TIMESTAMPTZ,
    started_at TIMESTAMPTZ,
    completed_at TIMESTAMPTZ,
    UNIQUE (business_id, subject, kind, idempotency_key),
    CHECK ((kind = 'delete' AND purge_after IS NOT NULL) OR kind = 'export')
);

CREATE INDEX idx_privacy_requests_scope
    ON privacy_requests (business_id, subject, requested_at DESC);
CREATE INDEX idx_privacy_requests_due_purge
    ON privacy_requests (purge_after)
    WHERE kind = 'delete' AND status = 'retention_hold';
