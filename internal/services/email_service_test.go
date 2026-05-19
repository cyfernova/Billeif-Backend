package services

import (
	"context"
	"strings"
	"testing"
	"time"

	"invoice-backend/pkg/logger"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	gormlogger "gorm.io/gorm/logger"
)

func TestEmailServiceCreateAccountDoesNotLookupEmptyUUID(t *testing.T) {
	sqlRecorder := &emailSQLRecorder{Interface: gormlogger.Discard}
	db := newEmailServiceTestDB(t, sqlRecorder)
	svc := &EmailService{db: db, log: logger.New()}

	account, err := svc.UpsertAccount(context.Background(), "biz-1", "", UpsertEmailAccountInput{
		Name:  "Acme Notifications",
		Email: "OWNER@EXAMPLE.COM",
	})
	if err != nil {
		t.Fatalf("create email account: %v", err)
	}

	if account.ID == "" {
		t.Fatal("expected created account to have an id")
	}
	if account.BusinessID != "biz-1" {
		t.Fatalf("expected business id biz-1, got %q", account.BusinessID)
	}
	if account.Email != "owner@example.com" {
		t.Fatalf("expected normalized email, got %q", account.Email)
	}
	if !account.IsDefault {
		t.Fatal("expected first account to be default")
	}

	for _, statement := range sqlRecorder.statements {
		if strings.Contains(statement, "email_accounts") &&
			(strings.Contains(statement, "id = \"\"") || strings.Contains(statement, "id <> \"\"")) {
			t.Fatalf("create account performed empty id lookup: %s", statement)
		}
	}
}

type emailSQLRecorder struct {
	gormlogger.Interface
	statements []string
}

func (r *emailSQLRecorder) Trace(ctx context.Context, begin time.Time, fc func() (string, int64), err error) {
	statement, _ := fc()
	r.statements = append(r.statements, statement)
}

func newEmailServiceTestDB(t *testing.T, log gormlogger.Interface) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open("file:"+strings.ReplaceAll(t.Name(), "/", "_")+"?mode=memory&cache=shared"), &gorm.Config{
		Logger: log,
	})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}

	statements := []string{
		`CREATE TABLE email_accounts (
			id TEXT PRIMARY KEY DEFAULT '11111111-1111-1111-1111-111111111111',
			business_id TEXT NOT NULL,
			name TEXT NOT NULL,
			email TEXT NOT NULL,
			sender_name TEXT,
			reply_to_email TEXT,
			provider TEXT NOT NULL,
			account_type TEXT NOT NULL,
			status TEXT NOT NULL,
			identity_arn TEXT,
			configuration_set TEXT,
			is_default BOOLEAN NOT NULL DEFAULT 0,
			track_deliveries BOOLEAN NOT NULL DEFAULT 1,
			last_tested_at DATETIME,
			verified_at DATETIME,
			metadata TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE email_deliveries (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			email_account_id TEXT,
			status TEXT NOT NULL,
			sent_at DATETIME,
			failed_at DATETIME,
			created_at DATETIME,
			deleted_at DATETIME
		)`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create email service test schema: %v", err)
		}
	}

	return db
}
