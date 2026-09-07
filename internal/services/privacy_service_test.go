package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/models"
)

type privacyRepositoryFake struct {
	request *models.PrivacyRequest
	audits  []*models.SecurityAuditEvent
}

func (f *privacyRepositoryFake) CreateOrGetPrivacyRequest(_ context.Context, request *models.PrivacyRequest, audit *models.SecurityAuditEvent) (*models.PrivacyRequest, bool, error) {
	f.audits = append(f.audits, audit)
	if f.request != nil {
		copy := *f.request
		return &copy, false, nil
	}
	copy := *request
	f.request = &copy
	return request, true, nil
}
func (f *privacyRepositoryFake) GetPrivacyRequest(context.Context, string, string, string) (*models.PrivacyRequest, error) {
	copy := *f.request
	return &copy, nil
}
func (f *privacyRepositoryFake) ClaimPrivacyRequest(_ context.Context, id, businessID, subject, kind, fromStatus string, now time.Time, audit *models.SecurityAuditEvent) (*models.PrivacyRequest, bool, error) {
	if f.request == nil || f.request.ID != id || f.request.BusinessID != businessID || f.request.Subject != subject || f.request.Kind != kind || f.request.Status != fromStatus ||
		(kind == models.PrivacyRequestDelete && (f.request.PurgeAfter == nil || f.request.PurgeAfter.After(now))) {
		return nil, false, nil
	}
	f.request.Status = models.PrivacyStatusRunning
	f.request.StartedAt = &now
	f.audits = append(f.audits, audit)
	copy := *f.request
	return &copy, true, nil
}
func (f *privacyRepositoryFake) SavePrivacyRequest(_ context.Context, request *models.PrivacyRequest) error {
	copy := *request
	f.request = &copy
	return nil
}
func (f *privacyRepositoryFake) SavePrivacyRequestWithAudit(ctx context.Context, request *models.PrivacyRequest, audit *models.SecurityAuditEvent) error {
	f.audits = append(f.audits, audit)
	return f.SavePrivacyRequest(ctx, request)
}
func (*privacyRepositoryFake) RecordSecurityAudit(context.Context, *models.SecurityAuditEvent) error {
	return nil
}

type privacyCleanupTargetFake struct {
	name   string
	err    error
	called int
}

type privacyPreflightTargetFake struct {
	privacyCleanupTargetFake
	preflightErr error
}

func (f *privacyPreflightTargetFake) Preflight(context.Context, string, string, string) error {
	return f.preflightErr
}

func (f *privacyCleanupTargetFake) Name() string { return f.name }
func (f *privacyCleanupTargetFake) Purge(context.Context, string, string, string) error {
	f.called++
	return f.err
}

func TestPrivacyDeletionIsIdempotentAndHeldBeforePurge(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	repository := &privacyRepositoryFake{}
	service := NewPrivacyService(repository, nil, nil, func() time.Time { return now })
	input := PrivacyRequestInput{BusinessID: "8cf06379-19ea-41f9-a0ce-230e99978807", Subject: "user-1", Kind: "delete", IdempotencyKey: "delete-1"}
	first, err := service.Request(context.Background(), input)
	if err != nil {
		t.Fatalf("first request: %v", err)
	}
	second, err := service.Request(context.Background(), input)
	if err != nil || second.ID != first.ID {
		t.Fatalf("replay=%+v err=%v", second, err)
	}
	if first.Status != models.PrivacyStatusHold || first.PurgeAfter == nil || !first.PurgeAfter.Equal(now.Add(30*24*time.Hour)) {
		t.Fatalf("request=%+v", first)
	}
}

func TestPrivacyCleanupAttemptsProviderAndObjectTargetsAndRequiresReconciliation(t *testing.T) {
	now := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	repository := &privacyRepositoryFake{}
	provider := &privacyCleanupTargetFake{name: "identity-provider", err: errors.New("provider unavailable")}
	objects := &privacyCleanupTargetFake{name: "object-storage"}
	service := NewPrivacyService(repository, nil, NewPrivacyCleanupCoordinator(provider, objects), func() time.Time { return now })
	request, err := service.Request(context.Background(), PrivacyRequestInput{BusinessID: "8cf06379-19ea-41f9-a0ce-230e99978807", Subject: "user-1", Kind: "delete", IdempotencyKey: "delete-1"})
	if err != nil {
		t.Fatalf("request: %v", err)
	}
	now = now.Add(31 * 24 * time.Hour)
	processed, err := service.PurgeDeletion(context.Background(), request.ID, request.BusinessID, request.Subject)
	if err == nil || processed.Status != models.PrivacyStatusRecon || provider.called != 1 || objects.called != 0 {
		t.Fatalf("processed=%+v provider=%d objects=%d err=%v", processed, provider.called, objects.called, err)
	}
	if len(repository.audits) != 3 || repository.audits[2].Outcome != "reconciliation_required" {
		t.Fatalf("audits=%+v", repository.audits)
	}
	provider.err = nil
	if replay, replayErr := service.PurgeDeletion(context.Background(), request.ID, request.BusinessID, request.Subject); replayErr != nil || replay == nil || replay.Status != models.PrivacyStatusDone || provider.called != 2 || objects.called != 1 {
		t.Fatalf("retry=%+v provider=%d objects=%d err=%v", replay, provider.called, objects.called, replayErr)
	}
}

func TestPrivacyCleanupPreflightAbortsEveryDestructiveTarget(t *testing.T) {
	database := &privacyPreflightTargetFake{privacyCleanupTargetFake: privacyCleanupTargetFake{name: "database"}, preflightErr: errors.New("ownership transfer required")}
	provider := &privacyCleanupTargetFake{name: "identity-provider"}
	err := NewPrivacyCleanupCoordinator(database, provider).Purge(context.Background(), "business", "subject", "request")
	if err == nil || database.called != 0 || provider.called != 0 {
		t.Fatalf("database=%d provider=%d err=%v", database.called, provider.called, err)
	}
}
