package postgres

import (
	"context"
	"reflect"
	"testing"
	"time"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPaymentPaginationOrdersEqualDatesConsistently(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err = db.Exec(`CREATE TABLE payments (id TEXT PRIMARY KEY, business_id TEXT, invoice_id TEXT, payment_date DATETIME, created_at DATETIME, deleted_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	date := time.Date(2026, 9, 6, 0, 0, 0, 0, time.UTC)
	for _, id := range []string{"a", "c", "b"} {
		if err = db.Exec(`INSERT INTO payments VALUES (?, ?, ?, ?, ?, NULL)`, id, "business", "invoice", date, date).Error; err != nil {
			t.Fatal(err)
		}
	}
	repo := NewPaymentRepository(db)
	for _, byInvoice := range []bool{false, true} {
		var got []string
		for page := 1; page <= 3; page++ {
			rows, total, listErr := repo.GetByBusinessID(context.Background(), "business", page, 1)
			if byInvoice {
				rows, total, listErr = repo.GetByInvoiceID(context.Background(), "invoice", page, 1)
			}
			if listErr != nil {
				t.Fatal(listErr)
			}
			if total != 3 || len(rows) != 1 {
				t.Fatalf("page %d: total=%d rows=%d", page, total, len(rows))
			}
			got = append(got, rows[0].ID)
		}
		if !reflect.DeepEqual(got, []string{"c", "b", "a"}) {
			t.Fatalf("byInvoice=%v: IDs=%v", byInvoice, got)
		}
	}
}
