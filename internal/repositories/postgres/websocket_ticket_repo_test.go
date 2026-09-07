package postgres

import (
	"context"
	"errors"
	"regexp"
	"testing"
	"time"

	"invoice-backend/internal/repositories/interfaces"

	"github.com/DATA-DOG/go-sqlmock"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestWebSocketTicketConsumeUsesOneAtomicConditionalUpdate(t *testing.T) {
	repository, mock, closeDatabase := newWebSocketTicketSQLMockRepository(t)
	defer closeDatabase()

	consumedAt := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	expiresAt := consumedAt.Add(time.Minute)
	query := regexp.QuoteMeta(`UPDATE websocket_tickets
		SET consumed_at = $1, updated_at = $2
		WHERE ticket_digest = $3
		  AND consumed_at IS NULL
		  AND expires_at > $4
		RETURNING id, ticket_digest, subject, business_id, expires_at, consumed_at, created_at, updated_at`)
	mock.ExpectQuery(query).
		WithArgs(consumedAt, consumedAt, "digest-1", consumedAt).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "ticket_digest", "subject", "business_id", "expires_at", "consumed_at", "created_at", "updated_at",
		}).AddRow(
			"ticket-1", "digest-1", "subject-1", "10000000-0000-0000-0000-000000000001",
			expiresAt, consumedAt, consumedAt.Add(-time.Second), consumedAt,
		))

	ticket, err := repository.ConsumeByDigest(context.Background(), "digest-1", consumedAt)
	if err != nil {
		t.Fatalf("ConsumeByDigest() error = %v", err)
	}
	if ticket.Subject != "subject-1" || ticket.BusinessID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("consumed ticket = %#v", ticket)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestWebSocketTicketConsumeMapsNoWinnerToInvalid(t *testing.T) {
	repository, mock, closeDatabase := newWebSocketTicketSQLMockRepository(t)
	defer closeDatabase()

	consumedAt := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`UPDATE websocket_tickets[\s\S]*ticket_digest = \$3[\s\S]*consumed_at IS NULL[\s\S]*expires_at > \$4`).
		WithArgs(consumedAt, consumedAt, "spent-or-expired", consumedAt).
		WillReturnRows(sqlmock.NewRows([]string{
			"id", "ticket_digest", "subject", "business_id", "expires_at", "consumed_at", "created_at", "updated_at",
		}))

	_, err := repository.ConsumeByDigest(context.Background(), "spent-or-expired", consumedAt)
	if !errors.Is(err, interfaces.ErrWebSocketTicketInvalid) {
		t.Fatalf("ConsumeByDigest() error = %v, want ErrWebSocketTicketInvalid", err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func newWebSocketTicketSQLMockRepository(t *testing.T) (*websocketTicketRepository, sqlmock.Sqlmock, func()) {
	t.Helper()
	sqlDB, mock, err := sqlmock.New(sqlmock.QueryMatcherOption(sqlmock.QueryMatcherRegexp))
	if err != nil {
		t.Fatalf("sqlmock.New(): %v", err)
	}
	db, err := gorm.Open(gormpostgres.New(gormpostgres.Config{Conn: sqlDB}), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		_ = sqlDB.Close()
		t.Fatalf("gorm.Open(): %v", err)
	}
	return &websocketTicketRepository{db: db}, mock, func() { _ = sqlDB.Close() }
}
