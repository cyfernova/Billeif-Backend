package services

import (
	"context"
	"errors"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"
)

func TestCreateInvoiceSubscriptionRejectsAutoSendUntilIssueDeliveryExists(t *testing.T) {
	service := NewBillingOpsService(nil, nil, nil, nil, nil, nil, nil, nil, logger.New())

	subscription, err := service.CreateInvoiceSubscription(context.Background(), CreateInvoiceSubscriptionInput{
		AutoSend: true,
	})

	if subscription != nil {
		t.Fatalf("subscription = %#v, want nil", subscription)
	}
	if !errors.Is(err, models.ErrInvalidInvoiceLifecycle) {
		t.Fatalf("error = %v, want invalid invoice lifecycle", err)
	}
}

func TestGenerateExistingAutoSendSubscriptionReportsUnsupportedLifecycle(t *testing.T) {
	subscription := &models.InvoiceSubscription{
		Status:   models.InvoiceSubscriptionStatusActive,
		AutoSend: true,
	}

	err := validateInvoiceSubscriptionGeneration(subscription)

	if !errors.Is(err, models.ErrInvalidInvoiceLifecycle) {
		t.Fatalf("error = %v, want invalid invoice lifecycle", err)
	}
}
