package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/razorpay"

	"github.com/google/uuid"
)

const (
	defaultRazorpaySubscriptionCycles int64 = 1200
	defaultMaintenanceRunTimeout            = 45 * time.Second
	defaultProviderFetchTimeout             = 3 * time.Second
	subscriptionCommandStart                = "start_renewable"
	subscriptionCommandPlanChange           = "schedule_plan_change"
	subscriptionCommandCancellation         = "schedule_cancellation"
)

var (
	ErrSubscriptionIdempotencyConflict = errors.New("subscription idempotency key conflict")
	ErrSubscriptionProviderUnknown     = errors.New("subscription provider outcome requires reconciliation")
	ErrSubscriptionProviderRejected    = errors.New("subscription provider rejected the mutation")
	ErrSubscriptionLifecycleConflict   = errors.New("subscription lifecycle conflict")
	ErrSubscriptionUnavailable         = errors.New("subscription lifecycle is unavailable")
	ErrSubscriptionInternal            = errors.New("subscription lifecycle internal failure")
	ErrSubscriptionWebhookRejected     = errors.New("subscription webhook was rejected")
)

type SubscriptionProvider interface {
	CreateSubscription(context.Context, razorpay.SubscriptionCreateParams) (*razorpay.Subscription, error)
	FetchSubscription(context.Context, string) (*razorpay.Subscription, error)
	UpdateSubscription(context.Context, string, razorpay.SubscriptionUpdateParams) (*razorpay.Subscription, error)
	CancelSubscription(context.Context, string, razorpay.SubscriptionCancelParams) (*razorpay.Subscription, error)
}

type SubscriptionLifecycleConfig struct {
	ProviderMode          string
	ProviderPlanIDs       map[string]string
	WebhookSecret         string
	TotalCount            int64
	GracePeriod           time.Duration
	MaintenanceRunTimeout time.Duration
	ProviderFetchTimeout  time.Duration
	Now                   func() time.Time
	Resolve               func(context.Context) (SubscriptionProviderSettings, error)
}

type SubscriptionProviderSettings struct {
	ProviderMode    string
	ProviderPlanIDs map[string]string
	WebhookSecret   string
}

type SubscriptionWebhookResult struct {
	Status    string `json:"status"`
	Duplicate bool   `json:"duplicate"`
	Code      string `json:"code,omitempty"`
}

var ErrInvalidSubscriptionWebhookSignature = errors.New("invalid subscription webhook signature")

type SubscriptionLifecycleService struct {
	repository interfaces.SubscriptionLifecycleRepository
	provider   SubscriptionProvider
	config     SubscriptionLifecycleConfig
	log        *logger.Logger
	eventLocks [64]sync.Mutex
}

type StartRenewableSubscriptionInput struct {
	PlanID         string `json:"plan_id" binding:"required"`
	IdempotencyKey string `json:"idempotency_key" binding:"required"`
}

type SubscriptionCheckoutResponse struct {
	SubscriptionID   string `json:"subscription_id"`
	Status           string `json:"status"`
	BillingMode      string `json:"billing_mode"`
	PendingPlanID    string `json:"pending_plan_id"`
	AuthorizationURL string `json:"authorization_url,omitempty"`
}

type ChangeSubscriptionPlanInput struct {
	PlanID         string `json:"plan_id" binding:"required"`
	IdempotencyKey string `json:"idempotency_key" binding:"required"`
}

type ScheduleSubscriptionCancellationInput struct {
	IdempotencyKey string `json:"idempotency_key" binding:"required"`
}

type SubscriptionMutationResponse struct {
	SubscriptionID         string     `json:"subscription_id"`
	Status                 string     `json:"status"`
	PlanID                 string     `json:"plan_id"`
	PendingPlanID          string     `json:"pending_plan_id,omitempty"`
	EffectiveAt            *time.Time `json:"effective_at,omitempty"`
	CancelAtPeriodEnd      bool       `json:"cancel_at_period_end"`
	ReconciliationRequired bool       `json:"reconciliation_required"`
}

type SubscriptionBillingHistoryResponse struct {
	Records []models.SubscriptionBillingRecord `json:"records"`
}

type SubscriptionAuditHistoryResponse struct {
	Records []models.SubscriptionAuditRecord `json:"records"`
}

type SubscriptionMaintenanceResult struct {
	Reconciled int `json:"reconciled"`
	Suspended  int `json:"suspended"`
	Failed     int `json:"failed"`
}

func NewSubscriptionLifecycleService(
	repository interfaces.SubscriptionLifecycleRepository,
	provider SubscriptionProvider,
	config SubscriptionLifecycleConfig,
	log *logger.Logger,
) *SubscriptionLifecycleService {
	if config.TotalCount <= 0 {
		config.TotalCount = defaultRazorpaySubscriptionCycles
	}
	if config.GracePeriod <= 0 {
		config.GracePeriod = 7 * 24 * time.Hour
	}
	if config.MaintenanceRunTimeout <= 0 {
		config.MaintenanceRunTimeout = defaultMaintenanceRunTimeout
	}
	if config.ProviderFetchTimeout <= 0 {
		config.ProviderFetchTimeout = defaultProviderFetchTimeout
	}
	if config.Now == nil {
		config.Now = time.Now
	}
	if log == nil {
		log = logger.Global()
	}
	return &SubscriptionLifecycleService{repository: repository, provider: provider, config: config, log: log.Named("subscription_lifecycle")}
}

func (s *SubscriptionLifecycleService) BillingHistory(ctx context.Context, businessID string, limit int) (*SubscriptionBillingHistoryResponse, error) {
	if strings.TrimSpace(businessID) == "" || s.repository == nil {
		return nil, fmt.Errorf("subscription scope is invalid")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	records, err := s.repository.ListBillingRecords(ctx, businessID, limit)
	if err != nil {
		return nil, ErrSubscriptionInternal
	}
	return &SubscriptionBillingHistoryResponse{Records: records}, nil
}

func (s *SubscriptionLifecycleService) AuditHistory(ctx context.Context, businessID string, limit int) (*SubscriptionAuditHistoryResponse, error) {
	if strings.TrimSpace(businessID) == "" || s.repository == nil {
		return nil, fmt.Errorf("subscription scope is invalid")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	records, err := s.repository.ListAuditRecords(ctx, businessID, limit)
	if err != nil {
		return nil, ErrSubscriptionInternal
	}
	return &SubscriptionAuditHistoryResponse{Records: records}, nil
}

// RunMaintenance performs a bounded, provider-verified reconciliation pass and
// expires due grace periods. It never retries a payment or mutates provider state.
func (s *SubscriptionLifecycleService) RunMaintenance(ctx context.Context, limit int) (SubscriptionMaintenanceResult, error) {
	result := SubscriptionMaintenanceResult{}
	if s.repository == nil || s.provider == nil {
		return result, fmt.Errorf("subscription maintenance is not configured")
	}
	if limit <= 0 || limit > 100 {
		limit = 50
	}
	runCtx, cancelRun := context.WithTimeout(ctx, s.config.MaintenanceRunTimeout)
	defer cancelRun()
	settings, err := s.settings(runCtx)
	if err != nil {
		return result, fmt.Errorf("subscription maintenance configuration unavailable")
	}
	due, err := s.repository.ListReconciliationDue(runCtx, settings.ProviderMode, limit)
	if err != nil {
		return result, fmt.Errorf("subscription reconciliation query failed")
	}
	for i := range due {
		if runCtx.Err() != nil {
			result.Failed += len(due) - i
			break
		}
		fetchCtx, cancelFetch := context.WithTimeout(runCtx, s.config.ProviderFetchTimeout)
		providerState, fetchErr := s.provider.FetchSubscription(fetchCtx, due[i].ProviderSubscriptionID)
		cancelFetch()
		if fetchErr != nil || providerState == nil {
			result.Failed++
			continue
		}
		if err := s.reconcileProviderState(runCtx, due[i].BusinessID, providerState, settings); err != nil {
			result.Failed++
			continue
		}
		result.Reconciled++
	}
	remaining := limit - len(due)
	if remaining == 0 {
		if result.Failed > 0 {
			return result, fmt.Errorf("subscription maintenance incomplete")
		}
		return result, nil
	}
	if runCtx.Err() != nil {
		return result, fmt.Errorf("subscription maintenance incomplete")
	}
	graceDue, err := s.repository.ListGraceDue(runCtx, settings.ProviderMode, s.config.Now().UTC(), remaining)
	if err != nil {
		return result, fmt.Errorf("subscription grace query failed")
	}
	for i := range graceDue {
		if err := s.suspendExpiredGrace(runCtx, graceDue[i].BusinessID); err != nil {
			result.Failed++
			continue
		}
		result.Suspended++
	}
	if result.Failed > 0 {
		return result, fmt.Errorf("subscription maintenance incomplete")
	}
	return result, nil
}

func (s *SubscriptionLifecycleService) reconcileProviderState(
	ctx context.Context, businessID string, providerState *razorpay.Subscription, settings SubscriptionProviderSettings,
) error {
	now := s.config.Now().UTC()
	return s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
		current, err := tx.GetByBusinessIDForUpdate(ctx, businessID)
		if err != nil || current.Status != models.SubscriptionStatusReconciliationRequired {
			return firstError(err, ErrSubscriptionLifecycleConflict)
		}
		if code := s.validateProviderSubscriptionMetadata(current, providerState, settings); code != "" {
			return ErrSubscriptionProviderUnknown
		}
		fromStatus := current.Status
		switch {
		case strings.EqualFold(providerState.Status, "cancelled"):
			current.Status = models.SubscriptionStatusCancelled
			current.CancelAtPeriodEnd = false
			current.CancellationEffectiveAt = nil
			current.CancelledAt = &now
		case providerState.PaidCount > current.LastProviderPaidCount:
			plan, ok := planForProviderID(providerState.PlanID, settings.ProviderPlanIDs)
			if !ok || providerState.CurrentStart <= 0 || providerState.CurrentEnd <= providerState.CurrentStart {
				return ErrSubscriptionProviderUnknown
			}
			start := time.Unix(providerState.CurrentStart, 0).UTC()
			end := time.Unix(providerState.CurrentEnd, 0).UTC()
			if validatePaidPeriodAdvance(current, providerState, start) != "" {
				return ErrSubscriptionProviderUnknown
			}
			applyPlanToAggregate(current, plan)
			current.Status = models.SubscriptionStatusActive
			current.ProviderPlanID = providerState.PlanID
			current.ProviderCustomerID = providerState.CustomerID
			current.LastProviderPaidCount = providerState.PaidCount
			current.PeriodStart, current.PeriodEnd, current.NextRenewalAt = &start, &end, &end
			current.StartDate, current.EndDate, current.NextBillingDate = start, &end, &end
			current.GraceDeadline = nil
			if current.PendingProviderPlanID == providerState.PlanID {
				current.PendingPlanID, current.PendingProviderPlanID = "", ""
				current.PendingPlanEffectiveAt = nil
			}
			providerObservationID := fmt.Sprintf("reconciliation:%s:%d", providerState.ID, providerState.PaidCount)
			if err := tx.CreateBillingRecord(ctx, &models.SubscriptionBillingRecord{
				ID: uuid.NewString(), BusinessID: current.BusinessID, SubscriptionID: current.ID,
				ProviderMode: current.ProviderMode, ProviderEventID: providerObservationID,
				AmountMinor: plan.Amount, Currency: plan.Currency, Status: "provider_verified",
				ReceiptReference: billingReceiptReference(current.BusinessID, providerObservationID),
				PeriodStart:      &start, PeriodEnd: &end, QuotaPeriodStart: &start, QuotaPeriodEnd: &end, OccurredAt: now,
			}); err != nil {
				return err
			}
		case providerState.HasScheduledChanges && current.PendingPlanID != "":
			current.Status = models.SubscriptionStatusRenewalPending
		case strings.EqualFold(providerState.Status, "active") && current.CancelAtPeriodEnd:
			current.Status = models.SubscriptionStatusCancellationScheduled
		default:
			return ErrSubscriptionProviderUnknown
		}
		current.ReconciliationCode = ""
		if err := tx.SaveSubscription(ctx, current); err != nil {
			return err
		}
		return tx.CreateAudit(ctx, lifecycleAudit(current, "provider_reconciliation", fromStatus, current.Status, "provider_state_verified", "", now))
	})
}

func (s *SubscriptionLifecycleService) suspendExpiredGrace(ctx context.Context, businessID string) error {
	now := s.config.Now().UTC()
	return s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
		current, err := tx.GetByBusinessIDForUpdate(ctx, businessID)
		if err != nil {
			return err
		}
		if current.GraceDeadline == nil || current.GraceDeadline.After(now) ||
			(current.Status != models.SubscriptionStatusPastDue && current.Status != models.SubscriptionStatusGracePeriod) {
			return ErrSubscriptionLifecycleConflict
		}
		fromStatus := current.Status
		current.Status = models.SubscriptionStatusSuspended
		if err := tx.SaveSubscription(ctx, current); err != nil {
			return err
		}
		return tx.CreateAudit(ctx, lifecycleAudit(current, "grace_expired", fromStatus, current.Status, "grace_period_expired", "", now))
	})
}

func (s *SubscriptionLifecycleService) StartRenewable(
	ctx context.Context,
	businessID, actorUserID string,
	input StartRenewableSubscriptionInput,
) (*SubscriptionCheckoutResponse, error) {
	settings, err := s.settings(ctx)
	if err != nil {
		return nil, err
	}
	planID := strings.ToLower(strings.TrimSpace(input.PlanID))
	plan, ok := paidSubscriptionPlan(planID)
	if !ok {
		return nil, fmt.Errorf("unsupported subscription plan")
	}
	providerPlanID := strings.TrimSpace(settings.ProviderPlanIDs[plan.ID])
	if !validProviderMode(settings.ProviderMode) || providerPlanID == "" || s.repository == nil || s.provider == nil {
		return nil, ErrSubscriptionUnavailable
	}
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 180 || strings.TrimSpace(businessID) == "" || strings.TrimSpace(actorUserID) == "" {
		return nil, fmt.Errorf("subscription command scope is invalid")
	}
	now := s.config.Now().UTC()
	requestHash := subscriptionCommandHash(businessID, actorUserID, subscriptionCommandStart, idempotencyKey, plan.ID)
	var aggregate *models.Subscription
	var previous *models.Subscription
	var replay bool
	var replayResponse *SubscriptionCheckoutResponse
	err = s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
		command, commandErr := tx.GetCommandForUpdate(ctx, businessID, actorUserID, subscriptionCommandStart, idempotencyKey)
		if commandErr == nil {
			if command.RequestHash != requestHash {
				return ErrSubscriptionIdempotencyConflict
			}
			if command.Status == "rejected" {
				return ErrSubscriptionProviderRejected
			}
			if command.Status != "completed" {
				return ErrSubscriptionProviderUnknown
			}
			replayResponse = checkoutResponseFromCommand(command)
			if replayResponse.Status == "" || replayResponse.BillingMode == "" || replayResponse.PendingPlanID == "" {
				return ErrSubscriptionProviderUnknown
			}
			replay = true
			return nil
		}
		if !errors.Is(commandErr, interfaces.ErrSubscriptionCommandNotFound) {
			return commandErr
		}

		current, currentErr := tx.GetByBusinessIDForUpdate(ctx, businessID)
		switch {
		case currentErr == nil:
			if current.BillingMode == models.SubscriptionBillingModeRenewable &&
				current.Status != models.SubscriptionStatusCancelled && current.Status != models.SubscriptionStatusExpired {
				return ErrSubscriptionLifecycleConflict
			}
			if current.Status != models.SubscriptionStatusCancelled && current.Status != models.SubscriptionStatusExpired &&
				subscriptionHasCurrentPaidAccess(current, now) && normalizePlanCode(current.Plan, current.PlanCode) != "free" {
				return ErrSubscriptionLifecycleConflict
			}
			previous = cloneSubscription(current)
			aggregate = current
		case errors.Is(currentErr, interfaces.ErrSubscriptionLifecycleNotFound):
			free := subscriptionPlanForCode("free")
			aggregate = &models.Subscription{
				ID: uuid.NewString(), BusinessID: businessID, Plan: free.LegacyPlan, PlanCode: free.PlanCode,
				CatalogVersion: CurrentSubscriptionCatalogVersion, Status: models.SubscriptionStatusPendingPayment,
				BillingMode: models.SubscriptionBillingModeRenewable, MaxInvoices: free.Quotas[QuotaInvoices],
				MaxCustomers: free.Quotas[QuotaCustomers], MaxUsers: free.Quotas[QuotaUsers], MaxStorageMB: free.Quotas[QuotaStorageMB],
				StartDate: now, LifecycleVersion: 1,
			}
		default:
			return currentErr
		}
		fromStatus := aggregate.Status
		if currentErr == nil {
			resetSubscriptionForRenewableRestart(aggregate, now)
		}
		aggregate.Status = models.SubscriptionStatusPendingPayment
		aggregate.BillingMode = models.SubscriptionBillingModeRenewable
		aggregate.ProviderMode = settings.ProviderMode
		aggregate.ProviderCustomerID = ""
		aggregate.ProviderSubscriptionID = ""
		aggregate.ProviderPlanID = ""
		aggregate.PendingPlanID = plan.ID
		aggregate.PendingProviderPlanID = providerPlanID
		aggregate.ReconciliationCode = ""
		if currentErr == nil {
			if err := tx.SaveSubscription(ctx, aggregate); err != nil {
				return err
			}
		} else if err := tx.CreateSubscription(ctx, aggregate); err != nil {
			return err
		}
		command = &models.SubscriptionCommand{
			ID: uuid.NewString(), BusinessID: businessID, SubscriptionID: aggregate.ID,
			ActorUserID: actorUserID, Action: subscriptionCommandStart, IdempotencyKey: idempotencyKey,
			RequestHash: requestHash, Status: "initializing", CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.CreateCommand(ctx, command); err != nil {
			return err
		}
		return tx.CreateAudit(ctx, &models.SubscriptionAuditRecord{
			ID: uuid.NewString(), BusinessID: businessID, SubscriptionID: aggregate.ID, ActorUserID: actorUserID,
			Action: subscriptionCommandStart, FromStatus: fromStatus, ToStatus: aggregate.Status,
			ToPlanID: plan.ID, ProviderMode: settings.ProviderMode, SanitizedCode: "subscription_start_requested", OccurredAt: now,
		})
	})
	if err != nil {
		return nil, err
	}
	if replay {
		return replayResponse, nil
	}

	created, createErr := s.provider.CreateSubscription(ctx, razorpay.SubscriptionCreateParams{
		PlanID: providerPlanID, TotalCount: s.config.TotalCount, Quantity: 1, CustomerNotify: false,
		Notes: map[string]string{
			"business_id": businessID, "subscription_id": aggregate.ID, "plan_id": plan.ID, "provider_mode": settings.ProviderMode,
		},
	})
	if createErr != nil {
		if providerMutationRejected(createErr) {
			if rejectErr := s.rejectProviderMutation(ctx, aggregate, previous, actorUserID, subscriptionCommandStart, idempotencyKey, requestHash, "provider_create_rejected", now); rejectErr != nil {
				return nil, ErrSubscriptionInternal
			}
			return nil, ErrSubscriptionProviderRejected
		}
		_ = s.markUnknownProviderOutcome(ctx, aggregate.ID, businessID, actorUserID, idempotencyKey, now, "provider_create_unknown")
		return nil, ErrSubscriptionProviderUnknown
	}
	if created == nil || strings.TrimSpace(created.ID) == "" || created.PlanID != providerPlanID {
		_ = s.markUnknownProviderOutcome(ctx, aggregate.ID, businessID, actorUserID, idempotencyKey, now, "provider_create_unknown")
		return nil, ErrSubscriptionProviderUnknown
	}

	err = s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
		current, err := tx.GetByBusinessIDForUpdate(ctx, businessID)
		if err != nil || current.ID != aggregate.ID {
			return firstError(err, ErrSubscriptionLifecycleConflict)
		}
		command, err := tx.GetCommandForUpdate(ctx, businessID, actorUserID, subscriptionCommandStart, idempotencyKey)
		if err != nil || command.RequestHash != requestHash || command.Status != "initializing" {
			return firstError(err, ErrSubscriptionLifecycleConflict)
		}
		current.ProviderSubscriptionID = created.ID
		current.ProviderCustomerID = created.CustomerID
		current.ProviderPlanID = created.PlanID
		current.Status = models.SubscriptionStatusPendingPayment
		current.ReconciliationCode = ""
		if err := tx.SaveSubscription(ctx, current); err != nil {
			return err
		}
		completed := s.config.Now().UTC()
		command.Status = "completed"
		command.ResponseStatus = models.SubscriptionStatusPendingPayment
		command.ResponseBillingMode = models.SubscriptionBillingModeRenewable
		command.ResponsePendingPlanID = plan.ID
		command.ResponseAuthorizationURL = created.ShortURL
		command.CompletedAt = &completed
		if err := tx.SaveCommand(ctx, command); err != nil {
			return err
		}
		aggregate = current
		return tx.CreateAudit(ctx, &models.SubscriptionAuditRecord{
			ID: uuid.NewString(), BusinessID: businessID, SubscriptionID: current.ID, ActorUserID: actorUserID,
			Action: "provider_subscription_created", FromStatus: models.SubscriptionStatusPendingPayment,
			ToStatus: models.SubscriptionStatusPendingPayment, ToPlanID: plan.ID, ProviderMode: settings.ProviderMode,
			SanitizedCode: "provider_subscription_created", OccurredAt: completed,
		})
	})
	if err != nil {
		_ = s.markUnknownProviderOutcome(ctx, aggregate.ID, businessID, actorUserID, idempotencyKey, now, "local_persistence_unknown")
		return nil, ErrSubscriptionProviderUnknown
	}
	return checkoutResponse(aggregate, created.ShortURL), nil
}

func (s *SubscriptionLifecycleService) HandleWebhook(
	ctx context.Context,
	signature, providerEventID string,
	rawBody []byte,
) (*SubscriptionWebhookResult, error) {
	settings, err := s.settings(ctx)
	if err != nil {
		return nil, err
	}
	receivedAt := s.config.Now().UTC()
	payloadHash := sha256Hex(rawBody)
	providerEventID = strings.TrimSpace(providerEventID)
	if providerEventID == "" {
		return nil, fmt.Errorf("missing Razorpay event identity")
	}
	unlockEvent := s.lockEventIdentity(settings.ProviderMode, providerEventID)
	defer unlockEvent()
	if strings.TrimSpace(settings.WebhookSecret) == "" || !razorpay.VerifyRazorpayWebhook(rawBody, signature, settings.WebhookSecret) {
		unverifiedIdentity := "unverified:" + sha256Hex([]byte(providerEventID))
		_ = s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
			existing, err := tx.GetEventForUpdate(ctx, "unverified", unverifiedIdentity)
			if err == nil {
				existing.ReplayCount++
				existing.LastReplayedAt = &receivedAt
				return tx.SaveEvent(ctx, existing)
			}
			if !errors.Is(err, interfaces.ErrRazorpayEventNotFound) {
				return err
			}
			return tx.CreateEvent(ctx, &models.RazorpayWebhookEvent{
				ID: uuid.NewString(), RazorpayEventID: unverifiedIdentity, ProviderMode: "unverified",
				EventType: "unverified", PayloadHash: payloadHash, SignatureVerified: false,
				ReceivedAt: receivedAt, ProcessingStatus: "rejected", SanitizedErrorCode: "invalid_signature",
				CreatedAt: receivedAt,
			})
		})
		return nil, ErrInvalidSubscriptionWebhookSignature
	}
	event, err := razorpay.ParseWebhookEvent(rawBody)
	if err != nil {
		_ = s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
			existing, findErr := tx.GetEventForUpdate(ctx, settings.ProviderMode, providerEventID)
			if findErr == nil {
				existing.ReplayCount++
				existing.LastReplayedAt = &receivedAt
				return tx.SaveEvent(ctx, existing)
			}
			if !errors.Is(findErr, interfaces.ErrRazorpayEventNotFound) {
				return findErr
			}
			return tx.CreateEvent(ctx, &models.RazorpayWebhookEvent{
				ID: uuid.NewString(), RazorpayEventID: providerEventID, ProviderMode: settings.ProviderMode,
				EventType: "invalid", PayloadHash: payloadHash, SignatureVerified: true, ReceivedAt: receivedAt,
				ProcessingStatus: "rejected", SanitizedErrorCode: "invalid_payload", CreatedAt: receivedAt,
			})
		})
		return nil, fmt.Errorf("invalid subscription webhook")
	}
	providerOccurredAt := receivedAt
	if event.CreatedAtEpoch > 0 {
		providerOccurredAt = time.Unix(event.CreatedAtEpoch, 0).UTC()
	}
	result := &SubscriptionWebhookResult{Status: "processed"}
	var domainErr error
	err = s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
		existing, findErr := tx.GetEventForUpdate(ctx, settings.ProviderMode, providerEventID)
		if findErr == nil {
			return s.recordSubscriptionEventReplayTx(ctx, tx, existing, payloadHash, providerEventID, receivedAt, result, &domainErr)
		}
		if !errors.Is(findErr, interfaces.ErrRazorpayEventNotFound) {
			return findErr
		}
		occurred := providerOccurredAt
		inbox := &models.RazorpayWebhookEvent{
			ID: uuid.NewString(), RazorpayEventID: providerEventID, ProviderMode: settings.ProviderMode,
			EventType: event.Event, PayloadHash: payloadHash, SignatureVerified: true,
			ReceivedAt: receivedAt, ProviderOccurredAt: &occurred, ProcessingStatus: "processing",
			AttemptCount: 1, CreatedAt: receivedAt,
		}
		if err := tx.CreateEvent(ctx, inbox); err != nil {
			return err
		}
		if !strings.HasPrefix(event.Event, "subscription.") || event.Payload.Subscription == nil {
			inbox.ProcessingStatus = "rejected"
			inbox.SanitizedErrorCode = "unsupported_subscription_event"
			result.Status, result.Code = inbox.ProcessingStatus, inbox.SanitizedErrorCode
			domainErr = ErrSubscriptionWebhookRejected
			return tx.SaveEvent(ctx, inbox)
		}

		providerSubscription := event.Payload.Subscription.Entity
		aggregate, loadErr := tx.GetByProviderIDForUpdate(ctx, settings.ProviderMode, providerSubscription.ID)
		if loadErr != nil && errors.Is(loadErr, interfaces.ErrSubscriptionLifecycleNotFound) {
			businessID := strings.TrimSpace(providerSubscription.Notes["business_id"])
			if businessID != "" {
				aggregate, loadErr = tx.GetByBusinessIDForUpdate(ctx, businessID)
			}
		}
		if loadErr != nil {
			inbox.ProcessingStatus = "reconciliation_required"
			inbox.SanitizedErrorCode = "subscription_unresolved"
			result.Status, result.Code = inbox.ProcessingStatus, inbox.SanitizedErrorCode
			domainErr = ErrSubscriptionProviderUnknown
			return tx.SaveEvent(ctx, inbox)
		}
		inbox.BusinessID = aggregate.BusinessID
		inbox.SubscriptionID = aggregate.ID
		if code := s.validateProviderSubscriptionIdentity(aggregate, &providerSubscription, settings); code != "" {
			fromStatus := aggregate.Status
			aggregate.Status = models.SubscriptionStatusReconciliationRequired
			aggregate.ReconciliationCode = code
			if err := tx.SaveSubscription(ctx, aggregate); err != nil {
				return err
			}
			inbox.ProcessingStatus = "reconciliation_required"
			inbox.SanitizedErrorCode = code
			result.Status, result.Code = inbox.ProcessingStatus, inbox.SanitizedErrorCode
			domainErr = ErrSubscriptionProviderUnknown
			if err := tx.CreateAudit(ctx, lifecycleAudit(aggregate, "provider_event_rejected", fromStatus, aggregate.Status, code, providerEventID, providerOccurredAt)); err != nil {
				return err
			}
			return tx.SaveEvent(ctx, inbox)
		}
		if subscriptionEventIsStale(aggregate, event, providerOccurredAt) {
			inbox.ProcessingStatus = "processed"
			inbox.SanitizedErrorCode = "stale_event_ignored"
			result.Status, result.Code = inbox.ProcessingStatus, inbox.SanitizedErrorCode
			now := s.config.Now().UTC()
			inbox.ProcessedAt = &now
			return tx.SaveEvent(ctx, inbox)
		}
		if code := validateProviderSubscriptionPlan(aggregate, &providerSubscription); code != "" {
			fromStatus := aggregate.Status
			aggregate.Status = models.SubscriptionStatusReconciliationRequired
			aggregate.ReconciliationCode = code
			if err := tx.SaveSubscription(ctx, aggregate); err != nil {
				return err
			}
			inbox.ProcessingStatus = "reconciliation_required"
			inbox.SanitizedErrorCode = code
			result.Status, result.Code = inbox.ProcessingStatus, inbox.SanitizedErrorCode
			domainErr = ErrSubscriptionProviderUnknown
			if err := tx.CreateAudit(ctx, lifecycleAudit(aggregate, "provider_event_rejected", fromStatus, aggregate.Status, code, providerEventID, providerOccurredAt)); err != nil {
				return err
			}
			return tx.SaveEvent(ctx, inbox)
		}

		code, applyErr := s.applySubscriptionEventTx(ctx, tx, aggregate, event, providerEventID, providerOccurredAt, settings)
		if applyErr != nil {
			fromStatus := aggregate.Status
			aggregate.Status = models.SubscriptionStatusReconciliationRequired
			aggregate.ReconciliationCode = code
			if saveErr := tx.SaveSubscription(ctx, aggregate); saveErr != nil {
				return saveErr
			}
			inbox.ProcessingStatus = "reconciliation_required"
			inbox.SanitizedErrorCode = code
			result.Status, result.Code = inbox.ProcessingStatus, inbox.SanitizedErrorCode
			domainErr = ErrSubscriptionProviderUnknown
			if auditErr := tx.CreateAudit(ctx, lifecycleAudit(aggregate, "provider_event_rejected", fromStatus, aggregate.Status, code, providerEventID, providerOccurredAt)); auditErr != nil {
				return auditErr
			}
			return tx.SaveEvent(ctx, inbox)
		}
		now := s.config.Now().UTC()
		inbox.ProcessingStatus = "processed"
		inbox.SanitizedErrorCode = code
		inbox.ProcessedAt = &now
		result.Status, result.Code = inbox.ProcessingStatus, inbox.SanitizedErrorCode
		return tx.SaveEvent(ctx, inbox)
	})
	if errors.Is(err, interfaces.ErrRazorpayEventAlreadyExists) {
		err = s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
			existing, findErr := tx.GetEventForUpdate(ctx, settings.ProviderMode, providerEventID)
			if findErr != nil {
				return findErr
			}
			return s.recordSubscriptionEventReplayTx(ctx, tx, existing, payloadHash, providerEventID, receivedAt, result, &domainErr)
		})
	}
	if err != nil {
		return nil, err
	}
	return result, domainErr
}

func subscriptionEventIsStale(aggregate *models.Subscription, event *razorpay.WebhookEvent, occurredAt time.Time) bool {
	if aggregate == nil || event == nil || aggregate.LastProviderEventAt == nil {
		return false
	}
	last := aggregate.LastProviderEventAt.UTC()
	if occurredAt.Before(last) {
		return true
	}
	if occurredAt.After(last) {
		return false
	}
	switch event.Event {
	case "subscription.charged":
		return event.Payload.Subscription == nil || event.Payload.Subscription.Entity.PaidCount <= aggregate.LastProviderPaidCount
	case "subscription.cancelled", "subscription.completed", "subscription.expired":
		return false
	default:
		return true
	}
}

func (s *SubscriptionLifecycleService) lockEventIdentity(providerMode, providerEventID string) func() {
	digest := sha256.Sum256([]byte(providerMode + "\x00" + providerEventID))
	lock := &s.eventLocks[int(digest[0])%len(s.eventLocks)]
	lock.Lock()
	return lock.Unlock
}

func (s *SubscriptionLifecycleService) recordSubscriptionEventReplayTx(
	ctx context.Context,
	tx interfaces.SubscriptionLifecycleRepository,
	existing *models.RazorpayWebhookEvent,
	payloadHash, providerEventID string,
	receivedAt time.Time,
	result *SubscriptionWebhookResult,
	domainErr *error,
) error {
	result.Status = existing.ProcessingStatus
	result.Code = existing.SanitizedErrorCode
	result.Duplicate = true
	*domainErr = subscriptionWebhookDomainError(existing.ProcessingStatus)
	existing.ReplayCount++
	existing.LastReplayedAt = &receivedAt
	if existing.PayloadHash != payloadHash {
		existing.ProcessingStatus = "reconciliation_required"
		existing.SanitizedErrorCode = "replay_payload_mismatch"
		result.Status = existing.ProcessingStatus
		result.Code = existing.SanitizedErrorCode
		*domainErr = ErrSubscriptionProviderUnknown
		if existing.BusinessID != "" {
			current, loadErr := tx.GetByBusinessIDForUpdate(ctx, existing.BusinessID)
			if loadErr == nil {
				fromStatus := current.Status
				current.Status = models.SubscriptionStatusReconciliationRequired
				current.ReconciliationCode = existing.SanitizedErrorCode
				if saveErr := tx.SaveSubscription(ctx, current); saveErr != nil {
					return saveErr
				}
				if auditErr := tx.CreateAudit(ctx, lifecycleAudit(current, "provider_event_replay_mismatch", fromStatus, current.Status, existing.SanitizedErrorCode, providerEventID, receivedAt)); auditErr != nil {
					return auditErr
				}
			}
		}
	}
	return tx.SaveEvent(ctx, existing)
}

func subscriptionWebhookDomainError(processingStatus string) error {
	switch processingStatus {
	case "reconciliation_required":
		return ErrSubscriptionProviderUnknown
	case "rejected":
		return ErrSubscriptionWebhookRejected
	default:
		return nil
	}
}

func (s *SubscriptionLifecycleService) SchedulePlanChange(
	ctx context.Context,
	businessID, actorUserID string,
	input ChangeSubscriptionPlanInput,
) (*SubscriptionMutationResponse, error) {
	settings, err := s.settings(ctx)
	if err != nil {
		return nil, err
	}
	targetPlanID := strings.ToLower(strings.TrimSpace(input.PlanID))
	targetPlan, ok := paidSubscriptionPlan(targetPlanID)
	providerPlanID := strings.TrimSpace(settings.ProviderPlanIDs[targetPlanID])
	if !ok {
		return nil, fmt.Errorf("unsupported subscription plan")
	}
	if providerPlanID == "" || !validProviderMode(settings.ProviderMode) || s.provider == nil || s.repository == nil {
		return nil, ErrSubscriptionUnavailable
	}
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 180 || strings.TrimSpace(businessID) == "" || strings.TrimSpace(actorUserID) == "" {
		return nil, fmt.Errorf("subscription command scope is invalid")
	}
	now := s.config.Now().UTC()
	requestHash := subscriptionCommandHash(businessID, actorUserID, subscriptionCommandPlanChange, idempotencyKey, targetPlanID)
	var aggregate *models.Subscription
	var previous *models.Subscription
	var replay bool
	err = s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
		command, commandErr := tx.GetCommandForUpdate(ctx, businessID, actorUserID, subscriptionCommandPlanChange, idempotencyKey)
		if commandErr == nil {
			if command.RequestHash != requestHash {
				return ErrSubscriptionIdempotencyConflict
			}
			if command.Status == "rejected" {
				return ErrSubscriptionProviderRejected
			}
			if command.Status != "completed" {
				return ErrSubscriptionProviderUnknown
			}
			current, err := tx.GetByBusinessIDForUpdate(ctx, businessID)
			aggregate, replay = current, true
			return err
		}
		if !errors.Is(commandErr, interfaces.ErrSubscriptionCommandNotFound) {
			return commandErr
		}
		current, err := tx.GetByBusinessIDForUpdate(ctx, businessID)
		if err != nil {
			return err
		}
		if current.BillingMode != models.SubscriptionBillingModeRenewable || current.ProviderMode != settings.ProviderMode ||
			current.ProviderSubscriptionID == "" || current.PeriodEnd == nil || current.Status != models.SubscriptionStatusActive ||
			current.CancelAtPeriodEnd || current.PendingPlanID != "" || currentCatalogPlanID(current) == targetPlanID {
			return ErrSubscriptionLifecycleConflict
		}
		fromStatus := current.Status
		previous = cloneSubscription(current)
		current.PendingPlanID = targetPlan.ID
		current.PendingProviderPlanID = providerPlanID
		effectiveAt := current.PeriodEnd.UTC()
		current.PendingPlanEffectiveAt = &effectiveAt
		current.Status = models.SubscriptionStatusRenewalPending
		if err := tx.SaveSubscription(ctx, current); err != nil {
			return err
		}
		command = &models.SubscriptionCommand{
			ID: uuid.NewString(), BusinessID: businessID, SubscriptionID: current.ID,
			ActorUserID: actorUserID, Action: subscriptionCommandPlanChange, IdempotencyKey: idempotencyKey,
			RequestHash: requestHash, Status: "initializing", CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.CreateCommand(ctx, command); err != nil {
			return err
		}
		aggregate = current
		return tx.CreateAudit(ctx, &models.SubscriptionAuditRecord{
			ID: uuid.NewString(), BusinessID: businessID, SubscriptionID: current.ID, ActorUserID: actorUserID,
			Action: subscriptionCommandPlanChange, FromStatus: fromStatus, ToStatus: current.Status,
			FromPlanID: currentCatalogPlanID(current), ToPlanID: targetPlan.ID, ProviderMode: current.ProviderMode,
			SanitizedCode: "plan_change_scheduled", OccurredAt: now,
		})
	})
	if err != nil {
		return nil, err
	}
	if replay {
		return mutationResponse(aggregate), nil
	}
	updated, providerErr := s.provider.UpdateSubscription(ctx, aggregate.ProviderSubscriptionID, razorpay.SubscriptionUpdateParams{
		PlanID: providerPlanID, ScheduleChangeAt: "cycle_end", CustomerNotify: false,
	})
	if providerErr != nil {
		if providerMutationRejected(providerErr) {
			if rejectErr := s.rejectProviderMutation(ctx, aggregate, previous, actorUserID, subscriptionCommandPlanChange, idempotencyKey, requestHash, "plan_change_provider_rejected", now); rejectErr != nil {
				return nil, ErrSubscriptionInternal
			}
			return nil, ErrSubscriptionProviderRejected
		}
		_ = s.markCommandUnknown(ctx, aggregate, actorUserID, subscriptionCommandPlanChange, idempotencyKey, "plan_change_provider_unknown", now)
		return nil, ErrSubscriptionProviderUnknown
	}
	if updated == nil || updated.ID != aggregate.ProviderSubscriptionID ||
		!updated.HasScheduledChanges || updated.ScheduleChangeAt != "cycle_end" {
		_ = s.markCommandUnknown(ctx, aggregate, actorUserID, subscriptionCommandPlanChange, idempotencyKey, "plan_change_provider_unknown", now)
		return nil, ErrSubscriptionProviderUnknown
	}
	err = s.completeCommand(ctx, aggregate.BusinessID, actorUserID, subscriptionCommandPlanChange, idempotencyKey, requestHash)
	if err != nil {
		_ = s.markCommandUnknown(ctx, aggregate, actorUserID, subscriptionCommandPlanChange, idempotencyKey, "plan_change_persistence_unknown", now)
		return nil, ErrSubscriptionProviderUnknown
	}
	return mutationResponse(aggregate), nil
}

func (s *SubscriptionLifecycleService) ScheduleCancellation(
	ctx context.Context,
	businessID, actorUserID string,
	input ScheduleSubscriptionCancellationInput,
) (*SubscriptionMutationResponse, error) {
	settings, err := s.settings(ctx)
	if err != nil {
		return nil, err
	}
	idempotencyKey := strings.TrimSpace(input.IdempotencyKey)
	if idempotencyKey == "" || len(idempotencyKey) > 180 || strings.TrimSpace(businessID) == "" || strings.TrimSpace(actorUserID) == "" || s.provider == nil || s.repository == nil {
		return nil, fmt.Errorf("subscription command scope is invalid")
	}
	now := s.config.Now().UTC()
	requestHash := subscriptionCommandHash(businessID, actorUserID, subscriptionCommandCancellation, idempotencyKey)
	var aggregate *models.Subscription
	var previous *models.Subscription
	var replay bool
	err = s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
		command, commandErr := tx.GetCommandForUpdate(ctx, businessID, actorUserID, subscriptionCommandCancellation, idempotencyKey)
		if commandErr == nil {
			if command.RequestHash != requestHash {
				return ErrSubscriptionIdempotencyConflict
			}
			if command.Status == "rejected" {
				return ErrSubscriptionProviderRejected
			}
			if command.Status != "completed" {
				return ErrSubscriptionProviderUnknown
			}
			current, err := tx.GetByBusinessIDForUpdate(ctx, businessID)
			aggregate, replay = current, true
			return err
		}
		if !errors.Is(commandErr, interfaces.ErrSubscriptionCommandNotFound) {
			return commandErr
		}
		current, err := tx.GetByBusinessIDForUpdate(ctx, businessID)
		if err != nil {
			return err
		}
		if current.BillingMode != models.SubscriptionBillingModeRenewable || current.ProviderMode != settings.ProviderMode ||
			current.ProviderSubscriptionID == "" || current.PeriodEnd == nil || current.Status != models.SubscriptionStatusActive ||
			current.CancelAtPeriodEnd || current.PendingPlanID != "" {
			return ErrSubscriptionLifecycleConflict
		}
		fromStatus := current.Status
		previous = cloneSubscription(current)
		effectiveAt := current.PeriodEnd.UTC()
		current.Status = models.SubscriptionStatusCancellationScheduled
		current.CancelAtPeriodEnd = true
		current.CancellationEffectiveAt = &effectiveAt
		if err := tx.SaveSubscription(ctx, current); err != nil {
			return err
		}
		command = &models.SubscriptionCommand{
			ID: uuid.NewString(), BusinessID: businessID, SubscriptionID: current.ID,
			ActorUserID: actorUserID, Action: subscriptionCommandCancellation, IdempotencyKey: idempotencyKey,
			RequestHash: requestHash, Status: "initializing", CreatedAt: now, UpdatedAt: now,
		}
		if err := tx.CreateCommand(ctx, command); err != nil {
			return err
		}
		aggregate = current
		return tx.CreateAudit(ctx, &models.SubscriptionAuditRecord{
			ID: uuid.NewString(), BusinessID: businessID, SubscriptionID: current.ID, ActorUserID: actorUserID,
			Action: subscriptionCommandCancellation, FromStatus: fromStatus, ToStatus: current.Status,
			FromPlanID: currentCatalogPlanID(current), ToPlanID: currentCatalogPlanID(current), ProviderMode: current.ProviderMode,
			SanitizedCode: "cancellation_scheduled", OccurredAt: now,
		})
	})
	if err != nil {
		return nil, err
	}
	if replay {
		return mutationResponse(aggregate), nil
	}
	cancelled, providerErr := s.provider.CancelSubscription(ctx, aggregate.ProviderSubscriptionID, razorpay.SubscriptionCancelParams{CancelAtCycleEnd: true})
	if providerErr != nil {
		if providerMutationRejected(providerErr) {
			if rejectErr := s.rejectProviderMutation(ctx, aggregate, previous, actorUserID, subscriptionCommandCancellation, idempotencyKey, requestHash, "cancellation_provider_rejected", now); rejectErr != nil {
				return nil, ErrSubscriptionInternal
			}
			return nil, ErrSubscriptionProviderRejected
		}
		_ = s.markCommandUnknown(ctx, aggregate, actorUserID, subscriptionCommandCancellation, idempotencyKey, "cancellation_provider_unknown", now)
		return nil, ErrSubscriptionProviderUnknown
	}
	if cancelled == nil || cancelled.ID != aggregate.ProviderSubscriptionID {
		_ = s.markCommandUnknown(ctx, aggregate, actorUserID, subscriptionCommandCancellation, idempotencyKey, "cancellation_provider_unknown", now)
		return nil, ErrSubscriptionProviderUnknown
	}
	if strings.EqualFold(cancelled.Status, "cancelled") {
		terminal, persistErr := s.finalizeImmediateCancellation(ctx, aggregate, actorUserID, idempotencyKey, requestHash)
		if persistErr != nil {
			return nil, ErrSubscriptionProviderUnknown
		}
		return mutationResponse(terminal), nil
	}
	if err := s.completeCommand(ctx, businessID, actorUserID, subscriptionCommandCancellation, idempotencyKey, requestHash); err != nil {
		_ = s.markCommandUnknown(ctx, aggregate, actorUserID, subscriptionCommandCancellation, idempotencyKey, "cancellation_persistence_unknown", now)
		return nil, ErrSubscriptionProviderUnknown
	}
	return mutationResponse(aggregate), nil
}

func (s *SubscriptionLifecycleService) finalizeImmediateCancellation(
	ctx context.Context,
	aggregate *models.Subscription,
	actorUserID, idempotencyKey, requestHash string,
) (*models.Subscription, error) {
	var terminal *models.Subscription
	err := s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
		current, err := tx.GetByBusinessIDForUpdate(ctx, aggregate.BusinessID)
		if err != nil || current.ID != aggregate.ID || current.Status != models.SubscriptionStatusCancellationScheduled {
			return firstError(err, ErrSubscriptionLifecycleConflict)
		}
		command, err := tx.GetCommandForUpdate(ctx, aggregate.BusinessID, actorUserID, subscriptionCommandCancellation, idempotencyKey)
		if err != nil || command.RequestHash != requestHash || command.Status != "initializing" {
			return firstError(err, ErrSubscriptionLifecycleConflict)
		}
		fromStatus := current.Status
		cancelledAt := s.config.Now().UTC()
		current.Status = models.SubscriptionStatusCancelled
		current.CancelAtPeriodEnd = false
		current.CancellationEffectiveAt = nil
		current.CancelledAt = &cancelledAt
		current.ReconciliationCode = ""
		if err := tx.SaveSubscription(ctx, current); err != nil {
			return err
		}
		command.Status = "completed"
		command.CompletedAt = &cancelledAt
		if err := tx.SaveCommand(ctx, command); err != nil {
			return err
		}
		if err := tx.CreateAudit(ctx, &models.SubscriptionAuditRecord{
			ID: uuid.NewString(), BusinessID: current.BusinessID, SubscriptionID: current.ID, ActorUserID: actorUserID,
			Action: "provider_cancellation_verified", FromStatus: fromStatus, ToStatus: current.Status,
			FromPlanID: currentCatalogPlanID(current), ToPlanID: currentCatalogPlanID(current), ProviderMode: current.ProviderMode,
			SanitizedCode: "provider_cancellation_verified", OccurredAt: cancelledAt,
		}); err != nil {
			return err
		}
		terminal = current
		return nil
	})
	return terminal, err
}

func (s *SubscriptionLifecycleService) completeCommand(ctx context.Context, businessID, actorUserID, action, idempotencyKey, requestHash string) error {
	return s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
		command, err := tx.GetCommandForUpdate(ctx, businessID, actorUserID, action, idempotencyKey)
		if err != nil || command.RequestHash != requestHash || command.Status != "initializing" {
			return firstError(err, ErrSubscriptionLifecycleConflict)
		}
		now := s.config.Now().UTC()
		command.Status = "completed"
		command.CompletedAt = &now
		return tx.SaveCommand(ctx, command)
	})
}

func (s *SubscriptionLifecycleService) markCommandUnknown(
	ctx context.Context,
	aggregate *models.Subscription,
	actorUserID, action, idempotencyKey, code string,
	occurredAt time.Time,
) error {
	return s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
		current, err := tx.GetByBusinessIDForUpdate(ctx, aggregate.BusinessID)
		if err != nil || current.ID != aggregate.ID {
			return firstError(err, ErrSubscriptionLifecycleConflict)
		}
		fromStatus := current.Status
		current.Status = models.SubscriptionStatusReconciliationRequired
		current.ReconciliationCode = code
		if err := tx.SaveSubscription(ctx, current); err != nil {
			return err
		}
		command, err := tx.GetCommandForUpdate(ctx, aggregate.BusinessID, actorUserID, action, idempotencyKey)
		if err == nil {
			command.Status = "reconciliation_required"
			command.SanitizedErrorCode = code
			if err := tx.SaveCommand(ctx, command); err != nil {
				return err
			}
		}
		return tx.CreateAudit(ctx, lifecycleAudit(current, action, fromStatus, current.Status, code, "", occurredAt))
	})
}

func (s *SubscriptionLifecycleService) rejectProviderMutation(
	ctx context.Context,
	aggregate, previous *models.Subscription,
	actorUserID, action, idempotencyKey, requestHash, code string,
	occurredAt time.Time,
) error {
	return s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
		current, err := tx.GetByBusinessIDForUpdate(ctx, aggregate.BusinessID)
		if err != nil || current.ID != aggregate.ID {
			return firstError(err, ErrSubscriptionLifecycleConflict)
		}
		fromStatus := current.Status
		fromPlan := currentCatalogPlanID(current)
		providerMode := current.ProviderMode
		restored := current
		if previous != nil {
			restored = cloneSubscription(previous)
			restored.LifecycleVersion = current.LifecycleVersion
		} else {
			resetSubscriptionForRenewableRestart(restored, occurredAt)
			restored.Status = models.SubscriptionStatusActive
			restored.BillingMode = models.SubscriptionBillingModeFree
		}
		if err := tx.SaveSubscription(ctx, restored); err != nil {
			return err
		}
		command, err := tx.GetCommandForUpdate(ctx, aggregate.BusinessID, actorUserID, action, idempotencyKey)
		if err != nil || command.RequestHash != requestHash || command.Status != "initializing" {
			return firstError(err, ErrSubscriptionLifecycleConflict)
		}
		completedAt := occurredAt.UTC()
		command.Status = "rejected"
		command.SanitizedErrorCode = code
		command.CompletedAt = &completedAt
		if err := tx.SaveCommand(ctx, command); err != nil {
			return err
		}
		return tx.CreateAudit(ctx, &models.SubscriptionAuditRecord{
			ID: uuid.NewString(), BusinessID: restored.BusinessID, SubscriptionID: restored.ID, ActorUserID: actorUserID,
			Action: action + "_rejected", FromStatus: fromStatus, ToStatus: restored.Status,
			FromPlanID: fromPlan, ToPlanID: currentCatalogPlanID(restored), ProviderMode: providerMode,
			SanitizedCode: code, OccurredAt: completedAt,
		})
	})
}

func providerMutationRejected(err error) bool {
	var providerStatus interface{ HTTPStatusCode() int }
	if !errors.As(err, &providerStatus) {
		return false
	}
	status := providerStatus.HTTPStatusCode()
	return status >= 400 && status < 500
}

func cloneSubscription(subscription *models.Subscription) *models.Subscription {
	if subscription == nil {
		return nil
	}
	clone := *subscription
	return &clone
}

func mutationResponse(subscription *models.Subscription) *SubscriptionMutationResponse {
	return &SubscriptionMutationResponse{
		SubscriptionID: subscription.ID, Status: subscription.Status, PlanID: currentCatalogPlanID(subscription),
		PendingPlanID: subscription.PendingPlanID, EffectiveAt: firstTime(subscription.PendingPlanEffectiveAt, subscription.CancellationEffectiveAt),
		CancelAtPeriodEnd:      subscription.CancelAtPeriodEnd,
		ReconciliationRequired: subscription.Status == models.SubscriptionStatusReconciliationRequired,
	}
}

func firstTime(values ...*time.Time) *time.Time {
	for _, value := range values {
		if value != nil {
			copy := value.UTC()
			return &copy
		}
	}
	return nil
}

func (s *SubscriptionLifecycleService) validateProviderSubscriptionMetadata(aggregate *models.Subscription, provider *razorpay.Subscription, settings SubscriptionProviderSettings) string {
	if code := s.validateProviderSubscriptionIdentity(aggregate, provider, settings); code != "" {
		return code
	}
	return validateProviderSubscriptionPlan(aggregate, provider)
}

func (s *SubscriptionLifecycleService) validateProviderSubscriptionIdentity(aggregate *models.Subscription, provider *razorpay.Subscription, settings SubscriptionProviderSettings) string {
	if aggregate == nil || provider == nil || aggregate.ProviderMode != settings.ProviderMode {
		return "provider_mode_mismatch"
	}
	if provider.ID != aggregate.ProviderSubscriptionID {
		return "provider_subscription_mismatch"
	}
	if provider.Notes["provider_mode"] != aggregate.ProviderMode {
		return "provider_mode_mismatch"
	}
	if provider.Notes["business_id"] != aggregate.BusinessID || provider.Notes["subscription_id"] != aggregate.ID {
		return "tenant_metadata_mismatch"
	}
	return ""
}

func validateProviderSubscriptionPlan(aggregate *models.Subscription, provider *razorpay.Subscription) string {
	if provider.PlanID != aggregate.ProviderPlanID && provider.PlanID != aggregate.PendingProviderPlanID {
		return "provider_plan_mismatch"
	}
	return ""
}

func validatePaidPeriodAdvance(aggregate *models.Subscription, provider *razorpay.Subscription, start time.Time) string {
	if aggregate.LastProviderPaidCount > 0 && aggregate.PendingProviderPlanID == provider.PlanID &&
		(aggregate.PendingPlanEffectiveAt == nil || !start.Equal(aggregate.PendingPlanEffectiveAt.UTC())) {
		return "pending_plan_boundary_mismatch"
	}
	if aggregate.LastProviderPaidCount > 0 && aggregate.PeriodEnd != nil && !start.Equal(aggregate.PeriodEnd.UTC()) {
		return "provider_period_not_monotonic"
	}
	return ""
}

func (s *SubscriptionLifecycleService) applySubscriptionEventTx(
	ctx context.Context,
	tx interfaces.SubscriptionLifecycleRepository,
	aggregate *models.Subscription,
	event *razorpay.WebhookEvent,
	providerEventID string,
	providerOccurredAt time.Time,
	settings SubscriptionProviderSettings,
) (string, error) {
	providerSubscription := event.Payload.Subscription.Entity
	fromStatus := aggregate.Status
	fromPlan := currentCatalogPlanID(aggregate)
	terminal := aggregate.Status == models.SubscriptionStatusCancelled || aggregate.Status == models.SubscriptionStatusExpired
	if terminal && event.Event != "subscription.cancelled" && event.Event != "subscription.completed" {
		return "terminal_event_ignored", nil
	}

	switch event.Event {
	case "subscription.activated":
		aggregate.Status = models.SubscriptionStatusRenewalPending
	case "subscription.charged":
		if aggregate.Status == models.SubscriptionStatusCancellationScheduled {
			return "cancellation_charge_race", ErrSubscriptionProviderUnknown
		}
		if event.Payload.Payment == nil {
			return "charged_payment_missing", ErrSubscriptionProviderUnknown
		}
		payment := event.Payload.Payment.Entity
		plan, ok := planForProviderID(providerSubscription.PlanID, settings.ProviderPlanIDs)
		if !ok || providerSubscription.PaidCount <= aggregate.LastProviderPaidCount {
			return "charged_count_or_plan_mismatch", ErrSubscriptionProviderUnknown
		}
		if payment.Amount != plan.Amount || !strings.EqualFold(payment.Currency, plan.Currency) ||
			(!payment.Captured && !strings.EqualFold(payment.Status, "captured")) {
			return "charged_amount_or_status_mismatch", ErrSubscriptionProviderUnknown
		}
		if providerSubscription.CurrentStart <= 0 || providerSubscription.CurrentEnd <= providerSubscription.CurrentStart {
			return "provider_period_invalid", ErrSubscriptionProviderUnknown
		}
		start := time.Unix(providerSubscription.CurrentStart, 0).UTC()
		end := time.Unix(providerSubscription.CurrentEnd, 0).UTC()
		if code := validatePaidPeriodAdvance(aggregate, &providerSubscription, start); code != "" {
			return code, ErrSubscriptionProviderUnknown
		}
		next := end
		applyPlanToAggregate(aggregate, plan)
		aggregate.Status = models.SubscriptionStatusActive
		aggregate.BillingMode = models.SubscriptionBillingModeRenewable
		aggregate.ProviderPlanID = providerSubscription.PlanID
		aggregate.ProviderCustomerID = providerSubscription.CustomerID
		aggregate.PeriodStart, aggregate.PeriodEnd, aggregate.NextRenewalAt = &start, &end, &next
		aggregate.StartDate, aggregate.EndDate, aggregate.NextBillingDate = start, &end, &next
		aggregate.GraceDeadline = nil
		aggregate.ReconciliationCode = ""
		aggregate.LastProviderPaidCount = providerSubscription.PaidCount
		if aggregate.PendingProviderPlanID == providerSubscription.PlanID {
			aggregate.PendingPlanID = ""
			aggregate.PendingProviderPlanID = ""
			aggregate.PendingPlanEffectiveAt = nil
		}
		if err := tx.CreateBillingRecord(ctx, &models.SubscriptionBillingRecord{
			ID: uuid.NewString(), BusinessID: aggregate.BusinessID, SubscriptionID: aggregate.ID,
			ProviderMode: aggregate.ProviderMode, ProviderEventID: providerEventID,
			ProviderInvoiceID: payment.InvoiceID, ProviderPaymentID: payment.ID,
			AmountMinor: payment.Amount, Currency: strings.ToUpper(payment.Currency), Status: "paid",
			ReceiptReference: billingReceiptReference(aggregate.BusinessID, providerEventID),
			PeriodStart:      &start, PeriodEnd: &end, QuotaPeriodStart: &start, QuotaPeriodEnd: &end,
			OccurredAt: providerOccurredAt,
		}); err != nil {
			return "billing_record_failed", err
		}
	case "subscription.pending":
		aggregate.Status = models.SubscriptionStatusPastDue
		deadlineBase := providerOccurredAt
		if aggregate.PeriodEnd != nil && aggregate.PeriodEnd.After(deadlineBase) {
			deadlineBase = *aggregate.PeriodEnd
		}
		deadline := deadlineBase.Add(s.config.GracePeriod)
		aggregate.GraceDeadline = &deadline
	case "subscription.halted", "subscription.paused":
		aggregate.Status = models.SubscriptionStatusSuspended
	case "subscription.cancelled":
		aggregate.Status = models.SubscriptionStatusCancelled
		aggregate.CancelAtPeriodEnd = false
		aggregate.CancellationEffectiveAt = nil
		cancelled := providerOccurredAt
		aggregate.CancelledAt = &cancelled
	case "subscription.completed", "subscription.expired":
		aggregate.Status = models.SubscriptionStatusExpired
	case "subscription.updated":
		if aggregate.PendingPlanID != "" {
			aggregate.Status = models.SubscriptionStatusRenewalPending
		}
	default:
		return "unsupported_subscription_event", fmt.Errorf("unsupported subscription event")
	}
	aggregate.LastProviderEventAt = &providerOccurredAt
	if providerSubscription.CurrentStart > 0 && providerSubscription.CurrentEnd > providerSubscription.CurrentStart && event.Event != "subscription.charged" {
		start := time.Unix(providerSubscription.CurrentStart, 0).UTC()
		end := time.Unix(providerSubscription.CurrentEnd, 0).UTC()
		aggregate.PeriodStart, aggregate.PeriodEnd = &start, &end
	}
	if err := tx.SaveSubscription(ctx, aggregate); err != nil {
		return "subscription_persistence_failed", err
	}
	toPlan := currentCatalogPlanID(aggregate)
	if err := tx.CreateAudit(ctx, &models.SubscriptionAuditRecord{
		ID: uuid.NewString(), BusinessID: aggregate.BusinessID, SubscriptionID: aggregate.ID,
		Action: event.Event, FromStatus: fromStatus, ToStatus: aggregate.Status,
		FromPlanID: fromPlan, ToPlanID: toPlan, ProviderMode: aggregate.ProviderMode,
		ProviderEventID: providerEventID, SanitizedCode: "provider_event_applied", OccurredAt: providerOccurredAt,
	}); err != nil {
		return "audit_persistence_failed", err
	}
	return "", nil
}

func planForProviderID(providerPlanID string, providerPlanIDs map[string]string) (SubscriptionPlan, bool) {
	for planID, configuredID := range providerPlanIDs {
		if configuredID == providerPlanID {
			return paidSubscriptionPlan(planID)
		}
	}
	return SubscriptionPlan{}, false
}

func (s *SubscriptionLifecycleService) settings(ctx context.Context) (SubscriptionProviderSettings, error) {
	if s.config.Resolve != nil {
		settings, err := s.config.Resolve(ctx)
		if err != nil {
			return SubscriptionProviderSettings{}, ErrSubscriptionUnavailable
		}
		return settings, nil
	}
	return SubscriptionProviderSettings{
		ProviderMode: s.config.ProviderMode, ProviderPlanIDs: s.config.ProviderPlanIDs, WebhookSecret: s.config.WebhookSecret,
	}, nil
}

func applyPlanToAggregate(aggregate *models.Subscription, plan SubscriptionPlan) {
	aggregate.Plan = plan.LegacyPlan
	aggregate.PlanCode = plan.PlanCode
	aggregate.CatalogVersion = CurrentSubscriptionCatalogVersion
	aggregate.MaxInvoices = plan.Quotas[QuotaInvoices]
	aggregate.MaxCustomers = plan.Quotas[QuotaCustomers]
	aggregate.MaxUsers = plan.Quotas[QuotaUsers]
	aggregate.MaxStorageMB = plan.Quotas[QuotaStorageMB]
}

func resetSubscriptionForRenewableRestart(aggregate *models.Subscription, now time.Time) {
	applyPlanToAggregate(aggregate, subscriptionPlanForCode("free"))
	aggregate.BillingMode = models.SubscriptionBillingModeFree
	aggregate.ProviderMode = ""
	aggregate.ProviderCustomerID = ""
	aggregate.ProviderSubscriptionID = ""
	aggregate.ProviderPlanID = ""
	aggregate.StartDate = now.UTC()
	aggregate.EndDate = nil
	aggregate.NextBillingDate = nil
	aggregate.PeriodStart = nil
	aggregate.PeriodEnd = nil
	aggregate.NextRenewalAt = nil
	aggregate.GraceDeadline = nil
	aggregate.CancelAtPeriodEnd = false
	aggregate.CancellationEffectiveAt = nil
	aggregate.CancelledAt = nil
	aggregate.PendingPlanID = ""
	aggregate.PendingProviderPlanID = ""
	aggregate.PendingPlanEffectiveAt = nil
	aggregate.LastProviderEventAt = nil
	aggregate.LastProviderPaidCount = 0
	aggregate.ReconciliationCode = ""
}

func currentCatalogPlanID(subscription *models.Subscription) string {
	return subscriptionPlanForCode(normalizePlanCode(subscription.Plan, subscription.PlanCode)).ID
}

func billingReceiptReference(businessID, providerEventID string) string {
	digest := sha256Hex([]byte(businessID + "\x00" + providerEventID))
	return "BIL-" + strings.ToUpper(digest[:16])
}

func lifecycleAudit(subscription *models.Subscription, action, fromStatus, toStatus, code, providerEventID string, occurredAt time.Time) *models.SubscriptionAuditRecord {
	return &models.SubscriptionAuditRecord{
		ID: uuid.NewString(), BusinessID: subscription.BusinessID, SubscriptionID: subscription.ID,
		Action: action, FromStatus: fromStatus, ToStatus: toStatus,
		ToPlanID: currentCatalogPlanID(subscription), ProviderMode: subscription.ProviderMode,
		ProviderEventID: providerEventID, SanitizedCode: code, OccurredAt: occurredAt,
	}
}

func sha256Hex(value []byte) string {
	digest := sha256.Sum256(value)
	return hex.EncodeToString(digest[:])
}

func (s *SubscriptionLifecycleService) markUnknownProviderOutcome(
	ctx context.Context, subscriptionID, businessID, actorUserID, idempotencyKey string, occurredAt time.Time, code string,
) error {
	return s.repository.Transaction(ctx, func(tx interfaces.SubscriptionLifecycleRepository) error {
		current, err := tx.GetByBusinessIDForUpdate(ctx, businessID)
		if err != nil || current.ID != subscriptionID {
			return firstError(err, ErrSubscriptionLifecycleConflict)
		}
		fromStatus := current.Status
		current.Status = models.SubscriptionStatusReconciliationRequired
		current.ReconciliationCode = code
		if err := tx.SaveSubscription(ctx, current); err != nil {
			return err
		}
		command, err := tx.GetCommandForUpdate(ctx, businessID, actorUserID, subscriptionCommandStart, idempotencyKey)
		if err == nil {
			command.Status = "reconciliation_required"
			command.SanitizedErrorCode = code
			if err := tx.SaveCommand(ctx, command); err != nil {
				return err
			}
		}
		return tx.CreateAudit(ctx, &models.SubscriptionAuditRecord{
			ID: uuid.NewString(), BusinessID: businessID, SubscriptionID: subscriptionID, ActorUserID: actorUserID,
			Action: subscriptionCommandStart, FromStatus: fromStatus, ToStatus: current.Status,
			ToPlanID: current.PendingPlanID, ProviderMode: current.ProviderMode, SanitizedCode: code, OccurredAt: occurredAt,
		})
	})
}

func checkoutResponse(subscription *models.Subscription, authorizationURL string) *SubscriptionCheckoutResponse {
	return &SubscriptionCheckoutResponse{
		SubscriptionID: subscription.ID, Status: subscription.Status, BillingMode: subscription.BillingMode,
		PendingPlanID: subscription.PendingPlanID, AuthorizationURL: authorizationURL,
	}
}

func checkoutResponseFromCommand(command *models.SubscriptionCommand) *SubscriptionCheckoutResponse {
	return &SubscriptionCheckoutResponse{
		SubscriptionID: command.SubscriptionID, Status: command.ResponseStatus, BillingMode: command.ResponseBillingMode,
		PendingPlanID: command.ResponsePendingPlanID, AuthorizationURL: command.ResponseAuthorizationURL,
	}
}

func subscriptionCommandHash(values ...string) string {
	digest := sha256.Sum256([]byte(strings.Join(values, "\x00")))
	return hex.EncodeToString(digest[:])
}

func validProviderMode(mode string) bool {
	return mode == models.ProviderModeTest || mode == models.ProviderModeLive
}

func subscriptionHasCurrentPaidAccess(subscription *models.Subscription, now time.Time) bool {
	if subscription == nil {
		return false
	}
	end := subscription.PeriodEnd
	if end == nil {
		end = subscription.EndDate
	}
	return end == nil || end.After(now.UTC())
}

func firstError(primary, fallback error) error {
	if primary != nil {
		return primary
	}
	return fallback
}
