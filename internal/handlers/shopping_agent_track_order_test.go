package handlers

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"invoice-backend/internal/models"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

func TestShoppingAgentTrackOrderRestrictsOrdersToAuthenticatedOwner(t *testing.T) {
	gin.SetMode(gin.TestMode)

	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s-%d?mode=memory&cache=shared", t.Name(), time.Now().UnixNano())), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite database: %v", err)
	}
	if err := db.Exec(`
		CREATE TABLE marketplace_orders (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			shopping_agent_id TEXT NOT NULL,
			merchant_agent_id TEXT NOT NULL,
			cart_mandate_id TEXT NOT NULL,
			payment_mandate_id TEXT,
			total_amount DECIMAL(15,2) NOT NULL,
			currency TEXT NOT NULL,
			status TEXT NOT NULL,
			razorpay_order_id TEXT,
			razorpay_payment_id TEXT,
			shipping_address TEXT,
			tracking_number TEXT,
			estimated_delivery DATETIME,
			delivered_at DATETIME,
			created_at DATETIME
		)
	`).Error; err != nil {
		t.Fatalf("create marketplace orders table: %v", err)
	}
	if err := db.Create(&models.MarketplaceOrder{
		ID:              "order-owned-by-user-1",
		UserID:          "user-1",
		ShoppingAgentID: "shopping-agent-1",
		MerchantAgentID: "merchant-agent-1",
		TotalAmount:     42,
		Currency:        "INR",
		Status:          "shipped",
	}).Error; err != nil {
		t.Fatalf("seed marketplace order: %v", err)
	}

	repo := postgresrepo.NewAP2Repository(db)
	service := services.NewShoppingAgentService(repo, nil, nil, nil, nil, nil, "", logger.New())
	handler := NewShoppingAgentHandler(service, repo, logger.New())
	router := gin.New()
	router.GET("/agents/shopping/orders/:id", func(c *gin.Context) {
		c.Set("user_id", c.GetHeader("X-Test-User-ID"))
		handler.TrackOrder(c)
	})

	tests := []struct {
		name       string
		userID     string
		orderID    string
		wantStatus int
		wantBody   string
	}{
		{name: "owner can track their order", userID: "user-1", orderID: "order-owned-by-user-1", wantStatus: http.StatusOK},
		{name: "different user cannot track another users order", userID: "user-2", orderID: "order-owned-by-user-1", wantStatus: http.StatusNotFound, wantBody: `{"error":"order not found"}`},
		{name: "absent order has the same public response", userID: "user-2", orderID: "missing-order", wantStatus: http.StatusNotFound, wantBody: `{"error":"order not found"}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/agents/shopping/orders/"+tt.orderID, nil)
			req.Header.Set("X-Test-User-ID", tt.userID)
			res := httptest.NewRecorder()

			router.ServeHTTP(res, req)

			if res.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d; body=%s", res.Code, tt.wantStatus, res.Body.String())
			}
			if tt.wantBody != "" && res.Body.String() != tt.wantBody {
				t.Fatalf("body = %q, want %q", res.Body.String(), tt.wantBody)
			}
		})
	}
}
