package postgres

import (
	"context"
	"testing"
	"time"

	"invoice-backend/internal/reporting"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestTrialBalanceAndBalanceSheetUsePostedMinorUnitHierarchy(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:accounting_reporting?mode=memory&cache=shared"), &gorm.Config{})
	require.NoError(t, err)
	for _, statement := range []string{
		`CREATE TABLE journals (id TEXT PRIMARY KEY,business_id TEXT NOT NULL,branch_id TEXT,status TEXT NOT NULL,posting_date DATETIME NOT NULL,deleted_at DATETIME)`,
		`CREATE TABLE journal_lines (id TEXT PRIMARY KEY,journal_id TEXT NOT NULL,account_code TEXT NOT NULL,account_name TEXT NOT NULL,entry_type TEXT NOT NULL,amount NUMERIC NOT NULL,currency TEXT NOT NULL)`,
		`CREATE TABLE accounting_accounts (id TEXT PRIMARY KEY,business_id TEXT NOT NULL,code TEXT NOT NULL,name TEXT NOT NULL,account_class TEXT NOT NULL,parent_code TEXT)`,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	businessID := "11111111-1111-4111-8111-111111111111"
	jan := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	feb := time.Date(2026, 2, 15, 0, 0, 0, 0, time.UTC)
	require.NoError(t, db.Exec(`INSERT INTO journals(id,business_id,status,posting_date) VALUES ('j1',?,'posted',?),('j2',?,'posted',?)`, businessID, jan, businessID, feb).Error)
	require.NoError(t, db.Exec(`INSERT INTO journal_lines(id,journal_id,account_code,account_name,entry_type,amount,currency) VALUES ('l1','j1','CASH','Cash','debit',100.00,'INR'),('l2','j1','OPENING_EQUITY','Opening equity','credit',100.00,'INR'),('l3','j2','CASH','Cash','debit',50.00,'INR'),('l4','j2','REV','Revenue','credit',50.00,'INR')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO accounting_accounts(id,business_id,code,name,account_class) VALUES ('a1',?,'CASH','Cash','asset'),('a2',?,'OPENING_EQUITY','Opening equity','equity'),('a3',?,'REV','Revenue','revenue')`, businessID, businessID, businessID).Error)
	repo := &reportingRepository{db: db}
	from := time.Date(2026, 2, 1, 0, 0, 0, 0, time.UTC)
	to := time.Date(2026, 2, 28, 23, 59, 59, 0, time.UTC)
	compareTo := time.Date(2026, 1, 31, 23, 59, 59, 0, time.UTC)
	query := reporting.Query{BusinessID: businessID, Page: 1, Limit: 50, Filters: reporting.Filters{DateFrom: &from, DateTo: &to, CompareTo: &compareTo, Currency: "INR"}}
	trial, err := repo.QueryReport(context.Background(), reporting.LookupOrPanic("trial_balance"), query)
	require.NoError(t, err)
	var cash map[string]interface{}
	for _, row := range trial.Rows {
		if row["account_code"] == "CASH" {
			cash = row
		}
	}
	require.NotNil(t, cash)
	require.EqualValues(t, 10000, cash["opening_debit_minor"])
	require.EqualValues(t, 5000, cash["period_debit_minor"])
	require.EqualValues(t, 15000, cash["closing_debit_minor"])
	require.EqualValues(t, 10000, cash["comparison_debit_minor"])
	require.EqualValues(t, 0, cash["comparison_credit_minor"])
	sheet, err := repo.QueryReport(context.Background(), reporting.LookupOrPanic("balance_sheet"), query)
	require.NoError(t, err)
	require.EqualValues(t, 15000, sheet.Totals["assets_minor"])
	require.EqualValues(t, 15000, sheet.Totals["equity_minor"])
	require.NoError(t, db.Exec(`INSERT INTO journals(id,business_id,status,posting_date) VALUES ('j3',?,'posted',?)`, businessID, feb).Error)
	require.NoError(t, db.Exec(`INSERT INTO journal_lines(id,journal_id,account_code,account_name,entry_type,amount,currency) VALUES ('l5','j3','9000','Legacy','debit',1.00,'INR')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO accounting_accounts(id,business_id,code,name,account_class) VALUES ('a4',?,'9000','Legacy','unclassified')`, businessID).Error)
	_, err = repo.QueryReport(context.Background(), reporting.LookupOrPanic("trial_balance"), query)
	require.ErrorContains(t, err, "classification is required")
}
