package services

import (
	"context"
	"strings"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/postgres"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestJournalPostingRollsBackStatusWhenLedgerProjectionFails(t *testing.T) {
	db := newJournalInvariantDB(t)
	service := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	ctx := context.Background()
	businessID := uuid.NewString()

	journal, err := service.CreateByBusiness(ctx, businessID, balancedJournalInput(models.JournalStatusDraft))
	require.NoError(t, err)
	require.Len(t, journal.Lines, 2)
	lineIDs := []string{journal.Lines[0].ID, journal.Lines[1].ID}

	require.NoError(t, db.Exec(`
		CREATE TRIGGER reject_ledger_insert
		BEFORE INSERT ON ledger_entries
		BEGIN
			SELECT RAISE(ABORT, 'synthetic ledger failure');
		END;
	`).Error)

	_, err = service.PostByBusiness(ctx, businessID, journal.ID)
	require.ErrorContains(t, err, "synthetic ledger failure")

	stored, err := postgres.NewJournalRepository(db).GetByID(ctx, journal.ID, businessID)
	require.NoError(t, err)
	require.Equal(t, models.JournalStatusDraft, stored.Status)
	require.Nil(t, stored.PostedAt)
	require.Equal(t, lineIDs, []string{stored.Lines[0].ID, stored.Lines[1].ID})

	var ledgerCount int64
	require.NoError(t, db.Model(&models.LedgerEntry{}).Where("transaction_id = ?", journal.ID).Count(&ledgerCount).Error)
	require.Zero(t, ledgerCount)
}

func TestPostedJournalCreationRollsBackWhenLedgerProjectionFails(t *testing.T) {
	db := newJournalInvariantDB(t)
	service := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	require.NoError(t, db.Exec(`
		CREATE TRIGGER reject_ledger_insert
		BEFORE INSERT ON ledger_entries
		BEGIN
			SELECT RAISE(ABORT, 'synthetic ledger failure');
		END;
	`).Error)

	_, err := service.CreateByBusiness(context.Background(), uuid.NewString(), balancedJournalInput(models.JournalStatusPosted))
	require.ErrorContains(t, err, "synthetic ledger failure")

	var journalCount int64
	require.NoError(t, db.Model(&models.Journal{}).Count(&journalCount).Error)
	require.Zero(t, journalCount)
	var lineCount int64
	require.NoError(t, db.Model(&models.JournalLine{}).Count(&lineCount).Error)
	require.Zero(t, lineCount)
}

func TestJournalBalanceUsesPersistedMinorUnits(t *testing.T) {
	db := newJournalInvariantDB(t)
	service := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())

	input := balancedJournalInput(models.JournalStatusDraft)
	input.Lines[0].Amount = 1.005
	input.Lines[1].Amount = 1.00

	_, err := service.CreateByBusiness(context.Background(), uuid.NewString(), input)
	require.ErrorContains(t, err, "journal is not balanced")
}

func TestJournalPostingPreservesLinesAndTenantScope(t *testing.T) {
	db := newJournalInvariantDB(t)
	service := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	ctx := context.Background()
	businessID := uuid.NewString()
	journal, err := service.CreateByBusiness(ctx, businessID, balancedJournalInput(models.JournalStatusDraft))
	require.NoError(t, err)
	lineIDs := []string{journal.Lines[0].ID, journal.Lines[1].ID}

	_, err = service.PostByBusiness(ctx, uuid.NewString(), journal.ID)
	require.ErrorContains(t, err, "journal not found")

	posted, err := service.PostByBusiness(ctx, businessID, journal.ID)
	require.NoError(t, err)
	require.Equal(t, models.JournalStatusPosted, posted.Status)
	require.NotNil(t, posted.PostedAt)
	require.Equal(t, lineIDs, []string{posted.Lines[0].ID, posted.Lines[1].ID})

	_, err = service.UpdateByBusiness(ctx, businessID, journal.ID, balancedJournalInput(models.JournalStatusDraft))
	require.ErrorContains(t, err, "only draft journals can be updated")
	_, err = service.PostByBusiness(ctx, businessID, journal.ID)
	require.ErrorContains(t, err, "journal already posted")

	var ledgerCount int64
	require.NoError(t, db.Model(&models.LedgerEntry{}).Where("transaction_id = ?", journal.ID).Count(&ledgerCount).Error)
	require.Equal(t, int64(2), ledgerCount)
}

func TestJournalReversalRollsBackWhenLedgerProjectionFails(t *testing.T) {
	db := newJournalInvariantDB(t)
	service := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	ctx := context.Background()
	businessID := uuid.NewString()
	journal, err := service.CreateByBusiness(ctx, businessID, balancedJournalInput(models.JournalStatusPosted))
	require.NoError(t, err)

	require.NoError(t, db.Exec(`
		CREATE TRIGGER reject_reversal_ledger_insert
		BEFORE INSERT ON ledger_entries
		BEGIN
			SELECT RAISE(ABORT, 'synthetic reversal ledger failure');
		END;
	`).Error)

	_, err = service.ReverseByBusiness(ctx, businessID, journal.ID)
	require.ErrorContains(t, err, "synthetic reversal ledger failure")

	stored, err := postgres.NewJournalRepository(db).GetByID(ctx, journal.ID, businessID)
	require.NoError(t, err)
	require.Equal(t, models.JournalStatusPosted, stored.Status)
	require.Nil(t, stored.ReversedAt)

	var reversalCount int64
	require.NoError(t, db.Model(&models.Journal{}).Where("reversal_of_id = ?", journal.ID).Count(&reversalCount).Error)
	require.Zero(t, reversalCount)
	var ledgerCount int64
	require.NoError(t, db.Model(&models.LedgerEntry{}).Where("transaction_id = ?", journal.ID).Count(&ledgerCount).Error)
	require.Equal(t, int64(2), ledgerCount)
}

func TestJournalMustBalanceEachCurrencyIndependently(t *testing.T) {
	db := newJournalInvariantDB(t)
	service := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	input := balancedJournalInput(models.JournalStatusDraft)
	input.Lines[1].Currency = "USD"

	_, err := service.CreateByBusiness(context.Background(), uuid.NewString(), input)
	require.ErrorContains(t, err, "journal is not balanced")
}

func balancedJournalInput(status string) CreateJournalInput {
	return CreateJournalInput{
		Name:   "Invariant journal",
		Status: status,
		Lines: []CreateJournalLineInput{
			{AccountCode: "CASH", AccountName: "Cash", EntryType: "debit", Amount: 100, Currency: "INR"},
			{AccountCode: "REVENUE", AccountName: "Revenue", EntryType: "credit", Amount: 100, Currency: "INR"},
		},
	}
}

func newJournalInvariantDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := "file:" + strings.ReplaceAll(t.Name(), "/", "_") + "?mode=memory&cache=shared&_busy_timeout=5000"
	db, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	require.NoError(t, err)
	for _, statement := range []string{
		`CREATE TABLE journals (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			business_id TEXT NOT NULL,
			name TEXT NOT NULL,
			reference TEXT,
			project_id TEXT,
			status TEXT NOT NULL DEFAULT 'draft',
			posting_date DATETIME NOT NULL,
			notes TEXT,
			source_type TEXT,
			source_id TEXT,
			reversal_of_id TEXT,
			posted_at DATETIME,
			reversed_at DATETIME,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE journal_lines (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			journal_id TEXT NOT NULL,
			account_code TEXT NOT NULL,
			account_name TEXT NOT NULL,
			entry_type TEXT NOT NULL,
			amount NUMERIC NOT NULL,
			currency TEXT NOT NULL,
			description TEXT,
			document_id TEXT,
			document_line_id TEXT,
			metadata TEXT DEFAULT '{}',
			created_at DATETIME,
			updated_at DATETIME
		)`,
		`CREATE TABLE ledger_entries (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			business_id TEXT NOT NULL,
			invoice_id TEXT,
			payment_id TEXT,
			project_id TEXT,
			transaction_id TEXT NOT NULL,
			entry_date DATETIME NOT NULL,
			entry_type TEXT NOT NULL,
			category TEXT,
			description TEXT NOT NULL,
			amount NUMERIC NOT NULL,
			currency TEXT NOT NULL,
			balance NUMERIC,
			created_at DATETIME
		)`,
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}
	return db
}
