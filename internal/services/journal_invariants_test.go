package services

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
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

func TestCanonicalInvoiceIssueEffectsCreateSourceLinkedPostedJournal(t *testing.T) {
	db := newJournalInvariantDB(t)
	journalService := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	accountingService := NewAccountingService(db, nil, journalService)
	journalService.WithAccounting(accountingService)
	inventoryService := (&InventoryService{db: db}).WithAccounting(accountingService)
	documentID := uuid.NewString()
	businessID := uuid.NewString()
	document := &models.Document{
		ID: documentID, BusinessID: businessID, DocumentType: models.DocumentTypeBillOfSupply,
		SerialNumber: "BOS/26-27/000001", Currency: "INR", IssueDate: time.Now().UTC(),
		Subtotal: 100, Total: 100,
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		return applyCanonicalInvoiceIssueEffects(
			context.Background(),
			tx,
			document,
			inventoryService,
			journalService,
		)
	})
	require.NoError(t, err)

	var journal models.Journal
	require.NoError(t, db.Preload("Lines").
		Where("business_id = ? AND source_type = ? AND source_id = ?", businessID, "document", documentID).
		First(&journal).Error)
	require.Equal(t, models.JournalStatusPosted, journal.Status)
	require.NotNil(t, journal.PostedAt)
	require.Len(t, journal.Lines, 2)
	var ledgerCount int64
	require.NoError(t, db.Model(&models.LedgerEntry{}).Where("transaction_id = ?", journal.ID).Count(&ledgerCount).Error)
	require.Equal(t, int64(2), ledgerCount)
}

func TestCanonicalInvoiceIssueEffectsConsumeScopedLockOverride(t *testing.T) {
	db := newJournalInvariantDB(t)
	journalService := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	accountingService := NewAccountingService(db, nil, journalService)
	journalService.WithAccounting(accountingService)
	inventoryService := (&InventoryService{db: db}).WithAccounting(accountingService)
	businessID := uuid.NewString()
	postingDate := accountingDate(time.Now().UTC())
	lockDate := postingDate
	require.NoError(t, db.Create(&models.AccountingPeriodPolicy{BusinessID: businessID, LockDate: &lockDate, ReversalPolicy: models.ReversalPolicyNextOpenPeriod, UpdatedBy: "owner"}).Error)
	override := &models.AccountingLockOverride{
		ID: uuid.NewString(), BusinessID: businessID, Subject: "owner", Action: "accounting_lock_override",
		Resource: "invoice:issued", CommandIdentity: uuid.NewString(), PostingDate: postingDate,
		Reason: "approved historical invoice issue",
	}
	require.NoError(t, db.Create(override).Error)
	document := &models.Document{
		ID: uuid.NewString(), BusinessID: businessID, DocumentType: models.DocumentTypeBillOfSupply,
		SerialNumber: "BOS/26-27/000002", Currency: "INR", IssueDate: postingDate,
		Subtotal: 100, Total: 100,
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		return applyCanonicalInvoiceIssueEffects(
			withPostingLockOverride(context.Background(), &override.ID),
			tx,
			document,
			inventoryService,
			journalService,
		)
	})
	require.NoError(t, err)
	require.NoError(t, db.First(override, "id = ?", override.ID).Error)
	require.NotNil(t, override.AppliedAt)
}

func TestJournalLessDocumentEffectsConsumeScopedLockOverride(t *testing.T) {
	db := newJournalInvariantDB(t)
	journalService := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	accountingService := NewAccountingService(db, nil, journalService)
	journalService.WithAccounting(accountingService)
	inventoryService := (&InventoryService{db: db}).WithAccounting(accountingService)
	businessID := uuid.NewString()
	postingDate := accountingDate(time.Now().UTC())
	require.NoError(t, db.Create(&models.AccountingPeriodPolicy{BusinessID: businessID, LockDate: &postingDate, ReversalPolicy: models.ReversalPolicyNextOpenPeriod, UpdatedBy: "owner"}).Error)
	override := &models.AccountingLockOverride{ID: uuid.NewString(), BusinessID: businessID, Subject: "owner", Action: "accounting_lock_override", Resource: "document:delivery", CommandIdentity: uuid.NewString(), PostingDate: postingDate, Reason: "approved historical delivery issue"}
	require.NoError(t, db.Create(override).Error)
	document := &models.Document{ID: uuid.NewString(), BusinessID: businessID, DocumentType: models.DocumentTypeDeliveryChallan, SerialNumber: "DC-1", Currency: "INR", IssueDate: postingDate, Status: models.DocumentStatusIssued}

	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return applyCanonicalInvoiceIssueEffects(withPostingLockOverride(context.Background(), &override.ID), tx, document, inventoryService, journalService)
	}))
	require.NoError(t, db.First(override, "id = ?", override.ID).Error)
	require.NotNil(t, override.AppliedAt)
}

func TestAccountingDiagnosticsRestrictJournalLedgerComparisonToCurrency(t *testing.T) {
	db := newJournalInvariantDB(t)
	for _, statement := range []string{
		`CREATE TABLE business_profiles (id TEXT PRIMARY KEY,currency TEXT NOT NULL)`,
		`CREATE TABLE accounting_inventory_opening_balances (business_id TEXT,currency TEXT,as_of_date DATETIME,quantity_micros INTEGER,unit_cost_minor INTEGER)`,
		`CREATE TABLE stock_moves (business_id TEXT,direction TEXT,quantity NUMERIC,unit_cost NUMERIC,recorded_at DATETIME,deleted_at DATETIME)`,
		`CREATE TABLE documents (business_id TEXT,currency TEXT,document_type TEXT,status TEXT,issue_date DATETIME,tax_total NUMERIC,cess_total NUMERIC,deleted_at DATETIME)`,
	} {
		require.NoError(t, db.Exec(statement).Error)
	}
	businessID := uuid.NewString()
	now := time.Now().UTC()
	require.NoError(t, db.Exec(`INSERT INTO business_profiles(id,currency) VALUES (?,?)`, businessID, "INR").Error)
	require.NoError(t, db.Exec(`INSERT INTO journals(id,business_id,name,status,posting_date) VALUES ('jinr',?,'INR','posted',?),('jeur',?,'EUR','posted',?)`, businessID, now, businessID, now).Error)
	require.NoError(t, db.Exec(`INSERT INTO journal_lines(id,journal_id,account_code,account_name,entry_type,amount,currency) VALUES ('linr','jinr','CASH','Cash','debit',10,'INR'),('leur','jeur','CASH','Cash','debit',20,'EUR')`).Error)
	require.NoError(t, db.Exec(`INSERT INTO ledger_entries(id,business_id,transaction_id,entry_date,entry_type,category,description,amount,currency) VALUES ('einr',?,'jinr',?,'debit','CASH','Cash',10,'INR')`, businessID, now).Error)

	result, err := NewAccountingService(db, nil, nil).Diagnostics(context.Background(), businessID, "INR", now)
	require.NoError(t, err)
	require.Zero(t, result.JournalLedgerDifferences)
}

func TestAccountingHierarchyRejectsCrossClassParent(t *testing.T) {
	db := newJournalInvariantDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE accounting_accounts (id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),business_id TEXT NOT NULL,code TEXT NOT NULL,name TEXT NOT NULL,account_class TEXT NOT NULL,parent_code TEXT,created_at DATETIME,updated_at DATETIME,UNIQUE(business_id,code))`).Error)
	service := NewAccountingService(db, nil, nil)
	businessID := uuid.NewString()
	_, err := service.UpsertAccount(context.Background(), businessID, "owner", "1000", AccountingAccountInput{Name: "Assets", AccountClass: "asset"})
	require.NoError(t, err)
	parent := "1000"
	_, err = service.UpsertAccount(context.Background(), businessID, "owner", "5000", AccountingAccountInput{Name: "Expense", AccountClass: "expense", ParentCode: &parent})
	require.ErrorContains(t, err, "same account class")
}

func TestBankStatementCSVRejectsAmountsOutsideExactMinorRange(t *testing.T) {
	content := []byte("external_id,transaction_at,amount_minor,currency\ntxn-1,2026-04-01,9007199254740992,INR\n")
	_, err := parseBankStatementCSV(content)
	require.ErrorContains(t, err, "invalid bank statement row 2")
}

func TestInventoryOpeningRejectsOverflowingValuation(t *testing.T) {
	require.True(t, validInventoryOpeningValue(1_000_000, maxExactAccountingMinor))
	require.False(t, validInventoryOpeningValue(2_000_000, maxExactAccountingMinor))
	require.False(t, validInventoryOpeningValue(-1, 100))
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

type acceptingStepUpRepository struct{}

func (acceptingStepUpRepository) CreateStepUpGrant(context.Context, *models.StepUpGrant) error {
	return nil
}
func (acceptingStepUpRepository) ConsumeStepUpGrant(context.Context, interfaces.StepUpConsumeRequest) (bool, error) {
	return true, nil
}

func TestAccountingLockBlocksEveryPostedJournalProjectionWithoutOverride(t *testing.T) {
	db := newJournalInvariantDB(t)
	businessID := uuid.NewString()
	lockDate := accountingDate(time.Now().UTC())
	require.NoError(t, db.Create(&models.AccountingPeriodPolicy{BusinessID: businessID, LockDate: &lockDate, ReversalPolicy: models.ReversalPolicyNextOpenPeriod, UpdatedBy: "owner"}).Error)
	service := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	input := balancedJournalInput(models.JournalStatusPosted)
	input.PostingDate = lockDate
	_, err := service.CreateByBusiness(context.Background(), businessID, input)
	require.ErrorIs(t, err, ErrAccountingPeriodLocked)
	var journalCount, ledgerCount int64
	require.NoError(t, db.Model(&models.Journal{}).Count(&journalCount).Error)
	require.NoError(t, db.Model(&models.LedgerEntry{}).Count(&ledgerCount).Error)
	require.Zero(t, journalCount)
	require.Zero(t, ledgerCount)
}

func TestAccountingLockOverrideIsScopedAuditedAndOneTime(t *testing.T) {
	db := newJournalInvariantDB(t)
	businessID := uuid.NewString()
	postingDate := accountingDate(time.Now().UTC())
	lockDate := postingDate
	require.NoError(t, db.Create(&models.AccountingPeriodPolicy{BusinessID: businessID, LockDate: &lockDate, ReversalPolicy: models.ReversalPolicyNextOpenPeriod, UpdatedBy: "owner"}).Error)
	security := NewSecurityService(acceptingStepUpRepository{}, SecurityServiceOptions{})
	service := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	accounting := NewAccountingService(db, security, service)
	service.WithAccounting(accounting)
	input := balancedJournalInput(models.JournalStatusPosted)
	input.PostingDate = postingDate
	auth := PostingAuthorization{Subject: "owner", Action: "accounting_lock_override", Resource: "journal:new", CommandIdentity: "opening-2026", Token: "scoped-token", Reason: "approved historical correction"}
	journal, err := service.CreateAuthorized(context.Background(), businessID, input, auth)
	require.NoError(t, err)
	require.NotNil(t, journal.LockOverrideID)
	var override models.AccountingLockOverride
	require.NoError(t, db.First(&override, "id=?", *journal.LockOverrideID).Error)
	require.NotNil(t, override.AppliedAt)
	_, err = service.CreateAuthorized(context.Background(), businessID, input, auth)
	require.ErrorIs(t, err, ErrAccountingPeriodLocked)
	var auditCount int64
	require.NoError(t, db.Model(&models.AccountingAuditEvent{}).Where("business_id=?", businessID).Count(&auditCount).Error)
	require.GreaterOrEqual(t, auditCount, int64(2))
}

func TestJournalReversalUsesBusinessDateAndActualEventTimestamp(t *testing.T) {
	db := newJournalInvariantDB(t)
	businessID := uuid.NewString()
	service := NewJournalService(db, postgres.NewJournalRepository(db), logger.New()).WithBusinessTimezoneProvider(businessTimezoneStub{profile: &models.BusinessProfile{Timezone: "Asia/Kolkata"}})
	now := time.Date(2026, 9, 5, 19, 10, 0, 0, time.UTC)
	service.now = func() time.Time { return now }
	input := balancedJournalInput(models.JournalStatusPosted)
	input.PostingDate = time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	journal, err := service.CreateByBusiness(context.Background(), businessID, input)
	require.NoError(t, err)
	reversal, err := service.ReverseByBusiness(context.Background(), businessID, journal.ID)
	require.NoError(t, err)
	require.Equal(t, input.PostingDate, reversal.PostingDate)
	require.NotNil(t, reversal.PostedAt)
	require.Equal(t, now, *reversal.PostedAt)
	stored, err := service.GetByBusiness(context.Background(), businessID, journal.ID)
	require.NoError(t, err)
	require.NotNil(t, stored.ReversedAt)
	require.True(t, now.Equal(*stored.ReversedAt))
}

func TestLockedPeriodReversalMovesToExplicitNextOpenPeriod(t *testing.T) {
	db := newJournalInvariantDB(t)
	businessID := uuid.NewString()
	service := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	input := balancedJournalInput(models.JournalStatusPosted)
	input.PostingDate = accountingDate(time.Now().AddDate(0, 0, -2))
	journal, err := service.CreateByBusiness(context.Background(), businessID, input)
	require.NoError(t, err)
	lockDate := accountingDate(time.Now().AddDate(0, 0, 1))
	require.NoError(t, db.Create(&models.AccountingPeriodPolicy{BusinessID: businessID, LockDate: &lockDate, ReversalPolicy: models.ReversalPolicyNextOpenPeriod, UpdatedBy: "owner"}).Error)
	reversal, err := service.ReverseByBusiness(context.Background(), businessID, journal.ID)
	require.NoError(t, err)
	require.Equal(t, lockDate.AddDate(0, 0, 1), accountingDate(reversal.PostingDate))
}

func TestLockedPeriodReversalBlockedAfterLockDateHasPassed(t *testing.T) {
	db := newJournalInvariantDB(t)
	businessID := uuid.NewString()
	service := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	input := balancedJournalInput(models.JournalStatusPosted)
	input.PostingDate = accountingDate(time.Now().AddDate(0, 0, -10))
	journal, err := service.CreateByBusiness(context.Background(), businessID, input)
	require.NoError(t, err)
	lockDate := accountingDate(time.Now().AddDate(0, 0, -5))
	require.NoError(t, db.Create(&models.AccountingPeriodPolicy{BusinessID: businessID, LockDate: &lockDate, ReversalPolicy: models.ReversalPolicyBlocked, UpdatedBy: "owner"}).Error)

	_, err = service.ReverseByBusiness(context.Background(), businessID, journal.ID)
	require.ErrorIs(t, err, ErrReversalBlocked)
}

type bankStatementObjectStoreFake struct{ content []byte }

func (f bankStatementObjectStoreFake) ReadPendingObject(context.Context, string, string) ([]byte, error) {
	return f.content, nil
}

func TestBankStatementImportIsTenantBoundAndIdempotent(t *testing.T) {
	db := newJournalInvariantDB(t)
	addBankInvariantTables(t, db)
	businessID, subject := uuid.NewString(), "owner"
	account := models.BankAccount{ID: uuid.NewString(), BusinessID: businessID, Name: "Operating", Currency: "INR", MaskedAccount: "****1234", LedgerAccount: "BANK", IsActive: true}
	require.NoError(t, db.Create(&account).Error)
	upload := &models.PendingUpload{ID: uuid.NewString(), BusinessID: businessID, UploaderID: subject, Kind: "bank_statement", Bucket: "private", ObjectKey: "statements/one.csv", ContentType: "text/csv", Status: models.PendingUploadStatusClean}
	require.NoError(t, db.Exec(`INSERT INTO security_pending_uploads(id,business_id,uploader_id,kind,status) VALUES (?,?,?,?,?)`, upload.ID, upload.BusinessID, upload.UploaderID, upload.Kind, upload.Status).Error)
	repo := &pendingUploadRepositoryFake{upload: upload}
	service := NewAccountingService(db, nil, nil).WithPendingStatementFiles(repo, bankStatementObjectStoreFake{content: []byte("external_id,transaction_at,amount_minor,currency,reference\ntxn-1,2026-04-01,10000,INR,deposit\n")})
	input := BankStatementInput{BankAccountID: account.ID, UploadID: upload.ID, IdempotencyKey: "statement-2026-04", PeriodFrom: time.Date(2026, 4, 1, 0, 0, 0, 0, time.UTC), PeriodTo: time.Date(2026, 4, 30, 0, 0, 0, 0, time.UTC)}

	first, err := service.ImportBankStatement(context.Background(), businessID, subject, input)
	require.NoError(t, err)
	second, err := service.ImportBankStatement(context.Background(), businessID, subject, input)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	var statements, transactions int64
	require.NoError(t, db.Model(&models.BankStatement{}).Count(&statements).Error)
	require.NoError(t, db.Model(&models.BankTransaction{}).Count(&transactions).Error)
	require.Equal(t, int64(1), statements)
	require.Equal(t, int64(1), transactions)

	changed := input
	changed.PeriodTo = changed.PeriodTo.AddDate(0, 0, -1)
	_, err = service.ImportBankStatement(context.Background(), businessID, subject, changed)
	require.ErrorContains(t, err, "idempotency key payload mismatch")
	_, err = service.ImportBankStatement(context.Background(), uuid.NewString(), subject, input)
	require.ErrorContains(t, err, "verified bank statement upload is required")
}

func TestConcurrentBankMatchLeavesOneActiveMatchAndSupportsUnmatch(t *testing.T) {
	db := newJournalInvariantDB(t)
	addBankInvariantTables(t, db)
	businessID := uuid.NewString()
	transaction := seedBankTransaction(t, db, businessID, 10000)
	journalService := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	journal, err := journalService.CreateByBusiness(context.Background(), businessID, CreateJournalInput{Name: "Deposit", Status: models.JournalStatusPosted, PostingDate: transaction.TransactionAt, Lines: []CreateJournalLineInput{{AccountCode: "BANK", AccountName: "Operating", EntryType: "debit", Amount: 100, Currency: "INR"}, {AccountCode: "REVENUE", AccountName: "Revenue", EntryType: "credit", Amount: 100, Currency: "INR"}}})
	require.NoError(t, err)
	var entry models.LedgerEntry
	require.NoError(t, db.Where("transaction_id=? AND category='BANK'", journal.ID).First(&entry).Error)
	service := NewAccountingService(db, nil, journalService)

	start := make(chan struct{})
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, matchErr := service.MatchBankTransaction(context.Background(), businessID, "owner", transaction.ID, entry.ID, "verified deposit")
			errs <- matchErr
		}()
	}
	close(start)
	wg.Wait()
	close(errs)
	successes := 0
	for matchErr := range errs {
		if matchErr == nil {
			successes++
		}
	}
	require.Equal(t, 1, successes)
	var active int64
	require.NoError(t, db.Model(&models.BankMatch{}).Where("bank_transaction_id=? AND unmatched_at IS NULL", transaction.ID).Count(&active).Error)
	require.Equal(t, int64(1), active)
	require.NoError(t, service.UnmatchBankTransaction(context.Background(), businessID, "owner", transaction.ID, "wrong entry"))
	require.NoError(t, db.Model(&models.BankMatch{}).Where("bank_transaction_id=? AND unmatched_at IS NULL", transaction.ID).Count(&active).Error)
	require.Zero(t, active)
}

func TestBankAdjustmentReplaysAfterReconciliationWithoutDuplicatePosting(t *testing.T) {
	db := newJournalInvariantDB(t)
	addBankInvariantTables(t, db)
	businessID := uuid.NewString()
	transaction := seedBankTransaction(t, db, businessID, -250)
	journalService := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	service := NewAccountingService(db, nil, journalService)
	input := BankAdjustmentInput{Kind: "fee", Reason: "monthly bank fee", IdempotencyKey: "fee-1"}

	first, err := service.CreateBankAdjustment(context.Background(), businessID, "owner", transaction.ID, input)
	require.NoError(t, err)
	statement, err := service.ReconcileStatement(context.Background(), businessID, "owner", transaction.StatementID, transaction.TransactionAt)
	require.NoError(t, err)
	require.Equal(t, models.BankStatementReconciled, statement.Status)
	second, err := service.CreateBankAdjustment(context.Background(), businessID, "owner", transaction.ID, input)
	require.NoError(t, err)
	require.Equal(t, first.ID, second.ID)
	var adjustmentJournals, activeMatches int64
	require.NoError(t, db.Model(&models.Journal{}).Where("business_id=? AND source_type='bank_adjustment'", businessID).Count(&adjustmentJournals).Error)
	require.NoError(t, db.Model(&models.BankMatch{}).Where("bank_transaction_id=? AND unmatched_at IS NULL", transaction.ID).Count(&activeMatches).Error)
	require.Equal(t, int64(1), adjustmentJournals)
	require.Equal(t, int64(1), activeMatches)
	require.Error(t, service.UnmatchBankTransaction(context.Background(), businessID, "owner", transaction.ID, "reopen"))
}

func seedBankTransaction(t *testing.T, db *gorm.DB, businessID string, amountMinor int64) models.BankTransaction {
	t.Helper()
	account := models.BankAccount{ID: uuid.NewString(), BusinessID: businessID, Name: "Operating", Currency: "INR", MaskedAccount: "****1234", LedgerAccount: "BANK", IsActive: true}
	require.NoError(t, db.Create(&account).Error)
	date := accountingDate(time.Now().UTC())
	statement := models.BankStatement{ID: uuid.NewString(), BusinessID: businessID, BankAccountID: account.ID, UploadID: uuid.NewString(), IdempotencyKey: uuid.NewString(), RequestHash: strings.Repeat("a", 64), Status: models.BankStatementPending, PeriodFrom: date, PeriodTo: date, ImportedBy: "owner"}
	require.NoError(t, db.Create(&statement).Error)
	transaction := models.BankTransaction{ID: uuid.NewString(), BusinessID: businessID, StatementID: statement.ID, ExternalID: uuid.NewString(), TransactionAt: date, AmountMinor: amountMinor, Currency: "INR", Reference: "bank reference"}
	require.NoError(t, db.Create(&transaction).Error)
	return transaction
}

func addBankInvariantTables(t *testing.T, db *gorm.DB) {
	t.Helper()
	for _, statement := range []string{
		`CREATE TABLE bank_accounts (id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),business_id TEXT NOT NULL,name TEXT NOT NULL,currency TEXT NOT NULL,masked_account TEXT NOT NULL,ledger_account TEXT NOT NULL,opening_minor INTEGER NOT NULL DEFAULT 0,is_active BOOLEAN NOT NULL DEFAULT TRUE,created_at DATETIME,updated_at DATETIME,deactivated_at DATETIME)`,
		`CREATE TABLE bank_statements (id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),business_id TEXT NOT NULL,bank_account_id TEXT NOT NULL,upload_id TEXT NOT NULL,idempotency_key TEXT NOT NULL,request_hash TEXT NOT NULL,status TEXT NOT NULL,period_from DATETIME NOT NULL,period_to DATETIME NOT NULL,imported_by TEXT NOT NULL,imported_at DATETIME,reconciled_at DATETIME,reconciled_by TEXT,UNIQUE(business_id,idempotency_key),UNIQUE(business_id,upload_id))`,
		`CREATE TABLE bank_transactions (id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),business_id TEXT NOT NULL,statement_id TEXT NOT NULL,external_id TEXT NOT NULL,transaction_at DATETIME NOT NULL,amount_minor INTEGER NOT NULL,currency TEXT NOT NULL,reference TEXT,description TEXT,created_at DATETIME,UNIQUE(statement_id,external_id))`,
		`CREATE TABLE bank_matches (id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),business_id TEXT NOT NULL,bank_transaction_id TEXT NOT NULL,ledger_entry_id TEXT NOT NULL,match_type TEXT NOT NULL,matched_by TEXT NOT NULL,reason TEXT NOT NULL,matched_at DATETIME,unmatched_at DATETIME,unmatched_by TEXT)`,
		`CREATE UNIQUE INDEX ux_active_bank_match ON bank_matches(bank_transaction_id) WHERE unmatched_at IS NULL`,
		`CREATE TABLE api_idempotency_keys (id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),business_id TEXT NOT NULL,command TEXT NOT NULL,idempotency_key TEXT NOT NULL,request_hash TEXT NOT NULL,status TEXT NOT NULL,result_type TEXT,result_id TEXT,created_at DATETIME,updated_at DATETIME,completed_at DATETIME,UNIQUE(business_id,command,idempotency_key))`,
		`CREATE TABLE security_pending_uploads (id TEXT PRIMARY KEY,business_id TEXT NOT NULL,uploader_id TEXT NOT NULL,kind TEXT NOT NULL,status TEXT NOT NULL)`,
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}
}

func TestAccountingPolicyChangeRequiresScopedStepUpAndReason(t *testing.T) {
	db := newJournalInvariantDB(t)
	businessID := uuid.NewString()
	repository := acceptingStepUpRepository{}
	service := NewAccountingService(db, NewSecurityService(repository, SecurityServiceOptions{}), nil)

	_, err := service.SetPolicy(context.Background(), businessID, "owner", AccountingPolicyInput{ReversalPolicy: models.ReversalPolicyBlocked}, PostingAuthorization{})
	require.ErrorIs(t, err, ErrAccountingPeriodLocked)

	policy, err := service.SetPolicy(context.Background(), businessID, "owner", AccountingPolicyInput{ReversalPolicy: models.ReversalPolicyBlocked}, PostingAuthorization{CommandIdentity: "policy-2026", Token: "step-up", Reason: "approved fiscal policy change"})
	require.NoError(t, err)
	require.Equal(t, models.ReversalPolicyBlocked, policy.ReversalPolicy)
	var audit models.AccountingAuditEvent
	require.NoError(t, db.Where("business_id=? AND event_type='period_policy_updated'", businessID).First(&audit).Error)
	require.Contains(t, audit.Reason, "approved fiscal policy change")
}

func TestPostedJournalRejectsForeignTenantBranch(t *testing.T) {
	db := newJournalInvariantDB(t)
	require.NoError(t, db.Exec(`CREATE TABLE branches (id TEXT PRIMARY KEY,business_id TEXT NOT NULL,deleted_at DATETIME)`).Error)
	businessID, foreignBusinessID, branchID := uuid.NewString(), uuid.NewString(), uuid.NewString()
	require.NoError(t, db.Exec(`INSERT INTO branches(id,business_id) VALUES (?,?)`, branchID, foreignBusinessID).Error)
	service := NewJournalService(db, postgres.NewJournalRepository(db), logger.New())
	input := balancedJournalInput(models.JournalStatusPosted)
	input.BranchID = &branchID
	_, err := service.CreateByBusiness(context.Background(), businessID, input)
	require.ErrorContains(t, err, "branch does not belong to business")
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
			branch_id TEXT,
			lock_override_id TEXT,
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
			branch_id TEXT,
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
		`CREATE TABLE accounting_period_policies (
			business_id TEXT PRIMARY KEY,
			lock_date DATE,
			reversal_policy TEXT NOT NULL DEFAULT 'next_open_period',
			updated_by TEXT NOT NULL,
			updated_at DATETIME
		)`,
		`CREATE TABLE accounting_lock_overrides (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			subject TEXT NOT NULL,
			action TEXT NOT NULL,
			resource TEXT NOT NULL,
			command_identity TEXT NOT NULL,
			posting_date DATE NOT NULL,
			reason TEXT NOT NULL,
			created_at DATETIME,
			applied_at DATETIME
		)`,
		`CREATE TABLE accounting_audit_events (
			id TEXT PRIMARY KEY DEFAULT (lower(hex(randomblob(16)))),
			business_id TEXT NOT NULL,
			subject TEXT NOT NULL,
			event_type TEXT NOT NULL,
			resource_type TEXT NOT NULL,
			resource_id TEXT NOT NULL,
			outcome TEXT NOT NULL,
			reason TEXT NOT NULL,
			occurred_at DATETIME
		)`,
	} {
		require.NoError(t, db.Exec(statement).Error, statement)
	}
	return db
}
