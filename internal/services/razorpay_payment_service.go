package services

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/razorpay"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

type RazorpayPaymentService struct {
	cfg      *config.Config
	db       *gorm.DB
	client   *razorpay.Client
	resolver ProviderConfigResolver
	guard    CapabilityGuard
	health   CapabilityOutcomeRecorder
	log      *logger.Logger
}

func (s *RazorpayPaymentService) WithCapabilityHealthRecorder(recorder CapabilityOutcomeRecorder) *RazorpayPaymentService {
	s.health = recorder
	return s
}

func (s *RazorpayPaymentService) WithCapabilityGuard(guard CapabilityGuard) *RazorpayPaymentService {
	s.guard = guard
	return s
}

func NewRazorpayPaymentService(cfg *config.Config, db *gorm.DB, log *logger.Logger, resolvers ...ProviderConfigResolver) *RazorpayPaymentService {
	if log == nil {
		log = logger.Global()
	}
	var client *razorpay.Client
	if cfg != nil {
		client = razorpay.NewClient(razorpay.Config{
			KeyID:         cfg.Razorpay.KeyID,
			KeySecret:     cfg.Razorpay.KeySecret,
			WebhookSecret: cfg.Razorpay.WebhookSecret,
			BaseURL:       cfg.Razorpay.BaseURL,
			Timeout:       time.Duration(cfg.Razorpay.Timeout) * time.Second,
		}, log.Named("razorpay"))
	}
	var resolver ProviderConfigResolver
	if len(resolvers) > 0 {
		resolver = resolvers[0]
	}
	return &RazorpayPaymentService{cfg: cfg, db: db, client: client, resolver: resolver, log: log.Named("razorpay_payments")}
}

func (s *RazorpayPaymentService) clientFor(ctx context.Context) (*razorpay.Client, error) {
	if s == nil || s.cfg == nil {
		return nil, fmt.Errorf("razorpay is not configured")
	}
	if s.resolver == nil {
		if s.client == nil || !s.client.Configured() {
			return nil, fmt.Errorf("razorpay is not configured")
		}
		return s.client, nil
	}
	resolved, err := s.resolver.ResolveProvider(ctx, s.cfg, config.SecretRazorpay)
	if err != nil {
		return nil, err
	}
	client := razorpay.NewClient(razorpay.Config{
		KeyID: resolved.Razorpay.KeyID, KeySecret: resolved.Razorpay.KeySecret,
		WebhookSecret: resolved.Razorpay.WebhookSecret, BaseURL: resolved.Razorpay.BaseURL,
		Timeout: time.Duration(resolved.Razorpay.Timeout) * time.Second,
	}, s.log)
	if !client.Configured() {
		return nil, fmt.Errorf("razorpay is not configured")
	}
	return client, nil
}

type RazorpayCreateOrderInput struct {
	TargetType     string `json:"target_type" binding:"required,oneof=plan store_order"`
	PlanID         string `json:"plan_id,omitempty"`
	StoreOrderID   string `json:"store_order_id,omitempty"`
	IdempotencyKey string `json:"idempotency_key" binding:"required"`
}

type RazorpayCreateOrderResponse struct {
	PaymentAttemptID string `json:"payment_attempt_id"`
	RazorpayKeyID    string `json:"razorpay_key_id"`
	RazorpayOrderID  string `json:"razorpay_order_id"`
	Amount           int64  `json:"amount"`
	Currency         string `json:"currency"`
}

type RazorpayVerifyPaymentInput struct {
	PaymentAttemptID  string `json:"payment_attempt_id" binding:"required"`
	RazorpayOrderID   string `json:"razorpay_order_id" binding:"required"`
	RazorpayPaymentID string `json:"razorpay_payment_id" binding:"required"`
	RazorpaySignature string `json:"razorpay_signature" binding:"required"`
}

type RazorpayVerifyPaymentResponse struct {
	Status string `json:"status"`
}

func (s *RazorpayPaymentService) CreateOrder(ctx context.Context, businessID, userID string, input RazorpayCreateOrderInput) (*RazorpayCreateOrderResponse, error) {
	if s == nil {
		return nil, fmt.Errorf("payment service is not configured")
	}
	capability := CapabilityRazorpay
	if strings.TrimSpace(input.TargetType) == "store_order" {
		capability = CapabilityStorefrontPayments
	}
	if err := requireCapability(ctx, s.guard, CapabilityRequest{
		BusinessID: businessID, UserID: userID, Platform: CapabilityPlatformWeb, Capability: capability,
	}); err != nil {
		return nil, err
	}
	if s.db == nil {
		return nil, fmt.Errorf("payment service is not configured")
	}
	client, err := s.clientFor(ctx)
	if err != nil {
		return nil, err
	}

	target, amountPaise, currency, err := s.resolvePaymentTarget(ctx, businessID, input)
	if err != nil {
		return nil, err
	}

	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" {
		return nil, fmt.Errorf("idempotency_key is required")
	}
	attempt, existed, err := s.claimPaymentAttempt(ctx, businessID, userID, idempotencyKey, target, amountPaise, currency)
	if err != nil {
		s.log.Warn("failed to create Razorpay payment order", "business_id", businessID, "user_id", userID, "target_type", input.TargetType, "error", err)
		return nil, err
	}
	if attempt.RazorpayOrderID == "" {
		var order *razorpay.Order
		if existed {
			order, err = s.recoverRazorpayOrder(ctx, client, attempt)
			if err == nil && order == nil {
				err = fmt.Errorf("payment attempt is still being initialized")
			}
		} else {
			order, err = s.createRazorpayOrder(ctx, client, attempt)
		}
		if s.health != nil {
			_ = s.health.RecordOutcome(businessID, capability, CapabilityProviderOutcome{Err: err})
		}
		if err != nil {
			s.log.Warn("failed to initialize Razorpay payment order", "payment_attempt_id", attempt.ID, "business_id", businessID, "error", err)
			return nil, err
		}
		attempt, err = s.attachRazorpayOrder(ctx, attempt, order)
		if err != nil {
			return nil, err
		}
	}

	s.log.Info("Razorpay payment order ready", "payment_attempt_id", attempt.ID, "business_id", businessID, "target_type", attempt.TargetType, "razorpay_order_id", attempt.RazorpayOrderID)
	return &RazorpayCreateOrderResponse{
		PaymentAttemptID: attempt.ID,
		RazorpayKeyID:    client.KeyID(),
		RazorpayOrderID:  attempt.RazorpayOrderID,
		Amount:           attempt.AmountPaise,
		Currency:         attempt.Currency,
	}, nil
}

func (s *RazorpayPaymentService) claimPaymentAttempt(
	ctx context.Context,
	businessID, userID, idempotencyKey string,
	target resolvedPaymentTarget,
	amountPaise int64,
	currency string,
) (*models.PaymentAttempt, bool, error) {
	candidate := models.PaymentAttempt{
		ID: uuid.NewString(), UserID: userID, BusinessID: businessID,
		TargetType: target.TargetType, TargetID: target.TargetID,
		AmountPaise: amountPaise, Currency: currency,
		Status: models.PaymentAttemptStatusCreated, IdempotencyKey: idempotencyKey,
	}
	var attempt models.PaymentAttempt
	existed := false
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		result := tx.Omit("RazorpayOrderID", "RazorpayPaymentID", "FailureReason", "PaidAt").Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "user_id"}, {Name: "business_id"}, {Name: "idempotency_key"}},
			DoNothing: true,
		}).Create(&candidate)
		if result.Error != nil {
			return result.Error
		}
		if result.RowsAffected == 1 {
			attempt = candidate
			return nil
		}
		existed = true
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
			Where("user_id = ? AND business_id = ? AND idempotency_key = ?", userID, businessID, idempotencyKey).
			First(&attempt).Error; err != nil {
			return err
		}
		if attempt.Status == models.PaymentAttemptStatusPaid {
			return fmt.Errorf("payment attempt is already paid")
		}
		if attempt.TargetType != target.TargetType || attempt.TargetID != target.TargetID || attempt.AmountPaise != amountPaise || !strings.EqualFold(attempt.Currency, currency) {
			return fmt.Errorf("idempotency key was already used for a different payment target")
		}
		return nil
	})
	return &attempt, existed, err
}

func (s *RazorpayPaymentService) createRazorpayOrder(ctx context.Context, client *razorpay.Client, attempt *models.PaymentAttempt) (*razorpay.Order, error) {
	order, createErr := client.CreateOrder(ctx, razorpay.OrderParams{
		Amount: attempt.AmountPaise, Currency: attempt.Currency, Receipt: truncateReceipt(attempt.ID),
		Notes: map[string]string{
			"payment_attempt_id": attempt.ID,
			"business_id":        attempt.BusinessID,
			"target_type":        attempt.TargetType,
			"target_id":          attempt.TargetID,
		},
	})
	if createErr == nil {
		return validateRecoveredRazorpayOrder(attempt, order)
	}
	recovered, recoverErr := s.recoverRazorpayOrder(ctx, client, attempt)
	if recoverErr != nil {
		return nil, fmt.Errorf("create Razorpay order: %w; reconcile order: %v", createErr, recoverErr)
	}
	if recovered == nil {
		return nil, createErr
	}
	return recovered, nil
}

func (s *RazorpayPaymentService) recoverRazorpayOrder(ctx context.Context, client *razorpay.Client, attempt *models.PaymentAttempt) (*razorpay.Order, error) {
	receipt := truncateReceipt(attempt.ID)
	orders, err := client.FetchOrdersByReceipt(ctx, receipt)
	if err != nil {
		return nil, err
	}
	var recovered *razorpay.Order
	for index := range orders {
		if orders[index].Receipt != receipt {
			continue
		}
		if recovered != nil {
			return nil, fmt.Errorf("multiple Razorpay orders found for unique receipt")
		}
		recovered = &orders[index]
	}
	if recovered == nil {
		return nil, nil
	}
	return validateRecoveredRazorpayOrder(attempt, recovered)
}

func validateRecoveredRazorpayOrder(attempt *models.PaymentAttempt, order *razorpay.Order) (*razorpay.Order, error) {
	if order == nil || strings.TrimSpace(order.ID) == "" {
		return nil, fmt.Errorf("razorpay did not return an order id")
	}
	if order.Receipt != truncateReceipt(attempt.ID) || order.Amount != attempt.AmountPaise || !strings.EqualFold(order.Currency, attempt.Currency) {
		return nil, fmt.Errorf("Razorpay order does not match payment attempt")
	}
	return order, nil
}

func (s *RazorpayPaymentService) attachRazorpayOrder(ctx context.Context, claimed *models.PaymentAttempt, order *razorpay.Order) (*models.PaymentAttempt, error) {
	var attempt models.PaymentAttempt
	err := s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).Where("id = ?", claimed.ID).First(&attempt).Error; err != nil {
			return err
		}
		if attempt.BusinessID != claimed.BusinessID || attempt.UserID != claimed.UserID {
			return fmt.Errorf("payment attempt not found")
		}
		if attempt.RazorpayOrderID != "" {
			if attempt.RazorpayOrderID != order.ID {
				return fmt.Errorf("payment attempt already references a different Razorpay order")
			}
			return nil
		}
		if _, err := validateRecoveredRazorpayOrder(&attempt, order); err != nil {
			return err
		}
		attempt.RazorpayOrderID = order.ID
		attempt.Status = models.PaymentAttemptStatusCreated
		return tx.Save(&attempt).Error
	})
	return &attempt, err
}

type resolvedPaymentTarget struct {
	TargetType string
	TargetID   string
}

func (s *RazorpayPaymentService) resolvePaymentTarget(ctx context.Context, businessID string, input RazorpayCreateOrderInput) (resolvedPaymentTarget, int64, string, error) {
	switch strings.TrimSpace(input.TargetType) {
	case models.PaymentAttemptTargetPlan:
		planID := strings.TrimSpace(input.PlanID)
		plan, ok := paidSubscriptionPlan(planID)
		if !ok {
			return resolvedPaymentTarget{}, 0, "", fmt.Errorf("unsupported plan_id")
		}
		return resolvedPaymentTarget{TargetType: models.PaymentAttemptTargetPlan, TargetID: plan.ID}, plan.Amount, plan.Currency, nil
	case models.PaymentAttemptTargetStoreOrder:
		orderID := strings.TrimSpace(input.StoreOrderID)
		if orderID == "" {
			return resolvedPaymentTarget{}, 0, "", fmt.Errorf("store_order_id is required")
		}
		var order models.StoreOrder
		if err := s.db.WithContext(ctx).
			Where("id = ? AND business_id = ? AND deleted_at IS NULL", orderID, businessID).
			First(&order).Error; err != nil {
			if errors.Is(err, gorm.ErrRecordNotFound) {
				return resolvedPaymentTarget{}, 0, "", fmt.Errorf("store order not found")
			}
			return resolvedPaymentTarget{}, 0, "", err
		}
		if order.PaymentStatus == models.StoreOrderPaymentStatusPaid {
			return resolvedPaymentTarget{}, 0, "", fmt.Errorf("store order is already paid")
		}
		if !strings.EqualFold(order.PaymentMethod, "online") {
			return resolvedPaymentTarget{}, 0, "", fmt.Errorf("store order is not configured for online payment")
		}
		amountPaise := currencyToMinorUnits(order.Total)
		if amountPaise <= 0 {
			return resolvedPaymentTarget{}, 0, "", fmt.Errorf("store order amount must be positive")
		}
		return resolvedPaymentTarget{TargetType: models.PaymentAttemptTargetStoreOrder, TargetID: order.ID}, amountPaise, defaultCurrency(order.Currency), nil
	default:
		return resolvedPaymentTarget{}, 0, "", fmt.Errorf("unsupported target_type")
	}
}

func (s *RazorpayPaymentService) VerifyPayment(ctx context.Context, businessID, userID string, input RazorpayVerifyPaymentInput) (*RazorpayVerifyPaymentResponse, error) {
	if s == nil || s.db == nil {
		return nil, fmt.Errorf("payment service is not configured")
	}
	client, err := s.clientFor(ctx)
	if err != nil {
		return nil, err
	}

	var resultStatus = "pending"
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		attempt, err := s.loadAttemptForUpdate(ctx, tx, strings.TrimSpace(input.PaymentAttemptID))
		if err != nil {
			return err
		}
		if attempt.BusinessID != businessID || attempt.UserID != userID {
			return fmt.Errorf("payment attempt not found")
		}
		if attempt.Status == models.PaymentAttemptStatusPaid {
			resultStatus = "verified"
			return nil
		}
		if attempt.RazorpayOrderID == "" {
			return fmt.Errorf("payment attempt is missing Razorpay order id")
		}
		if strings.TrimSpace(input.RazorpayOrderID) != "" && strings.TrimSpace(input.RazorpayOrderID) != attempt.RazorpayOrderID {
			return fmt.Errorf("payment order mismatch")
		}
		if !razorpay.VerifyRazorpayPaymentSignature(attempt.RazorpayOrderID, input.RazorpayPaymentID, input.RazorpaySignature, client.KeySecret()) {
			s.log.Warn("Razorpay signature verification failed", "payment_attempt_id", attempt.ID, "business_id", businessID)
			return fmt.Errorf("invalid payment signature")
		}

		trusted, err := s.fetchTrustedPaymentStatus(ctx, client, attempt, strings.TrimSpace(input.RazorpayPaymentID))
		if err != nil {
			return err
		}
		if !trusted {
			attempt.RazorpayPaymentID = strings.TrimSpace(input.RazorpayPaymentID)
			attempt.Status = models.PaymentAttemptStatusPending
			if err := tx.Save(attempt).Error; err != nil {
				return err
			}
			resultStatus = "pending"
			return nil
		}

		if err := s.markAttemptPaidTx(ctx, tx, attempt, strings.TrimSpace(input.RazorpayPaymentID), "checkout.verify"); err != nil {
			return err
		}
		resultStatus = "verified"
		return nil
	})
	if err != nil {
		return nil, err
	}
	return &RazorpayVerifyPaymentResponse{Status: resultStatus}, nil
}

func (s *RazorpayPaymentService) fetchTrustedPaymentStatus(ctx context.Context, client *razorpay.Client, attempt *models.PaymentAttempt, paymentID string) (bool, error) {
	payment, err := client.FetchPayment(ctx, paymentID)
	if err != nil {
		return false, err
	}
	if payment.OrderID != "" && payment.OrderID != attempt.RazorpayOrderID {
		return false, fmt.Errorf("payment does not belong to stored order")
	}
	if payment.Amount != attempt.AmountPaise || !strings.EqualFold(payment.Currency, attempt.Currency) {
		return false, fmt.Errorf("payment amount or currency mismatch")
	}
	if strings.EqualFold(payment.Status, "captured") || payment.Captured {
		return true, nil
	}

	order, err := client.FetchOrder(ctx, attempt.RazorpayOrderID)
	if err != nil {
		return false, err
	}
	if order.Amount != attempt.AmountPaise || !strings.EqualFold(order.Currency, attempt.Currency) {
		return false, fmt.Errorf("order amount or currency mismatch")
	}
	return strings.EqualFold(order.Status, "paid"), nil
}

func (s *RazorpayPaymentService) HandleWebhook(ctx context.Context, signature, eventID string, rawBody []byte) (bool, error) {
	if s == nil || s.db == nil {
		return false, fmt.Errorf("payment service is not configured")
	}
	client, err := s.clientFor(ctx)
	if err != nil || strings.TrimSpace(client.WebhookSecret()) == "" {
		return false, fmt.Errorf("razorpay webhook secret is not configured")
	}
	eventID = strings.TrimSpace(eventID)
	if eventID == "" {
		return false, fmt.Errorf("missing x-razorpay-event-id")
	}
	if !razorpay.VerifyRazorpayWebhook(rawBody, signature, client.WebhookSecret()) {
		return false, fmt.Errorf("invalid webhook signature")
	}

	event, err := razorpay.ParseWebhookEvent(rawBody)
	if err != nil {
		return false, err
	}

	duplicate := false
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var existing models.RazorpayWebhookEvent
		findErr := tx.Where("razorpay_event_id = ?", eventID).First(&existing).Error
		if findErr == nil {
			duplicate = true
			return nil
		}
		if findErr != nil && !errors.Is(findErr, gorm.ErrRecordNotFound) {
			return findErr
		}

		record := models.RazorpayWebhookEvent{ID: uuid.NewString(), RazorpayEventID: eventID, EventType: event.Event}
		if err := tx.Create(&record).Error; err != nil {
			return err
		}
		if err := s.applyWebhookEventTx(ctx, tx, event); err != nil {
			return err
		}
		now := time.Now().UTC()
		record.ProcessedAt = &now
		return tx.Save(&record).Error
	})
	if err != nil {
		return false, err
	}
	if duplicate {
		s.log.Info("duplicate Razorpay webhook ignored", "razorpay_event_id", eventID, "event_type", event.Event)
		return true, nil
	}
	s.log.Info("Razorpay webhook processed", "razorpay_event_id", eventID, "event_type", event.Event)
	return false, nil
}

func (s *RazorpayPaymentService) applyWebhookEventTx(ctx context.Context, tx *gorm.DB, event *razorpay.WebhookEvent) error {
	switch event.Event {
	case "payment.captured":
		if event.Payload.Payment == nil {
			return fmt.Errorf("payment.captured payload missing payment")
		}
		payment := event.Payload.Payment.Entity
		attempt, err := s.loadAttemptByOrderForUpdate(ctx, tx, payment.OrderID)
		if err != nil {
			return err
		}
		if attempt == nil {
			return nil
		}
		if payment.Amount != attempt.AmountPaise || !strings.EqualFold(payment.Currency, attempt.Currency) {
			return fmt.Errorf("webhook payment amount or currency mismatch")
		}
		if !strings.EqualFold(payment.Status, "captured") && !payment.Captured {
			return nil
		}
		return s.markAttemptPaidTx(ctx, tx, attempt, payment.ID, event.Event)
	case "payment.failed":
		if event.Payload.Payment == nil {
			return fmt.Errorf("payment.failed payload missing payment")
		}
		payment := event.Payload.Payment.Entity
		attempt, err := s.loadAttemptByOrderForUpdate(ctx, tx, payment.OrderID)
		if err != nil {
			return err
		}
		if attempt == nil {
			return nil
		}
		if attempt.Status == models.PaymentAttemptStatusPaid {
			return nil
		}
		attempt.Status = models.PaymentAttemptStatusFailed
		attempt.RazorpayPaymentID = payment.ID
		attempt.FailureReason = strings.TrimSpace(payment.ErrorDescription)
		return tx.Save(attempt).Error
	case "order.paid":
		if event.Payload.Order == nil {
			return fmt.Errorf("order.paid payload missing order")
		}
		order := event.Payload.Order.Entity
		attempt, err := s.loadAttemptByOrderForUpdate(ctx, tx, order.ID)
		if err != nil {
			return err
		}
		if attempt == nil {
			return nil
		}
		if order.Amount != attempt.AmountPaise || !strings.EqualFold(order.Currency, attempt.Currency) {
			return fmt.Errorf("webhook order amount or currency mismatch")
		}
		if !strings.EqualFold(order.Status, "paid") {
			return nil
		}
		return s.markAttemptPaidTx(ctx, tx, attempt, attempt.RazorpayPaymentID, event.Event)
	default:
		return nil
	}
}

func (s *RazorpayPaymentService) loadAttemptForUpdate(ctx context.Context, tx *gorm.DB, id string) (*models.PaymentAttempt, error) {
	var attempt models.PaymentAttempt
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", id).First(&attempt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("payment attempt not found")
		}
		return nil, err
	}
	return &attempt, nil
}

func (s *RazorpayPaymentService) loadAttemptByOrderForUpdate(ctx context.Context, tx *gorm.DB, orderID string) (*models.PaymentAttempt, error) {
	if strings.TrimSpace(orderID) == "" {
		return nil, fmt.Errorf("razorpay order id is required")
	}
	var attempt models.PaymentAttempt
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("razorpay_order_id = ?", orderID).First(&attempt).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	return &attempt, nil
}

func (s *RazorpayPaymentService) markAttemptPaidTx(ctx context.Context, tx *gorm.DB, attempt *models.PaymentAttempt, paymentID, source string) error {
	if attempt.Status == models.PaymentAttemptStatusPaid {
		return nil
	}

	now := time.Now().UTC()
	attempt.Status = models.PaymentAttemptStatusPaid
	attempt.RazorpayPaymentID = firstNonEmpty(strings.TrimSpace(paymentID), attempt.RazorpayPaymentID)
	attempt.PaidAt = &now
	attempt.FailureReason = ""
	if err := tx.WithContext(ctx).Save(attempt).Error; err != nil {
		return err
	}

	switch attempt.TargetType {
	case models.PaymentAttemptTargetPlan:
		if err := s.applyPlanPaymentTx(ctx, tx, attempt, now); err != nil {
			return err
		}
	case models.PaymentAttemptTargetStoreOrder:
		if err := s.applyStoreOrderPaymentTx(ctx, tx, attempt, now, source); err != nil {
			return err
		}
	default:
		return fmt.Errorf("unsupported payment attempt target type")
	}

	s.log.Info("payment verified", "payment_attempt_id", attempt.ID, "business_id", attempt.BusinessID, "target_type", attempt.TargetType)
	return nil
}

func (s *RazorpayPaymentService) applyPlanPaymentTx(ctx context.Context, tx *gorm.DB, attempt *models.PaymentAttempt, paidAt time.Time) error {
	plan, ok := paidSubscriptionPlan(attempt.TargetID)
	if !ok {
		return fmt.Errorf("unknown paid plan")
	}
	nextBilling := paidAt.AddDate(0, 1, 0)
	var subscription models.Subscription
	err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("business_id = ? AND deleted_at IS NULL", attempt.BusinessID).
		First(&subscription).Error
	switch {
	case err == nil:
		subscription.Plan = plan.LegacyPlan
		subscription.PlanCode = plan.PlanCode
		subscription.CatalogVersion = CurrentSubscriptionCatalogVersion
		subscription.Status = "active"
		subscription.MaxInvoices = plan.Quotas[QuotaInvoices]
		subscription.MaxCustomers = plan.Quotas[QuotaCustomers]
		subscription.MaxUsers = plan.Quotas[QuotaUsers]
		subscription.MaxStorageMB = plan.Quotas[QuotaStorageMB]
		subscription.StartDate = paidAt
		subscription.EndDate = &nextBilling
		subscription.NextBillingDate = &nextBilling
		return tx.WithContext(ctx).Save(&subscription).Error
	case errors.Is(err, gorm.ErrRecordNotFound):
		subscription = models.Subscription{
			ID:              uuid.NewString(),
			BusinessID:      attempt.BusinessID,
			Plan:            plan.LegacyPlan,
			PlanCode:        plan.PlanCode,
			CatalogVersion:  CurrentSubscriptionCatalogVersion,
			Status:          "active",
			MaxInvoices:     plan.Quotas[QuotaInvoices],
			MaxCustomers:    plan.Quotas[QuotaCustomers],
			MaxUsers:        plan.Quotas[QuotaUsers],
			MaxStorageMB:    plan.Quotas[QuotaStorageMB],
			StartDate:       paidAt,
			EndDate:         &nextBilling,
			NextBillingDate: &nextBilling,
		}
		return tx.WithContext(ctx).Create(&subscription).Error
	default:
		return err
	}
}

func (s *RazorpayPaymentService) applyStoreOrderPaymentTx(ctx context.Context, tx *gorm.DB, attempt *models.PaymentAttempt, paidAt time.Time, source string) error {
	var order models.StoreOrder
	if err := tx.WithContext(ctx).Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ? AND business_id = ? AND deleted_at IS NULL", attempt.TargetID, attempt.BusinessID).
		First(&order).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("store order not found")
		}
		return err
	}
	if order.PaymentStatus == models.StoreOrderPaymentStatusPaid {
		return nil
	}
	order.PaymentStatus = models.StoreOrderPaymentStatusPaid
	order.Status = models.StoreOrderStatusPaid
	order.GatewayOrderID = attempt.RazorpayOrderID
	order.GatewayPaymentID = attempt.RazorpayPaymentID
	order.WebhookReference = source
	order.PaidAt = &paidAt
	if err := tx.WithContext(ctx).Save(&order).Error; err != nil {
		return err
	}
	return tx.WithContext(ctx).Create(&models.StoreOrderEvent{
		ID:           uuid.NewString(),
		StoreOrderID: order.ID,
		EventType:    "store_order.paid",
		Status:       order.Status,
		Payload: mustMarshalMap(map[string]interface{}{
			"payment_attempt_id":  attempt.ID,
			"gateway_order_id":    attempt.RazorpayOrderID,
			"gateway_payment_id":  attempt.RazorpayPaymentID,
			"verification_source": source,
		}),
	}).Error
}

func truncateReceipt(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= 40 {
		return value
	}
	return value[:40]
}
