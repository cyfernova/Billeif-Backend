DROP TABLE IF EXISTS bank_matches;
DROP TABLE IF EXISTS bank_transactions;
DROP TABLE IF EXISTS bank_statements;
DROP TABLE IF EXISTS bank_accounts;
DROP TABLE IF EXISTS accounting_inventory_opening_balances;
DROP TABLE IF EXISTS accounting_opening_balance_commands;

DROP INDEX IF EXISTS idx_ledger_entries_branch_date;
ALTER TABLE ledger_entries DROP CONSTRAINT IF EXISTS fk_ledger_entries_branch_business;
ALTER TABLE ledger_entries DROP COLUMN IF EXISTS branch_id;

DROP INDEX IF EXISTS idx_journals_branch_date;
ALTER TABLE journals
	DROP CONSTRAINT IF EXISTS fk_journals_branch_business,
    DROP COLUMN IF EXISTS lock_override_id,
    DROP COLUMN IF EXISTS branch_id;
DROP INDEX IF EXISTS idx_invoices_branch_date;
ALTER TABLE invoices
	DROP CONSTRAINT IF EXISTS fk_invoices_branch_business,
	DROP COLUMN IF EXISTS branch_id;
DROP INDEX IF EXISTS uq_branches_id_business;

DROP TABLE IF EXISTS accounting_audit_events;
DROP TABLE IF EXISTS accounting_lock_overrides;
DROP TRIGGER IF EXISTS trg_business_accounting_period_policy ON business_profiles;
DROP FUNCTION IF EXISTS ensure_accounting_period_policy();
DROP TABLE IF EXISTS accounting_accounts;
DROP TABLE IF EXISTS accounting_period_policies;
