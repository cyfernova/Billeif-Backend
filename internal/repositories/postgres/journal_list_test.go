package postgres

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestJournalListIncludesLinesWithinBusinessScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	for _, sql := range []string{
		`CREATE TABLE journals (id TEXT PRIMARY KEY, business_id TEXT, name TEXT, posting_date DATETIME, created_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE journal_lines (id TEXT PRIMARY KEY, journal_id TEXT, account_code TEXT, account_name TEXT, entry_type TEXT, amount REAL, currency TEXT, deleted_at DATETIME)`,
		`INSERT INTO journals (id,business_id,name) VALUES ('own','business','Sample'),('other','other-business','Private')`,
		`INSERT INTO journal_lines (id,journal_id,account_code,account_name,entry_type,amount,currency) VALUES ('debit','own','CASH','Cash','debit',100,'INR'),('credit','own','AR','Receivable','credit',100,'INR'),('private','other','BANK','Bank','debit',999,'INR')`,
	} {
		if err := db.Exec(sql).Error; err != nil {
			t.Fatal(err)
		}
	}
	rows, total, err := NewJournalRepository(db).ListByBusinessID(context.Background(), "business", 1, 20)
	if err != nil || total != 1 || len(rows) != 1 {
		t.Fatalf("unexpected journal list: total=%d rows=%d err=%v", total, len(rows), err)
	}
	if len(rows[0].Lines) != 2 {
		t.Fatalf("journal list omitted ledger lines: %+v", rows[0])
	}
	for _, line := range rows[0].Lines {
		if line.JournalID != "own" || line.Amount != 100 {
			t.Fatalf("wrong journal line: %+v", line)
		}
	}
}
