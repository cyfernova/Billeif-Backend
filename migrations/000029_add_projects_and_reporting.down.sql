DROP TABLE IF EXISTS report_share_access_logs;
DROP TABLE IF EXISTS report_shares;
DROP TABLE IF EXISTS report_preferences;
DROP TABLE IF EXISTS report_runs;

DROP INDEX IF EXISTS idx_stock_moves_project_id;
DROP INDEX IF EXISTS idx_ledger_entries_project_id;
DROP INDEX IF EXISTS idx_journals_project_id;
DROP INDEX IF EXISTS idx_payments_project_id;
DROP INDEX IF EXISTS idx_invoices_project_id;
DROP INDEX IF EXISTS idx_documents_project_id;

ALTER TABLE stock_moves
    DROP COLUMN IF EXISTS project_id;

ALTER TABLE ledger_entries
    DROP COLUMN IF EXISTS project_id;

ALTER TABLE journals
    DROP COLUMN IF EXISTS project_id;

ALTER TABLE payments
    DROP COLUMN IF EXISTS project_id;

ALTER TABLE invoices
    DROP COLUMN IF EXISTS project_id;

ALTER TABLE documents
    DROP COLUMN IF EXISTS project_id;

DROP TABLE IF EXISTS projects;
