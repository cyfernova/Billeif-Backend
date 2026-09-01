package services

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"sort"
	"strings"
	"time"

	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
)

type OperationStatus string

const (
	OperationStatusQueued                 OperationStatus = "queued"
	OperationStatusInProgress             OperationStatus = "in_progress"
	OperationStatusSucceeded              OperationStatus = "succeeded"
	OperationStatusFailed                 OperationStatus = "failed"
	OperationStatusReconciliationRequired OperationStatus = "reconciliation_required"
	OperationStatusUnknown                OperationStatus = "unknown"
)

const (
	OperationActionRetry             = "retry"
	OperationActionReconcile         = "reconcile"
	OperationActionReprocessWebhook  = "reprocess_webhook"
	OperationActionRedriveDeadLetter = "redrive_dead_letter"
	OperationActionResolve           = "resolve"
)

const (
	OperationCodeStepUpRequired      = "step_up_required"
	OperationCodeUnsupportedRecovery = "unsupported_recovery"
	OperationCodeAccepted            = "accepted"
	OperationCodeUnsafeReplay        = "unsafe_replay"
)

const (
	OperationPrincipalBusiness = "business"
	OperationPrincipalOperator = "operator"
)

const (
	OperationTypeInvoiceRender       = "invoice_render"
	OperationTypeInvoiceDelivery     = "invoice_delivery"
	OperationTypeOutbox              = "outbox"
	OperationTypeRazorpayWebhook     = "razorpay_webhook"
	OperationTypeGSTEInvoice         = "gst_einvoice"
	OperationTypeGSTEWayBill         = "gst_ewaybill"
	OperationTypeRecurringInvoice    = "recurring_invoice"
	OperationTypeEmailDelivery       = "email_delivery"
	OperationTypeWhatsAppDelivery    = "whatsapp_delivery"
	OperationTypeNotification        = "notification"
	OperationTypeImport              = "import"
	OperationTypeVoiceReconciliation = "voice_reconciliation"
)

var (
	ErrInvalidOperationQuery        = errors.New("invalid operation query")
	ErrOperationNotFound            = errors.New("operation not found")
	ErrInvalidOperationRecovery     = errors.New("invalid operation recovery request")
	ErrUnsupportedOperationRecovery = errors.New("unsupported operation recovery")
	ErrUnsafeOperationReplay        = errors.New("unsafe operation replay")
	ErrOperationStepUpRequired      = errors.New("operation recovery step-up is required")
	ErrOperationAuditUnavailable    = errors.New("operation recovery audit is unavailable")
)

var supportedOperationTypes = map[string]struct{}{
	OperationTypeInvoiceRender: {}, OperationTypeInvoiceDelivery: {}, OperationTypeOutbox: {},
	OperationTypeRazorpayWebhook: {}, OperationTypeGSTEInvoice: {}, OperationTypeGSTEWayBill: {},
	OperationTypeRecurringInvoice: {}, OperationTypeEmailDelivery: {}, OperationTypeWhatsAppDelivery: {},
	OperationTypeNotification: {}, OperationTypeImport: {}, OperationTypeVoiceReconciliation: {},
}

var supportedOperationStatuses = map[OperationStatus]struct{}{
	OperationStatusQueued: {}, OperationStatusInProgress: {}, OperationStatusSucceeded: {},
	OperationStatusFailed: {}, OperationStatusReconciliationRequired: {}, OperationStatusUnknown: {},
}

type OperationResource struct {
	Type string `json:"type"`
	ID   string `json:"id"`
}

type OperationSummary struct {
	OperationID            string            `json:"operation_id"`
	Type                   string            `json:"type"`
	Resource               OperationResource `json:"resource"`
	Status                 OperationStatus   `json:"status"`
	Attempts               int               `json:"attempts"`
	LastAttemptAt          *time.Time        `json:"last_attempt_at,omitempty"`
	NextAttemptAt          *time.Time        `json:"next_attempt_at,omitempty"`
	Retryable              bool              `json:"retryable"`
	ReconciliationRequired bool              `json:"reconciliation_required"`
	DeadLetter             bool              `json:"dead_letter"`
	ErrorCode              string            `json:"error_code,omitempty"`
	CorrelationID          string            `json:"correlation_id,omitempty"`
	CreatedAt              time.Time         `json:"created_at"`
	UpdatedAt              time.Time         `json:"updated_at"`
	CompletedAt            *time.Time        `json:"completed_at,omitempty"`
}

type OperationRecoveryAction struct {
	Action          string `json:"action"`
	Available       bool   `json:"available"`
	RequirementCode string `json:"requirement_code,omitempty"`
}

type OperationOperatorDetail struct {
	OperationSummary
	SourceStatus    string                    `json:"source_status"`
	RecoveryActions []OperationRecoveryAction `json:"recovery_actions"`
}

type OperationTimelineEvent struct {
	Status     OperationStatus `json:"status"`
	Code       string          `json:"code,omitempty"`
	OccurredAt time.Time       `json:"occurred_at"`
}

type OperationTimeline struct {
	OperationID string                   `json:"operation_id"`
	Events      []OperationTimelineEvent `json:"events"`
}

type OperationListInput struct {
	Types    []string
	Statuses []OperationStatus
	Limit    int
	Cursor   string
}

type OperationListResponse struct {
	Operations       []OperationSummary `json:"operations"`
	NextCursor       string             `json:"next_cursor,omitempty"`
	UnavailableTypes []string           `json:"unavailable_types,omitempty"`
}

type OperationServiceOptions struct {
	Now            func() time.Time
	StepUpVerifier OperationStepUpVerifier
}

type OperationStepUpRequest struct {
	OperatorSubject string
	BusinessID      string
	OperationID     string
	Action          string
	Token           string
}

type OperationStepUpVerifier interface {
	VerifyOperationStepUp(context.Context, OperationStepUpRequest) error
}

type FailClosedOperationStepUpVerifier struct{}

func (FailClosedOperationStepUpVerifier) VerifyOperationStepUp(context.Context, OperationStepUpRequest) error {
	return ErrOperationStepUpRequired
}

type OperationRecoveryCapabilityRequest struct {
	BusinessID    string
	UserID        string
	OperationType string
	Action        string
}

type OperationRecoveryCapabilityGuard interface {
	RequireOperationRecovery(context.Context, OperationRecoveryCapabilityRequest) error
}

type OperationRecoveryInput struct {
	Action         string `json:"action"`
	Reason         string `json:"reason"`
	IdempotencyKey string `json:"idempotency_key"`
	CorrelationID  string `json:"correlation_id"`
}

type OperationRecoveryResponse struct {
	CommandID     string    `json:"command_id"`
	OperationID   string    `json:"operation_id"`
	Action        string    `json:"action"`
	ResultCode    string    `json:"result_code"`
	CorrelationID string    `json:"correlation_id"`
	Replayed      bool      `json:"replayed"`
	AcceptedAt    time.Time `json:"accepted_at"`
}

type OperationService struct {
	repository   interfaces.OperationRepository
	permissions  PermissionChecker
	capabilities OperationRecoveryCapabilityGuard
	stepUp       OperationStepUpVerifier
	now          func() time.Time
}

func NewOperationService(
	repository interfaces.OperationRepository,
	permissions PermissionChecker,
	capabilities OperationRecoveryCapabilityGuard,
	options OperationServiceOptions,
) *OperationService {
	now := options.Now
	if now == nil {
		now = time.Now
	}
	stepUp := options.StepUpVerifier
	if stepUp == nil {
		stepUp = FailClosedOperationStepUpVerifier{}
	}
	return &OperationService{
		repository: repository, permissions: permissions, capabilities: capabilities,
		stepUp: stepUp, now: now,
	}
}

func (s *OperationService) RecoverBusinessOperation(
	ctx context.Context,
	businessID, publicOperationID string,
	input OperationRecoveryInput,
) (*OperationRecoveryResponse, error) {
	if s == nil || s.repository == nil || !validOperationBusinessID(businessID) {
		return nil, ErrInvalidOperationRecovery
	}
	actor := ActorFromContext(ctx)
	if actor.UserID == "" || !validOperationRecoveryInput(input) {
		return nil, ErrInvalidOperationRecovery
	}
	operationType, operationID, ok := parsePublicOperationID(publicOperationID)
	if !ok {
		return nil, ErrOperationNotFound
	}
	if operationType != OperationTypeInvoiceRender || input.Action != OperationActionRetry {
		return nil, ErrUnsupportedOperationRecovery
	}
	record, err := s.repository.GetOperation(ctx, businessID, operationType, operationID)
	if errors.Is(err, interfaces.ErrOperationNotFound) {
		return nil, ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	if record == nil || record.ID != operationID || record.Type != operationType {
		return nil, ErrOperationNotFound
	}
	if err := requireMutationPermission(ctx, s.permissions, businessID, PermissionDocumentsManage); err != nil {
		return nil, err
	}
	if s.capabilities == nil {
		return nil, ErrUnsupportedOperationRecovery
	}
	if err := s.capabilities.RequireOperationRecovery(ctx, OperationRecoveryCapabilityRequest{
		BusinessID: businessID, UserID: actor.UserID, OperationType: operationType, Action: input.Action,
	}); err != nil {
		return nil, err
	}
	command := interfaces.RenderRecoveryCommand{
		BusinessID: businessID, OperationID: operationID, ActorSubject: actor.UserID,
		PrincipalKind: OperationPrincipalBusiness, Action: input.Action, Reason: strings.TrimSpace(input.Reason),
		IdempotencyKey: strings.TrimSpace(input.IdempotencyKey), CorrelationID: strings.TrimSpace(input.CorrelationID),
		OperationVersion: record.UpdatedAt.UTC().Format(time.RFC3339Nano), OccurredAt: s.now().UTC(),
	}
	command.RequestHash = operationRecoveryRequestHash(command)
	result, err := s.repository.RetryRender(ctx, command)
	if errors.Is(err, interfaces.ErrUnsupportedRecovery) {
		return nil, ErrUnsupportedOperationRecovery
	}
	if errors.Is(err, interfaces.ErrOperationRace) || errors.Is(err, interfaces.ErrUnsafeOperationReplay) {
		return nil, ErrUnsafeOperationReplay
	}
	if err != nil {
		return nil, err
	}
	if result == nil {
		return nil, ErrUnsupportedOperationRecovery
	}
	return &OperationRecoveryResponse{
		CommandID: result.CommandID, OperationID: publicOperationID, Action: input.Action,
		ResultCode: safeOperationCode(result.ResultCode), CorrelationID: safeOperationCode(result.CorrelationID),
		Replayed: result.Replayed, AcceptedAt: result.AcceptedAt.UTC(),
	}, nil
}

func (s *OperationService) RecoverOperatorOperation(
	ctx context.Context,
	businessID, publicOperationID string,
	input OperationRecoveryInput,
	stepUpToken string,
) (*OperationRecoveryResponse, error) {
	if s == nil || s.repository == nil || !validOperationBusinessID(businessID) || !validOperationRecoveryInput(input) {
		return nil, ErrInvalidOperationRecovery
	}
	actor := ActorFromContext(ctx)
	if actor.UserID == "" {
		return nil, ErrInvalidOperationRecovery
	}
	operationType, operationID, ok := parsePublicOperationID(publicOperationID)
	if !ok {
		return nil, ErrOperationNotFound
	}
	record, err := s.repository.GetOperation(ctx, businessID, operationType, operationID)
	if errors.Is(err, interfaces.ErrOperationNotFound) {
		return nil, ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	if record == nil || record.ID != operationID || record.Type != operationType {
		return nil, ErrOperationNotFound
	}
	decision := interfaces.OperationRecoveryDecision{
		BusinessID: businessID, OperationType: operationType, OperationID: operationID,
		ActorSubject: actor.UserID, PrincipalKind: OperationPrincipalOperator, Action: input.Action,
		Reason: strings.TrimSpace(input.Reason), IdempotencyKey: strings.TrimSpace(input.IdempotencyKey),
		CorrelationID:    strings.TrimSpace(input.CorrelationID),
		OperationVersion: record.UpdatedAt.UTC().Format(time.RFC3339Nano), OccurredAt: s.now().UTC(),
	}
	decision.RequestHash = operationRecoveryDecisionHash(decision)
	if highRiskOperationAction(input.Action) {
		if s.stepUp == nil || s.stepUp.VerifyOperationStepUp(ctx, OperationStepUpRequest{
			OperatorSubject: actor.UserID, BusinessID: businessID, OperationID: publicOperationID,
			Action: input.Action, Token: strings.TrimSpace(stepUpToken),
		}) != nil {
			decision.ResultCode = OperationCodeStepUpRequired
			if _, err := s.repository.RecordRecoveryDecision(ctx, decision); err != nil {
				if errors.Is(err, interfaces.ErrUnsafeOperationReplay) {
					return nil, ErrUnsafeOperationReplay
				}
				return nil, ErrOperationAuditUnavailable
			}
			return nil, ErrOperationStepUpRequired
		}
	}
	decision.ResultCode = OperationCodeUnsupportedRecovery
	if _, err := s.repository.RecordRecoveryDecision(ctx, decision); err != nil {
		if errors.Is(err, interfaces.ErrUnsafeOperationReplay) {
			return nil, ErrUnsafeOperationReplay
		}
		return nil, ErrOperationAuditUnavailable
	}
	return nil, ErrUnsupportedOperationRecovery
}

type operationCursor struct {
	SnapshotAt time.Time `json:"snapshot_at"`
	UpdatedAt  time.Time `json:"updated_at"`
	Type       string    `json:"type"`
	ID         string    `json:"id"`
	FilterHash string    `json:"filter_hash"`
}

func (s *OperationService) ListBusinessOperations(
	ctx context.Context,
	businessID string,
	input OperationListInput,
) (*OperationListResponse, error) {
	if s == nil || s.repository == nil || !validOperationBusinessID(businessID) {
		return nil, ErrInvalidOperationQuery
	}
	query, limit, filterHash, err := s.operationQuery(input)
	if err != nil {
		return nil, err
	}
	if input.Cursor == "" {
		query.SnapshotAt = s.now().UTC()
	} else {
		cursor, err := decodeOperationCursor(input.Cursor)
		if err != nil || cursor.FilterHash != filterHash || cursor.SnapshotAt.IsZero() ||
			cursor.UpdatedAt.IsZero() || cursor.Type == "" || cursor.ID == "" {
			return nil, ErrInvalidOperationQuery
		}
		query.SnapshotAt = cursor.SnapshotAt
		query.AfterUpdatedAt = &cursor.UpdatedAt
		query.AfterType = cursor.Type
		query.AfterID = cursor.ID
	}
	query.Limit = limit + 1
	page, err := s.repository.ListOperations(ctx, businessID, query)
	if err != nil {
		return nil, err
	}
	sortOperationRecords(page.Records)
	response := &OperationListResponse{
		Operations:       make([]OperationSummary, 0, min(limit, len(page.Records))),
		UnavailableTypes: normalizeUnavailableTypes(page.UnavailableTypes),
	}
	for i := 0; i < len(page.Records) && i < limit; i++ {
		response.Operations = append(response.Operations, publicOperation(page.Records[i]))
	}
	if len(page.Records) > limit && len(response.Operations) != 0 {
		last := page.Records[limit-1]
		response.NextCursor, err = encodeOperationCursor(operationCursor{
			SnapshotAt: query.SnapshotAt, UpdatedAt: last.UpdatedAt.UTC(), Type: last.Type,
			ID: last.ID, FilterHash: filterHash,
		})
		if err != nil {
			return nil, err
		}
	}
	return response, nil
}

func (s *OperationService) GetBusinessOperation(
	ctx context.Context,
	businessID, publicOperationID string,
) (*OperationSummary, error) {
	if s == nil || s.repository == nil || !validOperationBusinessID(businessID) {
		return nil, ErrOperationNotFound
	}
	operationType, operationID, ok := parsePublicOperationID(publicOperationID)
	if !ok {
		return nil, ErrOperationNotFound
	}
	record, err := s.repository.GetOperation(ctx, businessID, operationType, operationID)
	if errors.Is(err, interfaces.ErrOperationNotFound) {
		return nil, ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	if record == nil || record.ID != operationID || record.Type != operationType {
		return nil, ErrOperationNotFound
	}
	result := publicOperation(*record)
	return &result, nil
}

func (s *OperationService) GetOperatorOperation(
	ctx context.Context,
	businessID, publicOperationID string,
) (*OperationOperatorDetail, error) {
	operationType, operationID, ok := parsePublicOperationID(publicOperationID)
	if !ok || s == nil || s.repository == nil || !validOperationBusinessID(businessID) {
		return nil, ErrOperationNotFound
	}
	record, err := s.repository.GetOperation(ctx, businessID, operationType, operationID)
	if errors.Is(err, interfaces.ErrOperationNotFound) {
		return nil, ErrOperationNotFound
	}
	if err != nil {
		return nil, err
	}
	if record == nil || record.ID != operationID || record.Type != operationType {
		return nil, ErrOperationNotFound
	}
	return &OperationOperatorDetail{
		OperationSummary: publicOperation(*record),
		SourceStatus:     safeOperationCode(record.InternalStatus),
		RecoveryActions:  recoveryActions(*record),
	}, nil
}

func (s *OperationService) GetOperationTimeline(
	ctx context.Context,
	businessID, publicOperationID string,
	limit int,
) (*OperationTimeline, error) {
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || s == nil || s.repository == nil || !validOperationBusinessID(businessID) {
		return nil, ErrInvalidOperationQuery
	}
	operationType, operationID, ok := parsePublicOperationID(publicOperationID)
	if !ok {
		return nil, ErrOperationNotFound
	}
	if _, err := s.GetBusinessOperation(ctx, businessID, publicOperationID); err != nil {
		return nil, err
	}
	records, err := s.repository.ListOperationTimeline(ctx, businessID, operationType, operationID, limit)
	if err != nil {
		return nil, err
	}
	events := make([]OperationTimelineEvent, 0, len(records))
	for _, record := range records {
		events = append(events, OperationTimelineEvent{
			Status: normalizeOperationStatus(operationType, record.Status),
			Code:   safeOperationCode(record.Code), OccurredAt: record.OccurredAt.UTC(),
		})
	}
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].OccurredAt.Before(events[j].OccurredAt)
	})
	return &OperationTimeline{OperationID: publicOperationID, Events: events}, nil
}

func (s *OperationService) operationQuery(input OperationListInput) (interfaces.OperationRecordQuery, int, string, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 50
	}
	if limit < 1 || limit > 100 || len(input.Cursor) > 2048 {
		return interfaces.OperationRecordQuery{}, 0, "", ErrInvalidOperationQuery
	}
	types := normalizedStrings(input.Types)
	for _, operationType := range types {
		if _, ok := supportedOperationTypes[operationType]; !ok {
			return interfaces.OperationRecordQuery{}, 0, "", ErrInvalidOperationQuery
		}
	}
	statuses := make([]string, 0, len(input.Statuses))
	seenStatuses := map[OperationStatus]struct{}{}
	for _, status := range input.Statuses {
		if _, ok := supportedOperationStatuses[status]; !ok {
			return interfaces.OperationRecordQuery{}, 0, "", ErrInvalidOperationQuery
		}
		if _, exists := seenStatuses[status]; exists {
			continue
		}
		seenStatuses[status] = struct{}{}
		statuses = append(statuses, string(status))
	}
	sort.Strings(statuses)
	filterBytes, _ := json.Marshal(struct {
		Types    []string `json:"types"`
		Statuses []string `json:"statuses"`
	}{Types: types, Statuses: statuses})
	digest := sha256.Sum256(filterBytes)
	return interfaces.OperationRecordQuery{Types: types, Statuses: statuses}, limit, hex.EncodeToString(digest[:]), nil
}

func publicOperation(record interfaces.OperationRecord) OperationSummary {
	return OperationSummary{
		OperationID: record.Type + ":" + record.ID, Type: record.Type,
		Resource: OperationResource{Type: record.ResourceType, ID: record.ResourceID},
		Status:   normalizeOperationStatus(record.Type, record.InternalStatus), Attempts: max(record.Attempts, 0),
		LastAttemptAt: cloneOperationTime(record.LastAttemptAt), NextAttemptAt: cloneOperationTime(record.NextAttemptAt),
		Retryable: record.Retryable, ReconciliationRequired: record.ReconciliationRequired,
		DeadLetter: record.DeadLetter, ErrorCode: safeOperationCode(record.ErrorCode),
		CorrelationID: safeOperationCorrelationID(record.CorrelationID), CreatedAt: record.CreatedAt.UTC(),
		UpdatedAt: record.UpdatedAt.UTC(), CompletedAt: cloneOperationTime(record.CompletedAt),
	}
}

func normalizeOperationStatus(operationType, status string) OperationStatus {
	status = strings.ToLower(strings.TrimSpace(status))
	if status == "reconciliation_required" {
		return OperationStatusReconciliationRequired
	}
	switch operationType {
	case OperationTypeInvoiceRender:
		switch status {
		case "queued":
			return OperationStatusQueued
		case "processing":
			return OperationStatusInProgress
		case "completed", "obsolete":
			return OperationStatusSucceeded
		case "failed":
			return OperationStatusFailed
		}
	case OperationTypeInvoiceDelivery, OperationTypeEmailDelivery:
		switch status {
		case "waiting_for_render", "queued":
			return OperationStatusQueued
		case "processing":
			return OperationStatusInProgress
		case "sent", "delivered":
			return OperationStatusSucceeded
		case "failed", "bounced", "complained":
			return OperationStatusFailed
		}
	case OperationTypeOutbox:
		switch status {
		case "pending", "retrying":
			return OperationStatusQueued
		case "publishing":
			return OperationStatusInProgress
		case "published":
			return OperationStatusSucceeded
		}
	case OperationTypeRazorpayWebhook:
		switch status {
		case "received", "processing":
			return OperationStatusInProgress
		case "processed":
			return OperationStatusSucceeded
		case "rejected":
			return OperationStatusFailed
		}
	default:
		switch status {
		case "pending", "queued", "waiting":
			return OperationStatusQueued
		case "processing", "running", "retrying":
			return OperationStatusInProgress
		case "completed", "succeeded", "sent", "delivered", "processed":
			return OperationStatusSucceeded
		case "failed", "rejected", "bounced", "complained":
			return OperationStatusFailed
		}
	}
	return OperationStatusUnknown
}

func recoveryActions(record interfaces.OperationRecord) []OperationRecoveryAction {
	switch record.Type {
	case OperationTypeInvoiceRender:
		if normalizeOperationStatus(record.Type, record.InternalStatus) == OperationStatusFailed && record.Retryable {
			return []OperationRecoveryAction{{Action: OperationActionRetry, Available: true}}
		}
		return []OperationRecoveryAction{{Action: OperationActionRetry, Available: false, RequirementCode: OperationCodeUnsupportedRecovery}}
	case OperationTypeRazorpayWebhook:
		return []OperationRecoveryAction{
			{Action: OperationActionReprocessWebhook, Available: false, RequirementCode: OperationCodeStepUpRequired},
			{Action: OperationActionReconcile, Available: false, RequirementCode: OperationCodeStepUpRequired},
		}
	case OperationTypeGSTEInvoice, OperationTypeGSTEWayBill, OperationTypeVoiceReconciliation:
		return []OperationRecoveryAction{{Action: OperationActionReconcile, Available: false, RequirementCode: OperationCodeStepUpRequired}}
	case OperationTypeOutbox:
		return []OperationRecoveryAction{{Action: OperationActionRedriveDeadLetter, Available: false, RequirementCode: OperationCodeStepUpRequired}}
	default:
		return []OperationRecoveryAction{{Action: OperationActionRetry, Available: false, RequirementCode: OperationCodeUnsupportedRecovery}}
	}
}

func parsePublicOperationID(value string) (string, string, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 2 {
		return "", "", false
	}
	operationType, operationID := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	_, typeOK := supportedOperationTypes[operationType]
	_, idErr := uuid.Parse(operationID)
	return operationType, operationID, typeOK && idErr == nil
}

func validOperationBusinessID(value string) bool {
	_, err := uuid.Parse(strings.TrimSpace(value))
	return err == nil
}

func normalizedStrings(values []string) []string {
	seen := map[string]struct{}{}
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, exists := seen[value]; exists {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	sort.Strings(result)
	return result
}

func normalizeUnavailableTypes(values []string) []string {
	values = normalizedStrings(values)
	result := values[:0]
	for _, value := range values {
		if _, ok := supportedOperationTypes[value]; ok {
			result = append(result, value)
		}
	}
	return result
}

func sortOperationRecords(records []interfaces.OperationRecord) {
	sort.SliceStable(records, func(i, j int) bool {
		if !records[i].UpdatedAt.Equal(records[j].UpdatedAt) {
			return records[i].UpdatedAt.After(records[j].UpdatedAt)
		}
		if records[i].Type != records[j].Type {
			return records[i].Type < records[j].Type
		}
		return records[i].ID < records[j].ID
	})
}

func encodeOperationCursor(cursor operationCursor) (string, error) {
	payload, err := json.Marshal(cursor)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(payload), nil
}

func decodeOperationCursor(value string) (operationCursor, error) {
	payload, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return operationCursor{}, err
	}
	var cursor operationCursor
	if err := json.Unmarshal(payload, &cursor); err != nil {
		return operationCursor{}, err
	}
	return cursor, nil
}

func safeOperationCode(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > 80 {
		return "operation_error"
	}
	for _, character := range value {
		if (character < 'a' || character > 'z') && (character < '0' || character > '9') && character != '_' && character != '-' {
			return "operation_error"
		}
	}
	return value
}

func safeOperationCorrelationID(value string) string {
	parsed, err := uuid.Parse(strings.TrimSpace(value))
	if err != nil {
		return ""
	}
	return parsed.String()
}

func cloneOperationTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	clone := value.UTC()
	return &clone
}

func validOperationRecoveryInput(input OperationRecoveryInput) bool {
	reason := strings.TrimSpace(input.Reason)
	if input.Action == "" || len(input.Action) > 80 || reason == "" || len(reason) > 500 {
		return false
	}
	if _, err := uuid.Parse(strings.TrimSpace(input.IdempotencyKey)); err != nil {
		return false
	}
	if _, err := uuid.Parse(strings.TrimSpace(input.CorrelationID)); err != nil {
		return false
	}
	return true
}

func operationRecoveryRequestHash(command interfaces.RenderRecoveryCommand) string {
	payload, _ := json.Marshal(struct {
		BusinessID    string `json:"business_id"`
		OperationID   string `json:"operation_id"`
		ActorSubject  string `json:"actor_subject"`
		PrincipalKind string `json:"principal_kind"`
		Action        string `json:"action"`
		Reason        string `json:"reason"`
		CorrelationID string `json:"correlation_id"`
	}{
		BusinessID: command.BusinessID, OperationID: command.OperationID, ActorSubject: command.ActorSubject,
		PrincipalKind: command.PrincipalKind, Action: command.Action, Reason: command.Reason,
		CorrelationID: command.CorrelationID,
	})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func operationRecoveryDecisionHash(decision interfaces.OperationRecoveryDecision) string {
	payload, _ := json.Marshal(struct {
		BusinessID    string `json:"business_id"`
		OperationType string `json:"operation_type"`
		OperationID   string `json:"operation_id"`
		ActorSubject  string `json:"actor_subject"`
		PrincipalKind string `json:"principal_kind"`
		Action        string `json:"action"`
		Reason        string `json:"reason"`
		CorrelationID string `json:"correlation_id"`
	}{
		BusinessID: decision.BusinessID, OperationType: decision.OperationType, OperationID: decision.OperationID,
		ActorSubject: decision.ActorSubject, PrincipalKind: decision.PrincipalKind, Action: decision.Action,
		Reason: decision.Reason, CorrelationID: decision.CorrelationID,
	})
	digest := sha256.Sum256(payload)
	return hex.EncodeToString(digest[:])
}

func highRiskOperationAction(action string) bool {
	switch action {
	case OperationActionReconcile, OperationActionReprocessWebhook,
		OperationActionRedriveDeadLetter, OperationActionResolve:
		return true
	default:
		return false
	}
}
