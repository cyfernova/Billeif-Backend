package services

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/google/uuid"
)

var (
	ErrInvalidPrivacyRequest = errors.New("invalid privacy request")
	ErrPrivacyRetentionHold  = errors.New("privacy request is in retention hold")
)

type PrivacyExporter interface {
	Export(context.Context, string, string, string) (artifactKey, artifactHash string, err error)
}

type PrivacyExportDownloader interface {
	PresignExport(context.Context, string) (string, error)
}

type ReconciliationPrivacyExporter struct{ Code string }

func (e ReconciliationPrivacyExporter) Export(context.Context, string, string, string) (string, string, error) {
	code := strings.TrimSpace(e.Code)
	if code == "" {
		code = "export_adapter_unavailable"
	}
	return "", "", errors.New(code)
}

type PrivacyPurger interface {
	Purge(context.Context, string, string, string) error
}

type PrivacyCleanupTarget interface {
	Name() string
	Purge(context.Context, string, string, string) error
}

type PrivacyCleanupPreflight interface {
	Preflight(context.Context, string, string, string) error
}

type ReconciliationPrivacyCleanupTarget struct{ Target string }

func (t ReconciliationPrivacyCleanupTarget) Name() string { return strings.TrimSpace(t.Target) }
func (t ReconciliationPrivacyCleanupTarget) Purge(context.Context, string, string, string) error {
	return errors.New("cleanup adapter unavailable")
}

type PrivacyCleanupCoordinator struct{ targets []PrivacyCleanupTarget }

type PrivacyCleanupFailure struct {
	Targets []string
	causes  []error
}

func (e *PrivacyCleanupFailure) Error() string {
	return "privacy cleanup failed: " + strings.Join(e.Targets, ",")
}
func (e *PrivacyCleanupFailure) Unwrap() []error { return e.causes }

func NewPrivacyCleanupCoordinator(targets ...PrivacyCleanupTarget) *PrivacyCleanupCoordinator {
	return &PrivacyCleanupCoordinator{targets: targets}
}

func (c *PrivacyCleanupCoordinator) Purge(ctx context.Context, businessID, subject, requestID string) error {
	if c == nil || len(c.targets) == 0 {
		return errors.New("privacy cleanup targets are not configured")
	}
	var failureTargets []string
	var failures []error
	for _, target := range c.targets {
		preflight, ok := target.(PrivacyCleanupPreflight)
		if !ok {
			continue
		}
		if err := preflight.Preflight(ctx, businessID, subject, requestID); err != nil {
			name := "invalid-target"
			if target != nil && strings.TrimSpace(target.Name()) != "" {
				name = target.Name()
			}
			return &PrivacyCleanupFailure{Targets: []string{name}, causes: []error{fmt.Errorf("%s preflight: %w", name, err)}}
		}
	}
	for _, target := range c.targets {
		if target == nil || strings.TrimSpace(target.Name()) == "" {
			failureTargets = append(failureTargets, "invalid-target")
			failures = append(failures, errors.New("privacy cleanup target is invalid"))
			continue
		}
		if err := target.Purge(ctx, businessID, subject, requestID); err != nil {
			failureTargets = append(failureTargets, target.Name())
			failures = append(failures, fmt.Errorf("%s cleanup: %w", target.Name(), err))
			break
		}
	}
	if len(failures) != 0 {
		return &PrivacyCleanupFailure{Targets: failureTargets, causes: failures}
	}
	return nil
}

type PrivacyService struct {
	repository interfaces.PrivacyRepository
	exporter   PrivacyExporter
	purger     PrivacyPurger
	now        func() time.Time
}

type PrivacyRequestInput struct {
	BusinessID, Subject, Kind, IdempotencyKey string
}

func NewPrivacyService(repository interfaces.PrivacyRepository, exporter PrivacyExporter, purger PrivacyPurger, now func() time.Time) *PrivacyService {
	if now == nil {
		now = time.Now
	}
	return &PrivacyService{repository: repository, exporter: exporter, purger: purger, now: now}
}

func (s *PrivacyService) Request(ctx context.Context, input PrivacyRequestInput) (*models.PrivacyRequest, error) {
	input.BusinessID, input.Subject, input.Kind, input.IdempotencyKey = strings.TrimSpace(input.BusinessID), strings.TrimSpace(input.Subject), strings.ToLower(strings.TrimSpace(input.Kind)), strings.TrimSpace(input.IdempotencyKey)
	if s == nil || s.repository == nil || uuid.Validate(input.BusinessID) != nil || input.Subject == "" ||
		(input.Kind != models.PrivacyRequestExport && input.Kind != models.PrivacyRequestDelete) || input.IdempotencyKey == "" || len(input.IdempotencyKey) > 180 {
		return nil, ErrInvalidPrivacyRequest
	}
	now := s.now().UTC()
	request := &models.PrivacyRequest{
		ID: privacyRequestID(input), BusinessID: input.BusinessID, Subject: input.Subject, Kind: input.Kind,
		Status: models.PrivacyStatusPending, IdempotencyKey: input.IdempotencyKey,
		RequestHash: privacyRequestHash(input), RequestedAt: now,
	}
	if input.Kind == models.PrivacyRequestDelete {
		request.Status = models.PrivacyStatusHold
		purgeAfter := now.Add(30 * 24 * time.Hour)
		request.PurgeAfter = &purgeAfter
	}
	audit := privacyAuditEvent(request, "privacy_requested", "accepted", input.Kind, now)
	stored, _, err := s.repository.CreateOrGetPrivacyRequest(ctx, request, audit)
	if err != nil || stored == nil || stored.RequestHash != request.RequestHash {
		return nil, ErrInvalidPrivacyRequest
	}
	return stored, nil
}

func (s *PrivacyService) Get(ctx context.Context, id, businessID, subject string) (*models.PrivacyRequest, error) {
	if s == nil || s.repository == nil {
		return nil, ErrInvalidPrivacyRequest
	}
	request, err := s.repository.GetPrivacyRequest(ctx, strings.TrimSpace(id), strings.TrimSpace(businessID), strings.TrimSpace(subject))
	if err != nil || request == nil {
		return nil, ErrInvalidPrivacyRequest
	}
	return request, nil
}

func (s *PrivacyService) ExportDownload(ctx context.Context, id, businessID, subject string) (string, error) {
	request, err := s.Get(ctx, id, businessID, subject)
	downloader, ok := s.exporter.(PrivacyExportDownloader)
	if err != nil || !ok || request.Kind != models.PrivacyRequestExport || request.Status != models.PrivacyStatusDone || request.ArtifactKey == "" {
		return "", ErrInvalidPrivacyRequest
	}
	return downloader.PresignExport(ctx, request.ArtifactKey)
}

func (s *PrivacyService) ProcessExport(ctx context.Context, id, businessID, subject string) (*models.PrivacyRequest, error) {
	if s == nil || s.repository == nil || s.exporter == nil {
		return nil, ErrInvalidPrivacyRequest
	}
	now := s.now().UTC()
	stub := &models.PrivacyRequest{ID: id, BusinessID: businessID, Subject: subject}
	request, claimed, err := s.repository.ClaimPrivacyRequest(ctx, id, businessID, subject, models.PrivacyRequestExport, models.PrivacyStatusPending, now, privacyAuditEvent(stub, "privacy_processing", "accepted", "export_started", now))
	if err == nil && !claimed {
		request, claimed, err = s.repository.ClaimPrivacyRequest(ctx, id, businessID, subject, models.PrivacyRequestExport, models.PrivacyStatusRecon, now, privacyAuditEvent(stub, "privacy_processing", "accepted", "export_retry", now))
	}
	if err != nil || !claimed || request == nil {
		return nil, ErrInvalidPrivacyRequest
	}
	key, hash, exportErr := s.exporter.Export(ctx, businessID, subject, id)
	completed := s.now().UTC()
	request.CompletedAt = &completed
	if exportErr != nil {
		request.Status, request.ErrorCode = models.PrivacyStatusRecon, privacyFailureCode("export", exportErr)
	} else {
		request.Status, request.ArtifactKey, request.ArtifactHash, request.ErrorCode = models.PrivacyStatusDone, key, hash, ""
	}
	outcome := "completed"
	if exportErr != nil {
		outcome = "reconciliation_required"
	}
	if err := s.repository.SavePrivacyRequestWithAudit(ctx, request, privacyAuditEvent(request, "privacy_export", outcome, request.ErrorCode, completed)); err != nil {
		return nil, err
	}
	return request, exportErr
}

func (s *PrivacyService) PurgeDeletion(ctx context.Context, id, businessID, subject string) (*models.PrivacyRequest, error) {
	if s == nil || s.repository == nil || s.purger == nil {
		return nil, ErrInvalidPrivacyRequest
	}
	now := s.now().UTC()
	stub := &models.PrivacyRequest{ID: id, BusinessID: businessID, Subject: subject}
	request, claimed, err := s.repository.ClaimPrivacyRequest(ctx, id, businessID, subject, models.PrivacyRequestDelete, models.PrivacyStatusHold, now, privacyAuditEvent(stub, "privacy_processing", "accepted", "purge_started", now))
	if err == nil && !claimed {
		request, claimed, err = s.repository.ClaimPrivacyRequest(ctx, id, businessID, subject, models.PrivacyRequestDelete, models.PrivacyStatusRecon, now, privacyAuditEvent(stub, "privacy_processing", "accepted", "purge_retry", now))
	}
	if err != nil || !claimed || request == nil {
		return nil, ErrPrivacyRetentionHold
	}
	purgeErr := s.purger.Purge(ctx, businessID, subject, id)
	completed := s.now().UTC()
	request.CompletedAt = &completed
	if purgeErr != nil {
		request.Status, request.ErrorCode = models.PrivacyStatusRecon, privacyFailureCode("cleanup", purgeErr)
	} else {
		request.Status, request.ErrorCode = models.PrivacyStatusDone, ""
	}
	outcome := "completed"
	if purgeErr != nil {
		outcome = "reconciliation_required"
	}
	if err := s.repository.SavePrivacyRequestWithAudit(ctx, request, privacyAuditEvent(request, "privacy_purge", outcome, request.ErrorCode, completed)); err != nil {
		return nil, err
	}
	return request, purgeErr
}

func privacyFailureCode(prefix string, err error) string {
	var cleanup *PrivacyCleanupFailure
	if errors.As(err, &cleanup) && len(cleanup.Targets) != 0 {
		parts := make([]string, 0, len(cleanup.Targets))
		for _, target := range cleanup.Targets {
			parts = append(parts, strings.ReplaceAll(safeSecurityCode(target), "-", "_"))
		}
		code := prefix + "_" + strings.Join(parts, "_") + "_failed"
		if len(code) <= 80 {
			return code
		}
	}
	return prefix + "_failed"
}

func privacyRequestHash(input PrivacyRequestInput) string {
	digest := sha256.Sum256([]byte(input.BusinessID + "\x00" + input.Subject + "\x00" + input.Kind))
	return hex.EncodeToString(digest[:])
}

func privacyRequestID(input PrivacyRequestInput) string {
	return uuid.NewSHA1(uuid.NameSpaceOID, []byte(input.BusinessID+"\x00"+input.Subject+"\x00"+input.Kind+"\x00"+input.IdempotencyKey)).String()
}

func privacyAuditEvent(request *models.PrivacyRequest, eventType, outcome, reason string, occurredAt time.Time) *models.SecurityAuditEvent {
	if reason == "" {
		reason = "none"
	}
	eventID := uuid.NewSHA1(uuid.NameSpaceOID, []byte(request.ID+"\x00"+eventType+"\x00"+outcome+"\x00"+reason)).String()
	return &models.SecurityAuditEvent{
		ID: eventID, BusinessID: request.BusinessID, Subject: request.Subject, EventType: eventType,
		ResourceType: "privacy_request", ResourceID: request.ID, Outcome: outcome, ReasonCode: reason, OccurredAt: occurredAt,
	}
}
