package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"invoice-backend/internal/models"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/razorpay"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeSubscriptionProvider struct {
	createInput   razorpay.SubscriptionCreateParams
	created       *razorpay.Subscription
	createErr     error
	createCalls   int
	updateInput   razorpay.SubscriptionUpdateParams
	updateResult  *razorpay.Subscription
	updateErr     error
	updateStarted chan struct{}
	updateRelease chan struct{}
	cancelInput   razorpay.SubscriptionCancelParams
	cancelResult  *razorpay.Subscription
	cancelErr     error
	fetchResult   *razorpay.Subscription
	fetchErr      error
	fetchCalls    int
}

func (p *fakeSubscriptionProvider) CreateSubscription(_ context.Context, input razorpay.SubscriptionCreateParams) (*razorpay.Subscription, error) {
	p.createCalls++
	p.createInput = input
	return p.created, p.createErr
}

func (p *fakeSubscriptionProvider) FetchSubscription(context.Context, string) (*razorpay.Subscription, error) {
	p.fetchCalls++
	return p.fetchResult, p.fetchErr
}

func TestSubscriptionMaintenanceRepairsMissingChargeAndExpiresGraceWithoutDeletingData(t *testing.T) {
	db := newSubscriptionLifecycleTestDB(t)
	now := time.Date(2026, 10, 8, 12, 0, 0, 0, time.UTC)
	periodStart := now.Add(-37 * 24 * time.Hour)
	periodEnd := now.Add(-7 * 24 * time.Hour)
	businessReconcile, businessGrace := uuid.NewString(), uuid.NewString()
	businessLiveReconcile, businessLiveGrace := uuid.NewString(), uuid.NewString()
	reconcileID, graceID := uuid.NewString(), uuid.NewString()
	liveReconcileID, liveGraceID := uuid.NewString(), uuid.NewString()
	reconcile := models.Subscription{
		ID: reconcileID, BusinessID: businessReconcile, Plan: "starter", PlanCode: "pro", CatalogVersion: CurrentSubscriptionCatalogVersion,
		Status: models.SubscriptionStatusReconciliationRequired, BillingMode: models.SubscriptionBillingModeRenewable,
		ProviderMode: models.ProviderModeTest, ProviderSubscriptionID: "sub_reconcile_fixture", ProviderPlanID: "plan_pro_test_01",
		PeriodStart: &periodStart, PeriodEnd: &periodEnd, StartDate: periodStart, EndDate: &periodEnd,
		LastProviderPaidCount: 1, ReconciliationCode: "missing_webhook", LifecycleVersion: 1,
	}
	graceDeadline := now.Add(-time.Minute)
	grace := models.Subscription{
		ID: graceID, BusinessID: businessGrace, Plan: "starter", PlanCode: "pro", CatalogVersion: CurrentSubscriptionCatalogVersion,
		Status: models.SubscriptionStatusGracePeriod, BillingMode: models.SubscriptionBillingModeRenewable,
		ProviderMode: models.ProviderModeTest, ProviderSubscriptionID: "sub_grace_fixture", ProviderPlanID: "plan_pro_test_01",
		PeriodStart: &periodStart, PeriodEnd: &periodEnd, GraceDeadline: &graceDeadline, StartDate: periodStart, EndDate: &periodEnd, LifecycleVersion: 1,
	}
	liveReconcile := reconcile
	liveReconcile.ID, liveReconcile.BusinessID = liveReconcileID, businessLiveReconcile
	liveReconcile.ProviderMode, liveReconcile.ProviderSubscriptionID = models.ProviderModeLive, "sub_live_reconcile_fixture"
	liveGrace := grace
	liveGrace.ID, liveGrace.BusinessID = liveGraceID, businessLiveGrace
	liveGrace.ProviderMode, liveGrace.ProviderSubscriptionID = models.ProviderModeLive, "sub_live_grace_fixture"
	require.NoError(t, db.Create(&reconcile).Error)
	require.NoError(t, db.Create(&grace).Error)
	require.NoError(t, db.Create(&liveReconcile).Error)
	require.NoError(t, db.Create(&liveGrace).Error)
	newStart, newEnd := periodEnd, periodEnd.Add(30*24*time.Hour)
	provider := &fakeSubscriptionProvider{fetchResult: &razorpay.Subscription{
		ID: "sub_reconcile_fixture", PlanID: "plan_pro_test_01", CustomerID: "cust_fixture", Status: "active",
		CurrentStart: newStart.Unix(), CurrentEnd: newEnd.Unix(), PaidCount: 2,
		Notes: map[string]string{"business_id": businessReconcile, "subscription_id": reconcileID, "provider_mode": "test"},
	}}
	service := NewSubscriptionLifecycleService(postgresrepo.NewSubscriptionLifecycleRepository(db), provider, SubscriptionLifecycleConfig{
		ProviderMode: models.ProviderModeTest, ProviderPlanIDs: map[string]string{"pro_monthly": "plan_pro_test_01"}, Now: func() time.Time { return now },
	}, logger.New())

	result, err := service.RunMaintenance(context.Background(), 20)
	require.NoError(t, err)
	require.Equal(t, SubscriptionMaintenanceResult{Reconciled: 1, Suspended: 1}, result)
	require.Equal(t, 1, provider.fetchCalls)
	var repaired, suspended models.Subscription
	require.NoError(t, db.First(&repaired, "id = ?", reconcileID).Error)
	require.Equal(t, models.SubscriptionStatusActive, repaired.Status)
	require.EqualValues(t, 2, repaired.LastProviderPaidCount)
	require.Equal(t, newStart, *repaired.PeriodStart)
	require.NoError(t, db.First(&suspended, "id = ?", graceID).Error)
	require.Equal(t, models.SubscriptionStatusSuspended, suspended.Status)
	var untouchedLiveReconcile, untouchedLiveGrace models.Subscription
	require.NoError(t, db.First(&untouchedLiveReconcile, "id = ?", liveReconcileID).Error)
	require.NoError(t, db.First(&untouchedLiveGrace, "id = ?", liveGraceID).Error)
	require.Equal(t, models.SubscriptionStatusReconciliationRequired, untouchedLiveReconcile.Status)
	require.Equal(t, models.SubscriptionStatusGracePeriod, untouchedLiveGrace.Status)
	var billing []models.SubscriptionBillingRecord
	require.NoError(t, db.Where("subscription_id = ?", reconcileID).Find(&billing).Error)
	require.Len(t, billing, 1)
	require.Equal(t, "provider_verified", billing[0].Status)
}

func (p *fakeSubscriptionProvider) UpdateSubscription(_ context.Context, _ string, input razorpay.SubscriptionUpdateParams) (*razorpay.Subscription, error) {
	p.updateInput = input
	if p.updateStarted != nil {
		close(p.updateStarted)
	}
	if p.updateRelease != nil {
		<-p.updateRelease
	}
	return p.updateResult, p.updateErr
}

func (p *fakeSubscriptionProvider) CancelSubscription(_ context.Context, _ string, input razorpay.SubscriptionCancelParams) (*razorpay.Subscription, error) {
	p.cancelInput = input
	return p.cancelResult, p.cancelErr
}

func TestStartRenewableSubscriptionCreatesPendingProviderSubscriptionWithoutEarlyEntitlement(t *testing.T) {
	db := newSubscriptionLifecycleTestDB(t)
	businessID := uuid.NewString()
	require.NoError(t, db.Create(&models.Subscription{
		ID: uuid.NewString(), BusinessID: businessID, Plan: "free", PlanCode: "free",
		CatalogVersion: CurrentSubscriptionCatalogVersion, Status: models.SubscriptionStatusActive,
		BillingMode: models.SubscriptionBillingModeFree, StartDate: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC),
	}).Error)
	provider := &fakeSubscriptionProvider{created: &razorpay.Subscription{
		ID: "sub_internal_fixture", PlanID: "plan_pro_test_01", Status: "created",
		ShortURL: "https://rzp.io/i/checkout-fixture", CreatedAt: 1788278400,
	}}
	service := NewSubscriptionLifecycleService(
		postgresrepo.NewSubscriptionLifecycleRepository(db), provider,
		SubscriptionLifecycleConfig{
			ProviderMode:    models.ProviderModeTest,
			ProviderPlanIDs: map[string]string{"pro_monthly": "plan_pro_test_01"},
			Now:             func() time.Time { return time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC) },
		}, logger.New(),
	)

	response, err := service.StartRenewable(context.Background(), businessID, "user-a", StartRenewableSubscriptionInput{
		PlanID: "pro_monthly", IdempotencyKey: "command-a",
	})

	require.NoError(t, err)
	require.Equal(t, models.SubscriptionStatusPendingPayment, response.Status)
	require.Equal(t, "pro_monthly", response.PendingPlanID)
	require.Equal(t, "https://rzp.io/i/checkout-fixture", response.AuthorizationURL)
	require.EqualValues(t, 1200, provider.createInput.TotalCount)
	require.Equal(t, "plan_pro_test_01", provider.createInput.PlanID)
	require.Equal(t, businessID, provider.createInput.Notes["business_id"])
	require.Equal(t, response.SubscriptionID, provider.createInput.Notes["subscription_id"])
	require.Equal(t, "test", provider.createInput.Notes["provider_mode"])

	var stored models.Subscription
	require.NoError(t, db.First(&stored, "id = ?", response.SubscriptionID).Error)
	require.Equal(t, "sub_internal_fixture", stored.ProviderSubscriptionID)
	require.Equal(t, "plan_pro_test_01", stored.ProviderPlanID)
	require.Equal(t, "free", subscriptionPlanForSubscription(&stored, time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)).ID)
	raw, err := json.Marshal(response)
	require.NoError(t, err)
	require.NotContains(t, string(raw), "sub_internal_fixture")
	require.NotContains(t, string(raw), "plan_pro_test_01")
}

func TestSignedSubscriptionChargedActivatesOnceAndReplayReturnsPriorOutcome(t *testing.T) {
	db := newSubscriptionLifecycleTestDB(t)
	businessID := uuid.NewString()
	localID := uuid.NewString()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&models.Subscription{
		ID: localID, BusinessID: businessID, Plan: "free", PlanCode: "free",
		CatalogVersion: CurrentSubscriptionCatalogVersion, Status: models.SubscriptionStatusPendingPayment,
		BillingMode: models.SubscriptionBillingModeRenewable, ProviderMode: models.ProviderModeTest,
		ProviderSubscriptionID: "sub_internal_fixture", ProviderPlanID: "plan_pro_test_01",
		PendingPlanID: "pro_monthly", PendingProviderPlanID: "plan_pro_test_01",
		StartDate: now, LifecycleVersion: 1,
	}).Error)
	service := NewSubscriptionLifecycleService(
		postgresrepo.NewSubscriptionLifecycleRepository(db), &fakeSubscriptionProvider{},
		SubscriptionLifecycleConfig{
			ProviderMode: models.ProviderModeTest, WebhookSecret: "webhook-secret",
			ProviderPlanIDs: map[string]string{"pro_monthly": "plan_pro_test_01"},
			Now:             func() time.Time { return now },
		}, logger.New(),
	)
	raw := []byte(`{"event":"subscription.charged","created_at":1788264000,"payload":{"subscription":{"entity":{"id":"sub_internal_fixture","plan_id":"plan_pro_test_01","status":"active","current_start":1788264000,"current_end":1790856000,"charge_at":1790856000,"paid_count":1,"notes":{"business_id":"` + businessID + `","subscription_id":"` + localID + `","plan_id":"pro_monthly","provider_mode":"test"}}},"payment":{"entity":{"id":"pay_internal_fixture","invoice_id":"inv_internal_fixture","amount":29900,"currency":"INR","status":"captured","captured":true}}}}`)
	signature := hmacHex(string(raw), "webhook-secret")

	first, err := service.HandleWebhook(context.Background(), signature, "event-internal-fixture", raw)
	require.NoError(t, err)
	require.Equal(t, "processed", first.Status)
	require.False(t, first.Duplicate)
	second, err := service.HandleWebhook(context.Background(), signature, "event-internal-fixture", raw)
	require.NoError(t, err)
	require.True(t, second.Duplicate)
	require.Equal(t, first.Status, second.Status)

	var stored models.Subscription
	require.NoError(t, db.First(&stored, "id = ?", localID).Error)
	require.Equal(t, models.SubscriptionStatusActive, stored.Status)
	require.Equal(t, models.SubscriptionBillingModeRenewable, stored.BillingMode)
	require.Equal(t, "pro", stored.PlanCode)
	require.Empty(t, stored.PendingPlanID)
	require.NotNil(t, stored.PeriodStart)
	require.NotNil(t, stored.PeriodEnd)
	require.Equal(t, time.Unix(1788264000, 0).UTC(), *stored.PeriodStart)
	require.Equal(t, time.Unix(1790856000, 0).UTC(), *stored.PeriodEnd)
	require.EqualValues(t, 1, stored.LastProviderPaidCount)

	var billing []models.SubscriptionBillingRecord
	require.NoError(t, db.Find(&billing).Error)
	require.Len(t, billing, 1)
	require.EqualValues(t, 29900, billing[0].AmountMinor)
	require.Equal(t, "INR", billing[0].Currency)
	require.Equal(t, "paid", billing[0].Status)
	require.NotEmpty(t, billing[0].ReceiptReference)
	require.Equal(t, stored.PeriodStart, billing[0].QuotaPeriodStart)
	require.Equal(t, stored.PeriodEnd, billing[0].QuotaPeriodEnd)

	var inbox models.RazorpayWebhookEvent
	require.NoError(t, db.First(&inbox).Error)
	require.True(t, inbox.SignatureVerified)
	require.Len(t, inbox.PayloadHash, 64)
	require.Equal(t, "processed", inbox.ProcessingStatus)
	require.Equal(t, 1, inbox.ReplayCount)
	require.NotNil(t, inbox.LastReplayedAt)
}

func TestConcurrentFirstDeliveryAppliesSubscriptionEventExactlyOnce(t *testing.T) {
	db := newSubscriptionLifecycleTestDB(t)
	businessID, localID := uuid.NewString(), uuid.NewString()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&models.Subscription{
		ID: localID, BusinessID: businessID, Plan: "free", PlanCode: "free",
		CatalogVersion: CurrentSubscriptionCatalogVersion, Status: models.SubscriptionStatusPendingPayment,
		BillingMode: models.SubscriptionBillingModeRenewable, ProviderMode: models.ProviderModeTest,
		ProviderSubscriptionID: "sub_concurrent_fixture", ProviderPlanID: "plan_pro_test_01",
		PendingPlanID: "pro_monthly", PendingProviderPlanID: "plan_pro_test_01",
		StartDate: now, LifecycleVersion: 1,
	}).Error)
	service := NewSubscriptionLifecycleService(postgresrepo.NewSubscriptionLifecycleRepository(db), &fakeSubscriptionProvider{}, SubscriptionLifecycleConfig{
		ProviderMode: models.ProviderModeTest, WebhookSecret: "webhook-secret",
		ProviderPlanIDs: map[string]string{"pro_monthly": "plan_pro_test_01"},
		Now:             func() time.Time { return now },
	}, logger.New())
	raw := []byte(`{"event":"subscription.charged","created_at":1788264000,"payload":{"subscription":{"entity":{"id":"sub_concurrent_fixture","plan_id":"plan_pro_test_01","status":"active","current_start":1788264000,"current_end":1790856000,"paid_count":1,"notes":{"business_id":"` + businessID + `","subscription_id":"` + localID + `","plan_id":"pro_monthly","provider_mode":"test"}}},"payment":{"entity":{"id":"pay_concurrent_fixture","amount":29900,"currency":"INR","status":"captured","captured":true}}}}`)
	signature := hmacHex(string(raw), "webhook-secret")

	const deliveries = 8
	start := make(chan struct{})
	type outcome struct {
		result *SubscriptionWebhookResult
		err    error
	}
	outcomes := make(chan outcome, deliveries)
	for range deliveries {
		go func() {
			<-start
			result, err := service.HandleWebhook(context.Background(), signature, "event-concurrent-fixture", raw)
			outcomes <- outcome{result: result, err: err}
		}()
	}
	close(start)
	firstCount := 0
	for range deliveries {
		observed := <-outcomes
		require.NoError(t, observed.err)
		require.NotNil(t, observed.result)
		if !observed.result.Duplicate {
			firstCount++
		}
	}
	require.Equal(t, 1, firstCount)
	var billingCount int64
	require.NoError(t, db.Model(&models.SubscriptionBillingRecord{}).Count(&billingCount).Error)
	require.EqualValues(t, 1, billingCount)
	var inbox models.RazorpayWebhookEvent
	require.NoError(t, db.First(&inbox).Error)
	require.Equal(t, deliveries-1, inbox.ReplayCount)
}

func TestConcurrentPlanChangeAndCancellationSerializesAtNoProrationBoundary(t *testing.T) {
	db := newSubscriptionLifecycleTestDB(t)
	businessID := uuid.NewString()
	localID := uuid.NewString()
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	periodStart := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	periodEnd := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&models.Subscription{
		ID: localID, BusinessID: businessID, Plan: "starter", PlanCode: "pro",
		CatalogVersion: CurrentSubscriptionCatalogVersion, Status: models.SubscriptionStatusActive,
		BillingMode: models.SubscriptionBillingModeRenewable, ProviderMode: models.ProviderModeTest,
		ProviderSubscriptionID: "sub_internal_fixture", ProviderPlanID: "plan_pro_test_01",
		PeriodStart: &periodStart, PeriodEnd: &periodEnd, NextRenewalAt: &periodEnd,
		StartDate: periodStart, EndDate: &periodEnd, NextBillingDate: &periodEnd,
		LastProviderPaidCount: 1, LifecycleVersion: 1,
	}).Error)
	provider := &fakeSubscriptionProvider{
		updateStarted: make(chan struct{}), updateRelease: make(chan struct{}),
		updateResult: &razorpay.Subscription{
			ID: "sub_internal_fixture", PlanID: "plan_pro_test_01", Status: "active",
			HasScheduledChanges: true, ScheduleChangeAt: "cycle_end", ChangeScheduledAt: periodEnd.Unix(),
		},
	}
	service := NewSubscriptionLifecycleService(
		postgresrepo.NewSubscriptionLifecycleRepository(db), provider,
		SubscriptionLifecycleConfig{
			ProviderMode: models.ProviderModeTest,
			ProviderPlanIDs: map[string]string{
				"pro_monthly": "plan_pro_test_01", "rise_monthly": "plan_rise_test_01",
			},
			Now: func() time.Time { return now },
		}, logger.New(),
	)
	upgradeResult := make(chan error, 1)
	go func() {
		_, err := service.SchedulePlanChange(context.Background(), businessID, "user-a", ChangeSubscriptionPlanInput{
			PlanID: "rise_monthly", IdempotencyKey: "upgrade-a",
		})
		upgradeResult <- err
	}()
	<-provider.updateStarted

	_, cancelErr := service.ScheduleCancellation(context.Background(), businessID, "user-a", ScheduleSubscriptionCancellationInput{
		IdempotencyKey: "cancel-a",
	})
	require.ErrorIs(t, cancelErr, ErrSubscriptionLifecycleConflict)
	close(provider.updateRelease)
	require.NoError(t, <-upgradeResult)
	require.Equal(t, "plan_rise_test_01", provider.updateInput.PlanID)
	require.Equal(t, "cycle_end", provider.updateInput.ScheduleChangeAt)
	require.False(t, provider.updateInput.CustomerNotify)

	var stored models.Subscription
	require.NoError(t, db.First(&stored, "id = ?", localID).Error)
	require.Equal(t, models.SubscriptionStatusRenewalPending, stored.Status)
	require.Equal(t, "pro", stored.PlanCode, "higher plan must not be granted before the verified boundary event")
	require.Equal(t, "rise_monthly", stored.PendingPlanID)
	require.Equal(t, periodEnd, *stored.PendingPlanEffectiveAt)
	require.False(t, stored.CancelAtPeriodEnd)
}

func TestImmediateProviderCancellationPersistenceFailureRequiresReconciliation(t *testing.T) {
	db := newSubscriptionLifecycleTestDB(t)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	periodStart, periodEnd := now.AddDate(0, 0, -9), now.AddDate(0, 1, -9)
	businessID, localID := uuid.NewString(), uuid.NewString()
	require.NoError(t, db.Create(&models.Subscription{
		ID: localID, BusinessID: businessID, Plan: "starter", PlanCode: "pro",
		CatalogVersion: CurrentSubscriptionCatalogVersion, Status: models.SubscriptionStatusActive,
		BillingMode: models.SubscriptionBillingModeRenewable, ProviderMode: models.ProviderModeTest,
		ProviderSubscriptionID: "sub_internal_fixture", ProviderPlanID: "plan_pro_test_01",
		PeriodStart: &periodStart, PeriodEnd: &periodEnd, StartDate: periodStart, LifecycleVersion: 1,
	}).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_cancelled_subscription
		BEFORE UPDATE OF status ON subscriptions
		WHEN NEW.status = 'cancelled'
		BEGIN SELECT RAISE(FAIL, 'fixture cancellation persistence failure'); END`).Error)
	provider := &fakeSubscriptionProvider{cancelResult: &razorpay.Subscription{
		ID: "sub_internal_fixture", Status: "cancelled",
	}}
	service := NewSubscriptionLifecycleService(postgresrepo.NewSubscriptionLifecycleRepository(db), provider, SubscriptionLifecycleConfig{
		ProviderMode:    models.ProviderModeTest,
		ProviderPlanIDs: map[string]string{"pro_monthly": "plan_pro_test_01"},
		Now:             func() time.Time { return now },
	}, logger.New())

	_, err := service.ScheduleCancellation(context.Background(), businessID, "user-a", ScheduleSubscriptionCancellationInput{
		IdempotencyKey: "cancel-persistence-failure",
	})

	require.ErrorIs(t, err, ErrSubscriptionProviderUnknown)
	var stored models.Subscription
	require.NoError(t, db.First(&stored, "id = ?", localID).Error)
	require.Equal(t, models.SubscriptionStatusReconciliationRequired, stored.Status)
	require.Equal(t, "cancellation_persistence_unknown", stored.ReconciliationCode)
}

func TestSubscriptionWebhookRejectsInvalidSignatureWrongModeAndWrongPlanWithoutEntitlement(t *testing.T) {
	tests := []struct {
		name           string
		serviceMode    string
		providerPlanID string
		signature      string
		wantCode       string
		wantError      error
	}{
		{name: "invalid signature", serviceMode: "test", providerPlanID: "plan_pro_test_01", signature: "wrong", wantCode: "invalid_signature", wantError: ErrInvalidSubscriptionWebhookSignature},
		{name: "test live mismatch", serviceMode: "live", providerPlanID: "plan_pro_test_01", wantCode: "provider_mode_mismatch", wantError: ErrSubscriptionProviderUnknown},
		{name: "wrong plan", serviceMode: "test", providerPlanID: "plan_rise_test_01", wantCode: "provider_plan_mismatch", wantError: ErrSubscriptionProviderUnknown},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newSubscriptionLifecycleTestDB(t)
			now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
			businessID, localID := uuid.NewString(), uuid.NewString()
			require.NoError(t, db.Create(&models.Subscription{
				ID: localID, BusinessID: businessID, Plan: "free", PlanCode: "free", CatalogVersion: CurrentSubscriptionCatalogVersion,
				Status: models.SubscriptionStatusPendingPayment, BillingMode: models.SubscriptionBillingModeRenewable,
				ProviderMode: "test", ProviderSubscriptionID: "sub_fixture", ProviderPlanID: "plan_pro_test_01",
				PendingPlanID: "pro_monthly", PendingProviderPlanID: "plan_pro_test_01", StartDate: now, LifecycleVersion: 1,
			}).Error)
			service := NewSubscriptionLifecycleService(postgresrepo.NewSubscriptionLifecycleRepository(db), &fakeSubscriptionProvider{}, SubscriptionLifecycleConfig{
				ProviderMode: tc.serviceMode, WebhookSecret: "secret", ProviderPlanIDs: map[string]string{"pro_monthly": "plan_pro_test_01", "rise_monthly": "plan_rise_test_01"}, Now: func() time.Time { return now },
			}, logger.New())
			raw := []byte(fmt.Sprintf(`{"event":"subscription.activated","created_at":1788264000,"payload":{"subscription":{"entity":{"id":"sub_fixture","plan_id":"%s","status":"active","current_start":1788264000,"current_end":1790856000,"paid_count":0,"notes":{"business_id":"%s","subscription_id":"%s","provider_mode":"test"}}}}}`, tc.providerPlanID, businessID, localID))
			signature := tc.signature
			if signature == "" {
				signature = hmacHex(string(raw), "secret")
			}
			_, err := service.HandleWebhook(context.Background(), signature, "event-fixture", raw)
			require.ErrorIs(t, err, tc.wantError)
			var stored models.Subscription
			require.NoError(t, db.First(&stored, "id = ?", localID).Error)
			require.Equal(t, "free", stored.PlanCode)
			var inbox models.RazorpayWebhookEvent
			require.NoError(t, db.First(&inbox).Error)
			require.Equal(t, tc.wantCode, inbox.SanitizedErrorCode)
			if tc.wantError == ErrInvalidSubscriptionWebhookSignature {
				require.Empty(t, inbox.BusinessID, "unverified input must not resolve tenant identity")
			} else {
				require.Equal(t, businessID, inbox.BusinessID)
			}
		})
	}
}

func TestSubscriptionWebhookStaleFailureCannotRegressAndCancellationChargeRaceReconciles(t *testing.T) {
	for _, cancellationRace := range []bool{false, true} {
		t.Run(fmt.Sprintf("cancellation_race_%t", cancellationRace), func(t *testing.T) {
			db := newSubscriptionLifecycleTestDB(t)
			now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
			lastEvent := now.Add(-time.Hour)
			businessID, localID := uuid.NewString(), uuid.NewString()
			status := models.SubscriptionStatusActive
			if cancellationRace {
				status = models.SubscriptionStatusCancellationScheduled
			}
			require.NoError(t, db.Create(&models.Subscription{
				ID: localID, BusinessID: businessID, Plan: "starter", PlanCode: "pro", CatalogVersion: CurrentSubscriptionCatalogVersion,
				Status: status, BillingMode: models.SubscriptionBillingModeRenewable, ProviderMode: "test",
				ProviderSubscriptionID: "sub_fixture", ProviderPlanID: "plan_pro_test_01", StartDate: now.AddDate(0, -1, 0),
				LastProviderEventAt: &lastEvent, LastProviderPaidCount: 1, LifecycleVersion: 1,
			}).Error)
			service := NewSubscriptionLifecycleService(postgresrepo.NewSubscriptionLifecycleRepository(db), &fakeSubscriptionProvider{}, SubscriptionLifecycleConfig{
				ProviderMode: "test", WebhookSecret: "secret", ProviderPlanIDs: map[string]string{"pro_monthly": "plan_pro_test_01"}, Now: func() time.Time { return now },
			}, logger.New())
			eventName, occurred, payment := "subscription.halted", lastEvent.Add(-time.Minute).Unix(), ""
			if cancellationRace {
				eventName, occurred = "subscription.charged", now.Unix()
				payment = `,"payment":{"entity":{"id":"pay_fixture","amount":29900,"currency":"INR","status":"captured","captured":true}}`
			}
			raw := []byte(fmt.Sprintf(`{"event":"%s","created_at":%d,"payload":{"subscription":{"entity":{"id":"sub_fixture","plan_id":"plan_pro_test_01","status":"active","current_start":1788264000,"current_end":1790856000,"paid_count":2,"notes":{"business_id":"%s","subscription_id":"%s","provider_mode":"test"}}}%s}}`, eventName, occurred, businessID, localID, payment))
			result, err := service.HandleWebhook(context.Background(), hmacHex(string(raw), "secret"), "event-fixture", raw)
			var stored models.Subscription
			require.NoError(t, db.First(&stored, "id = ?", localID).Error)
			if cancellationRace {
				require.ErrorIs(t, err, ErrSubscriptionProviderUnknown)
				require.Equal(t, models.SubscriptionStatusReconciliationRequired, stored.Status)
				require.Equal(t, "cancellation_charge_race", result.Code)
			} else {
				require.NoError(t, err)
				require.Equal(t, models.SubscriptionStatusActive, stored.Status)
				require.Equal(t, "stale_event_ignored", result.Code)
			}
		})
	}
}

func TestSubscriptionProviderTimeoutIsRecordedForReconciliationAndNeverBlindlyRetried(t *testing.T) {
	db := newSubscriptionLifecycleTestDB(t)
	businessID := uuid.NewString()
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	require.NoError(t, db.Create(&models.Subscription{ID: uuid.NewString(), BusinessID: businessID, Plan: "free", PlanCode: "free", Status: "active", BillingMode: "free", StartDate: now}).Error)
	provider := &fakeSubscriptionProvider{createErr: errors.New("provider timeout")}
	service := NewSubscriptionLifecycleService(postgresrepo.NewSubscriptionLifecycleRepository(db), provider, SubscriptionLifecycleConfig{
		ProviderMode: "test", ProviderPlanIDs: map[string]string{"pro_monthly": "plan_fixture", "rise_monthly": "plan_rise_fixture"}, Now: func() time.Time { return now },
	}, logger.New())
	input := StartRenewableSubscriptionInput{PlanID: "pro_monthly", IdempotencyKey: "command-fixture"}
	_, firstErr := service.StartRenewable(context.Background(), businessID, "user", input)
	_, replayErr := service.StartRenewable(context.Background(), businessID, "user", input)
	require.ErrorIs(t, firstErr, ErrSubscriptionProviderUnknown)
	require.ErrorIs(t, replayErr, ErrSubscriptionProviderUnknown)
	require.Equal(t, 1, provider.createCalls)
	var stored models.Subscription
	require.NoError(t, db.First(&stored, "business_id = ?", businessID).Error)
	require.Equal(t, models.SubscriptionStatusReconciliationRequired, stored.Status)
}

func TestPendingRenewableCheckoutRejectsSecondActorBeforeAnotherProviderMutation(t *testing.T) {
	db := newSubscriptionLifecycleTestDB(t)
	now := time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC)
	businessID := uuid.NewString()
	require.NoError(t, db.Create(&models.Subscription{ID: uuid.NewString(), BusinessID: businessID, Plan: "free", PlanCode: "free", Status: "active", BillingMode: "free", StartDate: now}).Error)
	provider := &fakeSubscriptionProvider{created: &razorpay.Subscription{ID: "sub_fixture", PlanID: "plan_fixture"}}
	service := NewSubscriptionLifecycleService(postgresrepo.NewSubscriptionLifecycleRepository(db), provider, SubscriptionLifecycleConfig{
		ProviderMode: "test", ProviderPlanIDs: map[string]string{"pro_monthly": "plan_fixture", "rise_monthly": "plan_rise_fixture"}, Now: func() time.Time { return now },
	}, logger.New())
	_, err := service.StartRenewable(context.Background(), businessID, "user-a", StartRenewableSubscriptionInput{PlanID: "pro_monthly", IdempotencyKey: "first"})
	require.NoError(t, err)
	_, err = service.StartRenewable(context.Background(), businessID, "user-b", StartRenewableSubscriptionInput{PlanID: "rise_monthly", IdempotencyKey: "second"})
	require.ErrorIs(t, err, ErrSubscriptionLifecycleConflict)
	require.Equal(t, 1, provider.createCalls)
}

func TestOverQuotaDowngradeSchedulesWithoutDeletingUsage(t *testing.T) {
	db := newSubscriptionLifecycleTestDB(t)
	now := time.Date(2026, 9, 10, 12, 0, 0, 0, time.UTC)
	periodStart, periodEnd := now.AddDate(0, 0, -9), now.AddDate(0, 1, -9)
	businessID, localID := uuid.NewString(), uuid.NewString()
	require.NoError(t, db.Create(&models.Subscription{ID: localID, BusinessID: businessID, Plan: "enterprise", PlanCode: "biz", CatalogVersion: CurrentSubscriptionCatalogVersion, Status: "active", BillingMode: "renewable", ProviderMode: "test", ProviderSubscriptionID: "sub_fixture", ProviderPlanID: "plan_biz", PeriodStart: &periodStart, PeriodEnd: &periodEnd, StartDate: periodStart, LifecycleVersion: 1}).Error)
	require.NoError(t, db.Create(&models.SubscriptionQuotaUsage{BusinessID: businessID, FeatureKey: QuotaInvoices, PeriodStart: periodStart, UsedValue: 250}).Error)
	provider := &fakeSubscriptionProvider{updateResult: &razorpay.Subscription{ID: "sub_fixture", HasScheduledChanges: true, ScheduleChangeAt: "cycle_end"}}
	service := NewSubscriptionLifecycleService(postgresrepo.NewSubscriptionLifecycleRepository(db), provider, SubscriptionLifecycleConfig{ProviderMode: "test", ProviderPlanIDs: map[string]string{"pro_monthly": "plan_pro", "biz_monthly": "plan_biz"}, Now: func() time.Time { return now }}, logger.New())
	_, err := service.SchedulePlanChange(context.Background(), businessID, "user", ChangeSubscriptionPlanInput{PlanID: "pro_monthly", IdempotencyKey: "downgrade"})
	require.NoError(t, err)
	var usage models.SubscriptionQuotaUsage
	require.NoError(t, db.First(&usage, "business_id = ?", businessID).Error)
	require.EqualValues(t, 250, usage.UsedValue)
	var stored models.Subscription
	require.NoError(t, db.First(&stored, "id = ?", localID).Error)
	require.Equal(t, "biz", stored.PlanCode)
	require.Equal(t, "pro_monthly", stored.PendingPlanID)
}

func TestFailedRenewalEntersPastDueWithBoundedGrace(t *testing.T) {
	db := newSubscriptionLifecycleTestDB(t)
	now := time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)
	periodStart, periodEnd := now.AddDate(0, -1, 0), now
	businessID, localID := uuid.NewString(), uuid.NewString()
	require.NoError(t, db.Create(&models.Subscription{
		ID: localID, BusinessID: businessID, Plan: "starter", PlanCode: "pro", CatalogVersion: CurrentSubscriptionCatalogVersion,
		Status: models.SubscriptionStatusRenewalPending, BillingMode: models.SubscriptionBillingModeRenewable,
		ProviderMode: "test", ProviderSubscriptionID: "sub_fixture", ProviderPlanID: "plan_pro",
		PeriodStart: &periodStart, PeriodEnd: &periodEnd, StartDate: periodStart, EndDate: &periodEnd,
		LastProviderPaidCount: 1, LifecycleVersion: 1,
	}).Error)
	service := NewSubscriptionLifecycleService(postgresrepo.NewSubscriptionLifecycleRepository(db), &fakeSubscriptionProvider{}, SubscriptionLifecycleConfig{
		ProviderMode: "test", ProviderPlanIDs: map[string]string{"pro_monthly": "plan_pro"}, WebhookSecret: "secret",
		GracePeriod: 7 * 24 * time.Hour, Now: func() time.Time { return now },
	}, logger.New())
	raw := []byte(fmt.Sprintf(`{"event":"subscription.pending","created_at":%d,"payload":{"subscription":{"entity":{"id":"sub_fixture","plan_id":"plan_pro","status":"pending","current_start":%d,"current_end":%d,"paid_count":1,"notes":{"business_id":"%s","subscription_id":"%s","provider_mode":"test"}}}}}`, now.Unix(), periodStart.Unix(), periodEnd.Unix(), businessID, localID))
	_, err := service.HandleWebhook(context.Background(), hmacHex(string(raw), "secret"), "event-pending", raw)
	require.NoError(t, err)
	var stored models.Subscription
	require.NoError(t, db.First(&stored, "id = ?", localID).Error)
	require.Equal(t, models.SubscriptionStatusPastDue, stored.Status)
	require.Equal(t, now.Add(7*24*time.Hour), *stored.GraceDeadline)
	require.Equal(t, "pro", subscriptionPlanForSubscription(&stored, now.Add(time.Hour)).PlanCode)
	require.Equal(t, "free", subscriptionPlanForSubscription(&stored, stored.GraceDeadline.Add(time.Second)).PlanCode)
}

func newSubscriptionLifecycleTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{DisableForeignKeyConstraintWhenMigrating: true})
	require.NoError(t, err)
	statements := []string{
		`CREATE TABLE subscriptions (
			id TEXT PRIMARY KEY, business_id TEXT NOT NULL, plan TEXT NOT NULL, plan_code TEXT,
			catalog_version TEXT, status TEXT NOT NULL, billing_mode TEXT NOT NULL, provider_mode TEXT,
			provider_customer_id TEXT, provider_subscription_id TEXT, provider_plan_id TEXT,
			max_invoices INTEGER, max_customers INTEGER, max_users INTEGER, max_storage_mb INTEGER,
			start_date DATETIME NOT NULL, end_date DATETIME, next_billing_date DATETIME,
			period_start DATETIME, period_end DATETIME, next_renewal_at DATETIME, grace_deadline DATETIME,
			cancel_at_period_end NUMERIC, cancellation_effective_at DATETIME, cancelled_at DATETIME,
			pending_plan_id TEXT, pending_provider_plan_id TEXT, pending_plan_effective_at DATETIME,
			last_provider_event_at DATETIME, last_provider_paid_count INTEGER DEFAULT 0,
			reconciliation_code TEXT, lifecycle_version INTEGER DEFAULT 1,
			created_at DATETIME, updated_at DATETIME, deleted_at DATETIME
		)`,
		`CREATE UNIQUE INDEX ux_test_subscription_provider ON subscriptions(provider_mode, provider_subscription_id)`,
		`CREATE TABLE subscription_commands (
			id TEXT PRIMARY KEY, business_id TEXT NOT NULL, subscription_id TEXT, actor_user_id TEXT NOT NULL,
			action TEXT NOT NULL, idempotency_key TEXT NOT NULL, request_hash TEXT NOT NULL, status TEXT NOT NULL,
			sanitized_error_code TEXT, created_at DATETIME, updated_at DATETIME, completed_at DATETIME,
			UNIQUE (business_id, actor_user_id, action, idempotency_key)
		)`,
		`CREATE TABLE subscription_audit_records (
			id TEXT PRIMARY KEY, business_id TEXT NOT NULL, subscription_id TEXT, actor_user_id TEXT,
			action TEXT NOT NULL, from_status TEXT, to_status TEXT, from_plan_id TEXT, to_plan_id TEXT,
			provider_mode TEXT, provider_event_id TEXT, sanitized_code TEXT NOT NULL,
			occurred_at DATETIME NOT NULL, created_at DATETIME
		)`,
		`CREATE TABLE subscription_billing_records (
			id TEXT PRIMARY KEY, business_id TEXT NOT NULL, subscription_id TEXT NOT NULL, provider_mode TEXT NOT NULL,
			provider_event_id TEXT, provider_invoice_id TEXT, provider_payment_id TEXT, amount_minor INTEGER NOT NULL,
			currency TEXT NOT NULL, status TEXT NOT NULL, receipt_reference TEXT NOT NULL, period_start DATETIME,
			period_end DATETIME, quota_period_start DATETIME, quota_period_end DATETIME,
			occurred_at DATETIME NOT NULL, created_at DATETIME, UNIQUE(provider_mode, provider_event_id)
		)`,
		`CREATE TABLE razorpay_webhook_events (
			id TEXT PRIMARY KEY, razorpay_event_id TEXT NOT NULL, provider_mode TEXT NOT NULL, event_type TEXT NOT NULL,
			payload_hash TEXT NOT NULL, signature_verified NUMERIC NOT NULL, received_at DATETIME NOT NULL,
			provider_occurred_at DATETIME, processing_status TEXT NOT NULL, attempt_count INTEGER DEFAULT 0,
			sanitized_error_code TEXT, processed_at DATETIME, business_id TEXT, subscription_id TEXT,
			replay_count INTEGER DEFAULT 0, last_replayed_at DATETIME, created_at DATETIME,
			UNIQUE(provider_mode, razorpay_event_id)
		)`,
		`CREATE TABLE subscription_quota_usage (
			business_id TEXT NOT NULL, feature_key TEXT NOT NULL, period_start DATE NOT NULL,
			used_value INTEGER NOT NULL DEFAULT 0, created_at DATETIME, updated_at DATETIME,
			PRIMARY KEY (business_id, feature_key, period_start)
		)`,
	}
	for _, statement := range statements {
		require.NoError(t, db.Exec(statement).Error)
	}
	return db
}
