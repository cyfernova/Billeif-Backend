package services

import (
	"testing"
	"time"
)

func TestNewWebSocketConnectionBindsBusinessAndTwoHourTTL(t *testing.T) {
	now := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	record, err := newWebSocketConnection(
		"connection-1",
		"subject-1",
		" 10000000-0000-0000-0000-000000000001 ",
		now,
	)
	if err != nil {
		t.Fatalf("newWebSocketConnection() error = %v", err)
	}
	if record.BusinessID != "10000000-0000-0000-0000-000000000001" {
		t.Fatalf("business ID = %q", record.BusinessID)
	}
	if record.TTL != now.Add(maxWebSocketConnectionLifetime).Unix() {
		t.Fatalf("TTL = %d, want %d", record.TTL, now.Add(maxWebSocketConnectionLifetime).Unix())
	}
}

func TestNewWebSocketConnectionRequiresBusinessScope(t *testing.T) {
	if _, err := newWebSocketConnection("connection-1", "subject-1", "", time.Now()); err == nil {
		t.Fatal("expected missing business scope to fail")
	}
}
