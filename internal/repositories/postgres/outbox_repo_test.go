package postgres

import (
	"context"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/DATA-DOG/go-sqlmock"
	"github.com/google/uuid"
)

const claimOutboxSQL = `WITH ready AS .*SELECT id.*FROM outbox_events.*published_at IS NULL.*available_at <= \$[0-9]+.*lease_expires_at IS NULL OR lease_expires_at <= \$[0-9]+.*ORDER BY available_at, created_at, id.*LIMIT \$[0-9]+.*FOR UPDATE SKIP LOCKED.*UPDATE outbox_events AS events.*publish_attempts = events.publish_attempts \+ 1.*RETURNING events\.\*`

func TestOutboxRepositoryClaimUsesOrderedSkipLockedLeaseAndIncrementsAttempts(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &OutboxRepository{db: invoiceRepository.db}
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(2 * time.Minute)
	eventID := uuid.NewString()
	businessID := uuid.NewString()

	mock.ExpectQuery(claimOutboxSQL).
		WithArgs(now, now, 25, "owner-1", leaseUntil).
		WillReturnRows(outboxClaimRows().
			AddRow(
				eventID,
				businessID,
				"invoice",
				uuid.NewString(),
				"invoice.issued.v1",
				`{"schema_version":1}`,
				4,
				now,
				"owner-1",
				leaseUntil,
				nil,
				now.Add(-time.Minute),
			))

	events, err := repository.ClaimOutboxEvents(
		context.Background(),
		"owner-1",
		now,
		leaseUntil,
		25,
	)

	if err != nil {
		t.Fatalf("claim outbox events: %v", err)
	}
	if len(events) != 1 || events[0].ID != eventID || events[0].PublishAttempts != 4 ||
		events[0].LeaseOwner == nil || *events[0].LeaseOwner != "owner-1" {
		t.Fatalf("claimed events = %#v", events)
	}
}

func TestOutboxRepositoryConcurrentClaimsCanReturnDisjointSkipLockedRows(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	leaseUntil := now.Add(2 * time.Minute)
	eventIDs := []string{uuid.NewString(), uuid.NewString()}
	repositories := make([]*OutboxRepository, 0, 2)
	for index, owner := range []string{"owner-a", "owner-b"} {
		invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
		defer closeDatabase()
		repositories = append(repositories, &OutboxRepository{db: invoiceRepository.db})
		mock.ExpectQuery(claimOutboxSQL).
			WithArgs(now, now, 1, owner, leaseUntil).
			WillReturnRows(outboxClaimRows().
				AddRow(
					eventIDs[index],
					uuid.NewString(),
					"invoice",
					uuid.NewString(),
					"invoice.issued.v1",
					`{"schema_version":1}`,
					1,
					now,
					owner,
					leaseUntil,
					nil,
					now.Add(-time.Minute),
				))
	}

	var wg sync.WaitGroup
	results := make(chan string, 2)
	errs := make(chan error, 2)
	for index, owner := range []string{"owner-a", "owner-b"} {
		repository := repositories[index]
		owner := owner
		wg.Add(1)
		go func() {
			defer wg.Done()
			events, err := repository.ClaimOutboxEvents(
				context.Background(),
				owner,
				now,
				leaseUntil,
				1,
			)
			if err != nil {
				errs <- err
				return
			}
			results <- events[0].ID
		}()
	}
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatalf("concurrent claim: %v", err)
	}
	got := map[string]bool{}
	for eventID := range results {
		got[eventID] = true
	}
	if len(got) != 2 || !got[eventIDs[0]] || !got[eventIDs[1]] {
		t.Fatalf("concurrent claim IDs = %#v, want %#v", got, eventIDs)
	}
}

func TestOutboxRepositoryCompleteAndRetryRequireExactLeaseOwner(t *testing.T) {
	now := time.Date(2026, time.July, 30, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name string
		call func(*OutboxRepository, string) error
		sql  string
	}{
		{
			name: "complete",
			call: func(repository *OutboxRepository, eventID string) error {
				return repository.CompleteOutboxEvent(context.Background(), eventID, "owner-1", now)
			},
			sql: `UPDATE "outbox_events" SET .*"lease_expires_at"=\$[0-9]+.*"lease_owner"=\$[0-9]+.*"published_at"=\$[0-9]+.*WHERE id = \$[0-9]+ AND published_at IS NULL AND lease_owner = \$[0-9]+`,
		},
		{
			name: "retry",
			call: func(repository *OutboxRepository, eventID string) error {
				return repository.RetryOutboxEvent(
					context.Background(),
					eventID,
					"owner-1",
					now.Add(4*time.Minute),
				)
			},
			sql: `UPDATE "outbox_events" SET .*"available_at"=\$[0-9]+.*"lease_expires_at"=\$[0-9]+.*"lease_owner"=\$[0-9]+.*WHERE id = \$[0-9]+ AND published_at IS NULL AND lease_owner = \$[0-9]+`,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
			defer closeDatabase()
			repository := &OutboxRepository{db: invoiceRepository.db}
			eventID := uuid.NewString()
			mock.ExpectBegin()
			mock.ExpectExec(test.sql).WillReturnResult(sqlmock.NewResult(0, 1))
			mock.ExpectCommit()

			if err := test.call(repository, eventID); err != nil {
				t.Fatalf("%s outbox event: %v", test.name, err)
			}
			if err := mock.ExpectationsWereMet(); err != nil {
				t.Fatalf("SQL expectations: %v", err)
			}
		})
	}
}

func TestOutboxRepositoryRejectsStaleLeaseOwnerCAS(t *testing.T) {
	invoiceRepository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	repository := &OutboxRepository{db: invoiceRepository.db}
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "outbox_events".*lease_owner = \$[0-9]+`).
		WillReturnResult(sqlmock.NewResult(0, 0))
	mock.ExpectCommit()

	err := repository.CompleteOutboxEvent(
		context.Background(),
		uuid.NewString(),
		"stale-owner",
		time.Now().UTC(),
	)

	if err == nil || !strings.Contains(err.Error(), "lease owner") {
		t.Fatalf("stale owner error = %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestImmediateOutboxMarkerOnlyCompletesUnleasedEvents(t *testing.T) {
	repository, mock, closeDatabase := newStrictIssueSQLMockRepository(t)
	defer closeDatabase()
	mock.ExpectBegin()
	mock.ExpectExec(`UPDATE "outbox_events".*WHERE id = \$[0-9]+ AND published_at IS NULL AND lease_owner IS NULL AND lease_expires_at IS NULL`).
		WillReturnResult(sqlmock.NewResult(0, 1))
	mock.ExpectCommit()

	if err := repository.MarkOutboxPublished(
		context.Background(),
		uuid.NewString(),
		time.Now().UTC(),
	); err != nil {
		t.Fatalf("mark immediate outbox published: %v", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func outboxClaimRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id",
		"business_id",
		"aggregate_type",
		"aggregate_id",
		"event_type",
		"payload",
		"publish_attempts",
		"available_at",
		"lease_owner",
		"lease_expires_at",
		"published_at",
		"created_at",
	})
}
