package postgres

import (
	"context"
	"reflect"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestLedgerPaginationOrdersEqualDatesConsistently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(`CREATE TABLE ledger_entries (id TEXT PRIMARY KEY, business_id TEXT, entry_date DATETIME, created_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "c", "b", "d"} {
		if err = db.Exec(`INSERT INTO ledger_entries VALUES (?, ?, ?, ?)`, id, "business", date, date).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err = db.Exec(`INSERT INTO ledger_entries VALUES (?, ?, ?, ?)`, "z", "another-business", date, date).Error; err != nil {
		t.Fatal(err)
	}
	repo := NewLedgerRepository(db)
	for _, limit := range []int{1, 2, 3} {
		var got []string
		for page := 1; ; page++ {
			entries, total, listErr := repo.GetByBusinessID(context.Background(), "business", page, limit)
			if listErr != nil {
				t.Fatal(listErr)
			}
			if total != 4 {
				t.Fatalf("total = %d, want 4", total)
			}
			if len(entries) == 0 {
				break
			}
			for _, entry := range entries {
				got = append(got, entry.ID)
			}
		}
		if !reflect.DeepEqual(got, []string{"d", "c", "b", "a"}) {
			t.Fatalf("limit %d: IDs = %v", limit, got)
		}
	}
}
