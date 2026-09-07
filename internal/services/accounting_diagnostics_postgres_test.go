package services

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
)

func TestAccountingDiagnosticsPostgresMatchesTextReferencesToUUIDJournals(t *testing.T) {
	db := newPaymentPostgresIntegrationDB(t)
	for _, statement := range []string{
		`CREATE TABLE business_profiles (id UUID PRIMARY KEY,currency TEXT NOT NULL)`,
		`CREATE TABLE accounting_inventory_opening_balances (business_id UUID,currency TEXT,as_of_date TIMESTAMPTZ,quantity_micros BIGINT,unit_cost_minor BIGINT)`,
		`CREATE TABLE stock_moves (business_id UUID,direction TEXT,quantity NUMERIC,unit_cost NUMERIC,recorded_at TIMESTAMPTZ,deleted_at TIMESTAMPTZ)`,
		`CREATE TABLE documents (business_id UUID,currency TEXT,document_type TEXT,status TEXT,issue_date TIMESTAMPTZ,tax_total NUMERIC,cess_total NUMERIC,deleted_at TIMESTAMPTZ)`,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	businessID, journalID := uuid.NewString(), uuid.NewString()
	now := time.Now().UTC()
	require.NoError(t, db.Exec(`INSERT INTO business_profiles(id,currency) VALUES (?,?)`, businessID, "INR").Error)
	require.NoError(t, db.Exec(`INSERT INTO journals(id,business_id,name,status,posting_date) VALUES (?,?,'QA','posted',?)`, journalID, businessID, now).Error)
	require.NoError(t, db.Exec(`INSERT INTO journal_lines(id,journal_id,account_code,account_name,entry_type,amount,currency) VALUES (?,?,'CASH','Cash','debit',10,'INR')`, uuid.NewString(), journalID).Error)
	require.NoError(t, db.Exec(`INSERT INTO ledger_entries(id,business_id,transaction_id,entry_date,entry_type,category,description,amount,currency) VALUES (?,?,?,?,'debit','CASH','Cash',10,'INR')`, uuid.NewString(), businessID, journalID, now).Error)
	service := NewAccountingService(db, nil, nil)
	result, err := service.Diagnostics(context.Background(), businessID, "INR", now)
	require.NoError(t, err)
	require.Zero(t, result.JournalLedgerDifferences)

	// Legacy ledger references are arbitrary text and must remain visible as differences.
	require.NoError(t, db.Exec(`INSERT INTO ledger_entries(id,business_id,transaction_id,entry_date,entry_type,category,description,amount,currency) VALUES (?,?,'legacy-reference',?,'debit','CASH','Legacy',5,'INR')`, uuid.NewString(), businessID, now).Error)
	result, err = service.Diagnostics(context.Background(), businessID, "INR", now)
	require.NoError(t, err)
	require.EqualValues(t, 1, result.JournalLedgerDifferences)
}
