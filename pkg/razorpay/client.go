package razorpay

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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

type Payment struct {
	ID               string `json:"id"`
	Entity           string `json:"entity"`
	Amount           int64  `json:"amount"`
	Currency         string `json:"currency"`
	Status           string `json:"status"`
	Method           string `json:"method"`
	OrderID          string `json:"order_id"`
	Email            string `json:"email"`
	Contact          string `json:"contact"`
	Captured         bool   `json:"captured"`
	ErrorCode        string `json:"error_code"`
	ErrorDescription string `json:"error_description"`
	CreatedAtEpoch   int64  `json:"created_at"`
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

func (c *Client) FetchPayment(ctx context.Context, paymentID string) (*Payment, error) {
	var payment Payment
	if err := c.request(ctx, http.MethodGet, "/payments/"+paymentID, nil, &payment); err != nil {
		return nil, err
	}
	return &payment, nil
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
		c.log.Error("Razorpay API request failed", "method", method, "path", path, "duration_ms", time.Since(start).Milliseconds(), "error", err)
		return fmt.Errorf("razorpay request failed: %w", err)
	}
	defer resp.Body.Close()

	limitedBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		c.log.Warn("Razorpay API returned error", "method", method, "path", path, "status_code", resp.StatusCode, "duration_ms", time.Since(start).Milliseconds())
		return fmt.Errorf("razorpay API returned status %d", resp.StatusCode)
	}

	if out != nil {
		if err := json.Unmarshal(limitedBody, out); err != nil {
			return fmt.Errorf("decode Razorpay response: %w", err)
		}
	}

	c.log.Debug("Razorpay API request completed", "method", method, "path", path, "status_code", resp.StatusCode, "duration_ms", time.Since(start).Milliseconds())
	return nil
}
