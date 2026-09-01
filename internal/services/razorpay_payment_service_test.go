package services

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"
	"invoice-backend/pkg/razorpay"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type fakeRazorpayServer struct {
	server                *httptest.Server
	mu                    sync.Mutex
	orderCount            int
	lastAmount            int64
	lastCurrency          string
	orders                map[string]razorpay.Order
	payments              map[string]razorpay.Payment
	dropNextOrderResponse bool
}

func newFakeRazorpayServer(t *testing.T) *fakeRazorpayServer {
	t.Helper()
	fake := &fakeRazorpayServer{
		orders:   make(map[string]razorpay.Order),
		payments: make(map[string]razorpay.Payment),
	}
	fake.server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/v1/orders":
			var req razorpay.OrderParams
			if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			fake.mu.Lock()
			defer fake.mu.Unlock()
			for _, existing := range fake.orders {
				if existing.Receipt == req.Receipt {
					http.Error(w, "receipt already exists", http.StatusBadRequest)
					return
				}
			}
			fake.orderCount++
			id := "order_test_" + req.Receipt
			order := razorpay.Order{ID: id, Amount: req.Amount, Currency: req.Currency, Status: "created", Receipt: req.Receipt}
			fake.lastAmount = req.Amount
			fake.lastCurrency = req.Currency
			fake.orders[id] = order
			if fake.dropNextOrderResponse {
				fake.dropNextOrderResponse = false
				if hijacker, ok := w.(http.Hijacker); ok {
					connection, _, hijackErr := hijacker.Hijack()
					if hijackErr == nil {
						_ = connection.Close()
						return
					}
				}
			}
			_ = json.NewEncoder(w).Encode(order)
		case r.Method == http.MethodGet && r.URL.Path == "/v1/orders":
			receipt := r.URL.Query().Get("receipt")
			fake.mu.Lock()
			items := make([]razorpay.Order, 0, 1)
			for _, order := range fake.orders {
				if order.Receipt == receipt {
					items = append(items, order)
				}
			}
			fake.mu.Unlock()
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"entity": "collection", "count": len(items), "items": items})
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/orders/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/orders/")
			fake.mu.Lock()
			order, ok := fake.orders[id]
			fake.mu.Unlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			order.Status = "paid"
			_ = json.NewEncoder(w).Encode(order)
		case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/v1/payments/"):
			id := strings.TrimPrefix(r.URL.Path, "/v1/payments/")
			fake.mu.Lock()
			payment, ok := fake.payments[id]
			fake.mu.Unlock()
			if !ok {
				http.NotFound(w, r)
				return
			}
			_ = json.NewEncoder(w).Encode(payment)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(fake.server.Close)
	return fake
}

func (f *fakeRazorpayServer) baseURL() string {
	return f.server.URL + "/v1"
}

func (f *fakeRazorpayServer) addPayment(payment razorpay.Payment) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.payments[payment.ID] = payment
}

func (f *fakeRazorpayServer) createCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.orderCount
}

func (f *fakeRazorpayServer) dropNextCreateResponse() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.dropNextOrderResponse = true
}

func newRazorpayPaymentTestService(t *testing.T) (*RazorpayPaymentService, *gorm.DB, *fakeRazorpayServer) {
	t.Helper()
	db, err := gorm.Open(sqlite.Open("file:"+uuid.NewString()+"?mode=memory&cache=shared"), &gorm.Config{
		DisableForeignKeyConstraintWhenMigrating: true,
	})
	if err != nil {
		t.Fatalf("open sqlite db: %v", err)
	}
	createRazorpayPaymentTestSchema(t, db)
	fake := newFakeRazorpayServer(t)
	cfg := &config.Config{
		Razorpay: config.RazorpayConfig{
			KeyID:         "rzp_test_key",
			KeySecret:     "test_secret",
			WebhookSecret: "webhook_secret",
			BaseURL:       fake.baseURL(),
			Timeout:       5,
		},
	}
	return NewRazorpayPaymentService(cfg, db, logger.New()).WithCapabilityGuard(&recordingCapabilityGuard{}), db, fake
}

func TestRazorpayCapabilityProbeDoesNotCreateOrder(t *testing.T) {
	service, _, provider := newRazorpayPaymentTestService(t)

	outcome := service.ProbeGlobalCapability(context.Background())

	require.NoError(t, outcome.Err)
	require.Equal(t, 0, provider.createCount())
}

func createRazorpayPaymentTestSchema(t *testing.T, db *gorm.DB) {
	t.Helper()
	statements := []string{
		`CREATE TABLE payment_attempts (
			id TEXT PRIMARY KEY,
			user_id TEXT NOT NULL,
			business_id TEXT NOT NULL,
			target_type TEXT NOT NULL,
			target_id TEXT NOT NULL,
			amount_paise INTEGER NOT NULL,
			currency TEXT NOT NULL DEFAULT 'INR',
			razorpay_order_id TEXT UNIQUE,
			razorpay_payment_id TEXT,
			status TEXT NOT NULL DEFAULT 'created',
			idempotency_key TEXT NOT NULL,
			failure_reason TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			paid_at DATETIME
		)`,
		`CREATE UNIQUE INDEX idx_payment_attempt_idempotency ON payment_attempts (user_id, business_id, idempotency_key)`,
		`CREATE TABLE razorpay_webhook_events (
			id TEXT PRIMARY KEY,
			razorpay_event_id TEXT NOT NULL,
			provider_mode TEXT NOT NULL DEFAULT 'legacy_unknown',
			event_type TEXT NOT NULL,
			payload_hash TEXT NOT NULL DEFAULT '',
			signature_verified NUMERIC NOT NULL DEFAULT 0,
			received_at DATETIME,
			provider_occurred_at DATETIME,
			processing_status TEXT NOT NULL DEFAULT 'processed',
			attempt_count INTEGER NOT NULL DEFAULT 0,
			sanitized_error_code TEXT,
			processed_at DATETIME,
			business_id TEXT,
			subscription_id TEXT,
			replay_count INTEGER NOT NULL DEFAULT 0,
			last_replayed_at DATETIME,
			created_at DATETIME
		)`,
		`CREATE UNIQUE INDEX ux_test_payment_webhook_event ON razorpay_webhook_events (provider_mode, razorpay_event_id)`,
		`CREATE TABLE subscriptions (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			plan TEXT NOT NULL,
			plan_code TEXT,
			catalog_version TEXT,
			status TEXT NOT NULL DEFAULT 'active',
			billing_mode TEXT NOT NULL DEFAULT 'free', provider_mode TEXT, provider_customer_id TEXT,
			provider_subscription_id TEXT, provider_plan_id TEXT,
			max_invoices INTEGER DEFAULT 10,
			max_customers INTEGER DEFAULT 10,
			max_users INTEGER DEFAULT 3,
			max_storage_mb INTEGER DEFAULT 100,
			start_date DATETIME,
			end_date DATETIME,
			next_billing_date DATETIME,
			period_start DATETIME, period_end DATETIME, next_renewal_at DATETIME, grace_deadline DATETIME,
			cancel_at_period_end NUMERIC DEFAULT 0, cancellation_effective_at DATETIME, cancelled_at DATETIME,
			pending_plan_id TEXT, pending_provider_plan_id TEXT, pending_plan_effective_at DATETIME,
			last_provider_event_at DATETIME, last_provider_paid_count INTEGER DEFAULT 0,
			reconciliation_code TEXT, lifecycle_version INTEGER DEFAULT 1,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE store_orders (
			id TEXT PRIMARY KEY,
			business_id TEXT NOT NULL,
			storefront_id TEXT NOT NULL,
			branch_id TEXT,
			customer_id TEXT,
			coupon_id TEXT,
			sales_order_id TEXT,
			sales_invoice_id TEXT,
			public_token TEXT NOT NULL,
			order_number TEXT NOT NULL,
			status TEXT NOT NULL DEFAULT 'pending',
			payment_status TEXT NOT NULL DEFAULT 'pending',
			payment_method TEXT,
			currency TEXT NOT NULL DEFAULT 'INR',
			exchange_rate REAL DEFAULT 1,
			fx_provider TEXT,
			fx_base_currency TEXT,
			fx_quote_currency TEXT,
			fx_rate_timestamp DATETIME,
			subtotal REAL DEFAULT 0,
			discount_total REAL DEFAULT 0,
			tax_total REAL DEFAULT 0,
			shipping_total REAL DEFAULT 0,
			total REAL DEFAULT 0,
			snapshot TEXT DEFAULT '{}',
			billing_address TEXT DEFAULT '{}',
			shipping_address TEXT DEFAULT '{}',
			notes TEXT,
			idempotency_key TEXT,
			external_order_id TEXT,
			external_payment_id TEXT,
			gateway_order_id TEXT,
			gateway_payment_id TEXT,
			webhook_reference TEXT,
			ordered_at DATETIME,
			paid_at DATETIME,
			cancelled_at DATETIME,
			cancellation_reason TEXT,
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
		`CREATE TABLE store_order_events (
			id TEXT PRIMARY KEY,
			store_order_id TEXT NOT NULL,
			event_type TEXT NOT NULL,
			status TEXT,
			payload TEXT DEFAULT '{}',
			created_at DATETIME,
			updated_at DATETIME,
			deleted_at DATETIME
		)`,
	}
	for _, stmt := range statements {
		if err := db.Exec(stmt).Error; err != nil {
			t.Fatalf("create test schema: %v\n%s", err, stmt)
		}
	}
}

func TestRazorpayCreateOrderRejectsUnavailablePaymentCapabilityBeforeDatabaseOrProvider(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		targetType string
		capability CapabilityKey
	}{
		{name: "plan payment", targetType: "plan", capability: CapabilityRazorpay},
		{name: "storefront payment", targetType: "store_order", capability: CapabilityStorefrontPayments},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			guard := &recordingCapabilityGuard{err: &CapabilityUnavailableError{
				Code: "capability_unavailable", Capability: fixture.capability,
				State: CapabilityStateTemporarilyUnavailable, ReasonCode: ReasonProviderUnavailable,
			}}
			service := NewRazorpayPaymentService(&config.Config{}, nil, logger.New()).WithCapabilityGuard(guard)

			_, err := service.CreateOrder(context.Background(), "biz-1", "user-1", RazorpayCreateOrderInput{TargetType: fixture.targetType})

			var unavailable *CapabilityUnavailableError
			require.ErrorAs(t, err, &unavailable)
			require.Equal(t, fixture.capability, guard.request.Capability)
			require.Equal(t, "biz-1", guard.request.BusinessID)
			require.Equal(t, "user-1", guard.request.UserID)
		})
	}
}

func TestRazorpayPaymentCreateOrderServerAmountAndIdempotency(t *testing.T) {
	svc, _, fake := newRazorpayPaymentTestService(t)
	ctx := context.Background()
	businessID := uuid.NewString()

	first, err := svc.CreateOrder(ctx, businessID, "user_1", RazorpayCreateOrderInput{
		TargetType:     models.PaymentAttemptTargetPlan,
		PlanID:         "pro_monthly",
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("create first order: %v", err)
	}
	second, err := svc.CreateOrder(ctx, businessID, "user_1", RazorpayCreateOrderInput{
		TargetType:     models.PaymentAttemptTargetPlan,
		PlanID:         "pro_monthly",
		IdempotencyKey: "idem-1",
	})
	if err != nil {
		t.Fatalf("create idempotent order: %v", err)
	}
	if first.PaymentAttemptID != second.PaymentAttemptID || first.RazorpayOrderID != second.RazorpayOrderID {
		t.Fatalf("expected idempotent retry to return existing attempt/order")
	}
	if first.Amount != 29900 || fake.lastAmount != 29900 || fake.lastCurrency != "INR" {
		t.Fatalf("expected server-owned pro amount 29900 INR, got response=%d provider=%d %s", first.Amount, fake.lastAmount, fake.lastCurrency)
	}
	if fake.createCount() != 1 {
		t.Fatalf("expected one Razorpay order, got %d", fake.createCount())
	}
}

func TestRazorpayPaymentCreateOrderAllowsDistinctPendingAttempts(t *testing.T) {
	svc, _, fake := newRazorpayPaymentTestService(t)
	businessID := uuid.NewString()
	for _, key := range []string{"idem-distinct-1", "idem-distinct-2"} {
		if _, err := svc.CreateOrder(context.Background(), businessID, "user_1", RazorpayCreateOrderInput{
			TargetType: models.PaymentAttemptTargetPlan, PlanID: "pro_monthly", IdempotencyKey: key,
		}); err != nil {
			t.Fatalf("create order for %s: %v", key, err)
		}
	}
	if fake.createCount() != 2 {
		t.Fatalf("expected two distinct provider orders, got %d", fake.createCount())
	}
}

func TestRazorpayPaymentCreateOrderRecoversAmbiguousProviderSuccess(t *testing.T) {
	svc, _, fake := newRazorpayPaymentTestService(t)
	fake.dropNextCreateResponse()

	order, err := svc.CreateOrder(context.Background(), uuid.NewString(), "user_1", RazorpayCreateOrderInput{
		TargetType: models.PaymentAttemptTargetPlan, PlanID: "pro_monthly", IdempotencyKey: "idem-ambiguous",
	})
	if err != nil {
		t.Fatalf("recover ambiguous provider success: %v", err)
	}
	if order.RazorpayOrderID == "" {
		t.Fatal("expected recovered provider order id")
	}
	if fake.createCount() != 1 {
		t.Fatalf("expected exactly one provider order, got %d", fake.createCount())
	}
}

func TestRazorpayPaymentCreateOrderRetryRecoversAfterLocalPersistenceFailure(t *testing.T) {
	svc, db, fake := newRazorpayPaymentTestService(t)
	businessID := uuid.NewString()
	input := RazorpayCreateOrderInput{
		TargetType: models.PaymentAttemptTargetPlan, PlanID: "pro_monthly", IdempotencyKey: "idem-local-failure",
	}
	if err := db.Exec(`CREATE TRIGGER fail_provider_order_update
		BEFORE UPDATE OF razorpay_order_id ON payment_attempts
		WHEN NEW.razorpay_order_id <> ''
		BEGIN SELECT RAISE(ABORT, 'forced local persistence failure'); END`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	if _, err := svc.CreateOrder(context.Background(), businessID, "user_1", input); err == nil {
		t.Fatal("expected local provider-order persistence failure")
	}
	if err := db.Exec("DROP TRIGGER fail_provider_order_update").Error; err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}

	order, err := svc.CreateOrder(context.Background(), businessID, "user_1", input)
	if err != nil {
		t.Fatalf("recover persisted provider order: %v", err)
	}
	if order.RazorpayOrderID == "" {
		t.Fatal("expected recovered provider order id")
	}
	if fake.createCount() != 1 {
		t.Fatalf("expected retry not to create a second provider order, got %d", fake.createCount())
	}
}

func TestRazorpayPaymentCreateOrderLocalFailureDoesNotBlockOtherAttempts(t *testing.T) {
	svc, db, _ := newRazorpayPaymentTestService(t)
	businessID := uuid.NewString()
	if err := db.Exec(`CREATE TRIGGER fail_provider_order_update
		BEFORE UPDATE OF razorpay_order_id ON payment_attempts
		WHEN NEW.razorpay_order_id <> ''
		BEGIN SELECT RAISE(ABORT, 'forced local persistence failure'); END`).Error; err != nil {
		t.Fatalf("create failure trigger: %v", err)
	}
	if _, err := svc.CreateOrder(context.Background(), businessID, "user_1", RazorpayCreateOrderInput{
		TargetType: models.PaymentAttemptTargetPlan, PlanID: "pro_monthly", IdempotencyKey: "idem-orphaned-local",
	}); err == nil {
		t.Fatal("expected local provider-order persistence failure")
	}
	if err := db.Exec("DROP TRIGGER fail_provider_order_update").Error; err != nil {
		t.Fatalf("drop failure trigger: %v", err)
	}
	if _, err := svc.CreateOrder(context.Background(), businessID, "user_1", RazorpayCreateOrderInput{
		TargetType: models.PaymentAttemptTargetPlan, PlanID: "rise_monthly", IdempotencyKey: "idem-independent",
	}); err != nil {
		t.Fatalf("independent payment attempt was blocked by recoverable attempt: %v", err)
	}
}

func TestRazorpayPaymentVerifyRejectsInvalidSignature(t *testing.T) {
	svc, db, fake := newRazorpayPaymentTestService(t)
	ctx := context.Background()
	businessID := uuid.NewString()
	order, err := svc.CreateOrder(ctx, businessID, "user_1", RazorpayCreateOrderInput{
		TargetType:     models.PaymentAttemptTargetPlan,
		PlanID:         "pro_monthly",
		IdempotencyKey: "idem-invalid",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	fake.addPayment(razorpay.Payment{ID: "pay_bad_sig", OrderID: order.RazorpayOrderID, Amount: 29900, Currency: "INR", Status: "captured", Captured: true})

	_, err = svc.VerifyPayment(ctx, businessID, "user_1", RazorpayVerifyPaymentInput{
		PaymentAttemptID:  order.PaymentAttemptID,
		RazorpayOrderID:   order.RazorpayOrderID,
		RazorpayPaymentID: "pay_bad_sig",
		RazorpaySignature: "deadbeef",
	})
	if err == nil {
		t.Fatalf("expected invalid signature to be rejected")
	}
	var count int64
	db.Model(&models.Subscription{}).Where("business_id = ?", businessID).Count(&count)
	if count != 0 {
		t.Fatalf("invalid signature credited subscription")
	}
}

func TestRazorpayPaymentVerifyValidSignatureCreditsOnce(t *testing.T) {
	svc, db, fake := newRazorpayPaymentTestService(t)
	ctx := context.Background()
	businessID := uuid.NewString()
	order, err := svc.CreateOrder(ctx, businessID, "user_1", RazorpayCreateOrderInput{
		TargetType:     models.PaymentAttemptTargetPlan,
		PlanID:         "rise_monthly",
		IdempotencyKey: "idem-valid",
	})
	if err != nil {
		t.Fatalf("create order: %v", err)
	}
	fake.addPayment(razorpay.Payment{ID: "pay_valid", OrderID: order.RazorpayOrderID, Amount: 99900, Currency: "INR", Status: "captured", Captured: true})
	signature := hmacHex(order.RazorpayOrderID+"|"+"pay_valid", "test_secret")

	for i := 0; i < 2; i++ {
		resp, err := svc.VerifyPayment(ctx, businessID, "user_1", RazorpayVerifyPaymentInput{
			PaymentAttemptID:  order.PaymentAttemptID,
			RazorpayOrderID:   order.RazorpayOrderID,
			RazorpayPaymentID: "pay_valid",
			RazorpaySignature: signature,
		})
		if err != nil {
			t.Fatalf("verify payment #%d: %v", i+1, err)
		}
		if resp.Status != "verified" {
			t.Fatalf("expected verified, got %q", resp.Status)
		}
	}

	var subscriptions []models.Subscription
	if err := db.Where("business_id = ?", businessID).Find(&subscriptions).Error; err != nil {
		t.Fatalf("load subscriptions: %v", err)
	}
	if len(subscriptions) != 1 || subscriptions[0].PlanCode != "rise" || subscriptions[0].Status != "active" {
		t.Fatalf("expected one active rise subscription, got %+v", subscriptions)
	}
}

func TestRazorpayPaymentWebhookDuplicateIgnored(t *testing.T) {
	svc, db, _ := newRazorpayPaymentTestService(t)
	ctx := context.Background()
	businessID := uuid.NewString()
	storeOrderID := uuid.NewString()
	attemptID := uuid.NewString()
	orderID := "order_webhook"
	if err := db.Create(&models.StoreOrder{
		ID:            storeOrderID,
		BusinessID:    businessID,
		StorefrontID:  uuid.NewString(),
		PublicToken:   "public-token",
		OrderNumber:   "SO-1",
		Status:        models.StoreOrderStatusPending,
		PaymentStatus: models.StoreOrderPaymentStatusPending,
		PaymentMethod: "online",
		Currency:      "INR",
		Total:         123.45,
	}).Error; err != nil {
		t.Fatalf("create store order: %v", err)
	}
	if err := db.Create(&models.PaymentAttempt{
		ID:              attemptID,
		UserID:          "user_1",
		BusinessID:      businessID,
		TargetType:      models.PaymentAttemptTargetStoreOrder,
		TargetID:        storeOrderID,
		AmountPaise:     12345,
		Currency:        "INR",
		RazorpayOrderID: orderID,
		Status:          models.PaymentAttemptStatusCreated,
		IdempotencyKey:  "idem-webhook",
	}).Error; err != nil {
		t.Fatalf("create payment attempt: %v", err)
	}

	raw := []byte(`{"event":"payment.captured","payload":{"payment":{"entity":{"id":"pay_webhook","order_id":"order_webhook","amount":12345,"currency":"INR","status":"captured","captured":true}}}}`)
	signature := hmacHex(string(raw), "webhook_secret")
	duplicate, err := svc.HandleWebhook(ctx, signature, "evt_1", raw)
	if err != nil {
		t.Fatalf("handle webhook: %v", err)
	}
	if duplicate {
		t.Fatalf("first webhook should not be duplicate")
	}
	duplicate, err = svc.HandleWebhook(ctx, signature, "evt_1", raw)
	if err != nil {
		t.Fatalf("handle duplicate webhook: %v", err)
	}
	if !duplicate {
		t.Fatalf("expected duplicate webhook to be ignored")
	}

	var order models.StoreOrder
	if err := db.Where("id = ?", storeOrderID).First(&order).Error; err != nil {
		t.Fatalf("load store order: %v", err)
	}
	if order.PaymentStatus != models.StoreOrderPaymentStatusPaid || order.GatewayPaymentID != "pay_webhook" {
		t.Fatalf("expected paid store order, got %+v", order)
	}
	var eventCount int64
	db.Model(&models.RazorpayWebhookEvent{}).Where("razorpay_event_id = ?", "evt_1").Count(&eventCount)
	if eventCount != 1 {
		t.Fatalf("expected one webhook event record, got %d", eventCount)
	}
	var storeEventCount int64
	db.Model(&models.StoreOrderEvent{}).Where("store_order_id = ?", storeOrderID).Count(&storeEventCount)
	if storeEventCount != 1 {
		t.Fatalf("expected one store order event, got %d", storeEventCount)
	}
}

func hmacHex(message, secret string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	_, _ = mac.Write([]byte(message))
	return hex.EncodeToString(mac.Sum(nil))
}
