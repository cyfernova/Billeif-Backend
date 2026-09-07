package handlers

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestAccountingAccountsUseValidatedBusinessScope(t *testing.T) {
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Exec(`CREATE TABLE accounting_accounts (id TEXT PRIMARY KEY, business_id TEXT, code TEXT, name TEXT, account_class TEXT, parent_code TEXT, created_at DATETIME, updated_at DATETIME)`).Error; err != nil {
		t.Fatal(err)
	}
	for _, row := range []models.AccountingAccount{{ID: "first", BusinessID: "validated-business", Code: "CASH", Name: "Cash", AccountClass: "asset"}, {ID: "second", BusinessID: "claim-business", Code: "BANK", Name: "Other business bank", AccountClass: "asset"}} {
		if err := db.Create(&row).Error; err != nil {
			t.Fatal(err)
		}
	}
	handler := NewAccountingHandler(services.NewAccountingService(db, nil, nil))
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/accounting/accounts", nil)
	c.Set("validated_business_id", "validated-business")
	c.Set("business_id", "claim-business")
	handler.ListAccounts(c)
	var rows []models.AccountingAccount
	if w.Code != http.StatusOK || json.Unmarshal(w.Body.Bytes(), &rows) != nil || len(rows) != 1 || rows[0].Code != "CASH" {
		t.Fatalf("wrong business accounts: status=%d body=%s", w.Code, w.Body.String())
	}
}

func TestAccountingAccountsRejectMissingBusinessScope(t *testing.T) {
	handler := NewAccountingHandler(nil)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Request = httptest.NewRequest(http.MethodGet, "/accounting/accounts", nil)
	handler.ListAccounts(c)
	if w.Code != http.StatusForbidden {
		t.Fatalf("expected missing scope rejection, got %d", w.Code)
	}
}
