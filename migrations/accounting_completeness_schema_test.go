package migrationbundle

import "testing"

func TestAccountingCompletenessMigrationDefinesLockOpeningAndBankingContracts(t *testing.T) {
	body := migrationSQL(t, "000058_accounting_completeness.up.sql")
	requireSQLFragments(t, body,
		"CREATE TABLE accounting_period_policies",
		"CREATE TABLE accounting_accounts",
		"account_class IN ('asset','liability','equity','revenue','expense','unclassified')",
		"ELSE 'unclassified' END",
		"UPPER(jl.account_code)",
		"FOREIGN KEY (business_id, parent_code)",
		"reversal_policy IN ('next_open_period', 'blocked')",
		"CREATE TRIGGER trg_business_accounting_period_policy",
		"CREATE TABLE accounting_lock_overrides",
		"UNIQUE (business_id, command_identity)",
		"ADD COLUMN lock_override_id UUID",
		"CREATE TABLE accounting_opening_balance_commands",
		"UNIQUE (business_id, idempotency_key)",
		"CREATE TABLE accounting_inventory_opening_balances",
		"CREATE TABLE bank_accounts",
		"CREATE TABLE bank_statements",
		"UNIQUE (business_id, upload_id)",
		"CREATE TABLE bank_transactions",
		"CREATE UNIQUE INDEX ux_active_bank_match",
		"CREATE UNIQUE INDEX ux_active_bank_ledger_match",
		"ADD COLUMN branch_id UUID REFERENCES branches(id)",
		"fk_invoices_branch_business",
	)
	down := migrationSQL(t, "000058_accounting_completeness.down.sql")
	requireSQLFragments(t, down,
		"DROP TABLE IF EXISTS bank_matches",
		"DROP TABLE IF EXISTS accounting_inventory_opening_balances",
		"DROP TRIGGER IF EXISTS trg_business_accounting_period_policy",
		"DROP TABLE IF EXISTS accounting_period_policies",
	)
}
