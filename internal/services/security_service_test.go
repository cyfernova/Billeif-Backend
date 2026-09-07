package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
)

type stepUpRepositoryFake struct {
	record   *models.StepUpGrant
	consumed bool
}

func (f *stepUpRepositoryFake) CreateStepUpGrant(_ context.Context, grant *models.StepUpGrant) error {
	copy := *grant
	f.record = &copy
	return nil
}

func (f *stepUpRepositoryFake) ConsumeStepUpGrant(_ context.Context, request interfaces.StepUpConsumeRequest) (bool, error) {
	if f.record == nil || f.consumed || f.record.TokenHash != request.TokenHash ||
		f.record.Subject != request.Subject || f.record.BusinessID != request.BusinessID ||
		f.record.Action != request.Action || f.record.Resource != request.Resource ||
		f.record.CommandHash != request.CommandHash ||
		!f.record.ExpiresAt.After(request.ConsumedAt) {
		return false, nil
	}
	f.consumed = true
	return true, nil
}

func TestStepUpGrantBindsScopeAndIsConsumedExactlyOnce(t *testing.T) {
	now := time.Date(2026, 9, 2, 8, 0, 0, 0, time.UTC)
	repository := &stepUpRepositoryFake{}
	service := NewSecurityService(repository, SecurityServiceOptions{
		Now:   func() time.Time { return now },
		Token: func() (string, error) { return "unit-step-up-token-123456789", nil },
	})
	issued, err := service.IssueStepUp(context.Background(), StepUpIssueRequest{
		Subject: "operator-1", BusinessID: "8cf06379-19ea-41f9-a0ce-230e99978807",
		Action: "reconcile", Resource: "razorpay_webhook:83030e41-57aa-4e41-891b-8db05aa3ef0e",
		CommandIdentity: "command-1",
		Assurance:       AssuranceTOTP, AuthenticatedAt: now.Add(-time.Minute), TTL: 5 * time.Minute,
	})
	if err != nil {
		t.Fatalf("IssueStepUp: %v", err)
	}
	if issued.Token != "unit-step-up-token-123456789" || repository.record == nil || repository.record.TokenHash == issued.Token {
		t.Fatalf("issued=%+v stored=%+v", issued, repository.record)
	}
	request := OperationStepUpRequest{
		OperatorSubject: "operator-1", BusinessID: "8cf06379-19ea-41f9-a0ce-230e99978807",
		Action: "reconcile", OperationID: "razorpay_webhook:83030e41-57aa-4e41-891b-8db05aa3ef0e", Token: issued.Token,
		CommandIdentity: "command-1",
	}
	wrongCommand := request
	wrongCommand.CommandIdentity = "command-2"
	if err := service.VerifyOperationStepUp(context.Background(), wrongCommand); !errors.Is(err, ErrOperationStepUpRequired) {
		t.Fatalf("wrong command error=%v", err)
	}
	if err := service.VerifyOperationStepUp(context.Background(), request); err != nil {
		t.Fatalf("first consume: %v", err)
	}
	if err := service.VerifyOperationStepUp(context.Background(), request); !errors.Is(err, ErrOperationStepUpRequired) {
		t.Fatalf("replay error=%v", err)
	}
}
