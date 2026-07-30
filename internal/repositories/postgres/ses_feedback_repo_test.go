package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/sesfeedback"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

func TestSESFeedbackRepositoryAppliesCorrelatedFeedbackUnderRowLock(t *testing.T) {
	repositoryBase, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := NewSESFeedbackRepository(repositoryBase.db)
	event := feedbackRepositoryEvent(sesfeedback.EventTypeBounce)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status, delivered_at, failed_at.*FROM email_deliveries.*id = \$1.*business_id = \$2.*provider_message_id = \$3.*FOR UPDATE`).
		WithArgs(event.DeliveryID, event.BusinessID, event.ProviderMessageID).
		WillReturnRows(feedbackStatusRows("delivered", nil, nil))
	mock.ExpectExec(`UPDATE email_deliveries.*status = \$1.*failed_at = \$2.*updated_at = NOW\(\).*id = \$3.*business_id = \$4.*provider_message_id = \$5`).
		WithArgs("bounced", event.OccurredAt, event.DeliveryID, event.BusinessID, event.ProviderMessageID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repository.ApplyFeedback(context.Background(), event)

	if err != nil {
		t.Fatalf("apply feedback: %v", err)
	}
	if result.Status != "bounced" || !result.Changed {
		t.Fatalf("result = %#v", result)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestSESFeedbackRepositoryTreatsDuplicateAndLateFeedbackAsNoOp(t *testing.T) {
	for _, test := range []struct {
		name    string
		current string
		event   sesfeedback.EventType
	}{
		{name: "duplicate", current: "bounced", event: sesfeedback.EventTypeBounce},
		{name: "late lower precedence", current: "complained", event: sesfeedback.EventTypeDelivery},
	} {
		t.Run(test.name, func(t *testing.T) {
			repositoryBase, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			repository := NewSESFeedbackRepository(repositoryBase.db)
			event := feedbackRepositoryEvent(test.event)
			deliveredAt, failedAt := interface{}(nil), interface{}(nil)
			if test.event == sesfeedback.EventTypeDelivery {
				deliveredAt = event.OccurredAt
			} else {
				failedAt = event.OccurredAt
			}

			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT status, delivered_at, failed_at.*FOR UPDATE`).
				WithArgs(event.DeliveryID, event.BusinessID, event.ProviderMessageID).
				WillReturnRows(feedbackStatusRows(test.current, deliveredAt, failedAt))
			mock.ExpectCommit()

			result, err := repository.ApplyFeedback(context.Background(), event)

			if err != nil || result.Status != test.current || result.Changed {
				t.Fatalf("result/error = %#v/%v", result, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestSESFeedbackRepositoryRejectsMissingCorrelationAndPreSendState(t *testing.T) {
	tests := []struct {
		name    string
		rows    *sqlmock.Rows
		wantErr error
	}{
		{
			name:    "provider id is still empty",
			rows:    emptyFeedbackStatusRows(),
			wantErr: sesfeedback.ErrFeedbackUncorrelated,
		},
		{
			name:    "provider id mismatch",
			rows:    emptyFeedbackStatusRows(),
			wantErr: sesfeedback.ErrFeedbackUncorrelated,
		},
		{
			name:    "pre-send state",
			rows:    feedbackStatusRows("processing", nil, nil),
			wantErr: sesfeedback.ErrInvalidTransition,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repositoryBase, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			repository := NewSESFeedbackRepository(repositoryBase.db)
			event := feedbackRepositoryEvent(sesfeedback.EventTypeDelivery)

			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT status, delivered_at, failed_at.*FOR UPDATE`).
				WithArgs(event.DeliveryID, event.BusinessID, event.ProviderMessageID).
				WillReturnRows(test.rows)
			mock.ExpectRollback()

			result, err := repository.ApplyFeedback(context.Background(), event)

			if result.Changed || !errors.Is(err, test.wantErr) {
				t.Fatalf("result/error = %#v/%v, want %v", result, err, test.wantErr)
			}
		})
	}
}

func TestSESFeedbackRepositoryPreservesEarliestFailureTimestampAcrossPrecedenceChange(t *testing.T) {
	repositoryBase, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := NewSESFeedbackRepository(repositoryBase.db)
	event := feedbackRepositoryEvent(sesfeedback.EventTypeComplaint)
	earlierFailure := event.OccurredAt.Add(-time.Minute)

	mock.ExpectBegin()
	mock.ExpectQuery(`SELECT status, delivered_at, failed_at.*FOR UPDATE`).
		WithArgs(event.DeliveryID, event.BusinessID, event.ProviderMessageID).
		WillReturnRows(feedbackStatusRows("bounced", nil, earlierFailure))
	mock.ExpectExec(`UPDATE email_deliveries.*status = \$1.*failed_at = \$2.*updated_at = NOW\(\)`).
		WithArgs("complained", earlierFailure, event.DeliveryID, event.BusinessID, event.ProviderMessageID).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	result, err := repository.ApplyFeedback(context.Background(), event)

	if err != nil || result.Status != "complained" || !result.Changed {
		t.Fatalf("result/error = %#v/%v", result, err)
	}
}

func TestSESFeedbackRepositoryBackfillsEarlierTimestampWithoutLoweringStatus(t *testing.T) {
	tests := []struct {
		name        string
		current     string
		event       sesfeedback.EventType
		deliveredAt interface{}
		failedAt    interface{}
		column      string
	}{
		{
			name:    "delivery after complaint backfills delivered timestamp",
			current: "complained", event: sesfeedback.EventTypeDelivery,
			column: "delivered_at",
		},
		{
			name:    "earlier bounce duplicate moves failure timestamp backward",
			current: "bounced", event: sesfeedback.EventTypeBounce,
			failedAt: time.Date(2026, time.July, 30, 10, 2, 2, 0, time.UTC),
			column:   "failed_at",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repositoryBase, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			repository := NewSESFeedbackRepository(repositoryBase.db)
			event := feedbackRepositoryEvent(test.event)

			mock.ExpectBegin()
			mock.ExpectQuery(`SELECT status, delivered_at, failed_at.*FOR UPDATE`).
				WithArgs(event.DeliveryID, event.BusinessID, event.ProviderMessageID).
				WillReturnRows(feedbackStatusRows(test.current, test.deliveredAt, test.failedAt))
			mock.ExpectExec(`UPDATE email_deliveries.*status = \$1.*`+test.column+` = \$2.*updated_at = NOW\(\)`).
				WithArgs(test.current, event.OccurredAt, event.DeliveryID, event.BusinessID, event.ProviderMessageID).
				WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			result, err := repository.ApplyFeedback(context.Background(), event)

			if err != nil || result.Status != test.current || !result.Changed {
				t.Fatalf("result/error = %#v/%v", result, err)
			}
		})
	}
}

func feedbackStatusRows(status string, deliveredAt, failedAt interface{}) *sqlmock.Rows {
	return sqlmock.NewRows([]string{"status", "delivered_at", "failed_at"}).
		AddRow(status, deliveredAt, failedAt)
}

func emptyFeedbackStatusRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{"status", "delivered_at", "failed_at"})
}

func feedbackRepositoryEvent(eventType sesfeedback.EventType) sesfeedback.FeedbackEvent {
	return sesfeedback.FeedbackEvent{
		Type: eventType, BusinessID: uuid.NewString(), DeliveryID: uuid.NewString(),
		ProviderMessageID: "provider-message-1",
		OccurredAt:        time.Date(2026, time.July, 30, 10, 1, 2, 0, time.UTC),
	}
}
