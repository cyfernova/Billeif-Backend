package services

import (
	"context"
	"errors"
	"fmt"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

var (
	ErrPaymentMandateNotFound = errors.New("payment mandate not found")
	ErrInvalidPaymentStatus   = errors.New("invalid payment status")
)

type PaymentProcessorService struct {
	ap2Repo interfaces.AP2Repository
	log     *logger.Logger
}

func NewPaymentProcessorService(ap2Repo interfaces.AP2Repository, log *logger.Logger) *PaymentProcessorService {
	return &PaymentProcessorService{
		ap2Repo: ap2Repo,
		log:     log,
	}
}

type ProcessPaymentRequest struct {
	PaymentMandateID  string
	RazorpayOrderID   string
	RazorpayPaymentID string
	Status            string
}

func (s *PaymentProcessorService) ProcessPayment(ctx context.Context, req *ProcessPaymentRequest) error {
	paymentMandate, err := s.ap2Repo.GetPaymentMandateByID(ctx, req.PaymentMandateID)
	if err != nil {
		s.log.Error("payment mandate not found", "error", err, "payment_mandate_id", req.PaymentMandateID)
		return ErrPaymentMandateNotFound
	}

	validStatuses := map[string]bool{
		"authorized": true,
		"captured":   true,
		"failed":     true,
		"refunded":   true,
	}

	if !validStatuses[req.Status] {
		return ErrInvalidPaymentStatus
	}

	paymentMandate.RazorpayOrderID = &req.RazorpayOrderID
	paymentMandate.RazorpayPaymentID = &req.RazorpayPaymentID
	paymentMandate.Status = req.Status

	if err := s.ap2Repo.UpdatePaymentMandate(ctx, paymentMandate); err != nil {
		s.log.Error("failed to update payment mandate", "error", err, "payment_mandate_id", req.PaymentMandateID)
		return fmt.Errorf("failed to update payment mandate: %w", err)
	}

	validStatuses = map[string]bool{
		"authorized": true,
		"captured":   true,
		"failed":     true,
		"refunded":   true,
	}

	if !validStatuses[req.Status] {
		return ErrInvalidPaymentStatus
	}

	paymentMandate.RazorpayOrderID = &req.RazorpayOrderID
	paymentMandate.RazorpayPaymentID = &req.RazorpayPaymentID
	paymentMandate.Status = req.Status

	if err := s.ap2Repo.UpdatePaymentMandate(ctx, paymentMandate); err != nil {
		s.log.Error("failed to update payment mandate", "error", err, "payment_mandate_id", req.PaymentMandateID)
		return fmt.Errorf("failed to update payment mandate: %w", err)
	}

	if req.Status == "captured" {
		if err := s.updateOrderStatus(ctx, paymentMandate.CartMandateID, "paid"); err != nil {
			s.log.Error("failed to update order status", "error", err, "cart_mandate_id", paymentMandate.CartMandateID)
			return fmt.Errorf("failed to update order status: %w", err)
		}
	}

	s.log.Info("processed payment", "payment_mandate_id", req.PaymentMandateID, "status", req.Status)
	return nil
}

func (s *PaymentProcessorService) GetPaymentMandate(ctx context.Context, mandateID string) (*models.PaymentMandate, error) {
	paymentMandate, err := s.ap2Repo.GetPaymentMandateByID(ctx, mandateID)
	if err != nil {
		s.log.Error("failed to get payment mandate", "error", err, "mandate_id", mandateID)
		return nil, ErrPaymentMandateNotFound
	}
	return paymentMandate, nil
}

func (s *PaymentProcessorService) GetPaymentMandateByRazorpayOrder(ctx context.Context, orderID string) (*models.PaymentMandate, error) {
	paymentMandate, err := s.ap2Repo.GetPaymentMandateByRazorpayOrder(ctx, orderID)
	if err != nil {
		s.log.Error("failed to get payment mandate by razorpay order", "error", err, "order_id", orderID)
		return nil, ErrPaymentMandateNotFound
	}
	return paymentMandate, nil
}

func (s *PaymentProcessorService) GetUserPaymentMandates(ctx context.Context, userID string, page, limit int) ([]*models.PaymentMandate, int64, error) {
	paymentMandates, total, err := s.ap2Repo.GetPaymentMandatesByUser(ctx, userID, page, limit)
	if err != nil {
		s.log.Error("failed to get user payment mandates", "error", err, "user_id", userID)
		return nil, 0, fmt.Errorf("failed to get user payment mandates: %w", err)
	}
	return paymentMandates, total, nil
}

func (s *PaymentProcessorService) AuthorizePayment(ctx context.Context, mandateID string) error {
	if err := s.ap2Repo.UpdatePaymentMandateStatus(ctx, mandateID, "authorized"); err != nil {
		s.log.Error("failed to authorize payment", "error", err, "mandate_id", mandateID)
		return fmt.Errorf("failed to authorize payment: %w", err)
	}

	s.log.Info("authorized payment", "mandate_id", mandateID)
	return nil
}

func (s *PaymentProcessorService) CapturePayment(ctx context.Context, mandateID, razorpayPaymentID string) error {
	paymentMandate, err := s.ap2Repo.GetPaymentMandateByID(ctx, mandateID)
	if err != nil {
		return ErrPaymentMandateNotFound
	}

	paymentMandate.RazorpayPaymentID = &razorpayPaymentID
	paymentMandate.Status = "captured"

	if err := s.ap2Repo.UpdatePaymentMandate(ctx, paymentMandate); err != nil {
		s.log.Error("failed to capture payment", "error", err, "mandate_id", mandateID)
		return fmt.Errorf("failed to capture payment: %w", err)
	}

	if err := s.updateOrderStatus(ctx, paymentMandate.CartMandateID, "paid"); err != nil {
		s.log.Error("failed to update order status", "error", err, "cart_mandate_id", paymentMandate.CartMandateID)
		return fmt.Errorf("failed to update order status: %w", err)
	}

	s.log.Info("captured payment", "mandate_id", mandateID)
	return nil
}

func (s *PaymentProcessorService) FailPayment(ctx context.Context, mandateID string, reason string) error {
	if err := s.ap2Repo.UpdatePaymentMandateStatus(ctx, mandateID, "failed"); err != nil {
		s.log.Error("failed to fail payment", "error", err, "mandate_id", mandateID)
		return fmt.Errorf("failed to fail payment: %w", err)
	}

	s.log.Info("payment failed", "mandate_id", mandateID, "reason", reason)
	return nil
}

func (s *PaymentProcessorService) RefundPayment(ctx context.Context, mandateID string) error {
	if err := s.ap2Repo.UpdatePaymentMandateStatus(ctx, mandateID, "refunded"); err != nil {
		s.log.Error("failed to refund payment", "error", err, "mandate_id", mandateID)
		return fmt.Errorf("failed to refund payment: %w", err)
	}

	paymentMandate, err := s.ap2Repo.GetPaymentMandateByID(ctx, mandateID)
	if err != nil {
		return ErrPaymentMandateNotFound
	}

	if err := s.updateOrderStatus(ctx, paymentMandate.CartMandateID, "cancelled"); err != nil {
		s.log.Error("failed to update order status", "error", err, "cart_mandate_id", paymentMandate.CartMandateID)
		return fmt.Errorf("failed to update order status: %w", err)
	}

	s.log.Info("refunded payment", "mandate_id", mandateID)
	return nil
}

func (s *PaymentProcessorService) updateOrderStatus(ctx context.Context, cartMandateID, status string) error {
	order, err := s.ap2Repo.GetOrderByCartMandate(ctx, cartMandateID)
	if err != nil {
		return err
	}

	return s.ap2Repo.UpdateOrderStatus(ctx, order.ID, status)
}
