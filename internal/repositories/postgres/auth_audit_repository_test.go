package postgres

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"invoice-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLinkUserPhoneWithAuditIsAtomic(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name      string
		withAudit bool
		wantErr   bool
	}{
		{name: "commits phone and audit", withAudit: true},
		{name: "rolls back phone when audit fails", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
			if err != nil {
				t.Fatalf("open sqlite: %v", err)
			}
			if err := db.Exec(`CREATE TABLE users (id TEXT PRIMARY KEY, phone_number TEXT, updated_at DATETIME, deleted_at DATETIME)`).Error; err != nil {
				t.Fatalf("create users: %v", err)
			}
			if tc.withAudit {
				if err := db.Exec(`CREATE TABLE security_audit_events (id TEXT PRIMARY KEY, business_id TEXT, subject TEXT NOT NULL, event_type TEXT NOT NULL, resource_type TEXT NOT NULL, resource_id TEXT NOT NULL, outcome TEXT NOT NULL, reason_code TEXT NOT NULL, occurred_at DATETIME NOT NULL)`).Error; err != nil {
					t.Fatalf("create audits: %v", err)
				}
			}
			if err := db.Exec(`INSERT INTO users (id, phone_number) VALUES ('user-1', '')`).Error; err != nil {
				t.Fatalf("seed user: %v", err)
			}
			event := &models.SecurityAuditEvent{
				ID: uuid.NewString(), Subject: "user-1", EventType: "phone_link_confirmed", ResourceType: "phone_auth",
				ResourceID: "phone:hash", Outcome: "completed", ReasonCode: "otp_verified", OccurredAt: time.Now().UTC(),
			}
			linked, linkErr := NewSecurityRepository(db).(*securityRepository).LinkUserPhoneWithAudit(context.Background(), "user-1", "+919876543210", time.Now().UTC(), event)
			if tc.wantErr {
				if linkErr == nil || linked {
					t.Fatalf("linked=%t err=%v, want atomic failure", linked, linkErr)
				}
			} else if linkErr != nil || !linked {
				t.Fatalf("linked=%t err=%v, want success", linked, linkErr)
			}
			var phone string
			if err := db.Raw(`SELECT phone_number FROM users WHERE id = 'user-1'`).Scan(&phone).Error; err != nil {
				t.Fatalf("read phone: %v", err)
			}
			if tc.wantErr && phone != "" {
				t.Fatalf("phone escaped rollback: %q", phone)
			}
			if !tc.wantErr && phone != "+919876543210" {
				t.Fatalf("phone=%q, want linked phone", phone)
			}
		})
	}
}

func TestRecordSecurityAuditAllowsAccountEventWithoutBusiness(t *testing.T) {
	t.Parallel()

	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := db.Exec(`CREATE TABLE security_audit_events (id TEXT PRIMARY KEY, business_id TEXT, subject TEXT NOT NULL, event_type TEXT NOT NULL, resource_type TEXT NOT NULL, resource_id TEXT NOT NULL, outcome TEXT NOT NULL, reason_code TEXT NOT NULL, occurred_at DATETIME NOT NULL)`).Error; err != nil {
		t.Fatalf("create audit table: %v", err)
	}
	event := &models.SecurityAuditEvent{
		ID: uuid.NewString(), Subject: "phone:hash", EventType: "phone_login_started", ResourceType: "phone_auth",
		ResourceID: "phone:hash", Outcome: "rejected", ReasonCode: "authentication_failed", OccurredAt: time.Now().UTC(),
	}
	if err := NewSecurityRepository(db).RecordSecurityAudit(context.Background(), event); err != nil {
		t.Fatalf("record account audit: %v", err)
	}
	var businessID sql.NullString
	if err := db.Raw(`SELECT business_id FROM security_audit_events WHERE id = ?`, event.ID).Scan(&businessID).Error; err != nil {
		t.Fatalf("read account audit: %v", err)
	}
	if businessID.Valid {
		t.Fatalf("business_id = %q, want NULL", businessID.String)
	}
}
