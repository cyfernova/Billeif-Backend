package razorpay

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

type Client struct {
	key           string
	secret        string
	webhookSecret string
	baseURL       string
	httpClient    *http.Client
}

type Config struct {
	Key           string
	Secret        string
	WebhookSecret string
	BaseURL       string
	Timeout       time.Duration
}

func NewClient(cfg *Config) *Client {
	baseURL := cfg.BaseURL
	if baseURL == "" {
		baseURL = "https://api.razorpay.com/v1"
	}

	timeout := cfg.Timeout
	if timeout == 0 {
		timeout = 30 * time.Second
	}

	return &Client{
		key:           cfg.Key,
		secret:        cfg.Secret,
		webhookSecret: cfg.WebhookSecret,
		baseURL:       baseURL,
		httpClient: &http.Client{
			Timeout: timeout,
		},
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
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal order params: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/orders", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.key, c.secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("razorpay API error: %s", string(respBody))
	}

	var order Order
	if err := json.Unmarshal(respBody, &order); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &order, nil
}

func (c *Client) GetOrder(orderID string) (*Order, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/orders/"+orderID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.key, c.secret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("razorpay API error: %s", string(respBody))
	}

	var order Order
	if err := json.Unmarshal(respBody, &order); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &order, nil
}

func (c *Client) CreateCustomer(params *CustomerParams) (*Customer, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal customer params: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/customers", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.key, c.secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("razorpay API error: %s", string(respBody))
	}

	var customer Customer
	if err := json.Unmarshal(respBody, &customer); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &customer, nil
}

func (c *Client) CreateToken(params *TokenParams) (*Token, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal token params: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/tokens", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.key, c.secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("razorpay API error: %s", string(respBody))
	}

	var token Token
	if err := json.Unmarshal(respBody, &token); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &token, nil
}

func (c *Client) CreatePayment(params *PaymentParams) (*Payment, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal payment params: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/payments", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.key, c.secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("razorpay API error: %s", string(respBody))
	}

	var payment Payment
	if err := json.Unmarshal(respBody, &payment); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &payment, nil
}

func (c *Client) CapturePayment(params *CaptureParams) (*Payment, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal capture params: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/payments/"+params.PaymentID+"/capture", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.key, c.secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("razorpay API error: %s", string(respBody))
	}

	var payment Payment
	if err := json.Unmarshal(respBody, &payment); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &payment, nil
}

func (c *Client) GetPayment(paymentID string) (*Payment, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/payments/"+paymentID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.key, c.secret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("razorpay API error: %s", string(respBody))
	}

	var payment Payment
	if err := json.Unmarshal(respBody, &payment); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &payment, nil
}

func (c *Client) RefundPayment(params *RefundParams) (*Refund, error) {
	body, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal refund params: %w", err)
	}

	req, err := http.NewRequest("POST", c.baseURL+"/refunds", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.key, c.secret)
	req.Header.Set("Content-Type", "application/json")

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("razorpay API error: %s", string(respBody))
	}

	var refund Refund
	if err := json.Unmarshal(respBody, &refund); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &refund, nil
}

func (c *Client) GetRefund(refundID string) (*Refund, error) {
	req, err := http.NewRequest("GET", c.baseURL+"/refunds/"+refundID, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.SetBasicAuth(c.key, c.secret)

	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("failed to send request: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("razorpay API error: %s", string(respBody))
	}

	var refund Refund
	if err := json.Unmarshal(respBody, &refund); err != nil {
		return nil, fmt.Errorf("failed to unmarshal response: %w", err)
	}

	return &refund, nil
}
