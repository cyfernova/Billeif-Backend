ALTER TABLE bulk_jobs
    ADD COLUMN upload_id UUID REFERENCES security_pending_uploads(id) ON DELETE RESTRICT,
    ADD COLUMN validation_version INT NOT NULL DEFAULT 0,
    ADD COLUMN commit_command_id UUID,
    ADD COLUMN cancel_requested BOOLEAN NOT NULL DEFAULT FALSE,
    ADD COLUMN attempt_count INT NOT NULL DEFAULT 0,
    ADD COLUMN next_retry_at TIMESTAMPTZ,
    ADD COLUMN lease_owner VARCHAR(160),
    ADD COLUMN lease_expires_at TIMESTAMPTZ,
    ADD COLUMN retain_until TIMESTAMPTZ,
    ADD COLUMN artifact_state VARCHAR(30) NOT NULL DEFAULT 'pending',
    ADD COLUMN notification_state VARCHAR(30) NOT NULL DEFAULT 'pending';

ALTER TABLE bulk_jobs
    ADD CONSTRAINT ck_bulk_jobs_validation_version CHECK (validation_version >= 0),
    ADD CONSTRAINT ck_bulk_jobs_attempt_count CHECK (attempt_count >= 0),
    ADD CONSTRAINT ck_bulk_jobs_import_state CHECK (
        job_type NOT IN ('import_customers', 'import_vendors', 'import_products')
        OR status IN (
            'validating', 'validated', 'commit_queued', 'committing',
            'completed', 'failed', 'canceled', 'expired'
        )
    ) NOT VALID,
    ADD CONSTRAINT ck_bulk_jobs_commit_command CHECK (
        validation_version = 0
        OR
        status NOT IN ('commit_queued', 'committing', 'completed')
        OR job_type NOT IN ('import_customers', 'import_vendors', 'import_products')
        OR commit_command_id IS NOT NULL
    ),
    ADD CONSTRAINT ck_bulk_jobs_artifact_state CHECK (artifact_state IN ('pending', 'ready', 'expired', 'failed')),
    ADD CONSTRAINT ck_bulk_jobs_notification_state CHECK (notification_state IN ('pending', 'sent', 'failed', 'skipped'));

CREATE UNIQUE INDEX uq_bulk_jobs_import_commit_command
    ON bulk_jobs (business_id, created_by, commit_command_id)
    WHERE commit_command_id IS NOT NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX uq_bulk_jobs_import_upload
    ON bulk_jobs (business_id, upload_id)
    WHERE upload_id IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX idx_bulk_jobs_import_worker
    ON bulk_jobs (status, next_retry_at, lease_expires_at)
    WHERE job_type IN ('import_customers', 'import_vendors', 'import_products') AND deleted_at IS NULL;
CREATE INDEX idx_bulk_jobs_retention
    ON bulk_jobs (retain_until)
    WHERE retain_until IS NOT NULL AND deleted_at IS NULL;

ALTER TABLE bulk_job_rows
    ADD COLUMN validation_key VARCHAR(320),
    ADD COLUMN input_hash CHAR(64),
    ADD COLUMN error_code VARCHAR(80),
    ADD COLUMN error_details JSONB NOT NULL DEFAULT '{}',
    ADD COLUMN idempotency_key VARCHAR(180),
    ADD COLUMN attempt_count INT NOT NULL DEFAULT 0,
    ADD COLUMN committed_at TIMESTAMPTZ;

ALTER TABLE bulk_job_rows
    ADD CONSTRAINT ck_bulk_job_rows_attempt_count CHECK (attempt_count >= 0),
    ADD CONSTRAINT ck_bulk_job_rows_input_hash CHECK (input_hash IS NULL OR input_hash ~ '^[0-9a-f]{64}$');

CREATE UNIQUE INDEX uq_bulk_job_rows_number
    ON bulk_job_rows (bulk_job_id, row_number)
    WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX uq_bulk_job_rows_idempotency
    ON bulk_job_rows (idempotency_key)
    WHERE idempotency_key IS NOT NULL AND deleted_at IS NULL;
CREATE INDEX idx_bulk_job_rows_commit_batch
    ON bulk_job_rows (bulk_job_id, status, row_number)
    WHERE deleted_at IS NULL;
CREATE INDEX idx_bulk_job_rows_validation_key
    ON bulk_job_rows (bulk_job_id, validation_key)
    WHERE validation_key IS NOT NULL AND deleted_at IS NULL;

ALTER TABLE bulk_job_artifacts
    ADD COLUMN status VARCHAR(30) NOT NULL DEFAULT 'pending',
    ADD COLUMN expires_at TIMESTAMPTZ,
    ADD CONSTRAINT ck_bulk_job_artifacts_status CHECK (status IN ('pending', 'ready', 'expired', 'failed'));

CREATE INDEX idx_bulk_job_artifacts_expiry
    ON bulk_job_artifacts (expires_at)
    WHERE expires_at IS NOT NULL AND deleted_at IS NULL;
CREATE UNIQUE INDEX uq_bulk_job_artifacts_job_type
    ON bulk_job_artifacts (bulk_job_id, artifact_type)
    WHERE deleted_at IS NULL;

-- Legacy imports were queued without verified upload metadata and had no worker.
-- They cannot be replayed safely into the two-phase workflow, so make them
-- terminal and require a fresh validation command.
UPDATE bulk_jobs
SET status = 'failed',
    last_error = 'legacy_import_requires_revalidation',
    completed_at = COALESCE(completed_at, NOW()),
    retain_until = COALESCE(retain_until, NOW() + INTERVAL '30 days'),
    artifact_state = CASE WHEN file_key IS NULL OR file_key = '' THEN 'expired' ELSE 'ready' END,
    notification_state = 'skipped',
    updated_at = NOW()
WHERE job_type IN (
        'import_customers', 'import_vendors', 'import_products',
        'import_invoices', 'import_documents'
    )
  AND status IN ('pending', 'queued', 'processing')
  AND deleted_at IS NULL;

ALTER TABLE bulk_jobs VALIDATE CONSTRAINT ck_bulk_jobs_import_state;
