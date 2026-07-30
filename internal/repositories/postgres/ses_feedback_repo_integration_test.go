package postgres

import (
	"context"
	"fmt"
	"net/url"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/sesfeedback"

	"github.com/google/uuid"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestSESFeedbackRepositoryPostgresConcurrentOutOfOrderPrecedence(t *testing.T) {
	dsn := strings.TrimSpace(os.Getenv("MIGRATION_TEST_DATABASE_URL"))
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL is not configured; skipping SES feedback PostgreSQL integration")
	}
	parsed, err := url.Parse(dsn)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") {
		t.Fatal("MIGRATION_TEST_DATABASE_URL must be a PostgreSQL URL")
	}
	admin, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{})
	if err != nil {
		t.Fatalf("open disposable PostgreSQL database: %v", err)
	}
	if err := ensureEmptyDisposableDatabase(admin); err != nil {
		t.Fatal(err)
	}
	schema := "ses_feedback_" + strings.ReplaceAll(uuid.NewString(), "-", "")
	if err := admin.Exec(fmt.Sprintf(`CREATE SCHEMA "%s"`, schema)).Error; err != nil {
		t.Fatalf("create isolated schema: %v", err)
	}
	t.Cleanup(func() {
		if err := admin.Exec(fmt.Sprintf(`DROP SCHEMA IF EXISTS "%s" CASCADE`, schema)).Error; err != nil {
			t.Errorf("drop isolated schema: %v", err)
		}
	})
	query := parsed.Query()
	query.Set("search_path", schema)
	parsed.RawQuery = query.Encode()
	database, err := gorm.Open(gormpostgres.Open(parsed.String()), &gorm.Config{})
	if err != nil {
		t.Fatalf("open isolated PostgreSQL schema: %v", err)
	}
	sqlDatabase, err := database.DB()
	if err != nil {
		t.Fatalf("access PostgreSQL pool: %v", err)
	}
	sqlDatabase.SetMaxOpenConns(4)
	if err := database.Exec(`
CREATE TABLE email_deliveries (
	id UUID PRIMARY KEY,
	business_id UUID NOT NULL,
	status VARCHAR(40) NOT NULL,
	provider_message_id VARCHAR(255),
	delivered_at TIMESTAMPTZ,
	failed_at TIMESTAMPTZ,
	updated_at TIMESTAMPTZ NOT NULL,
	deleted_at TIMESTAMPTZ
);`).Error; err != nil {
		t.Fatalf("create feedback integration table: %v", err)
	}

	deliveryID, businessID := uuid.NewString(), uuid.NewString()
	providerMessageID := "provider-message-1"
	seededAt := time.Date(2026, time.July, 30, 9, 59, 0, 0, time.UTC)
	if err := database.Exec(
		`INSERT INTO email_deliveries
		 (id, business_id, status, provider_message_id, updated_at)
		 VALUES (?, ?, 'sent', ?, ?)`,
		deliveryID, businessID, providerMessageID, seededAt,
	).Error; err != nil {
		t.Fatalf("seed feedback delivery: %v", err)
	}

	deliveryAt := time.Date(2026, time.July, 30, 10, 1, 0, 0, time.UTC)
	complaintAt := deliveryAt.Add(time.Minute)
	repository := NewSESFeedbackRepository(database)
	events := []sesfeedback.FeedbackEvent{
		{
			Type: sesfeedback.EventTypeDelivery, BusinessID: businessID,
			DeliveryID: deliveryID, ProviderMessageID: providerMessageID, OccurredAt: deliveryAt,
		},
		{
			Type: sesfeedback.EventTypeComplaint, BusinessID: businessID,
			DeliveryID: deliveryID, ProviderMessageID: providerMessageID, OccurredAt: complaintAt,
		},
	}
	start := make(chan struct{})
	errs := make(chan error, len(events))
	var wait sync.WaitGroup
	for _, event := range events {
		event := event
		wait.Add(1)
		go func() {
			defer wait.Done()
			<-start
			_, applyErr := repository.ApplyFeedback(context.Background(), event)
			errs <- applyErr
		}()
	}
	close(start)
	wait.Wait()
	close(errs)
	for applyErr := range errs {
		if applyErr != nil {
			t.Fatalf("concurrent feedback: %v", applyErr)
		}
	}

	type feedbackRow struct {
		Status      string
		DeliveredAt *time.Time
		FailedAt    *time.Time
		UpdatedAt   time.Time
	}
	var row feedbackRow
	if err := database.Table("email_deliveries").
		Select("status", "delivered_at", "failed_at", "updated_at").
		Where("id = ?", deliveryID).
		Scan(&row).Error; err != nil {
		t.Fatalf("read concurrent feedback: %v", err)
	}
	if row.Status != "complained" ||
		row.DeliveredAt == nil || !row.DeliveredAt.Equal(deliveryAt) ||
		row.FailedAt == nil || !row.FailedAt.Equal(complaintAt) {
		t.Fatalf("concurrent feedback row = %#v", row)
	}

	earlierComplaint := events[1]
	earlierComplaint.OccurredAt = complaintAt.Add(-30 * time.Second)
	if _, err := repository.ApplyFeedback(context.Background(), earlierComplaint); err != nil {
		t.Fatalf("apply earlier complaint duplicate: %v", err)
	}
	if err := database.Table("email_deliveries").
		Select("status", "delivered_at", "failed_at", "updated_at").
		Where("id = ?", deliveryID).
		Scan(&row).Error; err != nil {
		t.Fatalf("read earlier complaint: %v", err)
	}
	updatedAfterEarlier := row.UpdatedAt
	if row.Status != "complained" || row.FailedAt == nil ||
		!row.FailedAt.Equal(earlierComplaint.OccurredAt) {
		t.Fatalf("earlier complaint row = %#v", row)
	}

	laterBounce := events[1]
	laterBounce.Type = sesfeedback.EventTypeBounce
	laterBounce.OccurredAt = complaintAt.Add(time.Minute)
	result, err := repository.ApplyFeedback(context.Background(), laterBounce)
	if err != nil {
		t.Fatalf("apply later bounce: %v", err)
	}
	if result.Changed {
		t.Fatalf("later lower-precedence feedback changed row: %#v", result)
	}
	if err := database.Table("email_deliveries").
		Select("status", "delivered_at", "failed_at", "updated_at").
		Where("id = ?", deliveryID).
		Scan(&row).Error; err != nil {
		t.Fatalf("read later bounce: %v", err)
	}
	if !row.UpdatedAt.Equal(updatedAfterEarlier) {
		t.Fatalf("no-op feedback changed updated_at: %s -> %s", updatedAfterEarlier, row.UpdatedAt)
	}
}
