package services

import (
	"context"
	"fmt"
	"math"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type JournalService struct {
	repo       interfaces.JournalRepository
	ledgerRepo interfaces.LedgerRepository
	log        *logger.Logger
}

func NewJournalService(repo interfaces.JournalRepository, ledgerRepo interfaces.LedgerRepository, log *logger.Logger) *JournalService {
	return &JournalService{repo: repo, ledgerRepo: ledgerRepo, log: log}
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
	if err := s.repo.Create(ctx, journal); err != nil {
		return nil, err
	}
	if journal.Status == models.JournalStatusPosted {
		if err := s.projectLedger(ctx, journal); err != nil {
			return nil, err
		}
		now := time.Now()
		journal.PostedAt = &now
		if err := s.repo.Update(ctx, journal); err != nil {
			return nil, err
		}
	}
	return journal, nil
}

func (s *JournalService) buildJournal(ctx context.Context, businessID string, input CreateJournalInput) (*models.Journal, error) {
	var debitTotal, creditTotal float64
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
	for _, line := range input.Lines {
		if line.EntryType == "debit" {
			debitTotal += line.Amount
		} else {
			creditTotal += line.Amount
		}
		journal.Lines = append(journal.Lines, &models.JournalLine{
			AccountCode: line.AccountCode,
			AccountName: line.AccountName,
			EntryType:   line.EntryType,
			Amount:      line.Amount,
			Currency:    defaultCurrency(line.Currency),
			Description: line.Description,
			DocumentID:  line.DocumentID,
			Metadata:    mustMarshalMap(line.Metadata),
		})
	}
	if math.Abs(debitTotal-creditTotal) > 0.005 {
		return nil, fmt.Errorf("journal is not balanced")
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
	existing, err := s.repo.GetByID(ctx, id, businessID)
	if err != nil {
		return nil, err
	}
	if existing.Status != models.JournalStatusDraft {
		return nil, fmt.Errorf("only draft journals can be updated")
	}
	rebuilt, err := s.buildJournal(ctx, businessID, input)
	if err != nil {
		return nil, err
	}
	existing.Name = rebuilt.Name
	existing.Reference = rebuilt.Reference
	existing.ProjectID = rebuilt.ProjectID
	existing.Status = rebuilt.Status
	existing.PostingDate = rebuilt.PostingDate
	existing.Notes = rebuilt.Notes
	existing.Lines = rebuilt.Lines
	if err := s.repo.Update(ctx, existing); err != nil {
		return nil, err
	}
	if existing.Status == models.JournalStatusPosted {
		now := time.Now()
		existing.PostedAt = &now
		if err := s.projectLedger(ctx, existing); err != nil {
			return nil, err
		}
		if err := s.repo.Update(ctx, existing); err != nil {
			return nil, err
		}
	}
	return existing, nil
}

func (s *JournalService) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	journal, err := s.repo.GetByID(ctx, id, businessID)
	if err != nil {
		return err
	}
	if journal.Status != models.JournalStatusDraft {
		return fmt.Errorf("only draft journals can be deleted")
	}
	return s.repo.Delete(ctx, id)
}

func (s *JournalService) PostByBusiness(ctx context.Context, businessID, id string) (*models.Journal, error) {
	journal, err := s.repo.GetByID(ctx, id, businessID)
	if err != nil {
		return nil, err
	}
	if journal.Status != models.JournalStatusDraft {
		return nil, fmt.Errorf("journal already posted")
	}
	now := time.Now()
	journal.Status = models.JournalStatusPosted
	journal.PostedAt = &now
	if err := s.repo.Update(ctx, journal); err != nil {
		return nil, err
	}
	if err := s.projectLedger(ctx, journal); err != nil {
		return nil, err
	}
	return journal, nil
}

func (s *JournalService) ReverseByBusiness(ctx context.Context, businessID, id string) (*models.Journal, error) {
	journal, err := s.repo.GetByID(ctx, id, businessID)
	if err != nil {
		return nil, err
	}
	if journal.Status != models.JournalStatusPosted {
		return nil, fmt.Errorf("only posted journals can be reversed")
	}
	reversal := &models.Journal{
		BusinessID:   businessID,
		Name:         "Reversal: " + journal.Name,
		Reference:    journal.Reference,
		ProjectID:    journal.ProjectID,
		Status:       models.JournalStatusPosted,
		PostingDate:  time.Now(),
		Notes:        "Auto reversal",
		ReversalOfID: &journal.ID,
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
	now := time.Now()
	reversal.PostedAt = &now
	if err := s.repo.Create(ctx, reversal); err != nil {
		return nil, err
	}
	if err := s.projectLedger(ctx, reversal); err != nil {
		return nil, err
	}
	journal.Status = models.JournalStatusReversed
	journal.ReversedAt = &now
	if err := s.repo.Update(ctx, journal); err != nil {
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

func (s *JournalService) projectLedger(ctx context.Context, journal *models.Journal) error {
	for _, line := range journal.Lines {
		entryType := line.EntryType
		entry := &models.LedgerEntry{
			BusinessID:    journal.BusinessID,
			TransactionID: journal.ID,
			EntryDate:     journal.PostingDate,
			EntryType:     entryType,
			Category:      line.AccountCode,
			Description:   line.AccountName + " - " + line.Description,
			Amount:        line.Amount,
			Currency:      line.Currency,
			ProjectID:     journal.ProjectID,
		}
		if line.DocumentID != nil {
			entry.InvoiceID = line.DocumentID
		}
		if err := s.ledgerRepo.Create(ctx, entry); err != nil {
			return err
		}
	}
	return nil
}
