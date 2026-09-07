DROP INDEX IF EXISTS uq_bulk_job_artifacts_job_type;
DROP INDEX IF EXISTS idx_bulk_job_artifacts_expiry;
ALTER TABLE bulk_job_artifacts
    DROP CONSTRAINT IF EXISTS ck_bulk_job_artifacts_status,
    DROP COLUMN IF EXISTS expires_at,
    DROP COLUMN IF EXISTS status;

DROP INDEX IF EXISTS idx_bulk_job_rows_validation_key;
DROP INDEX IF EXISTS idx_bulk_job_rows_commit_batch;
DROP INDEX IF EXISTS uq_bulk_job_rows_idempotency;
DROP INDEX IF EXISTS uq_bulk_job_rows_number;
ALTER TABLE bulk_job_rows
    DROP CONSTRAINT IF EXISTS ck_bulk_job_rows_input_hash,
    DROP CONSTRAINT IF EXISTS ck_bulk_job_rows_attempt_count,
    DROP COLUMN IF EXISTS committed_at,
    DROP COLUMN IF EXISTS attempt_count,
    DROP COLUMN IF EXISTS idempotency_key,
    DROP COLUMN IF EXISTS error_details,
    DROP COLUMN IF EXISTS error_code,
    DROP COLUMN IF EXISTS input_hash,
    DROP COLUMN IF EXISTS validation_key;

DROP INDEX IF EXISTS idx_bulk_jobs_retention;
DROP INDEX IF EXISTS idx_bulk_jobs_import_worker;
DROP INDEX IF EXISTS uq_bulk_jobs_import_upload;
DROP INDEX IF EXISTS uq_bulk_jobs_import_commit_command;
ALTER TABLE bulk_jobs
    DROP CONSTRAINT IF EXISTS ck_bulk_jobs_notification_state,
    DROP CONSTRAINT IF EXISTS ck_bulk_jobs_artifact_state,
    DROP CONSTRAINT IF EXISTS ck_bulk_jobs_commit_command,
    DROP CONSTRAINT IF EXISTS ck_bulk_jobs_import_state,
    DROP CONSTRAINT IF EXISTS ck_bulk_jobs_attempt_count,
    DROP CONSTRAINT IF EXISTS ck_bulk_jobs_validation_version,
    DROP COLUMN IF EXISTS notification_state,
    DROP COLUMN IF EXISTS artifact_state,
    DROP COLUMN IF EXISTS retain_until,
    DROP COLUMN IF EXISTS lease_expires_at,
    DROP COLUMN IF EXISTS lease_owner,
    DROP COLUMN IF EXISTS next_retry_at,
    DROP COLUMN IF EXISTS attempt_count,
    DROP COLUMN IF EXISTS cancel_requested,
    DROP COLUMN IF EXISTS commit_command_id,
    DROP COLUMN IF EXISTS validation_version,
    DROP COLUMN IF EXISTS upload_id;
