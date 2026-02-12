package razorpay

import (
	"context"
	"fmt"

	"invoice-backend/pkg/logger"
)

type RazorpayService struct {
	client *Client
	log    *logger.Logger
}

func NewRazorpayService(cfg *Config, log *logger.Logger) *RazorpayService {
	return &RazorpayService{
		client: NewClient(cfg, log),
		log:    log,
	}
}

type CreateOrderRequest struct {
	Amount         int64
	Currency       string
	Receipt        string
	PaymentCapture string
}

func (s *RazorpayService) CreateOrder(ctx context.Context, req *CreateOrderRequest) (string, error) {
	orderParams := &OrderParams{
		Amount:         req.Amount,
		Currency:       req.Currency,
		Receipt:        req.Receipt,
		PaymentCapture: req.PaymentCapture,
	}

	order, err := s.client.CreateOrder(orderParams)
	if err != nil {
		s.log.Error("failed to create Razorpay order", "error", err)
		return "", fmt.Errorf("failed to create order: %w", err)
	}

	s.log.Info("created Razorpay order", "order_id", order.ID, "amount", req.Amount)
	return order.ID, nil
}

type GetOrderRequest struct {
	OrderID string
}

func (s *RazorpayService) GetOrder(ctx context.Context, req *GetOrderRequest) (*Order, error) {
	order, err := s.client.GetOrder(req.OrderID)
	if err != nil {
		s.log.Error("failed to get Razorpay order", "error", err, "order_id", req.OrderID)
		return nil, fmt.Errorf("failed to get order: %w", err)
	}

	return order, nil
}

type CreateCustomerRequest struct {
	Name  string
	Email string
	Phone string
}

func (s *RazorpayService) CreateCustomer(ctx context.Context, req *CreateCustomerRequest) (string, error) {
	customerParams := &CustomerParams{
		Name:  req.Name,
		Email: req.Email,
		Phone: req.Phone,
	}

	customer, err := s.client.CreateCustomer(customerParams)
	if err != nil {
		s.log.Error("failed to create Razorpay customer", "error", err, "email", req.Email)
		return "", fmt.Errorf("failed to create customer: %w", err)
	}

	s.log.Info("created Razorpay customer", "customer_id", customer.ID)
	return customer.ID, nil
}

type CreateTokenRequest struct {
	CustomerID string
	Method     string
	CardNumber string
	CardName   string
	CardExpiry string
	CardCVV    string
}

func (s *RazorpayService) CreateToken(ctx context.Context, req *CreateTokenRequest) (string, error) {
	cardDetails := &CardDetails{
		Number: req.CardNumber,
		Name:   req.CardName,
		Expiry: req.CardExpiry,
		CVV:    req.CardCVV,
	}

	tokenParams := &TokenParams{
		CustomerID: req.CustomerID,
		Method:     req.Method,
		Card:       cardDetails,
	}

	token, err := s.client.CreateToken(tokenParams)
	if err != nil {
		s.log.Error("failed to create Razorpay token", "error", err, "customer_id", req.CustomerID)
		return "", fmt.Errorf("failed to create token: %w", err)
	}

	s.log.Info("created Razorpay token", "token_id", token.ID)
	return token.ID, nil
}

type CreatePaymentRequest struct {
	Amount     int64
	Currency   string
	OrderID    string
	Token      string
	CustomerID string
}

func (s *RazorpayService) CreatePayment(ctx context.Context, req *CreatePaymentRequest) (string, error) {
	paymentParams := &PaymentParams{
		Amount:     req.Amount,
		Currency:   req.Currency,
		OrderID:    req.OrderID,
		Token:      &req.Token,
		CustomerID: &req.CustomerID,
	}

	payment, err := s.client.CreatePayment(paymentParams)
	if err != nil {
		s.log.Error("failed to create Razorpay payment", "error", err, "order_id", req.OrderID)
		return "", fmt.Errorf("failed to create payment: %w", err)
	}

	s.log.Info("created Razorpay payment", "payment_id", payment.ID)
	return payment.ID, nil
}

type CapturePaymentRequest struct {
	PaymentID string
	Amount    int64
	Currency  string
}

func (s *RazorpayService) CapturePayment(ctx context.Context, req *CapturePaymentRequest) (string, error) {
	captureParams := &CaptureParams{
		PaymentID: req.PaymentID,
		Amount:    req.Amount,
		Currency:  req.Currency,
	}

	payment, err := s.client.CapturePayment(captureParams)
	if err != nil {
		s.log.Error("failed to capture Razorpay payment", "error", err, "payment_id", req.PaymentID)
		return "", fmt.Errorf("failed to capture payment: %w", err)
	}

	s.log.Info("captured Razorpay payment", "payment_id", payment.ID)
	return payment.ID, nil
}

type GetPaymentRequest struct {
	PaymentID string
}

func (s *RazorpayService) GetPayment(ctx context.Context, req *GetPaymentRequest) (*Payment, error) {
	payment, err := s.client.GetPayment(req.PaymentID)
	if err != nil {
		s.log.Error("failed to get Razorpay payment", "error", err, "payment_id", req.PaymentID)
		return nil, fmt.Errorf("failed to get payment: %w", err)
	}

	return payment, nil
}

type RefundRequest struct {
	PaymentID string
	Amount    int64
}

func (s *RazorpayService) Refund(ctx context.Context, req *RefundRequest) (string, error) {
	refundParams := &RefundParams{
		PaymentID: req.PaymentID,
		Amount:    req.Amount,
	}

	refund, err := s.client.RefundPayment(refundParams)
	if err != nil {
		s.log.Error("failed to process Razorpay refund", "error", err, "payment_id", req.PaymentID)
		return "", fmt.Errorf("failed to process refund: %w", err)
	}

	s.log.Info("processed Razorpay refund", "refund_id", refund.ID)
	return refund.ID, nil
}

type VerifyWebhookRequest struct {
	RawBody   []byte
	Signature string
}

func (s *RazorpayService) VerifyWebhookSignature(ctx context.Context, req *VerifyWebhookRequest) bool {
	handler := NewWebhookHandler(s.client.webhookSecret)
	return handler.VerifyWebhookSignature(req.RawBody, req.Signature)
}
