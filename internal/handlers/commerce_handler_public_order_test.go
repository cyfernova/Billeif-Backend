package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"testing"
	"time"

	"invoice-backend/internal/models"

	"github.com/gin-gonic/gin"
	"gorm.io/gorm"
)

type publicOrderServiceStub struct {
	order *models.StoreOrder
	err   error
}

func (s publicOrderServiceStub) GetPublicOrder(_ context.Context, _, _ string) (*models.StoreOrder, error) {
	return s.order, s.err
}

func TestCommerceHandlerPublicOrderSerializesOnlyAllowlistedFields(t *testing.T) {
	branchID := "branch-internal"
	customerID := "customer-internal"
	couponID := "coupon-internal"
	productID := "product-internal"
	variantID := "variant-internal"
	warehouseID := "warehouse-internal"
	orderedAt := time.Date(2026, time.March, 1, 10, 30, 0, 0, time.UTC)
	paidAt := time.Date(2026, time.March, 2, 10, 30, 0, 0, time.UTC)
	cancelledAt := time.Date(2026, time.March, 3, 10, 30, 0, 0, time.UTC)

	order := &models.StoreOrder{
		ID:                 "order-internal",
		BusinessID:         "business-internal",
		StorefrontID:       "storefront-internal",
		BranchID:           &branchID,
		CustomerID:         &customerID,
		CouponID:           &couponID,
		SalesOrderID:       publicOrderPointer("sales-order-internal"),
		SalesInvoiceID:     publicOrderPointer("sales-invoice-internal"),
		PublicToken:        "public-token-secret",
		OrderNumber:        "ORDER-2026-001",
		Status:             models.StoreOrderStatusPaid,
		PaymentStatus:      models.StoreOrderPaymentStatusPaid,
		PaymentMethod:      "online",
		Currency:           "INR",
		ExchangeRate:       82.35,
		FXProvider:         "internal-fx-provider",
		FXBaseCurrency:     "USD",
		FXQuoteCurrency:    "INR",
		FXRateTimestamp:    &orderedAt,
		Subtotal:           100,
		DiscountTotal:      10,
		TaxTotal:           16.2,
		ShippingTotal:      20,
		Total:              126.2,
		Snapshot:           `{"cost_price":42}`,
		BillingAddress:     `{"street":"private billing address"}`,
		ShippingAddress:    `{"street":"private shipping address"}`,
		Notes:              "private order note",
		IdempotencyKey:     "idempotency-secret",
		ExternalOrderID:    "external-order-internal",
		ExternalPaymentID:  "external-payment-internal",
		GatewayOrderID:     "gateway-order-secret",
		GatewayPaymentID:   "gateway-payment-secret",
		WebhookReference:   "webhook-reference-secret",
		OrderedAt:          orderedAt,
		PaidAt:             &paidAt,
		CancelledAt:        &cancelledAt,
		CancellationReason: "internal cancellation reason",
		CreatedAt:          orderedAt,
		UpdatedAt:          cancelledAt,
		Lines: []*models.StoreOrderLine{{
			ID:             "line-internal",
			StoreOrderID:   "order-internal",
			ProductID:      &productID,
			VariantID:      &variantID,
			WarehouseID:    &warehouseID,
			Title:          "Public product title",
			SKU:            "PUBLIC-SKU",
			Quantity:       2,
			UnitPrice:      50,
			DiscountAmount: 10,
			TaxRate:        18,
			TaxAmount:      16.2,
			LineTotal:      106.2,
			Snapshot:       `{"supplier_cost":25}`,
			CreatedAt:      orderedAt,
			UpdatedAt:      cancelledAt,
		}},
		Events: []*models.StoreOrderEvent{{
			ID:           "event-internal",
			StoreOrderID: "order-internal",
			EventType:    "payment.webhook.received",
			Status:       "paid",
			Payload:      `{"signature":"webhook-secret"}`,
			CreatedAt:    orderedAt,
			UpdatedAt:    cancelledAt,
		}},
	}
	handler := &CommerceHandler{publicOrders: publicOrderServiceStub{order: order}}
	router := gin.New()
	router.GET("/api/v1/public/store/:slug/orders/:token", handler.PublicOrder)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/public/store/my-store/orders/public-token-secret", nil))

	if response.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", response.Code, response.Body.String())
	}
	var body map[string]json.RawMessage
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode public order response: %v", err)
	}
	assertPublicOrderJSONKeys(t, body, []string{
		"order_number", "status", "payment_status", "payment_method", "currency",
		"subtotal", "discount_total", "tax_total", "shipping_total", "total",
		"ordered_at", "paid_at", "cancelled_at", "lines",
	})
	var lines []map[string]json.RawMessage
	if err := json.Unmarshal(body["lines"], &lines); err != nil {
		t.Fatalf("decode public order lines: %v", err)
	}
	if len(lines) != 1 {
		t.Fatalf("expected one public line, got %d", len(lines))
	}
	assertPublicOrderJSONKeys(t, lines[0], []string{
		"title", "sku", "quantity", "unit_price", "discount_amount", "tax_rate", "tax_amount", "line_total",
	})
}

func TestCommerceHandlerPublicOrderReturnsNotFoundForUnknownToken(t *testing.T) {
	handler := &CommerceHandler{publicOrders: publicOrderServiceStub{err: gorm.ErrRecordNotFound}}
	router := gin.New()
	router.GET("/api/v1/public/store/:slug/orders/:token", handler.PublicOrder)
	response := httptest.NewRecorder()
	router.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/public/store/my-store/orders/unknown-token", nil))

	if response.Code != http.StatusNotFound {
		t.Fatalf("expected 404, got %d: %s", response.Code, response.Body.String())
	}
}

func assertPublicOrderJSONKeys(t *testing.T, value map[string]json.RawMessage, expected []string) {
	t.Helper()
	actual := make([]string, 0, len(value))
	for key := range value {
		actual = append(actual, key)
	}
	sort.Strings(actual)
	sort.Strings(expected)
	if len(actual) != len(expected) {
		t.Fatalf("expected keys %v, got %v", expected, actual)
	}
	for i := range expected {
		if actual[i] != expected[i] {
			t.Fatalf("expected keys %v, got %v", expected, actual)
		}
	}
}

func publicOrderPointer(value string) *string {
	return &value
}
