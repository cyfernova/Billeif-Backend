package services

import (
	"context"
	"reflect"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestActivityPaginationOrdersEqualTimestampsConsistently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(`CREATE TABLE activity_logs (id TEXT PRIMARY KEY, business_id TEXT, entity_type TEXT, created_at DATETIME, deleted_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "c", "b"} {
		if err = db.Exec(`INSERT INTO activity_logs VALUES (?, ?, ?, ?, NULL)`, id, "business", "invoice", date).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Exec(`INSERT INTO activity_logs VALUES (?, ?, ?, ?, NULL)`, "z", "business", "payment", date).Error; err != nil {
		t.Fatal(err)
	}
	svc := &BillingOpsService{db: db}
	var got []string
	for page := 1; page <= 3; page++ {
		rows, total, listErr := svc.ListActivityLogs(context.Background(), ActivityLogFilter{BusinessID: "business", EntityType: "invoice", Page: page, Limit: 1})
		if listErr != nil {
			t.Fatal(listErr)
		}
		if total != 3 || len(rows) != 1 {
			t.Fatalf("page %d: total=%d rows=%d", page, total, len(rows))
		}
		got = append(got, rows[0].ID)
	}
	if !reflect.DeepEqual(got, []string{"c", "b", "a"}) {
		t.Fatalf("IDs=%v", got)
	}
}
