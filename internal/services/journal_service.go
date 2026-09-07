package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type JournalService struct {
	db         *gorm.DB
	repo       interfaces.JournalRepository
	log        *logger.Logger
	accounting *AccountingService
	businesses businessTimezoneProvider
	now        func() time.Time
}

func NewJournalService(db *gorm.DB, repo interfaces.JournalRepository, log *logger.Logger) *JournalService {
	return &JournalService{db: db, repo: repo, log: log, now: time.Now}
}

func (s *JournalService) WithBusinessTimezoneProvider(provider businessTimezoneProvider) *JournalService {
	s.businesses = provider
	return s
}

func (s *JournalService) WithAccounting(accounting *AccountingService) *JournalService {
	s.accounting = accounting
	return s
}

type CreateJournalLineInput struct {
	AccountCode  string                 `json:"account_code" binding:"required"`
	AccountName  string                 `json:"account_name" binding:"required"`
	EntryType    string                 `json:"entry_type" binding:"required,oneof=debit credit"`
	Amount       float64                `json:"amount" binding:"required,gt=0"`
	Currency     string                 `json:"currency"`
	Description  string                 `json:"description"`
	DocumentID   *string                `json:"document_id,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`
	AccountClass string                 `json:"account_class,omitempty" binding:"omitempty,oneof=asset liability equity revenue expense"`
	ParentCode   *string                `json:"parent_code,omitempty"`
}

type CreateJournalInput struct {
	BusinessID  string                   `json:"business_id,omitempty"`
	Name        string                   `json:"name" binding:"required"`
	Reference   string                   `json:"reference"`
	ProjectID   string                   `json:"project_id,omitempty" binding:"omitempty,uuid"`
	BranchID    *string                  `json:"branch_id,omitempty" binding:"omitempty,uuid"`
	PostingDate time.Time                `json:"posting_date"`
	Notes       string                   `json:"notes"`
	Status      string                   `json:"status"`
	Lines       []CreateJournalLineInput `json:"lines" binding:"required,min=2,dive"`
}

func (s *JournalService) CreateByBusiness(ctx context.Context, businessID string, input CreateJournalInput) (*models.Journal, error) {
	return s.CreateAuthorized(ctx, businessID, input, PostingAuthorization{})
}

func (s *JournalService) CreateAuthorized(ctx context.Context, businessID string, input CreateJournalInput, authorization PostingAuthorization) (*models.Journal, error) {
	journal, err := s.buildJournal(ctx, businessID, input)
	if err != nil {
		return nil, err
	}
	if journal.Status == models.JournalStatusPosted {
		now := time.Now()
		journal.PostedAt = &now
		if s.accounting != nil {
			overrideID, overrideErr := s.accounting.PrepareLockOverride(ctx, businessID, journal.PostingDate, authorization)
			if overrideErr != nil {
				return nil, overrideErr
			}
			journal.LockOverrideID = overrideID
		}
	}
	if err := s.requireDatabase(); err != nil {
		return nil, err
	}
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := createJournalTx(tx, journal); err != nil {
			return err
		}
		if journal.Status == models.JournalStatusPosted {
			return projectJournalLedgerTx(tx, journal)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return journal, nil
}

func (s *JournalService) buildJournal(ctx context.Context, businessID string, input CreateJournalInput) (*models.Journal, error) {
	if input.BranchID != nil {
		var count int64
		if s.db == nil || s.db.WithContext(ctx).Model(&models.Branch{}).Where("id=? AND business_id=? AND deleted_at IS NULL", *input.BranchID, businessID).Count(&count).Error != nil || count != 1 {
			return nil, fmt.Errorf("branch does not belong to business")
		}
	}
	totals := make(map[string]struct{ debit, credit int64 })
	journal := &models.Journal{
		BusinessID:  businessID,
		Name:        input.Name,
		Reference:   input.Reference,
		ProjectID:   projectIDPointer(input.ProjectID),
		BranchID:    input.BranchID,
		Status:      input.Status,
		PostingDate: input.PostingDate,
		Notes:       input.Notes,
	}
	if journal.PostingDate.IsZero() {
		journal.PostingDate = time.Now()
	}
	if journal.Status == "" {
		journal.Status = models.JournalStatusDraft
	}
	if journal.Status != models.JournalStatusDraft && journal.Status != models.JournalStatusPosted {
		return nil, fmt.Errorf("unsupported journal status")
	}
	for _, line := range input.Lines {
		if line.EntryType != "debit" && line.EntryType != "credit" {
			return nil, fmt.Errorf("unsupported journal entry type")
		}
		minor, err := journalMinorUnits(line.Amount)
		if err != nil {
			return nil, err
		}
		currency := defaultCurrency(line.Currency)
		total := totals[currency]
		if line.EntryType == "debit" {
			total.debit += minor
		} else {
			total.credit += minor
		}
		totals[currency] = total
		metadata := make(map[string]interface{}, len(line.Metadata)+2)
		for key, value := range line.Metadata {
			metadata[key] = value
		}
		if line.AccountClass != "" {
			metadata["account_class"] = strings.ToLower(strings.TrimSpace(line.AccountClass))
		}
		if line.ParentCode != nil && strings.TrimSpace(*line.ParentCode) != "" {
			metadata["parent_code"] = strings.ToUpper(strings.TrimSpace(*line.ParentCode))
		}
		journal.Lines = append(journal.Lines, &models.JournalLine{
			AccountCode: line.AccountCode,
			AccountName: line.AccountName,
			EntryType:   line.EntryType,
			Amount:      float64(minor) / 100,
			Currency:    currency,
			Description: line.Description,
			DocumentID:  line.DocumentID,
			Metadata:    mustMarshalMap(metadata),
		})
	}
	for _, total := range totals {
		if total.debit != total.credit {
			return nil, fmt.Errorf("journal is not balanced")
		}
	}
	return journal, nil
}

func (s *JournalService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Journal, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

func (s *JournalService) List(ctx context.Context, businessID string, page, limit int) ([]*models.Journal, int64, error) {
	return s.repo.ListByBusinessID(ctx, businessID, page, limit)
}

func (s *JournalService) UpdateByBusiness(ctx context.Context, businessID, id string, input CreateJournalInput) (*models.Journal, error) {
	return s.UpdateAuthorized(ctx, businessID, id, input, PostingAuthorization{})
}

func (s *JournalService) UpdateAuthorized(ctx context.Context, businessID, id string, input CreateJournalInput, authorization PostingAuthorization) (*models.Journal, error) {
	rebuilt, err := s.buildJournal(ctx, businessID, input)
	if err != nil {
		return nil, err
	}
	if err := s.requireDatabase(); err != nil {
		return nil, err
	}
	var preparedOverrideID *string
	if rebuilt.Status == models.JournalStatusPosted && s.accounting != nil {
		preparedOverrideID, err = s.accounting.PrepareLockOverride(ctx, businessID, rebuilt.PostingDate, authorization)
		if err != nil {
			return nil, err
		}
	}
	var existing models.Journal
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := loadJournalForUpdateTx(tx, businessID, id, &existing); err != nil {
			return err
		}
		if existing.Status != models.JournalStatusDraft {
			return fmt.Errorf("only draft journals can be updated")
		}
		existing.Name = rebuilt.Name
		existing.Reference = rebuilt.Reference
		existing.ProjectID = rebuilt.ProjectID
		existing.Status = rebuilt.Status
		existing.PostingDate = rebuilt.PostingDate
		existing.Notes = rebuilt.Notes
		existing.Lines = rebuilt.Lines
		if existing.Status == models.JournalStatusPosted {
			now := time.Now()
			existing.PostedAt = &now
			existing.LockOverrideID = preparedOverrideID
		}
		existing.BranchID = rebuilt.BranchID
		if err := replaceDraftJournalTx(tx, &existing); err != nil {
			return err
		}
		if existing.Status == models.JournalStatusPosted {
			return projectJournalLedgerTx(tx, &existing)
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return &existing, nil
}

func (s *JournalService) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	if err := s.requireDatabase(); err != nil {
		return err
	}
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var journal models.Journal
		if err := loadJournalForUpdateTx(tx, businessID, id, &journal); err != nil {
			return err
		}
		if journal.Status != models.JournalStatusDraft {
			return fmt.Errorf("only draft journals can be deleted")
		}
		return tx.Where("id = ? AND business_id = ?", id, businessID).Delete(&models.Journal{}).Error
	})
}

func (s *JournalService) PostByBusiness(ctx context.Context, businessID, id string) (*models.Journal, error) {
	return s.PostAuthorized(ctx, businessID, id, PostingAuthorization{})
}

func (s *JournalService) PostAuthorized(ctx context.Context, businessID, id string, authorization PostingAuthorization) (*models.Journal, error) {
	if err := s.requireDatabase(); err != nil {
		return nil, err
	}
	var preparedOverrideID *string
	if s.accounting != nil {
		current, currentErr := s.repo.GetByID(ctx, id, businessID)
		if currentErr != nil {
			return nil, currentErr
		}
		if current.Status != models.JournalStatusDraft {
			return nil, fmt.Errorf("journal already posted")
		}
		preparedOverrideID, currentErr = s.accounting.PrepareLockOverride(ctx, businessID, current.PostingDate, authorization)
		if currentErr != nil {
			return nil, currentErr
		}
	}
	var journal models.Journal
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := loadJournalForUpdateTx(tx, businessID, id, &journal); err != nil {
			return err
		}
		if journal.Status != models.JournalStatusDraft {
			return fmt.Errorf("journal already posted")
		}
		now := time.Now()
		journal.Status = models.JournalStatusPosted
		journal.PostedAt = &now
		journal.LockOverrideID = preparedOverrideID
		if err := tx.Model(&models.Journal{}).
			Where("id = ? AND business_id = ? AND status = ?", id, businessID, models.JournalStatusDraft).
			Updates(map[string]interface{}{"status": journal.Status, "posted_at": journal.PostedAt, "lock_override_id": journal.LockOverrideID}).Error; err != nil {
			return err
		}
		return projectJournalLedgerTx(tx, &journal)
	}); err != nil {
		return nil, err
	}
	return &journal, nil
}

func (s *JournalService) ReverseByBusiness(ctx context.Context, businessID, id string) (*models.Journal, error) {
	if err := s.requireDatabase(); err != nil {
		return nil, err
	}
	location, err := businessCalendarLocation(ctx, s.businesses, businessID)
	if err != nil {
		return nil, err
	}
	now := s.now().UTC()
	localNow := now.In(location)
	requestedDate := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, time.UTC)
	var reversal *models.Journal
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var journal models.Journal
		if err := loadJournalForUpdateTx(tx, businessID, id, &journal); err != nil {
			return err
		}
		if journal.Status != models.JournalStatusPosted {
			return fmt.Errorf("only posted journals can be reversed")
		}
		if journal.SourceType == "payment" || journal.SourceType == "payment_reversal" {
			return fmt.Errorf("payment journals must be reversed through the payment workflow")
		}
		if journal.SourceType == "bank_adjustment" {
			return fmt.Errorf("bank adjustment journals must be reversed through the bank reconciliation workflow")
		}
		postingDate, dateErr := reversalPostingDateTx(tx, businessID, journal.PostingDate, requestedDate)
		if dateErr != nil {
			return dateErr
		}
		reversal = &models.Journal{
			BusinessID:   businessID,
			Name:         "Reversal: " + journal.Name,
			Reference:    journal.Reference,
			ProjectID:    journal.ProjectID,
			BranchID:     journal.BranchID,
			Status:       models.JournalStatusPosted,
			PostingDate:  postingDate,
			Notes:        "Auto reversal",
			ReversalOfID: &journal.ID,
			PostedAt:     &now,
		}
		for _, line := range journal.Lines {
			entryType := "debit"
			if line.EntryType == "debit" {
				entryType = "credit"
			}
			reversal.Lines = append(reversal.Lines, &models.JournalLine{
				AccountCode: line.AccountCode,
				AccountName: line.AccountName,
				EntryType:   entryType,
				Amount:      line.Amount,
				Currency:    line.Currency,
				Description: "Reversal of " + journal.Name,
				DocumentID:  line.DocumentID,
				Metadata:    line.Metadata,
			})
		}
		if err := createJournalTx(tx, reversal); err != nil {
			return err
		}
		if err := projectJournalLedgerTx(tx, reversal); err != nil {
			return err
		}
		journal.Status = models.JournalStatusReversed
		journal.ReversedAt = &now
		return tx.Model(&models.Journal{}).
			Where("id = ? AND business_id = ? AND status = ?", id, businessID, models.JournalStatusPosted).
			Updates(map[string]interface{}{"status": journal.Status, "reversed_at": journal.ReversedAt}).Error
	}); err != nil {
		return nil, err
	}
	return reversal, nil
}

func (s *JournalService) CreateAutoJournalForDocument(ctx context.Context, document *models.Document) (*models.Journal, error) {
	if err := s.requireDatabase(); err != nil {
		return nil, err
	}
	var journal *models.Journal
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var err error
		journal, err = s.CreateAutoJournalForDocumentTx(ctx, tx, document)
		return err
	}); err != nil {
		return nil, err
	}
	return journal, nil
}

func (s *JournalService) CreateAutoJournalForDocumentTx(ctx context.Context, tx *gorm.DB, document *models.Document) (*models.Journal, error) {
	return s.CreateAutoJournalForDocumentTxAuthorized(ctx, tx, document, nil)
}

func (s *JournalService) CreateAutoJournalForDocumentTxAuthorized(ctx context.Context, tx *gorm.DB, document *models.Document, overrideID *string) (*models.Journal, error) {
	if tx == nil {
		return nil, fmt.Errorf("journal transaction is required")
	}
	if document == nil || document.ID == "" || document.BusinessID == "" {
		return nil, fmt.Errorf("issued document is required")
	}
	lines := buildJournalLinesForDocument(document)
	if len(lines) == 0 {
		return nil, nil
	}
	journal, err := s.buildJournal(ctx, document.BusinessID, CreateJournalInput{
		Name:        fmt.Sprintf("%s %s", document.DocumentType, document.SerialNumber),
		Reference:   document.SerialNumber,
		ProjectID:   normalizeProjectID(derefString(document.ProjectID)),
		BranchID:    document.BranchID,
		PostingDate: document.IssueDate,
		Status:      models.JournalStatusPosted,
		Lines:       lines,
	})
	if err != nil {
		return nil, err
	}
	journal.SourceType = "document"
	journal.SourceID = &document.ID
	journal.LockOverrideID = overrideID
	postedAt := time.Now().UTC()
	journal.PostedAt = &postedAt
	if err := createJournalTx(tx.WithContext(ctx), journal); err != nil {
		return nil, err
	}
	if err := projectJournalLedgerTx(tx.WithContext(ctx), journal); err != nil {
		return nil, err
	}
	return journal, nil
}

func buildJournalLinesForDocument(document *models.Document) []CreateJournalLineInput {
	var lines []CreateJournalLineInput
	switch document.DocumentType {
	case models.DocumentTypeSalesInvoice, models.DocumentTypeBillOfSupply:
		lines = []CreateJournalLineInput{
			{AccountCode: "AR", AccountName: "Accounts Receivable", EntryType: "debit", Amount: document.Total, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "REV", AccountName: "Revenue", EntryType: "credit", Amount: document.Subtotal - document.DiscountTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "OUT_GST", AccountName: "Output GST", EntryType: "credit", Amount: document.TaxTotal + document.CessTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
		}
	case models.DocumentTypeCreditNote:
		lines = []CreateJournalLineInput{
			{AccountCode: "REV", AccountName: "Revenue", EntryType: "debit", Amount: document.Subtotal - document.DiscountTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "OUT_GST", AccountName: "Output GST", EntryType: "debit", Amount: document.TaxTotal + document.CessTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "AR", AccountName: "Accounts Receivable", EntryType: "credit", Amount: document.Total, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
		}
	case models.DocumentTypePurchaseInvoice, models.DocumentTypeExpense:
		lines = []CreateJournalLineInput{
			{AccountCode: "INV", AccountName: "Inventory / Expense", EntryType: "debit", Amount: document.Subtotal - document.DiscountTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "IN_GST", AccountName: "Input GST", EntryType: "debit", Amount: document.TaxTotal + document.CessTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "AP", AccountName: "Accounts Payable", EntryType: "credit", Amount: document.Total, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
		}
	case models.DocumentTypeDebitNote:
		lines = []CreateJournalLineInput{
			{AccountCode: "AP", AccountName: "Accounts Payable", EntryType: "debit", Amount: document.Total, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "INV", AccountName: "Inventory / Expense", EntryType: "credit", Amount: document.Subtotal - document.DiscountTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "IN_GST", AccountName: "Input GST", EntryType: "credit", Amount: document.TaxTotal + document.CessTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
		}
	default:
		return nil
	}
	result := make([]CreateJournalLineInput, 0, len(lines))
	for _, line := range lines {
		if line.Amount == 0 {
			continue
		}
		result = append(result, line)
	}
	return result
}

func (s *JournalService) requireDatabase() error {
	if s == nil || s.db == nil {
		return fmt.Errorf("journal database is not configured")
	}
	return nil
}

func journalMinorUnits(amount float64) (int64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount <= 0 {
		return 0, fmt.Errorf("journal line amount must be positive")
	}
	minor := int64(math.Floor((amount * 100) + 0.500000001))
	if minor <= 0 {
		return 0, fmt.Errorf("journal line amount must be positive after rounding")
	}
	return minor, nil
}

func createJournalTx(tx *gorm.DB, journal *models.Journal) error {
	if err := tx.Omit(clause.Associations).Create(journal).Error; err != nil {
		return err
	}
	for _, line := range journal.Lines {
		line.JournalID = journal.ID
	}
	if len(journal.Lines) == 0 {
		return nil
	}
	return tx.Create(&journal.Lines).Error
}

func replaceDraftJournalTx(tx *gorm.DB, journal *models.Journal) error {
	if err := tx.Model(&models.Journal{}).
		Where("id = ? AND business_id = ?", journal.ID, journal.BusinessID).
		Updates(map[string]interface{}{
			"name": journal.Name, "reference": journal.Reference, "project_id": journal.ProjectID,
			"branch_id": journal.BranchID, "lock_override_id": journal.LockOverrideID,
			"status": journal.Status, "posting_date": journal.PostingDate, "notes": journal.Notes,
			"posted_at": journal.PostedAt,
		}).Error; err != nil {
		return err
	}
	if err := tx.Where("journal_id = ?", journal.ID).Delete(&models.JournalLine{}).Error; err != nil {
		return err
	}
	for _, line := range journal.Lines {
		line.JournalID = journal.ID
	}
	if len(journal.Lines) == 0 {
		return nil
	}
	return tx.Create(&journal.Lines).Error
}

func loadJournalForUpdateTx(tx *gorm.DB, businessID, id string, journal *models.Journal) error {
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Preload("Lines").
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
		First(journal).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return fmt.Errorf("journal not found")
	}
	return err
}

func projectJournalLedgerTx(tx *gorm.DB, journal *models.Journal) error {
	if err := enforceJournalLockTx(tx, journal); err != nil {
		return err
	}
	if journal.BranchID != nil {
		var count int64
		if err := tx.Model(&models.Branch{}).Where("id=? AND business_id=? AND deleted_at IS NULL", *journal.BranchID, journal.BusinessID).Count(&count).Error; err != nil || count != 1 {
			return fmt.Errorf("branch does not belong to business")
		}
	}
	trackAccounts := tx.Migrator().HasTable(&models.AccountingAccount{})
	for _, line := range journal.Lines {
		if trackAccounts {
			var account models.AccountingAccount
			err := tx.Where("business_id=? AND code=?", journal.BusinessID, line.AccountCode).First(&account).Error
			if errors.Is(err, gorm.ErrRecordNotFound) {
				metadata := unmarshalJSONMap(line.Metadata)
				accountClass, _ := metadata["account_class"].(string)
				if accountClass == "" {
					accountClass = accountingAccountClass(line.AccountCode)
				}
				if !validAccountingAccountClass(accountClass) {
					return fmt.Errorf("account_class is required for new account %s", line.AccountCode)
				}
				var parentCode *string
				if value, _ := metadata["parent_code"].(string); strings.TrimSpace(value) != "" {
					value = strings.ToUpper(strings.TrimSpace(value))
					parentCode = &value
				}
				if err := validateAccountingParentTx(tx, journal.BusinessID, line.AccountCode, accountClass, parentCode); err != nil {
					return err
				}
				account = models.AccountingAccount{BusinessID: journal.BusinessID, Code: line.AccountCode, Name: line.AccountName, AccountClass: accountClass, ParentCode: parentCode}
				if err := tx.Create(&account).Error; err != nil {
					return err
				}
			} else if err != nil {
				return err
			} else if !validAccountingAccountClass(account.AccountClass) {
				return fmt.Errorf("account_class is required for account %s", line.AccountCode)
			}
		}
		entry := &models.LedgerEntry{
			BusinessID:    journal.BusinessID,
			TransactionID: journal.ID,
			EntryDate:     journal.PostingDate,
			EntryType:     line.EntryType,
			Category:      line.AccountCode,
			Description:   line.AccountName + " - " + line.Description,
			Amount:        line.Amount,
			Currency:      line.Currency,
			ProjectID:     journal.ProjectID,
			BranchID:      journal.BranchID,
		}
		if line.DocumentID != nil {
			entry.InvoiceID = line.DocumentID
		}
		if err := tx.Create(entry).Error; err != nil {
			return err
		}
	}
	return nil
}

func accountingAccountClass(code string) string {
	switch {
	case code == "CASH" || code == "BANK" || code == "AR" || code == "INV" || code == "IN_GST" || code == "TDS_RECEIVABLE" || code == "GST_TDS_RECEIVABLE" || strings.HasPrefix(code, "ASSET"):
		return "asset"
	case code == "AP" || code == "OUT_GST" || strings.HasPrefix(code, "LIAB"):
		return "liability"
	case code == "REV" || code == "REVENUE" || code == "INTEREST_INCOME" || strings.HasPrefix(code, "REV"):
		return "revenue"
	case code == "EQUITY" || code == "OPENING_EQUITY" || strings.HasPrefix(code, "EQUITY"):
		return "equity"
	case code == "BANK_FEE":
		return "expense"
	default:
		return ""
	}
}
