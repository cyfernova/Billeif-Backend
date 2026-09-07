package razorpay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"invoice-backend/pkg/logger"
)

const defaultBaseURL = "https://api.razorpay.com/v1"

type Config struct {
	KeyID         string
	KeySecret     string
	WebhookSecret string
	BaseURL       string
	Timeout       time.Duration
}

type Client struct {
	keyID         string
	keySecret     string
	webhookSecret string
	baseURL       string
	httpClient    *http.Client
	log           *logger.Logger
}

// HTTPError preserves provider status for internal health classification while
// deliberately excluding response bodies and credential detail.
type HTTPError struct {
	statusCode int
}

func (e *HTTPError) Error() string       { return "razorpay provider request failed" }
func (e *HTTPError) HTTPStatusCode() int { return e.statusCode }

func NewClient(cfg Config, log *logger.Logger) *Client {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	if baseURL == "" {
		baseURL = defaultBaseURL
	}
	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	if log == nil {
		log = logger.Global()
	}
	return &Client{
		keyID:         strings.TrimSpace(cfg.KeyID),
		keySecret:     cfg.KeySecret,
		webhookSecret: cfg.WebhookSecret,
		baseURL:       baseURL,
		httpClient:    &http.Client{Timeout: timeout},
		log:           log.Named("razorpay_client"),
	}
}

func (c *Client) KeyID() string {
	if c == nil {
		return ""
	}
	return c.keyID
}

func (c *Client) KeySecret() string {
	if c == nil {
		return ""
	}
	return c.keySecret
}

func (c *Client) WebhookSecret() string {
	if c == nil {
		return ""
	}
	return c.webhookSecret
}

func (c *Client) Configured() bool {
	return c != nil && strings.TrimSpace(c.keyID) != "" && strings.TrimSpace(c.keySecret) != ""
}

type OrderParams struct {
	Amount   int64             `json:"amount"`
	Currency string            `json:"currency"`
	Receipt  string            `json:"receipt,omitempty"`
	Notes    map[string]string `json:"notes,omitempty"`
}

type Order struct {
	ID             string            `json:"id"`
	Entity         string            `json:"entity"`
	Amount         int64             `json:"amount"`
	AmountPaid     int64             `json:"amount_paid"`
	AmountDue      int64             `json:"amount_due"`
	Currency       string            `json:"currency"`
	Receipt        string            `json:"receipt"`
	Status         string            `json:"status"`
	Attempts       int64             `json:"attempts"`
	Notes          map[string]string `json:"notes"`
	CreatedAtEpoch int64             `json:"created_at"`
}

type OrderCollection struct {
	Entity string  `json:"entity"`
	Count  int     `json:"count"`
	Items  []Order `json:"items"`
}

type Payment struct {
	ID               string `json:"id"`
	Entity           string `json:"entity"`
	Amount           int64  `json:"amount"`
	Currency         string `json:"currency"`
	Status           string `json:"status"`
	Method           string `json:"method"`
	OrderID          string `json:"order_id"`
	InvoiceID        string `json:"invoice_id"`
	Email            string `json:"email"`
	Contact          string `json:"contact"`
	Captured         bool   `json:"captured"`
	ErrorCode        string `json:"error_code"`
	ErrorDescription string `json:"error_description"`
	CreatedAtEpoch   int64  `json:"created_at"`
}

type SubscriptionCreateParams struct {
	PlanID         string            `json:"plan_id"`
	TotalCount     int64             `json:"total_count"`
	Quantity       int64             `json:"quantity"`
	CustomerNotify bool              `json:"customer_notify"`
	Notes          map[string]string `json:"notes"`
}

type SubscriptionUpdateParams struct {
	PlanID           string `json:"plan_id"`
	ScheduleChangeAt string `json:"schedule_change_at"`
	CustomerNotify   bool   `json:"customer_notify"`
}

type SubscriptionCancelParams struct {
	CancelAtCycleEnd bool `json:"cancel_at_cycle_end"`
}

type Subscription struct {
	ID                  string            `json:"id"`
	Entity              string            `json:"entity"`
	PlanID              string            `json:"plan_id"`
	CustomerID          string            `json:"customer_id"`
	Status              string            `json:"status"`
	CurrentStart        int64             `json:"current_start"`
	CurrentEnd          int64             `json:"current_end"`
	EndedAt             int64             `json:"ended_at"`
	ChargeAt            int64             `json:"charge_at"`
	StartAt             int64             `json:"start_at"`
	EndAt               int64             `json:"end_at"`
	TotalCount          int64             `json:"total_count"`
	PaidCount           int64             `json:"paid_count"`
	RemainingCount      int64             `json:"remaining_count"`
	CreatedAt           int64             `json:"created_at"`
	ShortURL            string            `json:"short_url"`
	HasScheduledChanges bool              `json:"has_scheduled_changes"`
	ScheduleChangeAt    string            `json:"schedule_change_at"`
	ChangeScheduledAt   int64             `json:"change_scheduled_at"`
	Notes               map[string]string `json:"notes"`
}

func (c *Client) CreateOrder(ctx context.Context, params OrderParams) (*Order, error) {
	var order Order
	if err := c.request(ctx, http.MethodPost, "/orders", params, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

func (c *Client) FetchOrder(ctx context.Context, orderID string) (*Order, error) {
	var order Order
	if err := c.request(ctx, http.MethodGet, "/orders/"+orderID, nil, &order); err != nil {
		return nil, err
	}
	return &order, nil
}

func (c *Client) FetchOrdersByReceipt(ctx context.Context, receipt string) ([]Order, error) {
	receipt = strings.TrimSpace(receipt)
	if receipt == "" {
		return nil, fmt.Errorf("Razorpay order receipt is required")
	}
	query := url.Values{}
	query.Set("receipt", receipt)
	query.Set("count", "100")
	var collection OrderCollection
	if err := c.request(ctx, http.MethodGet, "/orders?"+query.Encode(), nil, &collection); err != nil {
		return nil, err
	}
	return collection.Items, nil
}

// Probe performs a bounded read-only authenticated request. It establishes
// provider reachability without creating an order or exposing provider data.
func (c *Client) Probe(ctx context.Context) error {
	var collection OrderCollection
	return c.request(ctx, http.MethodGet, "/orders?count=1", nil, &collection)
}

func (c *Client) FetchPayment(ctx context.Context, paymentID string) (*Payment, error) {
	var payment Payment
	if err := c.request(ctx, http.MethodGet, "/payments/"+paymentID, nil, &payment); err != nil {
		return nil, err
	}
	return &payment, nil
}

func (c *Client) CreateSubscription(ctx context.Context, params SubscriptionCreateParams) (*Subscription, error) {
	var subscription Subscription
	if err := c.request(ctx, http.MethodPost, "/subscriptions", params, &subscription); err != nil {
		return nil, err
	}
	return &subscription, nil
}

func (c *Client) FetchSubscription(ctx context.Context, subscriptionID string) (*Subscription, error) {
	var subscription Subscription
	if err := c.request(ctx, http.MethodGet, "/subscriptions/"+url.PathEscape(subscriptionID), nil, &subscription); err != nil {
		return nil, err
	}
	return &subscription, nil
}

func (c *Client) UpdateSubscription(ctx context.Context, subscriptionID string, params SubscriptionUpdateParams) (*Subscription, error) {
	var subscription Subscription
	if err := c.request(ctx, http.MethodPatch, "/subscriptions/"+url.PathEscape(subscriptionID), params, &subscription); err != nil {
		return nil, err
	}
	return &subscription, nil
}

func (c *Client) CancelSubscription(ctx context.Context, subscriptionID string, params SubscriptionCancelParams) (*Subscription, error) {
	var subscription Subscription
	path := "/subscriptions/" + url.PathEscape(subscriptionID) + "/cancel"
	if err := c.request(ctx, http.MethodPost, path, params, &subscription); err != nil {
		return nil, err
	}
	return &subscription, nil
}

func (c *Client) request(ctx context.Context, method, path string, payload interface{}, out interface{}) error {
	if !c.Configured() {
		return fmt.Errorf("razorpay client is not configured")
	}

	var body io.Reader
	if payload != nil {
		raw, err := json.Marshal(payload)
		if err != nil {
			return fmt.Errorf("marshal Razorpay request: %w", err)
		}
		body = bytes.NewReader(raw)
	}

	req, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return fmt.Errorf("build Razorpay request: %w", err)
	}
	req.SetBasicAuth(c.keyID, c.keySecret)
	req.Header.Set("Accept", "application/json")
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	start := time.Now()
	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.log.Error("Razorpay API request failed", "method", method, "endpoint", razorpayEndpointClass(path), "duration_ms", time.Since(start).Milliseconds(), "code", "provider_transport_error")
		return fmt.Errorf("razorpay request failed")
	}
	defer resp.Body.Close()

	limitedBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		c.log.Warn("Razorpay API returned error", "method", method, "endpoint", razorpayEndpointClass(path), "status_code", resp.StatusCode, "duration_ms", time.Since(start).Milliseconds())
		return &HTTPError{statusCode: resp.StatusCode}
	}

	if out != nil {
		if err := json.Unmarshal(limitedBody, out); err != nil {
			return fmt.Errorf("decode Razorpay response: %w", err)
		}
	}

	c.log.Debug("Razorpay API request completed", "method", method, "endpoint", razorpayEndpointClass(path), "status_code", resp.StatusCode, "duration_ms", time.Since(start).Milliseconds())
	return nil
}

func razorpayEndpointClass(path string) string {
	path = strings.SplitN(path, "?", 2)[0]
	parts := strings.Split(strings.Trim(path, "/"), "/")
	if len(parts) >= 2 && parts[0] == "subscriptions" {
		parts[1] = ":subscription"
	}
	if len(parts) >= 2 && (parts[0] == "payments" || parts[0] == "orders") {
		parts[1] = ":resource"
	}
	return "/" + strings.Join(parts, "/")
}
