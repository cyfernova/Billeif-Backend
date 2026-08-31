package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type JournalService struct {
	db   *gorm.DB
	repo interfaces.JournalRepository
	log  *logger.Logger
}

func NewJournalService(db *gorm.DB, repo interfaces.JournalRepository, log *logger.Logger) *JournalService {
	return &JournalService{db: db, repo: repo, log: log}
}

type CreateJournalLineInput struct {
	AccountCode string                 `json:"account_code" binding:"required"`
	AccountName string                 `json:"account_name" binding:"required"`
	EntryType   string                 `json:"entry_type" binding:"required,oneof=debit credit"`
	Amount      float64                `json:"amount" binding:"required,gt=0"`
	Currency    string                 `json:"currency"`
	Description string                 `json:"description"`
	DocumentID  *string                `json:"document_id,omitempty"`
	Metadata    map[string]interface{} `json:"metadata,omitempty"`
}

type CreateJournalInput struct {
	BusinessID  string                   `json:"business_id,omitempty"`
	Name        string                   `json:"name" binding:"required"`
	Reference   string                   `json:"reference"`
	ProjectID   string                   `json:"project_id,omitempty" binding:"omitempty,uuid"`
	PostingDate time.Time                `json:"posting_date"`
	Notes       string                   `json:"notes"`
	Status      string                   `json:"status"`
	Lines       []CreateJournalLineInput `json:"lines" binding:"required,min=2,dive"`
}

func (s *JournalService) CreateByBusiness(ctx context.Context, businessID string, input CreateJournalInput) (*models.Journal, error) {
	journal, err := s.buildJournal(ctx, businessID, input)
	if err != nil {
		return nil, err
	}
	if journal.Status == models.JournalStatusPosted {
		now := time.Now()
		journal.PostedAt = &now
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
	_ = ctx
	totals := make(map[string]struct{ debit, credit int64 })
	journal := &models.Journal{
		BusinessID:  businessID,
		Name:        input.Name,
		Reference:   input.Reference,
		ProjectID:   projectIDPointer(input.ProjectID),
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
		journal.Lines = append(journal.Lines, &models.JournalLine{
			AccountCode: line.AccountCode,
			AccountName: line.AccountName,
			EntryType:   line.EntryType,
			Amount:      float64(minor) / 100,
			Currency:    currency,
			Description: line.Description,
			DocumentID:  line.DocumentID,
			Metadata:    mustMarshalMap(line.Metadata),
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
	rebuilt, err := s.buildJournal(ctx, businessID, input)
	if err != nil {
		return nil, err
	}
	if err := s.requireDatabase(); err != nil {
		return nil, err
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
		}
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
	if err := s.requireDatabase(); err != nil {
		return nil, err
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
		if err := tx.Model(&models.Journal{}).
			Where("id = ? AND business_id = ? AND status = ?", id, businessID, models.JournalStatusDraft).
			Updates(map[string]interface{}{"status": journal.Status, "posted_at": journal.PostedAt}).Error; err != nil {
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
	var reversal *models.Journal
	if err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var journal models.Journal
		if err := loadJournalForUpdateTx(tx, businessID, id, &journal); err != nil {
			return err
		}
		if journal.Status != models.JournalStatusPosted {
			return fmt.Errorf("only posted journals can be reversed")
		}
		now := time.Now()
		reversal = &models.Journal{
			BusinessID:   businessID,
			Name:         "Reversal: " + journal.Name,
			Reference:    journal.Reference,
			ProjectID:    journal.ProjectID,
			Status:       models.JournalStatusPosted,
			PostingDate:  now,
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
	lines := buildJournalLinesForDocument(document)
	if len(lines) == 0 {
		return nil, nil
	}
	return s.CreateByBusiness(ctx, document.BusinessID, CreateJournalInput{
		Name:        fmt.Sprintf("%s %s", document.DocumentType, document.SerialNumber),
		Reference:   document.SerialNumber,
		ProjectID:   normalizeProjectID(derefString(document.ProjectID)),
		PostingDate: document.IssueDate,
		Status:      models.JournalStatusPosted,
		Lines:       lines,
	})
}

func buildJournalLinesForDocument(document *models.Document) []CreateJournalLineInput {
	switch document.DocumentType {
	case models.DocumentTypeSalesInvoice, models.DocumentTypeBillOfSupply:
		return []CreateJournalLineInput{
			{AccountCode: "AR", AccountName: "Accounts Receivable", EntryType: "debit", Amount: document.Total, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "REV", AccountName: "Revenue", EntryType: "credit", Amount: document.Subtotal - document.DiscountTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "OUT_GST", AccountName: "Output GST", EntryType: "credit", Amount: document.TaxTotal + document.CessTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
		}
	case models.DocumentTypeCreditNote:
		return []CreateJournalLineInput{
			{AccountCode: "REV", AccountName: "Revenue", EntryType: "debit", Amount: document.Subtotal - document.DiscountTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "OUT_GST", AccountName: "Output GST", EntryType: "debit", Amount: document.TaxTotal + document.CessTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "AR", AccountName: "Accounts Receivable", EntryType: "credit", Amount: document.Total, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
		}
	case models.DocumentTypePurchaseInvoice, models.DocumentTypeExpense:
		return []CreateJournalLineInput{
			{AccountCode: "INV", AccountName: "Inventory / Expense", EntryType: "debit", Amount: document.Subtotal - document.DiscountTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "IN_GST", AccountName: "Input GST", EntryType: "debit", Amount: document.TaxTotal + document.CessTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "AP", AccountName: "Accounts Payable", EntryType: "credit", Amount: document.Total, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
		}
	case models.DocumentTypeDebitNote:
		return []CreateJournalLineInput{
			{AccountCode: "AP", AccountName: "Accounts Payable", EntryType: "debit", Amount: document.Total, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "INV", AccountName: "Inventory / Expense", EntryType: "credit", Amount: document.Subtotal - document.DiscountTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
			{AccountCode: "IN_GST", AccountName: "Input GST", EntryType: "credit", Amount: document.TaxTotal + document.CessTotal, Currency: document.Currency, Description: document.SerialNumber, DocumentID: &document.ID},
		}
	default:
		return nil
	}
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
	return int64(math.Floor((amount * 100) + 0.500000001)), nil
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
	for _, line := range journal.Lines {
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
