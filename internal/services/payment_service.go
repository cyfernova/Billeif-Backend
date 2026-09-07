package services

import (
	"context"
	"errors"
	"fmt"
	"math"
	"strings"
	"time"

	"invoice-backend/internal/idempotency"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const paymentCreateCommand = "payment.create.v1"

type PaymentService struct {
	db          *gorm.DB
	repo        interfaces.PaymentRepository
	invoiceRepo interfaces.InvoiceRepository
	documents   *DocumentService
	journals    *JournalService
	log         *logger.Logger
}

func NewPaymentService(db *gorm.DB, repo interfaces.PaymentRepository, invoiceRepo interfaces.InvoiceRepository, documents *DocumentService, journals *JournalService, log *logger.Logger) *PaymentService {
	return &PaymentService{db: db, repo: repo, invoiceRepo: invoiceRepo, documents: documents, journals: journals, log: log}
}

type CreatePaymentInput struct {
	InvoiceID      string               `json:"invoice_id" binding:"required,uuid"`
	IdempotencyKey string               `json:"-"`
	ProjectID      string               `json:"project_id,omitempty" binding:"omitempty,uuid"`
	Amount         float64              `json:"amount" binding:"required,gt=0"`
	PaymentType    string               `json:"payment_type"`
	PaymentMethod  string               `json:"payment_method" binding:"required"`
	PaymentDate    string               `json:"payment_date"`
	Reference      string               `json:"reference"`
	Notes          string               `json:"notes"`
	Withholding    *WithholdingInput    `json:"withholding,omitempty"`
	Authorization  PostingAuthorization `json:"-"`
}

func (s *PaymentService) Create(ctx context.Context, businessID string, input CreatePaymentInput) (*models.Payment, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("payment database is not configured")
	}
	if s.journals == nil {
		return nil, fmt.Errorf("payment journal service is not configured")
	}
	input.IdempotencyKey = strings.TrimSpace(input.IdempotencyKey)
	key, err := uuid.Parse(input.IdempotencyKey)
	if err != nil {
		return nil, &idempotency.InvalidKeyError{}
	}
	input.IdempotencyKey = key.String()
	paymentDate := time.Now()
	if input.PaymentDate != "" {
		parsed, parseErr := time.Parse(time.RFC3339, input.PaymentDate)
		if parseErr != nil {
			return nil, fmt.Errorf("invalid payment_date")
		}
		paymentDate = parsed
	}
	paymentMinor, err := paymentMinorUnits(input.Amount, false)
	if err != nil {
		return nil, err
	}
	input.Amount = float64(paymentMinor) / 100
	if input.Withholding != nil {
		withholdingMinor, amountErr := paymentMinorUnits(input.Withholding.Amount, true)
		if amountErr != nil {
			return nil, fmt.Errorf("invalid withholding amount: %w", amountErr)
		}
		input.Withholding.Amount = float64(withholdingMinor) / 100
		if input.Withholding.Amount > 0 && strings.TrimSpace(input.Withholding.SectionCode) == "" {
			return nil, fmt.Errorf("withholding section code is required")
		}
		if input.Withholding.TaxableAmount > 0 {
			taxableMinor, taxableErr := paymentMinorUnits(input.Withholding.TaxableAmount, true)
			if taxableErr != nil {
				return nil, fmt.Errorf("invalid withholding taxable amount: %w", taxableErr)
			}
			input.Withholding.TaxableAmount = float64(taxableMinor) / 100
		}
	}
	requestHash, err := idempotency.CanonicalHash(input)
	if err != nil {
		return nil, err
	}
	if replayID, replayed, replayErr := lookupCompletedAPICommand(ctx, s.db, businessID, paymentCreateCommand, input.IdempotencyKey, requestHash, "payment"); replayErr != nil {
		return nil, replayErr
	} else if replayed {
		return s.repo.GetByID(ctx, replayID, businessID)
	}
	var preparedOverrideID *string
	if s.journals != nil && s.journals.accounting != nil {
		preparedOverrideID, err = s.journals.accounting.PrepareLockOverride(ctx, businessID, paymentDate, input.Authorization)
		if err != nil {
			return nil, err
		}
	}

	var (
		invoice         models.Invoice
		payment         *models.Payment
		replayPaymentID string
	)
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		replayedID, claimErr := claimPaymentCreateTx(tx, businessID, input.IdempotencyKey, requestHash)
		if claimErr != nil {
			return claimErr
		}
		if replayedID != "" {
			replayPaymentID = replayedID
			return nil
		}

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", input.InvoiceID, businessID).
			First(&invoice).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("invoice not found")
			}
			return err
		}
		if !paymentAllowedForInvoiceStatus(invoice.Status) {
			return fmt.Errorf("invoice is not payable in status %s", invoice.Status)
		}
		balanceMinor, balanceErr := paymentMinorUnits(invoice.BalanceDue, true)
		if balanceErr != nil {
			return fmt.Errorf("invalid invoice balance: %w", balanceErr)
		}
		paidMinor, paidErr := paymentMinorUnits(invoice.PaidAmount, true)
		if paidErr != nil {
			return fmt.Errorf("invalid invoice paid amount: %w", paidErr)
		}
		totalMinor, totalErr := paymentMinorUnits(invoice.Total, true)
		if totalErr != nil {
			return fmt.Errorf("invalid invoice total: %w", totalErr)
		}
		if paidMinor+balanceMinor != totalMinor {
			return fmt.Errorf("invoice payment totals are inconsistent")
		}
		withholdingMinor := int64(0)
		if input.Withholding != nil {
			withholdingMinor, _ = paymentMinorUnits(input.Withholding.Amount, true)
		}
		settlementMinor := paymentMinor + withholdingMinor
		if settlementMinor > balanceMinor {
			return fmt.Errorf("payment settlement exceeds invoice balance")
		}

		projectID := normalizeProjectID(input.ProjectID)
		if projectID == "" && invoice.ProjectID != nil {
			projectID = *invoice.ProjectID
		}
		if projectID == "" {
			projectID = extractProjectIDFromTags(nestedMap(unmarshalJSONMap(invoice.TaxProfile), "report_tags"))
		}
		payment = &models.Payment{
			InvoiceID:       input.InvoiceID,
			BusinessID:      invoice.BusinessID,
			ProjectID:       projectIDPointer(projectID),
			Amount:          input.Amount,
			Currency:        invoice.Currency,
			PaymentDate:     paymentDate,
			PaymentType:     coalesceString(input.PaymentType, "normal"),
			Status:          models.PaymentStatusPosted,
			PaymentMethod:   input.PaymentMethod,
			Reference:       input.Reference,
			Notes:           input.Notes,
			WithholdingData: mustMarshalMap(withholdingToMap(input.Withholding)),
		}
		if err := tx.Create(payment).Error; err != nil {
			return fmt.Errorf("failed to create payment: %w", err)
		}

		if err := s.syncPaymentWithholdingTx(ctx, tx, payment, input.Withholding); err != nil {
			return err
		}

		invoice.BalanceDue = float64(balanceMinor-settlementMinor) / 100
		invoice.PaidAmount = float64(paidMinor+settlementMinor) / 100
		if invoice.BalanceDue == 0 {
			invoice.Status = models.InvoiceStatusPaid
			now := time.Now().UTC()
			invoice.PaidAt = &now
		} else {
			invoice.Status = models.InvoiceStatusPartiallyPaid
			invoice.PaidAt = nil
		}
		if err := tx.Model(&models.Invoice{}).
			Where("id = ? AND business_id = ?", invoice.ID, businessID).
			Updates(map[string]interface{}{
				"paid_amount": invoice.PaidAmount,
				"balance_due": invoice.BalanceDue,
				"status":      invoice.Status,
				"paid_at":     invoice.PaidAt,
			}).Error; err != nil {
			return fmt.Errorf("failed to update invoice: %w", err)
		}
		journal, journalErr := s.buildPaymentJournal(ctx, payment, &invoice)
		if journalErr != nil {
			return journalErr
		}
		journal.LockOverrideID = preparedOverrideID
		if err := createJournalTx(tx, journal); err != nil {
			return err
		}
		if err := projectJournalLedgerTx(tx, journal); err != nil {
			return err
		}
		return completePaymentCreateClaimTx(tx, businessID, input.IdempotencyKey, requestHash, payment.ID)
	})

	if err != nil {
		s.log.Error("payment transaction failed", "invoice_id", input.InvoiceID, "error", err)
		return nil, err
	}
	if replayPaymentID != "" {
		return s.repo.GetByID(ctx, replayPaymentID, businessID)
	}
	if s.documents != nil {
		if err := s.documents.SyncLegacyInvoicePayment(ctx, invoice.ID, invoice.PaidAmount, invoice.BalanceDue, invoice.Status); err != nil {
			s.log.Error("failed to sync mirrored document payment state", "invoice_id", invoice.ID, "payment_id", payment.ID, "error", err)
		}
	}

	return payment, nil
}

func (s *PaymentService) CreateByBusiness(ctx context.Context, businessID string, input CreatePaymentInput) (*models.Payment, error) {
	return s.Create(ctx, businessID, input)
}

func (s *PaymentService) GetByBusiness(ctx context.Context, businessID, id string) (*models.Payment, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

func (s *PaymentService) ListByInvoice(ctx context.Context, invoiceID string, page, limit int) ([]*models.Payment, int64, error) {
	return s.repo.GetByInvoiceID(ctx, invoiceID, page, limit)
}

func (s *PaymentService) ListByBusiness(ctx context.Context, businessID string, page, limit int) ([]*models.Payment, int64, error) {
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
}

func (s *PaymentService) ListByInvoiceAndBusiness(ctx context.Context, businessID, invoiceID string, page, limit int) ([]*models.Payment, int64, error) {
	_, err := s.invoiceRepo.GetByID(ctx, invoiceID, businessID)
	if err != nil {
		return nil, 0, fmt.Errorf("invoice not found: %w", err)
	}
	return s.repo.GetByInvoiceID(ctx, invoiceID, page, limit)
}

type UpdatePaymentInput struct {
	ProjectID     *string           `json:"project_id,omitempty"`
	Amount        float64           `json:"amount"`
	PaymentType   string            `json:"payment_type"`
	PaymentMethod string            `json:"payment_method"`
	Reference     string            `json:"reference"`
	Notes         string            `json:"notes"`
	Withholding   *WithholdingInput `json:"withholding,omitempty"`
}

type ReversePaymentInput struct {
	Reason string `json:"reason" binding:"required"`
}

func (s *PaymentService) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdatePaymentInput) (*models.Payment, error) {
	payment, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return nil, err
	}
	_ = input
	return payment, fmt.Errorf("posted payments are immutable; reverse the payment instead")
}

func (s *PaymentService) Delete(ctx context.Context, id string) error {
	_ = ctx
	_ = id
	return fmt.Errorf("posted payments are immutable; reverse the payment instead")
}

func (s *PaymentService) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	_, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
	}
	return fmt.Errorf("posted payments are immutable; reverse the payment instead")
}

func (s *PaymentService) ReverseByBusiness(ctx context.Context, businessID, id string, input ReversePaymentInput) (*models.Payment, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("payment database is not configured")
	}
	reason := strings.TrimSpace(input.Reason)
	if reason == "" {
		return nil, fmt.Errorf("payment reversal reason is required")
	}

	var (
		payment models.Payment
		invoice models.Invoice
	)
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", id, businessID).
			First(&payment).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("payment not found")
			}
			return err
		}
		if payment.Status == models.PaymentStatusReversed {
			return fmt.Errorf("payment is already reversed")
		}
		if payment.Status != models.PaymentStatusPosted {
			return fmt.Errorf("payment cannot be reversed in status %s", payment.Status)
		}

		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", payment.InvoiceID, businessID).
			First(&invoice).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("invoice not found")
			}
			return err
		}

		var original models.Journal
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Preload("Lines").
			Where("business_id = ? AND source_type = ? AND source_id = ? AND deleted_at IS NULL", businessID, "payment", payment.ID).
			First(&original).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return fmt.Errorf("payment cannot be reversed without its posted journal")
			}
			return err
		}
		if original.Status != models.JournalStatusPosted {
			return fmt.Errorf("payment journal is not posted")
		}
		reversalDate, err := reversalPostingDateTx(tx, businessID, original.PostingDate, time.Now().UTC())
		if err != nil {
			return err
		}

		paymentMinor, err := paymentMinorUnits(payment.Amount, false)
		if err != nil {
			return err
		}
		withholdingMinor := int64(0)
		if withholding := mapToWithholdingInput(unmarshalJSONMap(payment.WithholdingData)); withholding != nil {
			withholdingMinor, err = paymentMinorUnits(withholding.Amount, true)
			if err != nil {
				return fmt.Errorf("invalid payment withholding: %w", err)
			}
		}
		settlementMinor := paymentMinor + withholdingMinor
		paidMinor, err := paymentMinorUnits(invoice.PaidAmount, true)
		if err != nil {
			return fmt.Errorf("invalid invoice paid amount: %w", err)
		}
		balanceMinor, err := paymentMinorUnits(invoice.BalanceDue, true)
		if err != nil {
			return fmt.Errorf("invalid invoice balance: %w", err)
		}
		totalMinor, err := paymentMinorUnits(invoice.Total, true)
		if err != nil {
			return fmt.Errorf("invalid invoice total: %w", err)
		}
		if paidMinor+balanceMinor != totalMinor {
			return fmt.Errorf("invoice payment totals are inconsistent")
		}
		if settlementMinor > paidMinor || balanceMinor+settlementMinor > totalMinor {
			return fmt.Errorf("payment reversal would violate invoice totals")
		}

		paidMinor -= settlementMinor
		balanceMinor += settlementMinor
		invoice.PaidAmount = float64(paidMinor) / 100
		invoice.BalanceDue = float64(balanceMinor) / 100
		invoice.PaidAt = nil
		switch {
		case balanceMinor == 0:
			invoice.Status = models.InvoiceStatusPaid
			now := time.Now().UTC()
			invoice.PaidAt = &now
		case paidMinor == 0 && !invoice.DueDate.IsZero() && invoice.DueDate.Before(time.Now()):
			invoice.Status = models.InvoiceStatusOverdue
		case paidMinor == 0:
			invoice.Status = models.InvoiceStatusSent
		default:
			invoice.Status = models.InvoiceStatusPartiallyPaid
		}
		invoiceResult := tx.Model(&models.Invoice{}).
			Where("id = ? AND business_id = ?", invoice.ID, businessID).
			Updates(map[string]interface{}{
				"paid_amount": invoice.PaidAmount,
				"balance_due": invoice.BalanceDue,
				"status":      invoice.Status,
				"paid_at":     invoice.PaidAt,
			})
		if invoiceResult.Error != nil {
			return fmt.Errorf("failed to restore invoice payment totals: %w", invoiceResult.Error)
		}
		if invoiceResult.RowsAffected != 1 {
			return fmt.Errorf("invoice payment totals were not restored")
		}

		now := time.Now().UTC()
		reversal := &models.Journal{
			BusinessID:   businessID,
			Name:         "Reversal: " + original.Name,
			Reference:    original.Reference,
			ProjectID:    original.ProjectID,
			BranchID:     original.BranchID,
			Status:       models.JournalStatusPosted,
			PostingDate:  reversalDate,
			Notes:        reason,
			SourceType:   "payment_reversal",
			SourceID:     &payment.ID,
			ReversalOfID: &original.ID,
			PostedAt:     &now,
		}
		for _, line := range original.Lines {
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
				Description: "Reversal of " + original.Name,
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
		journalResult := tx.Model(&models.Journal{}).
			Where("id = ? AND business_id = ? AND status = ?", original.ID, businessID, models.JournalStatusPosted).
			Updates(map[string]interface{}{"status": models.JournalStatusReversed, "reversed_at": now})
		if journalResult.Error != nil {
			return journalResult.Error
		}
		if journalResult.RowsAffected != 1 {
			return fmt.Errorf("payment journal was not reversed")
		}

		payment.Status = models.PaymentStatusReversed
		payment.ReversalReason = reason
		payment.ReversedAt = &now
		paymentResult := tx.Model(&models.Payment{}).
			Where("id = ? AND business_id = ? AND status = ?", payment.ID, businessID, models.PaymentStatusPosted).
			Updates(map[string]interface{}{
				"status":          payment.Status,
				"reversal_reason": payment.ReversalReason,
				"reversed_at":     payment.ReversedAt,
			})
		if paymentResult.Error != nil {
			return paymentResult.Error
		}
		if paymentResult.RowsAffected != 1 {
			return fmt.Errorf("payment was not reversed")
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if s.documents != nil {
		if err := s.documents.SyncLegacyInvoicePayment(ctx, invoice.ID, invoice.PaidAmount, invoice.BalanceDue, invoice.Status); err != nil {
			s.log.Error("failed to sync mirrored document payment reversal", "invoice_id", invoice.ID, "payment_id", payment.ID, "error", err)
		}
	}
	return &payment, nil
}

func (s *PaymentService) syncPaymentWithholdingTx(ctx context.Context, tx *gorm.DB, payment *models.Payment, withholding *WithholdingInput) error {
	if payment == nil {
		return nil
	}
	if err := tx.WithContext(ctx).Where("payment_id = ?", payment.ID).Delete(&models.PaymentWithholding{}).Error; err != nil {
		return err
	}
	if withholding == nil || withholding.SectionCode == "" || withholding.Amount == 0 {
		return nil
	}
	record := &models.PaymentWithholding{
		BusinessID:      payment.BusinessID,
		PaymentID:       payment.ID,
		InvoiceID:       &payment.InvoiceID,
		SectionCode:     withholding.SectionCode,
		WithholdingType: coalesceString(withholding.WithholdingType, models.WithholdingTypeTDS),
		Rate:            withholding.Rate,
		TaxableAmount:   withholding.TaxableAmount,
		Amount:          withholding.Amount,
		Metadata:        mustMarshalMap(withholding.Metadata),
	}
	return tx.WithContext(ctx).Create(record).Error
}

func settlementAmount(amount float64, withholding *WithholdingInput) float64 {
	if withholding == nil {
		return amount
	}
	return amount + withholding.Amount
}

func paymentMinorUnits(amount float64, allowZero bool) (int64, error) {
	if math.IsNaN(amount) || math.IsInf(amount, 0) || amount < 0 || (!allowZero && amount == 0) {
		return 0, fmt.Errorf("payment amount must be positive")
	}
	return int64(math.Floor((amount * 100) + 0.500000001)), nil
}

func paymentAllowedForInvoiceStatus(status string) bool {
	switch status {
	case models.InvoiceStatusIssued, models.InvoiceStatusSent, models.InvoiceStatusPartiallyPaid, models.InvoiceStatusOverdue:
		return true
	default:
		return false
	}
}

func claimPaymentCreateTx(tx *gorm.DB, businessID, key, requestHash string) (string, error) {
	claim := &models.APIIdempotencyKey{
		BusinessID: businessID, Command: paymentCreateCommand, IdempotencyKey: key,
		RequestHash: requestHash, Status: models.IdempotencyStatusInProgress,
	}
	result := tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "business_id"}, {Name: "command"}, {Name: "idempotency_key"}},
		DoNothing: true,
	}).Create(claim)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 1 {
		return "", nil
	}
	var existing models.APIIdempotencyKey
	if err := tx.Where("business_id = ? AND command = ? AND idempotency_key = ?", businessID, paymentCreateCommand, key).
		First(&existing).Error; err != nil {
		return "", err
	}
	if existing.RequestHash != requestHash {
		return "", &idempotency.ConflictError{}
	}
	if existing.Status != models.IdempotencyStatusCompleted || existing.ResultType == nil ||
		*existing.ResultType != "payment" || existing.ResultID == nil {
		return "", &idempotency.InProgressError{}
	}
	return *existing.ResultID, nil
}

func lookupCompletedAPICommand(ctx context.Context, db *gorm.DB, businessID, command, key, requestHash, resultType string) (string, bool, error) {
	if db == nil {
		return "", false, nil
	}
	var existing models.APIIdempotencyKey
	err := db.WithContext(ctx).Where("business_id = ? AND command = ? AND idempotency_key = ?", businessID, command, key).First(&existing).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	if existing.RequestHash != requestHash {
		return "", false, &idempotency.ConflictError{}
	}
	if existing.Status != models.IdempotencyStatusCompleted || existing.ResultType == nil || *existing.ResultType != resultType || existing.ResultID == nil {
		return "", false, &idempotency.InProgressError{}
	}
	return *existing.ResultID, true, nil
}

func claimAPICommandTx(tx *gorm.DB, businessID, command, key, requestHash, resultType string) (string, error) {
	claim := &models.APIIdempotencyKey{BusinessID: businessID, Command: command, IdempotencyKey: key, RequestHash: requestHash, Status: models.IdempotencyStatusInProgress}
	result := tx.Clauses(clause.OnConflict{Columns: []clause.Column{{Name: "business_id"}, {Name: "command"}, {Name: "idempotency_key"}}, DoNothing: true}).Create(claim)
	if result.Error != nil {
		return "", result.Error
	}
	if result.RowsAffected == 1 {
		return "", nil
	}
	var existing models.APIIdempotencyKey
	if err := tx.Where("business_id=? AND command=? AND idempotency_key=?", businessID, command, key).First(&existing).Error; err != nil {
		return "", err
	}
	if existing.RequestHash != requestHash {
		return "", &idempotency.ConflictError{}
	}
	if existing.Status != models.IdempotencyStatusCompleted || existing.ResultType == nil || *existing.ResultType != resultType || existing.ResultID == nil {
		return "", &idempotency.InProgressError{}
	}
	return *existing.ResultID, nil
}

func completeAPICommandTx(tx *gorm.DB, businessID, command, key, requestHash, resultType, resultID string) error {
	now := time.Now().UTC()
	result := tx.Model(&models.APIIdempotencyKey{}).Where("business_id=? AND command=? AND idempotency_key=? AND request_hash=? AND status=?", businessID, command, key, requestHash, models.IdempotencyStatusInProgress).Updates(map[string]interface{}{"status": models.IdempotencyStatusCompleted, "result_type": resultType, "result_id": resultID, "completed_at": now, "updated_at": now})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("idempotency claim was not completed")
	}
	return nil
}

func completePaymentCreateClaimTx(tx *gorm.DB, businessID, key, requestHash, paymentID string) error {
	now := time.Now().UTC()
	result := tx.Model(&models.APIIdempotencyKey{}).
		Where("business_id = ? AND command = ? AND idempotency_key = ? AND request_hash = ? AND status = ?",
			businessID, paymentCreateCommand, key, requestHash, models.IdempotencyStatusInProgress).
		Updates(map[string]interface{}{
			"status": models.IdempotencyStatusCompleted, "result_type": "payment", "result_id": paymentID,
			"completed_at": now, "updated_at": now,
		})
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("payment idempotency claim was not completed")
	}
	return nil
}

func withholdingToMap(input *WithholdingInput) map[string]interface{} {
	if input == nil {
		return map[string]interface{}{}
	}
	return map[string]interface{}{
		"section_code":     input.SectionCode,
		"withholding_type": input.WithholdingType,
		"rate":             input.Rate,
		"taxable_amount":   input.TaxableAmount,
		"amount":           input.Amount,
		"metadata":         input.Metadata,
	}
}

func mapToWithholdingInput(data map[string]interface{}) *WithholdingInput {
	if len(data) == 0 {
		return nil
	}
	input := &WithholdingInput{
		SectionCode:     readStringCandidate(data, "section_code"),
		WithholdingType: readStringCandidate(data, "withholding_type"),
		Rate:            readFloatCandidate(data, "rate"),
		TaxableAmount:   readFloatCandidate(data, "taxable_amount"),
		Amount:          readFloatCandidate(data, "amount"),
		Metadata:        nestedMap(data, "metadata"),
	}
	if input.SectionCode == "" && input.Amount == 0 {
		return nil
	}
	return input
}

// PaymentServiceTestable is a test-friendly version of PaymentService
type PaymentServiceTestable struct {
	repo        PaymentRepositoryTestable
	invoiceRepo PaymentInvoiceRepositoryTestable
	log         *logger.Logger
}

// PaymentInvoiceRepositoryTestable is the testable interface for InvoiceRepository
type PaymentInvoiceRepositoryTestable interface {
	Create(ctx context.Context, invoice *models.Invoice) error
	GetByID(ctx context.Context, id, businessID string) (*models.Invoice, error)
	GetByIDInternal(ctx context.Context, id string) (*models.Invoice, error)
	GetByInvoiceNo(ctx context.Context, businessID, invoiceNo string) (*models.Invoice, error)
	GetItems(ctx context.Context, invoiceID string) ([]*models.InvoiceItem, error)
	Update(ctx context.Context, invoice *models.Invoice) error
	UpdateStatus(ctx context.Context, invoiceID string, status string) error
	UpdatePDFURL(ctx context.Context, invoiceID, pdfURL string) error
	Delete(ctx context.Context, id string) error
}

// PaymentRepositoryTestable is the testable interface for PaymentRepository
type PaymentRepositoryTestable interface {
	Create(ctx context.Context, payment *models.Payment) error
	GetByID(ctx context.Context, id, businessID string) (*models.Payment, error)
	GetByBusinessID(ctx context.Context, businessID string, page, limit int) ([]*models.Payment, int64, error)
	GetByInvoiceID(ctx context.Context, invoiceID string, page, limit int) ([]*models.Payment, int64, error)
	Update(ctx context.Context, payment *models.Payment) error
	Delete(ctx context.Context, id string) error
}

// NewPaymentServiceForTesting creates a PaymentServiceTestable for unit testing
func NewPaymentServiceForTesting(
	repo PaymentRepositoryTestable,
	invoiceRepo PaymentInvoiceRepositoryTestable,
	log *logger.Logger,
) *PaymentServiceTestable {
	return &PaymentServiceTestable{
		repo:        repo,
		invoiceRepo: invoiceRepo,
		log:         log,
	}
}

// Create creates a payment (testable version - simplified without transaction)
func (s *PaymentServiceTestable) Create(ctx context.Context, businessID string, input CreatePaymentInput) (*models.Payment, error) {
	invoice, err := s.invoiceRepo.GetByID(ctx, input.InvoiceID, businessID)
	if err != nil {
		return nil, fmt.Errorf("invoice not found: %w", err)
	}

	paymentDate := time.Now()
	if input.PaymentDate != "" {
		if parsed, err := time.Parse(time.RFC3339, input.PaymentDate); err == nil {
			paymentDate = parsed
		}
	}

	payment := &models.Payment{
		InvoiceID:     input.InvoiceID,
		BusinessID:    invoice.BusinessID,
		ProjectID:     invoice.ProjectID,
		Amount:        input.Amount,
		Currency:      invoice.Currency,
		PaymentDate:   paymentDate,
		PaymentMethod: input.PaymentMethod,
		Reference:     input.Reference,
		Notes:         input.Notes,
	}

	if err := s.repo.Create(ctx, payment); err != nil {
		return nil, fmt.Errorf("failed to create payment: %w", err)
	}

	// Update invoice paid amount
	invoice.PaidAmount += input.Amount
	invoice.BalanceDue = invoice.Total - invoice.PaidAmount
	if invoice.PaidAmount >= invoice.Total {
		invoice.Status = "paid"
		now := time.Now()
		invoice.PaidAt = &now
	} else {
		invoice.Status = models.InvoiceStatusPartiallyPaid
	}

	if err := s.invoiceRepo.Update(ctx, invoice); err != nil {
		return nil, fmt.Errorf("failed to update invoice: %w", err)
	}

	return payment, nil
}

// GetByBusiness retrieves a payment by business ID and payment ID
func (s *PaymentServiceTestable) GetByBusiness(ctx context.Context, businessID, id string) (*models.Payment, error) {
	return s.repo.GetByID(ctx, id, businessID)
}

// ListByInvoice retrieves payments for an invoice with pagination
func (s *PaymentServiceTestable) ListByInvoice(ctx context.Context, invoiceID string, page, limit int) ([]*models.Payment, int64, error) {
	return s.repo.GetByInvoiceID(ctx, invoiceID, page, limit)
}

// ListByBusiness retrieves payments for a business with pagination.
func (s *PaymentServiceTestable) ListByBusiness(ctx context.Context, businessID string, page, limit int) ([]*models.Payment, int64, error) {
	return s.repo.GetByBusinessID(ctx, businessID, page, limit)
}

// ListByInvoiceAndBusiness retrieves payments for an invoice scoped to business
func (s *PaymentServiceTestable) ListByInvoiceAndBusiness(ctx context.Context, businessID, invoiceID string, page, limit int) ([]*models.Payment, int64, error) {
	_, err := s.invoiceRepo.GetByID(ctx, invoiceID, businessID)
	if err != nil {
		return nil, 0, fmt.Errorf("invoice not found: %w", err)
	}
	return s.repo.GetByInvoiceID(ctx, invoiceID, page, limit)
}

// UpdateByBusiness updates a payment (testable version)
func (s *PaymentServiceTestable) UpdateByBusiness(ctx context.Context, businessID, id string, input UpdatePaymentInput) (*models.Payment, error) {
	payment, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return nil, err
	}

	if input.Amount > 0 {
		payment.Amount = input.Amount
	}
	if input.PaymentMethod != "" {
		payment.PaymentMethod = input.PaymentMethod
	}
	if input.Reference != "" {
		payment.Reference = input.Reference
	}
	if input.Notes != "" {
		payment.Notes = input.Notes
	}
	if input.ProjectID != nil {
		payment.ProjectID = projectIDPointer(*input.ProjectID)
	}

	if err := s.repo.Update(ctx, payment); err != nil {
		return nil, err
	}
	return payment, nil
}

// DeleteByBusiness deletes a payment (testable version)
func (s *PaymentServiceTestable) DeleteByBusiness(ctx context.Context, businessID, id string) error {
	payment, err := s.GetByBusiness(ctx, businessID, id)
	if err != nil {
		return err
	}
	return s.repo.Delete(ctx, payment.ID)
}

func (s *PaymentService) buildPaymentJournal(ctx context.Context, payment *models.Payment, invoice *models.Invoice) (*models.Journal, error) {
	assetCode := "BANK"
	assetName := "Bank Account"
	if strings.Contains(strings.ToLower(payment.PaymentMethod), "cash") {
		assetCode = "CASH"
		assetName = "Cash"
	}

	documentID := invoice.ID
	branchID := invoice.BranchID
	if branchID == nil && s.db != nil && s.db.Migrator().HasTable(&models.Document{}) {
		var document models.Document
		if err := s.db.WithContext(ctx).Select("branch_id").Where("id=? AND business_id=? AND deleted_at IS NULL", invoice.ID, invoice.BusinessID).First(&document).Error; err == nil {
			branchID = document.BranchID
		} else if !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, err
		}
	}
	metadata := map[string]interface{}{
		"payment_id":     payment.ID,
		"payment_method": payment.PaymentMethod,
	}
	withholding := mapToWithholdingInput(unmarshalJSONMap(payment.WithholdingData))
	settlement := settlementAmount(payment.Amount, withholding)
	lines := []CreateJournalLineInput{
		{
			AccountCode: assetCode,
			AccountName: assetName,
			EntryType:   "debit",
			Amount:      payment.Amount,
			Currency:    payment.Currency,
			Description: fmt.Sprintf("Receipt for invoice %s", models.StringValue(invoice.InvoiceNo)),
			DocumentID:  &documentID,
			Metadata:    metadata,
		},
	}
	if withholding != nil && withholding.Amount > 0 {
		accountCode := "TDS_RECEIVABLE"
		accountName := "TDS Receivable"
		if withholding.WithholdingType == models.WithholdingTypeGSTTDS {
			accountCode = "GST_TDS_RECEIVABLE"
			accountName = "GST TDS Receivable"
		}
		lines = append(lines, CreateJournalLineInput{
			AccountCode: accountCode,
			AccountName: accountName,
			EntryType:   "debit",
			Amount:      withholding.Amount,
			Currency:    payment.Currency,
			Description: fmt.Sprintf("Withholding for invoice %s", models.StringValue(invoice.InvoiceNo)),
			DocumentID:  &documentID,
			Metadata:    metadata,
		})
	}
	lines = append(lines, CreateJournalLineInput{
		AccountCode: "AR",
		AccountName: "Accounts Receivable",
		EntryType:   "credit",
		Amount:      settlement,
		Currency:    payment.Currency,
		Description: fmt.Sprintf("Settlement for invoice %s", models.StringValue(invoice.InvoiceNo)),
		DocumentID:  &documentID,
		Metadata:    metadata,
	})

	journal, err := s.journals.buildJournal(ctx, invoice.BusinessID, CreateJournalInput{
		Name:        fmt.Sprintf("Payment %s", models.StringValue(invoice.InvoiceNo)),
		Reference:   coalesceString(payment.Reference, payment.ID),
		ProjectID:   normalizeProjectID(derefString(payment.ProjectID)),
		BranchID:    branchID,
		PostingDate: payment.PaymentDate,
		Status:      models.JournalStatusPosted,
		Notes:       payment.Notes,
		Lines:       lines,
	})
	if err != nil {
		return nil, err
	}
	journal.SourceType = "payment"
	journal.SourceID = &payment.ID
	now := time.Now().UTC()
	journal.PostedAt = &now
	return journal, nil
}
