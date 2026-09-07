package postgres

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"github.com/DATA-DOG/go-sqlmock"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestNotificationCreateUsesScopedEventConflictKey(t *testing.T) {
	repository, mock, closeDatabase := newNotificationSQLMockRepository(t)
	defer closeDatabase()
	now := time.Date(2026, time.August, 31, 13, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`INSERT INTO notifications[\s\S]*ON CONFLICT \(business_id, user_id, source_event_key\)[\s\S]*RETURNING`).
		WithArgs("business-a", "user-a", "event-1", "invoice", "Invoice ready", "Open the invoice", "invoice", "invoice-1", now, now).
		WillReturnRows(notificationRows().AddRow("notification-1", "business-a", "user-a", "event-1", "invoice", "Invoice ready", "Open the invoice", "invoice", "invoice-1", nil, now, now))

	created, err := repository.CreateIdempotent(context.Background(), &models.Notification{
		BusinessID: "business-a", UserID: "user-a", SourceEventKey: "event-1", Type: "invoice",
		Title: "Invoice ready", Body: "Open the invoice", ResourceType: "invoice", ResourceID: "invoice-1",
		CreatedAt: now, UpdatedAt: now,
	})
	if err != nil || created.ID != "notification-1" {
		t.Fatalf("CreateIdempotent() = %#v, %v", created, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestNotificationReadMutationsRequireBusinessAndUserScope(t *testing.T) {
	repository, mock, closeDatabase := newNotificationSQLMockRepository(t)
	defer closeDatabase()
	now := time.Date(2026, time.August, 31, 13, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`UPDATE notifications[\s\S]*id = \$3[\s\S]*business_id = \$4[\s\S]*user_id = \$5[\s\S]*RETURNING`).
		WithArgs(now, now, "notification-1", "business-a", "user-a").
		WillReturnRows(notificationRows())

	_, err := repository.MarkRead(context.Background(), "notification-1", "business-a", "user-a", now)
	if !errors.Is(err, interfaces.ErrNotificationNotFound) {
		t.Fatalf("MarkRead() error = %v", err)
	}
	mock.ExpectExec(`UPDATE notifications[\s\S]*business_id = \$3[\s\S]*user_id = \$4[\s\S]*read_at IS NULL`).
		WithArgs(now, now, "business-a", "user-a").
		WillReturnResult(sqlmock.NewResult(0, 2))
	updated, err := repository.MarkAllRead(context.Background(), "business-a", "user-a", now)
	if err != nil || updated != 2 {
		t.Fatalf("MarkAllRead() = %d, %v", updated, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func TestNotificationListRequiresBusinessAndUserScope(t *testing.T) {
	repository, mock, closeDatabase := newNotificationSQLMockRepository(t)
	defer closeDatabase()
	now := time.Date(2026, time.August, 31, 13, 0, 0, 0, time.UTC)
	mock.ExpectQuery(`SELECT \* FROM "notifications" WHERE business_id = \$1 AND user_id = \$2 ORDER BY created_at DESC, id DESC LIMIT \$3`).
		WithArgs("business-a", "user-a", 100).
		WillReturnRows(notificationRows().AddRow("notification-1", "business-a", "user-a", "event-1", "system", "Ready", "Done", "", "", nil, now, now))

	listed, err := repository.List(context.Background(), "business-a", "user-a", 100)
	if err != nil || len(listed) != 1 || listed[0].BusinessID != "business-a" || listed[0].UserID != "user-a" {
		t.Fatalf("List() = %#v, %v", listed, err)
	}
	if err := mock.ExpectationsWereMet(); err != nil {
		t.Fatalf("SQL expectations: %v", err)
	}
}

func notificationRows() *sqlmock.Rows {
	return sqlmock.NewRows([]string{
		"id", "business_id", "user_id", "source_event_key", "type", "title", "body",
		"resource_type", "resource_id", "read_at", "created_at", "updated_at",
	})
}

func newNotificationSQLMockRepository(t *testing.T) (*notificationRepository, sqlmock.Sqlmock, func()) {
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
	return &notificationRepository{db: db}, mock, func() { _ = sqlDB.Close() }
}
