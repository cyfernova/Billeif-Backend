package razorpay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"

	"invoice-backend/pkg/logger"
)

type Client struct {
	key           string
	secret        string
	webhookSecret string
	baseURL       string
	httpClient    *http.Client
	log           *logger.Logger
}

type Config struct {
	Key           string
	Secret        string
	WebhookSecret string
	BaseURL       string
	Timeout       time.Duration
}

func NewClient(cfg *Config, log *logger.Logger) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.razorpay.com/v1"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}
	if log == nil {
		log = logger.Global()
	}

	return &Client{
		key:           cfg.Key,
		secret:        cfg.Secret,
		webhookSecret: cfg.WebhookSecret,
		baseURL:       baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
		log: log.Named("razorpay_client"),
	}
}

type OrderParams struct {
	Amount         int64             `json:"amount"`
	Currency       string            `json:"currency"`
	Receipt        string            `json:"receipt"`
	Notes          map[string]string `json:"notes,omitempty"`
	PaymentCapture string            `json:"payment_capture,omitempty"`
}

type Order struct {
	ID       string            `json:"id"`
	Entity   string            `json:"entity"`
	Amount   int64             `json:"amount"`
	Currency string            `json:"currency"`
	Status   string            `json:"status"`
	Receipt  string            `json:"receipt"`
	Notes    map[string]string `json:"notes"`
}

type CustomerParams struct {
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"contact"`
}

type Customer struct {
	ID    string `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
	Phone string `json:"contact"`
}

type TokenParams struct {
	CustomerID string       `json:"customer_id"`
	Method     string       `json:"method"`
	Card       *CardDetails `json:"card,omitempty"`
}

type CardDetails struct {
	Number string `json:"number"`
	Name   string `json:"name"`
	Expiry string `json:"expiry"`
	CVV    string `json:"cvv"`
}

type Token struct {
	ID        string `json:"id"`
	Entity    string `json:"entity"`
	Method    string `json:"method"`
	CreatedAt int64  `json:"created_at"`
}

type RefundParams struct {
	PaymentID string            `json:"payment_id"`
	Amount    int64             `json:"amount,omitempty"`
	Notes     map[string]string `json:"notes,omitempty"`
}

type Refund struct {
	ID        string `json:"id"`
	Amount    int64  `json:"amount"`
	Status    string `json:"status"`
	CreatedAt int64  `json:"created_at"`
}

type PaymentParams struct {
	Amount        int64             `json:"amount"`
	Currency      string            `json:"currency"`
	OrderID       string            `json:"order_id"`
	PaymentMethod *string           `json:"payment_method,omitempty"`
	CustomerID    *string           `json:"customer_id,omitempty"`
	Token         *string           `json:"token,omitempty"`
	Notes         map[string]string `json:"notes,omitempty"`
}

type Payment struct {
	ID         string `json:"id"`
	Amount     int64  `json:"amount"`
	Currency   string `json:"currency"`
	Status     string `json:"status"`
	Method     string `json:"method"`
	OrderID    string `json:"order_id"`
	CapturedAt *int64 `json:"captured_at,omitempty"`
}

type CaptureParams struct {
	PaymentID string `json:"payment_id"`
	Amount    int64  `json:"amount,omitempty"`
	Currency  string `json:"currency,omitempty"`
}

func (c *Client) CreateOrder(params *OrderParams) (*Order, error) {
	var order Order
	if err := c.request("POST", "/orders", params, &order); err != nil {
		return nil, err
	}

	return &order, nil
}

func (c *Client) GetOrder(orderID string) (*Order, error) {
	var order Order
	if err := c.request("GET", "/orders/"+orderID, nil, &order); err != nil {
		return nil, err
	}

	return &order, nil
}

func (c *Client) CreateCustomer(params *CustomerParams) (*Customer, error) {
	var customer Customer
	if err := c.request("POST", "/customers", params, &customer); err != nil {
		return nil, err
	}

	return &customer, nil
}

func (c *Client) CreateToken(params *TokenParams) (*Token, error) {
	var token Token
	if err := c.request("POST", "/tokens", params, &token); err != nil {
		return nil, err
	}

	return &token, nil
}

func (c *Client) CreatePayment(params *PaymentParams) (*Payment, error) {
	var payment Payment
	if err := c.request("POST", "/payments", params, &payment); err != nil {
		return nil, err
	}

	return &payment, nil
}

func (c *Client) CapturePayment(params *CaptureParams) (*Payment, error) {
	var payment Payment
	if err := c.request("POST", "/payments/"+params.PaymentID+"/capture", params, &payment); err != nil {
		return nil, err
	}

	return &payment, nil
}

func (c *Client) GetPayment(paymentID string) (*Payment, error) {
	var payment Payment
	if err := c.request("GET", "/payments/"+paymentID, nil, &payment); err != nil {
		return nil, err
	}

	return &payment, nil
}

func (c *Client) RefundPayment(params *RefundParams) (*Refund, error) {
	var refund Refund
	if err := c.request("POST", "/refunds", params, &refund); err != nil {
		return nil, err
	}

	return &refund, nil
}

func (c *Client) GetRefund(refundID string) (*Refund, error) {
	var refund Refund
	if err := c.request("GET", "/refunds/"+refundID, nil, &refund); err != nil {
		return nil, err
	}

	return &refund, nil
}

func (c *Client) request(method, path string, payload interface{}, out interface{}) error {
	start := time.Now()
	req, err := c.buildRequest(method, path, payload)
	if err != nil {
		c.log.Error("failed to build Razorpay request", "method", method, "path", path, "error", err)
		return err
	}

	resp, err := c.httpClient.Do(req)
	if err != nil {
		c.log.Error("failed Razorpay API call",
			"method", method,
			"path", path,
			"duration_ms", time.Since(start).Milliseconds(),
			"error", err,
		)
		return fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	duration := time.Since(start)
	if resp.StatusCode != http.StatusOK {
		c.log.Error("Razorpay API returned non-200",
			"method", method,
			"path", path,
			"status_code", resp.StatusCode,
			"duration_ms", duration.Milliseconds(),
		)
		return fmt.Errorf("razorpay API error: %s", string(respBody))
	}

	if out != nil {
		if err := json.Unmarshal(respBody, out); err != nil {
			c.log.Error("failed to decode Razorpay response",
				"method", method,
				"path", path,
				"duration_ms", duration.Milliseconds(),
				"error", err,
			)
			return fmt.Errorf("failed to unmarshal response: %w", err)
		}
	}

	c.log.Debug("Razorpay API call completed",
		"method", method,
		"path", path,
		"status_code", resp.StatusCode,
		"duration_ms", duration.Milliseconds(),
	)
	return nil
}

func (c *Client) buildRequest(method, path string, payload interface{}) (*http.Request, error) {
	var body io.Reader
	if payload != nil {
		jsonBody, err := json.Marshal(payload)
		if err != nil {
			return nil, fmt.Errorf("failed to marshal request payload: %w", err)
		}
		body = bytes.NewReader(jsonBody)
	}

	req, err := http.NewRequest(method, c.baseURL+path, body)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}
	req.SetBasicAuth(c.key, c.secret)
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	return req, nil
}
