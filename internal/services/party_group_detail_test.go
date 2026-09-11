package services

import (
	"context"
	"testing"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestPartyGroupDetailMatchesListBalances(t *testing.T) {
	db, err := gorm.Open(sqlite.Open("file:"+t.Name()+"?mode=memory&cache=shared"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	statements := []string{
		`CREATE TABLE party_groups (id TEXT PRIMARY KEY, business_id TEXT, name TEXT, updated_at DATETIME, deleted_at DATETIME)`,
		`CREATE TABLE party_group_members (id TEXT PRIMARY KEY, party_group_id TEXT, party_type TEXT, party_id TEXT, deleted_at DATETIME)`,
		`CREATE TABLE invoices (id TEXT PRIMARY KEY, business_id TEXT, customer_id TEXT, total REAL, paid_amount REAL, balance_due REAL, deleted_at DATETIME)`,
		`CREATE TABLE documents (id TEXT PRIMARY KEY, business_id TEXT, party_type TEXT, party_id TEXT, total REAL, paid_amount REAL, balance_due REAL, deleted_at DATETIME)`,
		`INSERT INTO party_groups VALUES ('group', 'business', 'QA group', CURRENT_TIMESTAMP, NULL)`,
		`INSERT INTO party_group_members VALUES ('member1', 'group', 'customer', 'customer', NULL), ('member2', 'group', 'vendor', 'vendor', NULL)`,
		`INSERT INTO invoices VALUES ('invoice', 'business', 'customer', 1000, 250, 750, NULL), ('other', 'other-business', 'customer', 9999, 0, 9999, NULL)`,
		`INSERT INTO documents VALUES ('document', 'business', 'vendor', 'vendor', 500, 100, 400, NULL)`,
	}
	for _, statement := range statements {
		if err = db.Exec(statement).Error; err != nil {
			t.Fatal(err)
		}
	}
	svc := &BillingOpsService{db: db}
	ctx := context.Background()
	list, total, err := svc.ListPartyGroups(ctx, "business", 1, 20)
	if err != nil || total != 1 || len(list) != 1 {
		t.Fatalf("list: total=%d rows=%d err=%v", total, len(list), err)
	}
	detail, err := svc.GetPartyGroup(ctx, "business", "group")
	if err != nil {
		t.Fatal(err)
	}
	if detail.ReceivableTotal != 750 || detail.PayableTotal != 400 || detail.NetBalance != 350 || detail.MemberCount != 2 {
		t.Fatalf("detail: receivable=%v payable=%v net=%v members=%d", detail.ReceivableTotal, detail.PayableTotal, detail.NetBalance, detail.MemberCount)
	}
	if detail.ReceivableTotal != list[0].ReceivableTotal || detail.PayableTotal != list[0].PayableTotal {
		t.Fatal("detail balances differ from list")
	}
	if _, err := svc.GetPartyGroup(ctx, "other-business", "group"); err == nil {
		t.Fatal("group exposed across businesses")
	}
}
