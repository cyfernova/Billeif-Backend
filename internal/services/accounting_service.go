package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var (
	ErrAccountingPeriodLocked = errors.New("accounting period is locked")
	ErrReversalBlocked        = errors.New("reversal is blocked by accounting policy")
	accountingCurrencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
)

const bankAdjustmentCommand = "bank.adjustment.v1"

const maxExactAccountingMinor int64 = 9007199254740991

type AccountingService struct {
	db       *gorm.DB
	security *SecurityService
	journals *JournalService
	now      func() time.Time
	uploads  interfaces.PendingUploadRepository
	objects  interface {
		ReadPendingObject(context.Context, string, string) ([]byte, error)
	}
}

func (s *AccountingService) WithPendingStatementFiles(uploads interfaces.PendingUploadRepository, objects interface {
	ReadPendingObject(context.Context, string, string) ([]byte, error)
}) *AccountingService {
	s.uploads = uploads
	s.objects = objects
	return s
}

func NewAccountingService(db *gorm.DB, security *SecurityService, journals *JournalService) *AccountingService {
	return &AccountingService{db: db, security: security, journals: journals, now: time.Now}
}

type AccountingPolicyInput struct {
	LockDate       *time.Time `json:"lock_date"`
	ReversalPolicy string     `json:"reversal_policy" binding:"required,oneof=next_open_period blocked"`
}

type AccountingAccountInput struct {
	Name         string  `json:"name" binding:"required"`
	AccountClass string  `json:"account_class" binding:"required,oneof=asset liability equity revenue expense"`
	ParentCode   *string `json:"parent_code,omitempty"`
}

func (s *AccountingService) ListAccounts(ctx context.Context, businessID string) ([]models.AccountingAccount, error) {
	var rows []models.AccountingAccount
	err := s.db.WithContext(ctx).Where("business_id=?", businessID).Order("account_class, parent_code, code").Find(&rows).Error
	return rows, err
}

func (s *AccountingService) UpsertAccount(ctx context.Context, businessID, subject, code string, input AccountingAccountInput) (*models.AccountingAccount, error) {
	code = strings.ToUpper(strings.TrimSpace(code))
	input.Name = strings.TrimSpace(input.Name)
	input.AccountClass = strings.ToLower(strings.TrimSpace(input.AccountClass))
	if code == "" || len(code) > 60 || input.Name == "" || len(input.Name) > 120 || !validAccountingAccountClass(input.AccountClass) {
		return nil, fmt.Errorf("invalid accounting account")
	}
	if input.ParentCode != nil {
		parent := strings.ToUpper(strings.TrimSpace(*input.ParentCode))
		if parent == "" {
			input.ParentCode = nil
		} else {
			if parent == code {
				return nil, fmt.Errorf("account cannot parent itself")
			}
			input.ParentCode = &parent
		}
	}
	account := &models.AccountingAccount{BusinessID: businessID, Code: code, Name: input.Name, AccountClass: input.AccountClass, ParentCode: input.ParentCode}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockAccountingTenantTx(tx, businessID); err != nil {
			return err
		}
		var tenantAccounts []models.AccountingAccount
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("business_id=?", businessID).Order("code").Find(&tenantAccounts).Error; err != nil {
			return err
		}
		if err := validateAccountingParentTx(tx, businessID, code, input.AccountClass, input.ParentCode); err != nil {
			return err
		}
		var incompatibleChildren int64
		if err := tx.Model(&models.AccountingAccount{}).Where("business_id=? AND parent_code=? AND account_class<>?", businessID, code, input.AccountClass).Count(&incompatibleChildren).Error; err != nil {
			return err
		}
		if incompatibleChildren != 0 {
			return fmt.Errorf("child accounts must use the same account class")
		}
		if err := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "business_id"}, {Name: "code"}}, DoUpdates: clause.AssignmentColumns([]string{"name", "account_class", "parent_code", "updated_at"})}).Create(account).Error; err != nil {
			return err
		}
		if err := tx.Where("business_id=? AND code=?", businessID, code).First(account).Error; err != nil {
			return err
		}
		return writeAccountingAuditTx(tx, businessID, subject, "accounting_account_upserted", "accounting_account", code, "completed", input.AccountClass)
	})
	return account, err
}

func validateAccountingParentTx(tx *gorm.DB, businessID, code, accountClass string, parentCode *string) error {
	current := parentCode
	for depth := 0; current != nil && depth < 100; depth++ {
		if *current == code {
			return fmt.Errorf("account hierarchy cycle")
		}
		var parent models.AccountingAccount
		if err := tx.Where("business_id=? AND code=?", businessID, *current).First(&parent).Error; err != nil {
			return fmt.Errorf("parent account not found")
		}
		if parent.AccountClass != accountClass {
			return fmt.Errorf("parent account must use the same account class")
		}
		current = parent.ParentCode
	}
	if current != nil {
		return fmt.Errorf("account hierarchy is too deep")
	}
	return nil
}

func validAccountingAccountClass(value string) bool {
	return value == "asset" || value == "liability" || value == "equity" || value == "revenue" || value == "expense"
}

func lockAccountingTenantTx(tx *gorm.DB, businessID string) error {
	if tx.Dialector.Name() != "postgres" {
		return nil
	}
	return tx.Exec("SELECT pg_advisory_xact_lock(hashtextextended(?, 0))", businessID).Error
}

func (s *AccountingService) SetPolicy(ctx context.Context, businessID, subject string, input AccountingPolicyInput, auth PostingAuthorization) (*models.AccountingPeriodPolicy, error) {
	if s == nil || s.db == nil || uuid.Validate(businessID) != nil || strings.TrimSpace(subject) == "" {
		return nil, fmt.Errorf("invalid accounting policy")
	}
	if input.ReversalPolicy != models.ReversalPolicyNextOpenPeriod && input.ReversalPolicy != models.ReversalPolicyBlocked {
		return nil, fmt.Errorf("invalid reversal policy")
	}
	if input.LockDate != nil {
		date := accountingDate(*input.LockDate)
		input.LockDate = &date
	}
	current, err := s.GetPolicy(ctx, businessID)
	if err != nil {
		return nil, err
	}
	if sameAccountingPolicy(current, input) {
		return current, nil
	}
	auth.Subject = strings.TrimSpace(subject)
	auth.Action = "accounting_policy_change"
	auth.Resource = "accounting-policy:" + businessID
	auth.CommandIdentity = strings.TrimSpace(auth.CommandIdentity)
	auth.Token = strings.TrimSpace(auth.Token)
	auth.Reason = strings.TrimSpace(auth.Reason)
	if s.security == nil || auth.Subject == "" || auth.CommandIdentity == "" || len(auth.Reason) < 8 || len(auth.Reason) > 500 {
		return nil, ErrAccountingPeriodLocked
	}
	if err := s.security.ConsumeStepUp(ctx, StepUpConsumeRequest{
		Subject: auth.Subject, BusinessID: businessID, Action: auth.Action, Resource: auth.Resource,
		CommandIdentity: auth.CommandIdentity, Token: auth.Token,
	}); err != nil {
		return nil, ErrAccountingPeriodLocked
	}
	policy := &models.AccountingPeriodPolicy{BusinessID: businessID, LockDate: input.LockDate, ReversalPolicy: input.ReversalPolicy, UpdatedBy: subject}
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "business_id"}},
			DoUpdates: clause.AssignmentColumns([]string{"lock_date", "reversal_policy", "updated_by", "updated_at"}),
		}).Create(policy).Error; err != nil {
			return err
		}
		oldValue, _ := json.Marshal(current)
		newValue, _ := json.Marshal(policy)
		return writeAccountingAuditTx(tx, businessID, subject, "period_policy_updated", "accounting_policy", businessID, "completed", auth.Reason+" old="+string(oldValue)+" new="+string(newValue))
	})
	return policy, err
}

func sameAccountingPolicy(current *models.AccountingPeriodPolicy, input AccountingPolicyInput) bool {
	if current == nil || current.ReversalPolicy != input.ReversalPolicy || (current.LockDate == nil) != (input.LockDate == nil) {
		return false
	}
	return current.LockDate == nil || accountingDate(*current.LockDate).Equal(accountingDate(*input.LockDate))
}

func (s *AccountingService) GetPolicy(ctx context.Context, businessID string) (*models.AccountingPeriodPolicy, error) {
	var policy models.AccountingPeriodPolicy
	err := s.db.WithContext(ctx).Where("business_id = ?", businessID).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return &models.AccountingPeriodPolicy{BusinessID: businessID, ReversalPolicy: models.ReversalPolicyNextOpenPeriod}, nil
	}
	return &policy, err
}

type PostingAuthorization struct {
	Subject, Action, Resource, CommandIdentity, Token, Reason string
}

type postingLockOverrideContextKey struct{}

func withPostingLockOverride(ctx context.Context, overrideID *string) context.Context {
	if overrideID == nil {
		return ctx
	}
	return context.WithValue(ctx, postingLockOverrideContextKey{}, *overrideID)
}

func postingLockOverrideFromContext(ctx context.Context) *string {
	overrideID, _ := ctx.Value(postingLockOverrideContextKey{}).(string)
	if overrideID == "" {
		return nil
	}
	return &overrideID
}

func (s *AccountingService) PrepareLockOverride(ctx context.Context, businessID string, postingDate time.Time, auth PostingAuthorization) (*string, error) {
	if s == nil || s.db == nil {
		return nil, nil
	}
	policy, err := s.GetPolicy(ctx, businessID)
	if err != nil {
		return nil, err
	}
	if policy.LockDate == nil || accountingDate(postingDate).After(accountingDate(*policy.LockDate)) {
		return nil, nil
	}
	auth.Subject = strings.TrimSpace(auth.Subject)
	auth.Action = strings.TrimSpace(auth.Action)
	auth.Resource = strings.TrimSpace(auth.Resource)
	auth.CommandIdentity = strings.TrimSpace(auth.CommandIdentity)
	auth.Reason = strings.TrimSpace(auth.Reason)
	if s.security == nil || auth.Subject == "" || auth.Action == "" || auth.Resource == "" || auth.CommandIdentity == "" || len(auth.Reason) < 8 || len(auth.Reason) > 500 {
		return nil, ErrAccountingPeriodLocked
	}
	var existing models.AccountingLockOverride
	err = s.db.WithContext(ctx).
		Where("business_id = ? AND command_identity = ?", businessID, auth.CommandIdentity).
		First(&existing).Error
	if err == nil {
		if existing.Subject == auth.Subject && existing.Action == auth.Action && existing.Resource == auth.Resource &&
			existing.PostingDate.Equal(accountingDate(postingDate)) && existing.Reason == auth.Reason && existing.AppliedAt == nil {
			return &existing.ID, nil
		}
		return nil, ErrAccountingPeriodLocked
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if err := s.security.ConsumeStepUp(ctx, StepUpConsumeRequest{
		Subject: auth.Subject, BusinessID: businessID, Action: auth.Action, Resource: auth.Resource,
		CommandIdentity: auth.CommandIdentity, Token: auth.Token,
	}); err != nil {
		return nil, ErrAccountingPeriodLocked
	}
	override := &models.AccountingLockOverride{
		ID: uuid.NewString(), BusinessID: businessID, Subject: auth.Subject, Action: auth.Action,
		Resource: auth.Resource, CommandIdentity: auth.CommandIdentity, PostingDate: accountingDate(postingDate), Reason: auth.Reason,
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(override).Error; err != nil {
			return err
		}
		return writeAccountingAuditTx(tx, businessID, auth.Subject, "lock_override_authorized", "posting_date", override.PostingDate.Format("2006-01-02"), "accepted", auth.Reason)
	}); err != nil {
		return nil, err
	}
	return &override.ID, nil
}

func enforceJournalLockTx(tx *gorm.DB, journal *models.Journal) error {
	if journal == nil || journal.Status != models.JournalStatusPosted {
		return nil
	}
	var policy models.AccountingPeriodPolicy
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("business_id = ?", journal.BusinessID).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("read accounting period policy: %w", err)
	}
	if policy.LockDate == nil || accountingDate(journal.PostingDate).After(accountingDate(*policy.LockDate)) {
		return nil
	}
	if journal.LockOverrideID == nil || *journal.LockOverrideID == "" {
		return ErrAccountingPeriodLocked
	}
	now := time.Now().UTC()
	result := tx.Model(&models.AccountingLockOverride{}).
		Where("id = ? AND business_id = ? AND posting_date = ? AND applied_at IS NULL", *journal.LockOverrideID, journal.BusinessID, accountingDate(journal.PostingDate)).
		Update("applied_at", now)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return ErrAccountingPeriodLocked
	}
	return writeAccountingAuditTx(tx, journal.BusinessID, "system", "lock_override_applied", "journal", journal.ID, "completed", *journal.LockOverrideID)
}

func reversalPostingDateTx(tx *gorm.DB, businessID string, original, requested time.Time) (time.Time, error) {
	original = accountingDate(original)
	requested = accountingDate(requested)
	var policy models.AccountingPeriodPolicy
	err := tx.Clauses(clause.Locking{Strength: "SHARE"}).Where("business_id = ?", businessID).First(&policy).Error
	if errors.Is(err, gorm.ErrRecordNotFound) || policy.LockDate == nil || original.After(accountingDate(*policy.LockDate)) {
		return requested, nil
	}
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return time.Time{}, err
	}
	if policy.ReversalPolicy == models.ReversalPolicyBlocked {
		return time.Time{}, ErrReversalBlocked
	}
	return accountingDate(*policy.LockDate).AddDate(0, 0, 1), nil
}

type OpeningBalanceLineInput struct {
	AccountCode  string  `json:"account_code" binding:"required"`
	AccountName  string  `json:"account_name" binding:"required"`
	AccountClass string  `json:"account_class" binding:"required,oneof=asset liability equity revenue expense"`
	ParentCode   *string `json:"parent_code,omitempty"`
	EntryType    string  `json:"entry_type" binding:"required,oneof=debit credit"`
	AmountMinor  int64   `json:"amount_minor" binding:"required,gt=0"`
	Currency     string  `json:"currency" binding:"required,len=3"`
}

type InventoryOpeningInput struct {
	ProductID      string `json:"product_id" binding:"required,uuid"`
	WarehouseID    string `json:"warehouse_id" binding:"required,uuid"`
	QuantityMicros int64  `json:"quantity_micros" binding:"gte=0"`
	UnitCostMinor  int64  `json:"unit_cost_minor" binding:"gte=0"`
	Currency       string `json:"currency" binding:"required,len=3"`
}

type OpeningBalanceInput struct {
	IdempotencyKey string                    `json:"idempotency_key" binding:"required"`
	AsOfDate       time.Time                 `json:"as_of_date" binding:"required"`
	BranchID       *string                   `json:"branch_id,omitempty"`
	Lines          []OpeningBalanceLineInput `json:"lines" binding:"required,min=2,dive"`
	Inventory      []InventoryOpeningInput   `json:"inventory,omitempty"`
	Authorization  PostingAuthorization      `json:"-"`
}

func (s *AccountingService) PostOpeningBalance(ctx context.Context, businessID, subject string, input OpeningBalanceInput) (*models.Journal, error) {
	if s == nil || s.db == nil || s.journals == nil || strings.TrimSpace(input.IdempotencyKey) == "" || len(input.IdempotencyKey) > 180 {
		return nil, fmt.Errorf("invalid opening balance command")
	}
	input.AsOfDate = accountingDate(input.AsOfDate)
	requestHash, err := accountingRequestHash(struct {
		AsOfDate  time.Time
		BranchID  *string
		Lines     []OpeningBalanceLineInput
		Inventory []InventoryOpeningInput
	}{input.AsOfDate, input.BranchID, input.Lines, input.Inventory})
	if err != nil {
		return nil, err
	}
	var existing models.OpeningBalanceCommand
	if err := s.db.WithContext(ctx).Where("business_id = ? AND idempotency_key = ?", businessID, input.IdempotencyKey).First(&existing).Error; err == nil {
		if existing.RequestHash != requestHash {
			return nil, fmt.Errorf("idempotency key payload mismatch")
		}
		return s.journals.GetByBusiness(ctx, businessID, existing.JournalID)
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	createInput := CreateJournalInput{Name: "Opening balances", Reference: input.IdempotencyKey, PostingDate: input.AsOfDate, Status: models.JournalStatusPosted, BranchID: input.BranchID}
	for _, line := range input.Lines {
		currency := strings.ToUpper(strings.TrimSpace(line.Currency))
		if line.AmountMinor <= 0 || line.AmountMinor > 9007199254740991 || !accountingCurrencyPattern.MatchString(currency) {
			return nil, fmt.Errorf("invalid opening balance line")
		}
		amount := float64(line.AmountMinor) / 100
		if int64(math.Round(amount*100)) != line.AmountMinor {
			return nil, fmt.Errorf("opening balance amount is not exactly representable")
		}
		createInput.Lines = append(createInput.Lines, CreateJournalLineInput{AccountCode: line.AccountCode, AccountName: line.AccountName, AccountClass: line.AccountClass, ParentCode: line.ParentCode, EntryType: line.EntryType, Amount: amount, Currency: currency})
	}
	journal, err := s.journals.buildJournal(ctx, businessID, createInput)
	if err != nil {
		return nil, err
	}
	journal.SourceType = "opening_balance"
	journal.BranchID = input.BranchID
	journal.PostedAt = accountingTimePointer(s.now().UTC())
	resource := "opening:" + input.IdempotencyKey
	input.Authorization.Subject = subject
	if input.Authorization.Action == "" {
		input.Authorization.Action = "accounting_lock_override"
	}
	if input.Authorization.Resource == "" {
		input.Authorization.Resource = resource
	}
	if input.Authorization.CommandIdentity == "" {
		input.Authorization.CommandIdentity = input.IdempotencyKey
	}
	overrideID, err := s.PrepareLockOverride(ctx, businessID, input.AsOfDate, input.Authorization)
	if err != nil {
		return nil, err
	}
	journal.LockOverrideID = overrideID
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := validateInventoryOpeningScopeTx(tx, businessID, input.Inventory); err != nil {
			return err
		}
		if err := createJournalTx(tx, journal); err != nil {
			return err
		}
		if err := projectJournalLedgerTx(tx, journal); err != nil {
			return err
		}
		command := &models.OpeningBalanceCommand{BusinessID: businessID, IdempotencyKey: input.IdempotencyKey, RequestHash: requestHash, JournalID: journal.ID, Subject: subject}
		if err := tx.Create(command).Error; err != nil {
			return err
		}
		for _, fact := range input.Inventory {
			row := &models.InventoryOpeningBalance{BusinessID: businessID, CommandID: command.ID, ProductID: fact.ProductID, WarehouseID: fact.WarehouseID, QuantityMicros: fact.QuantityMicros, UnitCostMinor: fact.UnitCostMinor, Currency: strings.ToUpper(fact.Currency), AsOfDate: input.AsOfDate}
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		}
		return writeAccountingAuditTx(tx, businessID, subject, "opening_balance_posted", "journal", journal.ID, "completed", input.IdempotencyKey)
	})
	if err != nil {
		var replay models.OpeningBalanceCommand
		if replayErr := s.db.WithContext(ctx).Where("business_id = ? AND idempotency_key = ?", businessID, input.IdempotencyKey).First(&replay).Error; replayErr == nil {
			if replay.RequestHash != requestHash {
				return nil, fmt.Errorf("idempotency key payload mismatch")
			}
			return s.journals.GetByBusiness(ctx, businessID, replay.JournalID)
		}
	}
	return journal, err
}

func validateInventoryOpeningScopeTx(tx *gorm.DB, businessID string, rows []InventoryOpeningInput) error {
	for _, row := range rows {
		if row.QuantityMicros < 0 || row.UnitCostMinor < 0 || !accountingCurrencyPattern.MatchString(strings.ToUpper(row.Currency)) || !validInventoryOpeningValue(row.QuantityMicros, row.UnitCostMinor) {
			return fmt.Errorf("invalid inventory opening fact")
		}
		var productCount, warehouseCount int64
		if err := tx.Table("products").Where("id = ? AND business_id = ? AND deleted_at IS NULL", row.ProductID, businessID).Count(&productCount).Error; err != nil {
			return err
		}
		if err := tx.Table("warehouses").Where("id = ? AND business_id = ? AND deleted_at IS NULL", row.WarehouseID, businessID).Count(&warehouseCount).Error; err != nil {
			return err
		}
		if productCount != 1 || warehouseCount != 1 {
			return fmt.Errorf("inventory opening fact is outside business scope")
		}
	}
	return nil
}

type ReconciliationDiagnostics struct {
	JournalLedgerDifferences   int64  `json:"journal_ledger_differences"`
	InventoryGLDifferenceMinor int64  `json:"inventory_gl_difference_minor"`
	TaxGLDifferenceMinor       int64  `json:"tax_gl_difference_minor"`
	ReadOnly                   bool   `json:"read_only"`
	Currency                   string `json:"currency"`
}

func (s *AccountingService) Diagnostics(ctx context.Context, businessID, currency string, through time.Time) (*ReconciliationDiagnostics, error) {
	through = endOfAccountingDate(through)
	currency = strings.ToUpper(strings.TrimSpace(currency))
	if !accountingCurrencyPattern.MatchString(currency) {
		return nil, fmt.Errorf("diagnostic currency is required")
	}
	result := &ReconciliationDiagnostics{ReadOnly: true, Currency: currency}
	journalSQL := `WITH journal_facts AS (
		SELECT CAST(j.id AS TEXT) AS journal_id,jl.account_code,jl.entry_type,jl.currency,CAST(ROUND(jl.amount*100) AS BIGINT) amount_minor,COUNT(*) fact_count
		FROM journals j JOIN journal_lines jl ON jl.journal_id=j.id
		WHERE j.business_id=? AND j.status IN ('posted','reversed') AND j.deleted_at IS NULL AND j.posting_date<=? AND jl.currency=?
		GROUP BY j.id,jl.account_code,jl.entry_type,jl.currency,CAST(ROUND(jl.amount*100) AS BIGINT)
	), ledger_facts AS (
		SELECT le.transaction_id AS journal_id,le.category AS account_code,le.entry_type,le.currency,CAST(ROUND(le.amount*100) AS BIGINT) amount_minor,COUNT(*) fact_count
		FROM ledger_entries le WHERE le.business_id=? AND le.entry_date<=? AND le.currency=?
		GROUP BY le.transaction_id,le.category,le.entry_type,le.currency,CAST(ROUND(le.amount*100) AS BIGINT)
	) SELECT COUNT(*) FROM journal_facts jf FULL OUTER JOIN ledger_facts lf
	ON lf.journal_id=jf.journal_id AND lf.account_code=jf.account_code AND lf.entry_type=jf.entry_type AND lf.currency=jf.currency AND lf.amount_minor=jf.amount_minor
	WHERE jf.journal_id IS NULL OR lf.journal_id IS NULL OR jf.fact_count<>lf.fact_count`
	if err := s.db.WithContext(ctx).Raw(journalSQL, businessID, through, currency, businessID, through, currency).Scan(&result.JournalLedgerDifferences).Error; err != nil {
		return nil, err
	}
	var inventoryMinor, inventoryGLMinor int64
	if err := s.db.WithContext(ctx).Raw(`SELECT COALESCE(SUM((CAST(quantity_micros AS NUMERIC) * CAST(unit_cost_minor AS NUMERIC)) / 1000000),0) FROM accounting_inventory_opening_balances WHERE business_id = ? AND currency=? AND as_of_date <= ?`, businessID, currency, through).Scan(&inventoryMinor).Error; err != nil {
		return nil, err
	}
	var movementMinor int64
	var baseCurrency string
	if err := s.db.WithContext(ctx).Table("business_profiles").Select("currency").Where("id=?", businessID).Scan(&baseCurrency).Error; err != nil {
		return nil, err
	}
	if strings.EqualFold(baseCurrency, currency) {
		if err := s.db.WithContext(ctx).Raw(`SELECT COALESCE(SUM(CASE WHEN direction='in' THEN 1 WHEN direction='out' THEN -1 ELSE 0 END * CAST(ROUND(quantity*unit_cost*100) AS BIGINT)),0) FROM stock_moves WHERE business_id=? AND deleted_at IS NULL AND recorded_at<=?`, businessID, through).Scan(&movementMinor).Error; err != nil {
			return nil, err
		}
	}
	inventoryMinor += movementMinor
	if err := s.db.WithContext(ctx).Raw(`SELECT COALESCE(SUM(CASE WHEN jl.entry_type='debit' THEN CAST(ROUND(jl.amount*100) AS BIGINT) ELSE -CAST(ROUND(jl.amount*100) AS BIGINT) END),0) FROM journal_lines jl JOIN journals j ON j.id=jl.journal_id WHERE j.business_id=? AND j.status IN ('posted','reversed') AND j.posting_date<=? AND jl.account_code='INV' AND jl.currency=?`, businessID, through, currency).Scan(&inventoryGLMinor).Error; err != nil {
		return nil, err
	}
	result.InventoryGLDifferenceMinor = inventoryMinor - inventoryGLMinor
	var documentTaxMinor, taxGLMinor int64
	if err := s.db.WithContext(ctx).Raw(`SELECT COALESCE(SUM(CASE WHEN document_type IN ('sales_invoice','bill_of_supply') THEN CAST(ROUND((tax_total+cess_total)*100) AS BIGINT) WHEN document_type='credit_note' THEN -CAST(ROUND((tax_total+cess_total)*100) AS BIGINT) WHEN document_type IN ('purchase_invoice','expense') THEN -CAST(ROUND((tax_total+cess_total)*100) AS BIGINT) WHEN document_type='debit_note' THEN CAST(ROUND((tax_total+cess_total)*100) AS BIGINT) ELSE 0 END),0) FROM documents WHERE business_id=? AND currency=? AND deleted_at IS NULL AND issue_date<=? AND status NOT IN ('draft','cancelled','void')`, businessID, currency, through).Scan(&documentTaxMinor).Error; err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Raw(`SELECT COALESCE(SUM(CASE WHEN jl.entry_type='credit' THEN CAST(ROUND(jl.amount*100) AS BIGINT) ELSE -CAST(ROUND(jl.amount*100) AS BIGINT) END),0) FROM journal_lines jl JOIN journals j ON j.id=jl.journal_id WHERE j.business_id=? AND j.status IN ('posted','reversed') AND j.posting_date<=? AND jl.account_code IN ('OUT_GST','IN_GST') AND jl.currency=?`, businessID, through, currency).Scan(&taxGLMinor).Error; err != nil {
		return nil, err
	}
	result.TaxGLDifferenceMinor = documentTaxMinor - taxGLMinor
	return result, nil
}

type BankAccountInput struct {
	Name, Currency, MaskedAccount, LedgerAccount string
	OpeningMinor                                 int64
}

func (s *AccountingService) CreateBankAccount(ctx context.Context, businessID, subject string, input BankAccountInput) (*models.BankAccount, error) {
	input.Name, input.Currency = strings.TrimSpace(input.Name), strings.ToUpper(strings.TrimSpace(input.Currency))
	input.MaskedAccount, input.LedgerAccount = strings.TrimSpace(input.MaskedAccount), strings.ToUpper(strings.TrimSpace(input.LedgerAccount))
	if input.Name == "" || !accountingCurrencyPattern.MatchString(input.Currency) || input.MaskedAccount == "" || input.LedgerAccount == "" {
		return nil, fmt.Errorf("invalid bank account")
	}
	account := &models.BankAccount{BusinessID: businessID, Name: input.Name, Currency: input.Currency, MaskedAccount: input.MaskedAccount, LedgerAccount: input.LedgerAccount, OpeningMinor: input.OpeningMinor, IsActive: true}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockAccountingTenantTx(tx, businessID); err != nil {
			return err
		}
		var ledger models.AccountingAccount
		ledgerErr := tx.Where("business_id=? AND code=?", businessID, input.LedgerAccount).First(&ledger).Error
		switch {
		case errors.Is(ledgerErr, gorm.ErrRecordNotFound):
			ledger = models.AccountingAccount{BusinessID: businessID, Code: input.LedgerAccount, Name: input.Name, AccountClass: "asset"}
			if err := tx.Create(&ledger).Error; err != nil {
				return err
			}
		case ledgerErr != nil:
			return ledgerErr
		case ledger.AccountClass != "asset":
			return fmt.Errorf("bank ledger account must be an asset")
		}
		if err := tx.Create(account).Error; err != nil {
			return err
		}
		return writeAccountingAuditTx(tx, businessID, subject, "bank_account_created", "bank_account", account.ID, "completed", account.Name)
	})
	return account, err
}

func (s *AccountingService) ListBankAccounts(ctx context.Context, businessID string) ([]models.BankAccount, error) {
	var rows []models.BankAccount
	err := s.db.WithContext(ctx).Where("business_id = ?", businessID).Order("name ASC").Find(&rows).Error
	return rows, err
}

type BankTransactionState struct {
	models.BankTransaction
	Matched bool   `json:"matched"`
	MatchID string `json:"match_id,omitempty"`
}

func (s *AccountingService) ListBankTransactions(ctx context.Context, businessID, statementID string, unreconciledOnly bool) ([]BankTransactionState, error) {
	var statementCount int64
	if err := s.db.WithContext(ctx).Model(&models.BankStatement{}).Where("id=? AND business_id=?", statementID, businessID).Count(&statementCount).Error; err != nil {
		return nil, err
	}
	if statementCount != 1 {
		return nil, fmt.Errorf("bank statement not found")
	}
	query := s.db.WithContext(ctx).Table("bank_transactions bt").Select("bt.*, CASE WHEN bm.id IS NULL THEN FALSE ELSE TRUE END AS matched, COALESCE(CAST(bm.id AS TEXT),'') AS match_id").Joins("LEFT JOIN bank_matches bm ON bm.bank_transaction_id=bt.id AND bm.unmatched_at IS NULL").Where("bt.business_id=? AND bt.statement_id=?", businessID, statementID)
	if unreconciledOnly {
		query = query.Where("bm.id IS NULL")
	}
	var rows []BankTransactionState
	err := query.Order("bt.transaction_at ASC,bt.id ASC").Find(&rows).Error
	return rows, err
}

func (s *AccountingService) ListAccountingAudit(ctx context.Context, businessID string, limit int) ([]models.AccountingAuditEvent, error) {
	if limit <= 0 || limit > 200 {
		limit = 100
	}
	var rows []models.AccountingAuditEvent
	err := s.db.WithContext(ctx).Where("business_id=?", businessID).Order("occurred_at DESC,id DESC").Limit(limit).Find(&rows).Error
	return rows, err
}

type BankTransactionInput struct {
	ExternalID    string    `json:"external_id" binding:"required"`
	TransactionAt time.Time `json:"transaction_at" binding:"required"`
	AmountMinor   int64     `json:"amount_minor" binding:"required"`
	Currency      string    `json:"currency" binding:"required,len=3"`
	Reference     string    `json:"reference"`
	Description   string    `json:"description"`
}

type BankStatementInput struct {
	BankAccountID  string    `json:"bank_account_id" binding:"required,uuid"`
	UploadID       string    `json:"upload_id" binding:"required,uuid"`
	IdempotencyKey string    `json:"idempotency_key" binding:"required"`
	PeriodFrom     time.Time `json:"period_from" binding:"required"`
	PeriodTo       time.Time `json:"period_to" binding:"required"`
}

func (s *AccountingService) ImportBankStatement(ctx context.Context, businessID, subject string, input BankStatementInput) (*models.BankStatement, error) {
	if input.PeriodTo.Before(input.PeriodFrom) || strings.TrimSpace(input.IdempotencyKey) == "" || len(input.IdempotencyKey) > 180 {
		return nil, fmt.Errorf("invalid bank statement")
	}
	requestHash, hashErr := accountingRequestHash(input)
	if hashErr != nil {
		return nil, hashErr
	}
	var existing models.BankStatement
	if err := s.db.WithContext(ctx).Where("business_id=? AND idempotency_key=?", businessID, input.IdempotencyKey).First(&existing).Error; err == nil {
		if existing.RequestHash != requestHash {
			return nil, fmt.Errorf("idempotency key payload mismatch")
		}
		return &existing, nil
	}
	if err := s.db.WithContext(ctx).Where("business_id=? AND upload_id=?", businessID, input.UploadID).First(&existing).Error; err == nil {
		return nil, fmt.Errorf("bank statement upload was already imported")
	} else if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	if s.uploads == nil || s.objects == nil {
		return nil, fmt.Errorf("bank statement file service unavailable")
	}
	upload, uploadErr := s.uploads.GetPendingUpload(ctx, input.UploadID, businessID, subject)
	if uploadErr != nil || upload.Status != models.PendingUploadStatusClean || upload.Kind != "bank_statement" || upload.ContentType != "text/csv" {
		return nil, fmt.Errorf("verified bank statement upload is required")
	}
	content, readErr := s.objects.ReadPendingObject(ctx, upload.Bucket, upload.ObjectKey)
	if readErr != nil {
		return nil, fmt.Errorf("read bank statement: %w", readErr)
	}
	transactions, parseErr := parseBankStatementCSV(content)
	if parseErr != nil {
		return nil, parseErr
	}
	statement := &models.BankStatement{BusinessID: businessID, BankAccountID: input.BankAccountID, UploadID: input.UploadID, IdempotencyKey: input.IdempotencyKey, RequestHash: requestHash, Status: models.BankStatementPending, PeriodFrom: accountingDate(input.PeriodFrom), PeriodTo: accountingDate(input.PeriodTo), ImportedBy: subject}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var account models.BankAccount
		if err := tx.Where("id=? AND business_id=? AND is_active=TRUE", input.BankAccountID, businessID).First(&account).Error; err != nil {
			return fmt.Errorf("bank account not found")
		}
		var count int64
		if err := tx.Table("security_pending_uploads").Where("id=? AND business_id=? AND uploader_id=? AND status='clean' AND kind='bank_statement'", input.UploadID, businessID, subject).Count(&count).Error; err != nil || count != 1 {
			return fmt.Errorf("verified bank statement upload is required")
		}
		if err := tx.Create(statement).Error; err != nil {
			return err
		}
		for _, item := range transactions {
			currency := strings.ToUpper(strings.TrimSpace(item.Currency))
			if !validExactAccountingMinor(item.AmountMinor) || currency != account.Currency || !accountingCurrencyPattern.MatchString(currency) || item.TransactionAt.Before(statement.PeriodFrom) || item.TransactionAt.After(endOfAccountingDate(statement.PeriodTo)) {
				return fmt.Errorf("invalid bank transaction")
			}
			row := &models.BankTransaction{BusinessID: businessID, StatementID: statement.ID, ExternalID: strings.TrimSpace(item.ExternalID), TransactionAt: accountingDate(item.TransactionAt), AmountMinor: item.AmountMinor, Currency: currency, Reference: strings.TrimSpace(item.Reference), Description: strings.TrimSpace(item.Description)}
			if row.ExternalID == "" {
				return fmt.Errorf("invalid bank transaction")
			}
			if err := tx.Create(row).Error; err != nil {
				return err
			}
		}
		return writeAccountingAuditTx(tx, businessID, subject, "bank_statement_imported", "bank_statement", statement.ID, "completed", input.IdempotencyKey)
	})
	if err != nil {
		var replay models.BankStatement
		if replayErr := s.db.WithContext(ctx).Where("business_id=? AND idempotency_key=?", businessID, input.IdempotencyKey).First(&replay).Error; replayErr == nil {
			if replay.RequestHash != requestHash {
				return nil, fmt.Errorf("idempotency key payload mismatch")
			}
			return &replay, nil
		}
		if uploadErr := s.db.WithContext(ctx).Where("business_id=? AND upload_id=?", businessID, input.UploadID).First(&replay).Error; uploadErr == nil {
			return nil, fmt.Errorf("bank statement upload was already imported")
		}
	}
	return statement, err
}

type BankMatchSuggestion struct {
	LedgerEntryID string    `json:"ledger_entry_id"`
	TransactionID string    `json:"transaction_id"`
	EntryDate     time.Time `json:"entry_date"`
	AmountMinor   int64     `json:"amount_minor"`
	Description   string    `json:"description"`
	Score         int       `json:"score"`
	Kind          string    `json:"kind"`
}

func (s *AccountingService) SuggestBankMatches(ctx context.Context, businessID, transactionID string) ([]BankMatchSuggestion, error) {
	var transaction models.BankTransaction
	if err := s.db.WithContext(ctx).Where("id=? AND business_id=?", transactionID, businessID).First(&transaction).Error; err != nil {
		return nil, fmt.Errorf("bank transaction not found")
	}
	if !validExactAccountingMinor(transaction.AmountMinor) {
		return nil, fmt.Errorf("bank transaction amount is invalid")
	}
	var account models.BankAccount
	if err := s.db.WithContext(ctx).Table("bank_accounts ba").Joins("JOIN bank_statements bs ON bs.bank_account_id=ba.id").Where("bs.id=? AND bs.business_id=? AND ba.business_id=?", transaction.StatementID, businessID, businessID).First(&account).Error; err != nil {
		return nil, fmt.Errorf("bank account not found")
	}
	requiredSide := bankLedgerSide(transaction.AmountMinor)
	type candidate struct {
		ID, TransactionID, Description string
		EntryDate                      time.Time
		Amount                         float64
	}
	var candidates []candidate
	from, to := transaction.TransactionAt.AddDate(0, 0, -3), transaction.TransactionAt.AddDate(0, 0, 3)
	err := s.db.WithContext(ctx).Table("ledger_entries le").
		Select("le.id, le.transaction_id, le.description, le.entry_date, le.amount").
		Joins("JOIN journals j ON j.id=le.transaction_id AND j.business_id=le.business_id").
		Where("le.business_id=? AND le.currency=? AND le.category=? AND le.entry_type=? AND le.entry_date BETWEEN ? AND ? AND CAST(ROUND(le.amount*100) AS BIGINT)=?", businessID, transaction.Currency, account.LedgerAccount, requiredSide, from, to, abs64(transaction.AmountMinor)).
		Where("j.status=? AND j.deleted_at IS NULL", models.JournalStatusPosted).
		Where("NOT EXISTS (SELECT 1 FROM bank_matches bm WHERE bm.ledger_entry_id=le.id AND bm.unmatched_at IS NULL)").
		Order("le.entry_date ASC, le.id ASC").Limit(50).Scan(&candidates).Error
	if err != nil {
		return nil, err
	}
	needle := normalizeBankText(transaction.Reference + " " + transaction.Description)
	result := make([]BankMatchSuggestion, 0, len(candidates))
	for _, item := range candidates {
		haystack := normalizeBankText(item.TransactionID + " " + item.Description)
		days := int(math.Abs(item.EntryDate.Sub(transaction.TransactionAt).Hours() / 24))
		score := 70 - (days * 10)
		kind := "fuzzy"
		if needle != "" && (strings.Contains(haystack, needle) || strings.Contains(needle, haystack)) {
			score, kind = 100, "exact"
		} else {
			score += boundedTokenOverlap(needle, haystack, 20)
		}
		if score >= 50 {
			result = append(result, BankMatchSuggestion{LedgerEntryID: item.ID, TransactionID: item.TransactionID, EntryDate: item.EntryDate, AmountMinor: int64(math.Round(item.Amount * 100)), Description: item.Description, Score: score, Kind: kind})
		}
	}
	sort.Slice(result, func(i, j int) bool {
		if result[i].Score == result[j].Score {
			return result[i].LedgerEntryID < result[j].LedgerEntryID
		}
		return result[i].Score > result[j].Score
	})
	return result, nil
}

func (s *AccountingService) MatchBankTransaction(ctx context.Context, businessID, subject, transactionID, ledgerEntryID, reason string) (*models.BankMatch, error) {
	reason = strings.TrimSpace(reason)
	if len(reason) < 3 || len(reason) > 500 {
		return nil, fmt.Errorf("match reason is required")
	}
	match := &models.BankMatch{BusinessID: businessID, BankTransactionID: transactionID, LedgerEntryID: ledgerEntryID, MatchType: "manual", MatchedBy: subject, Reason: reason}
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var transaction models.BankTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND business_id=? AND EXISTS (SELECT 1 FROM bank_statements bs WHERE bs.id=bank_transactions.statement_id AND bs.business_id=? AND bs.status=?)", transactionID, businessID, businessID, models.BankStatementPending).First(&transaction).Error; err != nil {
			return fmt.Errorf("bank transaction not found")
		}
		if !validExactAccountingMinor(transaction.AmountMinor) {
			return fmt.Errorf("bank transaction amount is invalid")
		}
		var account models.BankAccount
		if err := tx.Table("bank_accounts ba").Joins("JOIN bank_statements bs ON bs.bank_account_id=ba.id").Where("bs.id=? AND bs.business_id=? AND ba.business_id=?", transaction.StatementID, businessID, businessID).First(&account).Error; err != nil {
			return fmt.Errorf("bank account not found")
		}
		var entry models.LedgerEntry
		if err := tx.Table("ledger_entries le").Select("le.*").Joins("JOIN journals j ON j.id=le.transaction_id AND j.business_id=le.business_id").Where("le.id=? AND le.business_id=? AND le.currency=? AND le.category=? AND le.entry_type=? AND j.status=? AND j.deleted_at IS NULL", ledgerEntryID, businessID, transaction.Currency, account.LedgerAccount, bankLedgerSide(transaction.AmountMinor), models.JournalStatusPosted).First(&entry).Error; err != nil {
			return fmt.Errorf("ledger entry not found")
		}
		if int64(math.Round(entry.Amount*100)) != abs64(transaction.AmountMinor) {
			return fmt.Errorf("bank and ledger amounts differ")
		}
		if err := tx.Create(match).Error; err != nil {
			return err
		}
		return writeAccountingAuditTx(tx, businessID, subject, "bank_transaction_matched", "bank_transaction", transactionID, "completed", reason)
	})
	return match, err
}

func bankLedgerSide(amountMinor int64) string {
	if amountMinor > 0 {
		return "debit"
	}
	return "credit"
}

func (s *AccountingService) UnmatchBankTransaction(ctx context.Context, businessID, subject, transactionID, reason string) error {
	reason = strings.TrimSpace(reason)
	if len(reason) < 3 {
		return fmt.Errorf("unmatch reason is required")
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var statement models.BankStatement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Joins("JOIN bank_transactions bt ON bt.statement_id=bank_statements.id").
			Where("bt.id=? AND bt.business_id=? AND bank_statements.business_id=?", transactionID, businessID, businessID).
			First(&statement).Error; err != nil || statement.Status != models.BankStatementPending {
			return fmt.Errorf("bank statement is already reconciled")
		}
		var active models.BankMatch
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("business_id=? AND bank_transaction_id=? AND unmatched_at IS NULL", businessID, transactionID).First(&active).Error; err != nil {
			return fmt.Errorf("active bank match not found")
		}
		if active.MatchType == "fee" || active.MatchType == "interest" {
			if err := reverseBankAdjustmentMatchTx(tx, businessID, transactionID, &active, s.now().UTC()); err != nil {
				return err
			}
		}
		now := s.now().UTC()
		result := tx.Model(&models.BankMatch{}).Where("id=? AND unmatched_at IS NULL", active.ID).Updates(map[string]interface{}{"unmatched_at": now, "unmatched_by": subject})
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected != 1 {
			return fmt.Errorf("active bank match not found")
		}
		return writeAccountingAuditTx(tx, businessID, subject, "bank_transaction_unmatched", "bank_transaction", transactionID, "completed", reason)
	})
}

func reverseBankAdjustmentMatchTx(tx *gorm.DB, businessID, transactionID string, match *models.BankMatch, requested time.Time) error {
	if match == nil || match.LedgerEntryID == "" {
		return fmt.Errorf("bank adjustment match is invalid")
	}
	var entry models.LedgerEntry
	if err := tx.Where("id=? AND business_id=?", match.LedgerEntryID, businessID).First(&entry).Error; err != nil {
		return fmt.Errorf("bank adjustment ledger entry not found")
	}
	var original models.Journal
	if err := loadJournalForUpdateTx(tx, businessID, entry.TransactionID, &original); err != nil {
		return err
	}
	if original.Status != models.JournalStatusPosted || original.SourceType != "bank_adjustment" || original.SourceID == nil || *original.SourceID != transactionID {
		return fmt.Errorf("bank adjustment journal is not reversible")
	}
	postingDate, err := reversalPostingDateTx(tx, businessID, original.PostingDate, requested)
	if err != nil {
		return err
	}
	reversal := &models.Journal{BusinessID: businessID, Name: "Reversal: " + original.Name, Reference: original.Reference, ProjectID: original.ProjectID, BranchID: original.BranchID, Status: models.JournalStatusPosted, PostingDate: postingDate, Notes: "Bank reconciliation adjustment reversal", SourceType: "bank_adjustment_reversal", SourceID: &transactionID, ReversalOfID: &original.ID, PostedAt: &postingDate}
	for _, line := range original.Lines {
		entryType := "debit"
		if line.EntryType == "debit" {
			entryType = "credit"
		}
		reversal.Lines = append(reversal.Lines, &models.JournalLine{AccountCode: line.AccountCode, AccountName: line.AccountName, EntryType: entryType, Amount: line.Amount, Currency: line.Currency, Description: "Reversal of " + original.Name, DocumentID: line.DocumentID, Metadata: line.Metadata})
	}
	if err := createJournalTx(tx, reversal); err != nil {
		return err
	}
	if err := projectJournalLedgerTx(tx, reversal); err != nil {
		return err
	}
	result := tx.Model(&models.Journal{}).Where("id=? AND business_id=? AND status=?", original.ID, businessID, models.JournalStatusPosted).Updates(map[string]interface{}{"status": models.JournalStatusReversed, "reversed_at": postingDate})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("bank adjustment journal changed during reversal")
	}
	return nil
}

type BankAdjustmentInput struct {
	Kind           string               `json:"kind" binding:"required,oneof=fee interest"`
	Reason         string               `json:"reason" binding:"required"`
	IdempotencyKey string               `json:"idempotency_key" binding:"required"`
	Authorization  PostingAuthorization `json:"-"`
}

func (s *AccountingService) CreateBankAdjustment(ctx context.Context, businessID, subject, transactionID string, input BankAdjustmentInput) (*models.Journal, error) {
	if s == nil || s.journals == nil || (input.Kind != "fee" && input.Kind != "interest") || len(strings.TrimSpace(input.Reason)) < 3 || strings.TrimSpace(input.IdempotencyKey) == "" {
		return nil, fmt.Errorf("invalid bank adjustment")
	}
	input.Kind = strings.TrimSpace(input.Kind)
	input.Reason = strings.TrimSpace(input.Reason)
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	requestHash, err := accountingRequestHash(struct {
		TransactionID string `json:"transaction_id"`
		Kind          string `json:"kind"`
		Reason        string `json:"reason"`
	}{TransactionID: transactionID, Kind: input.Kind, Reason: input.Reason})
	if err != nil {
		return nil, err
	}
	if replayID, replayed, replayErr := lookupCompletedAPICommand(ctx, s.db, businessID, bankAdjustmentCommand, input.IdempotencyKey, requestHash, "journal"); replayErr != nil {
		return nil, replayErr
	} else if replayed {
		return s.journals.GetByBusiness(ctx, businessID, replayID)
	}
	var transaction models.BankTransaction
	if err := s.db.WithContext(ctx).Where("id=? AND business_id=?", transactionID, businessID).First(&transaction).Error; err != nil {
		return nil, fmt.Errorf("bank transaction not found")
	}
	if (input.Kind == "fee" && transaction.AmountMinor > 0) || (input.Kind == "interest" && transaction.AmountMinor < 0) {
		return nil, fmt.Errorf("bank adjustment direction is invalid")
	}
	var statement models.BankStatement
	if err := s.db.WithContext(ctx).Where("id=? AND business_id=?", transaction.StatementID, businessID).First(&statement).Error; err != nil {
		return nil, err
	}
	if statement.Status != models.BankStatementPending {
		return nil, fmt.Errorf("bank statement is already reconciled")
	}
	if !validExactAccountingMinor(transaction.AmountMinor) {
		return nil, fmt.Errorf("bank transaction amount is outside exact accounting range")
	}
	var account models.BankAccount
	if err := s.db.WithContext(ctx).Where("id=? AND business_id=?", statement.BankAccountID, businessID).First(&account).Error; err != nil {
		return nil, err
	}
	amount := float64(abs64(transaction.AmountMinor)) / 100
	var lines []CreateJournalLineInput
	if input.Kind == "fee" {
		lines = []CreateJournalLineInput{{AccountCode: "BANK_FEE", AccountName: "Bank fees", EntryType: "debit", Amount: amount, Currency: transaction.Currency}, {AccountCode: account.LedgerAccount, AccountName: account.Name, EntryType: "credit", Amount: amount, Currency: transaction.Currency}}
	} else {
		lines = []CreateJournalLineInput{{AccountCode: account.LedgerAccount, AccountName: account.Name, EntryType: "debit", Amount: amount, Currency: transaction.Currency}, {AccountCode: "INTEREST_INCOME", AccountName: "Interest income", EntryType: "credit", Amount: amount, Currency: transaction.Currency}}
	}
	journal, err := s.journals.buildJournal(ctx, businessID, CreateJournalInput{Name: "Bank " + input.Kind + " adjustment", Reference: transaction.Reference, PostingDate: transaction.TransactionAt, Status: models.JournalStatusPosted, Notes: input.Reason, Lines: lines})
	if err != nil {
		return nil, err
	}
	journal.SourceType = "bank_adjustment"
	journal.SourceID = &transaction.ID
	journal.PostedAt = accountingTimePointer(s.now().UTC())
	input.Authorization.Subject = subject
	if input.Authorization.Action == "" {
		input.Authorization.Action = "accounting_lock_override"
	}
	if input.Authorization.Resource == "" {
		input.Authorization.Resource = "bank-adjustment:" + transactionID
	}
	if input.Authorization.CommandIdentity == "" {
		input.Authorization.CommandIdentity = input.IdempotencyKey
	}
	overrideID, err := s.PrepareLockOverride(ctx, businessID, transaction.TransactionAt, input.Authorization)
	if err != nil {
		return nil, err
	}
	journal.LockOverrideID = overrideID
	var replayJournalID string
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		replayedID, claimErr := claimAPICommandTx(tx, businessID, bankAdjustmentCommand, input.IdempotencyKey, requestHash, "journal")
		if claimErr != nil {
			return claimErr
		}
		if replayedID != "" {
			replayJournalID = replayedID
			return nil
		}
		var lockedStatement models.BankStatement
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND business_id=? AND status=?", statement.ID, businessID, models.BankStatementPending).First(&lockedStatement).Error; err != nil {
			return fmt.Errorf("bank statement is already reconciled")
		}
		var lockedTransaction models.BankTransaction
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND business_id=? AND statement_id=?", transactionID, businessID, lockedStatement.ID).First(&lockedTransaction).Error; err != nil {
			return fmt.Errorf("bank transaction not found")
		}
		var active int64
		if err := tx.Table("bank_matches").Where("business_id=? AND bank_transaction_id=? AND unmatched_at IS NULL", businessID, transactionID).Count(&active).Error; err != nil {
			return err
		}
		if active != 0 {
			return fmt.Errorf("bank transaction is already matched")
		}
		if err := createJournalTx(tx, journal); err != nil {
			return err
		}
		if err := projectJournalLedgerTx(tx, journal); err != nil {
			return err
		}
		var entry models.LedgerEntry
		if err := tx.Where("business_id=? AND transaction_id=? AND category=?", businessID, journal.ID, account.LedgerAccount).First(&entry).Error; err != nil {
			return err
		}
		match := &models.BankMatch{BusinessID: businessID, BankTransactionID: transactionID, LedgerEntryID: entry.ID, MatchType: input.Kind, MatchedBy: subject, Reason: input.Reason}
		if err := tx.Create(match).Error; err != nil {
			return err
		}
		if err := writeAccountingAuditTx(tx, businessID, subject, "bank_adjustment_posted", "journal", journal.ID, "completed", input.Kind); err != nil {
			return err
		}
		return completeAPICommandTx(tx, businessID, bankAdjustmentCommand, input.IdempotencyKey, requestHash, "journal", journal.ID)
	})
	if replayJournalID != "" {
		return s.journals.GetByBusiness(ctx, businessID, replayJournalID)
	}
	return journal, err
}

func (s *AccountingService) ReconcileStatement(ctx context.Context, businessID, subject, statementID string, reconciliationDate time.Time) (*models.BankStatement, error) {
	var statement models.BankStatement
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id=? AND business_id=?", statementID, businessID).First(&statement).Error; err != nil {
			return fmt.Errorf("bank statement not found")
		}
		if statement.Status == models.BankStatementReconciled {
			return nil
		}
		var unmatched int64
		if err := tx.Table("bank_transactions bt").Where("bt.statement_id=? AND NOT EXISTS (SELECT 1 FROM bank_matches bm WHERE bm.bank_transaction_id=bt.id AND bm.unmatched_at IS NULL)", statementID).Count(&unmatched).Error; err != nil {
			return err
		}
		if unmatched != 0 {
			return fmt.Errorf("bank statement has unreconciled transactions")
		}
		date := accountingDate(reconciliationDate)
		if date.Before(statement.PeriodTo) {
			return fmt.Errorf("reconciliation date precedes statement period")
		}
		statement.Status = models.BankStatementReconciled
		statement.ReconciledAt = &date
		statement.ReconciledBy = subject
		if err := tx.Model(&models.BankStatement{}).Where("id=? AND business_id=? AND status=?", statementID, businessID, models.BankStatementPending).Updates(map[string]interface{}{"status": statement.Status, "reconciled_at": date, "reconciled_by": subject}).Error; err != nil {
			return err
		}
		return writeAccountingAuditTx(tx, businessID, subject, "bank_statement_reconciled", "bank_statement", statementID, "completed", date.Format("2006-01-02"))
	})
	return &statement, err
}

func writeAccountingAuditTx(tx *gorm.DB, businessID, subject, eventType, resourceType, resourceID, outcome, reason string) error {
	return tx.Create(&models.AccountingAuditEvent{BusinessID: businessID, Subject: subject, EventType: eventType, ResourceType: resourceType, ResourceID: resourceID, Outcome: outcome, Reason: reason}).Error
}

func accountingDate(value time.Time) time.Time {
	if value.IsZero() {
		value = time.Now().UTC()
	}
	value = value.UTC()
	return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC)
}

func accountingRequestHash(value any) (string, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return "", err
	}
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:]), nil
}

func parseBankStatementCSV(content []byte) ([]BankTransactionInput, error) {
	if len(content) == 0 || len(content) > 25*1024*1024 {
		return nil, fmt.Errorf("invalid bank statement file")
	}
	reader := csv.NewReader(bytes.NewReader(content))
	reader.TrimLeadingSpace = true
	reader.FieldsPerRecord = -1
	header, err := reader.Read()
	if err != nil {
		return nil, fmt.Errorf("read bank statement header: %w", err)
	}
	indexes := map[string]int{}
	for i, name := range header {
		indexes[strings.ToLower(strings.TrimSpace(name))] = i
	}
	for _, required := range []string{"external_id", "transaction_at", "amount_minor", "currency"} {
		if _, ok := indexes[required]; !ok {
			return nil, fmt.Errorf("bank statement column %s is required", required)
		}
	}
	rows := make([]BankTransactionInput, 0)
	for line := 2; line <= 10002; line++ {
		record, readErr := reader.Read()
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read bank statement row %d: %w", line, readErr)
		}
		if len(rows) >= 10000 {
			return nil, fmt.Errorf("bank statement row limit exceeded")
		}
		value := func(name string) string {
			index, ok := indexes[name]
			if !ok || index >= len(record) {
				return ""
			}
			return strings.TrimSpace(record[index])
		}
		date, dateErr := time.Parse("2006-01-02", value("transaction_at"))
		amount, amountErr := strconv.ParseInt(value("amount_minor"), 10, 64)
		if dateErr != nil || amountErr != nil || !validExactAccountingMinor(amount) {
			return nil, fmt.Errorf("invalid bank statement row %d", line)
		}
		rows = append(rows, BankTransactionInput{ExternalID: value("external_id"), TransactionAt: date, AmountMinor: amount, Currency: strings.ToUpper(value("currency")), Reference: value("reference"), Description: value("description")})
	}
	if len(rows) == 0 {
		return nil, fmt.Errorf("bank statement has no transactions")
	}
	return rows, nil
}

func endOfAccountingDate(value time.Time) time.Time {
	return accountingDate(value).Add(24*time.Hour - time.Nanosecond)
}
func accountingTimePointer(value time.Time) *time.Time { return &value }
func abs64(value int64) int64 {
	if value < 0 {
		return -value
	}
	return value
}

func validExactAccountingMinor(value int64) bool {
	return value != 0 && value >= -maxExactAccountingMinor && value <= maxExactAccountingMinor
}

func validInventoryOpeningValue(quantityMicros, unitCostMinor int64) bool {
	if quantityMicros < 0 || unitCostMinor < 0 {
		return false
	}
	value := new(big.Int).Mul(big.NewInt(quantityMicros), big.NewInt(unitCostMinor))
	value.Div(value, big.NewInt(1000000))
	return value.IsInt64() && value.Int64() <= maxExactAccountingMinor
}
func normalizeBankText(value string) string {
	return strings.Join(strings.Fields(strings.ToLower(regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(value, " "))), " ")
}
func boundedTokenOverlap(a, b string, maximum int) int {
	if a == "" || b == "" {
		return 0
	}
	seen := map[string]struct{}{}
	for _, token := range strings.Fields(a) {
		if len(token) >= 3 {
			seen[token] = struct{}{}
		}
	}
	matches := 0
	for _, token := range strings.Fields(b) {
		if _, ok := seen[token]; ok {
			matches++
		}
	}
	if matches*5 > maximum {
		return maximum
	}
	return matches * 5
}
