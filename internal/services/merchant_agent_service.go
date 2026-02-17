package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

var (
	ErrProductNotFound = errors.New("product not found")
	ErrUnauthorized    = errors.New("unauthorized operation")
)

type MerchantAgentService struct {
	ap2Repo interfaces.AP2Repository
	log     *logger.Logger
}

func NewMerchantAgentService(ap2Repo interfaces.AP2Repository, log *logger.Logger) *MerchantAgentService {
	return &MerchantAgentService{
		ap2Repo: ap2Repo,
		log:     log,
	}
}

type AddProductRequest struct {
	AgentID        string
	Name           string
	Description    *string
	Price          float64
	Currency       string
	InventoryCount int
	Images         []string
	Categories     []string
	IsAvailable    bool
}

func (s *MerchantAgentService) AddProduct(ctx context.Context, req *AddProductRequest) (*models.MarketplaceProduct, error) {
	agent, err := s.ap2Repo.GetAgentByID(ctx, req.AgentID)
	if err != nil {
		return nil, fmt.Errorf("agent not found: %w", err)
	}

	if agent.Type != "merchant" {
		return nil, errors.New("agent must be a merchant agent to add products")
	}

	imagesJSON, _ := json.Marshal(req.Images)
	categoriesJSON, _ := json.Marshal(req.Categories)

	product := &models.MarketplaceProduct{
		AgentID:        req.AgentID,
		Name:           req.Name,
		Description:    req.Description,
		Price:          req.Price,
		Currency:       req.Currency,
		InventoryCount: req.InventoryCount,
		IsAvailable:    req.IsAvailable,
		Images:         string(imagesJSON),
		Categories:     string(categoriesJSON),
	}

	if err := s.ap2Repo.CreateMarketplaceProduct(ctx, product); err != nil {
		s.log.Error("failed to create marketplace product", "error", err, "agent_id", req.AgentID)
		return nil, fmt.Errorf("failed to create product: %w", err)
	}

	s.log.Info("added marketplace product", "product_id", product.ID, "agent_id", req.AgentID)
	return product, nil
}

func (s *MerchantAgentService) GetProduct(ctx context.Context, productID string) (*models.MarketplaceProduct, error) {
	return s.ap2Repo.GetMarketplaceProductByID(ctx, productID)
}

func (s *MerchantAgentService) GetProducts(ctx context.Context, agentID string, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	return s.ap2Repo.GetProductsByAgent(ctx, agentID, page, limit)
}

type UpdateProductRequest struct {
	ProductID      string
	AgentID        string
	Name           *string
	Description    *string
	Price          *float64
	InventoryCount *int
	IsAvailable    *bool
	Images         []string
	Categories     []string
}

func (s *MerchantAgentService) UpdateProduct(ctx context.Context, req *UpdateProductRequest) (*models.MarketplaceProduct, error) {
	product, err := s.ap2Repo.GetMarketplaceProductByID(ctx, req.ProductID)
	if err != nil {
		return nil, ErrProductNotFound
	}

	if product.AgentID != req.AgentID {
		return nil, ErrUnauthorized
	}

	if req.Name != nil {
		product.Name = *req.Name
	}
	if req.Description != nil {
		product.Description = req.Description
	}
	if req.Price != nil {
		product.Price = *req.Price
	}
	if req.InventoryCount != nil {
		product.InventoryCount = *req.InventoryCount
	}
	if req.IsAvailable != nil {
		product.IsAvailable = *req.IsAvailable
	}

	if req.Images != nil {
		imagesJSON, _ := json.Marshal(req.Images)
		product.Images = string(imagesJSON)
	}

	if req.Categories != nil {
		categoriesJSON, _ := json.Marshal(req.Categories)
		product.Categories = string(categoriesJSON)
	}

	if err := s.ap2Repo.UpdateMarketplaceProduct(ctx, product); err != nil {
		s.log.Error("failed to update marketplace product", "error", err, "product_id", req.ProductID)
		return nil, fmt.Errorf("failed to update product: %w", err)
	}

	s.log.Info("updated marketplace product", "product_id", product.ID)
	return product, nil
}

func (s *MerchantAgentService) DeleteProduct(ctx context.Context, productID, agentID string) error {
	product, err := s.ap2Repo.GetMarketplaceProductByID(ctx, productID)
	if err != nil {
		return ErrProductNotFound
	}

	if product.AgentID != agentID {
		return ErrUnauthorized
	}

	if err := s.ap2Repo.DeleteMarketplaceProduct(ctx, productID); err != nil {
		s.log.Error("failed to delete marketplace product", "error", err, "product_id", productID)
		return fmt.Errorf("failed to delete product: %w", err)
	}

	s.log.Info("deleted marketplace product", "product_id", productID)
	return nil
}

func (s *MerchantAgentService) GetOrders(ctx context.Context, merchantAgentID string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	return s.ap2Repo.GetOrdersByAgent(ctx, merchantAgentID, page, limit)
}

func (s *MerchantAgentService) ProcessOrder(ctx context.Context, orderID, merchantAgentID string) (*models.MarketplaceOrder, error) {
	order, err := s.ap2Repo.GetOrderByID(ctx, orderID)
	if err != nil {
		return nil, fmt.Errorf("order not found: %w", err)
	}

	if order.MerchantAgentID != merchantAgentID {
		return nil, ErrUnauthorized
	}

	if err := s.ap2Repo.UpdateOrderStatus(ctx, orderID, "confirmed"); err != nil {
		s.log.Error("failed to update order status", "error", err, "order_id", orderID)
		return nil, fmt.Errorf("failed to process order: %w", err)
	}

	s.log.Info("processed order", "order_id", orderID)
	return order, nil
}

func (s *MerchantAgentService) UpdateOrderStatus(ctx context.Context, orderID, merchantAgentID, status string) error {
	order, err := s.ap2Repo.GetOrderByID(ctx, orderID)
	if err != nil {
		return fmt.Errorf("order not found: %w", err)
	}

	if order.MerchantAgentID != merchantAgentID {
		return ErrUnauthorized
	}

	if err := s.ap2Repo.UpdateOrderStatus(ctx, orderID, status); err != nil {
		s.log.Error("failed to update order status", "error", err, "order_id", orderID)
		return fmt.Errorf("failed to update order status: %w", err)
	}

	s.log.Info("updated order status", "order_id", orderID, "status", status)
	return nil
}

type UpdateShippingInfoRequest struct {
	OrderID           string
	MerchantAgentID   string
	TrackingNumber    string
	EstimatedDelivery string
}

func (s *MerchantAgentService) UpdateShippingInfo(ctx context.Context, req *UpdateShippingInfoRequest) (*models.MarketplaceOrder, error) {
	order, err := s.ap2Repo.GetOrderByID(ctx, req.OrderID)
	if err != nil {
		return nil, fmt.Errorf("order not found: %w", err)
	}

	if order.MerchantAgentID != req.MerchantAgentID {
		return nil, ErrUnauthorized
	}

	order.TrackingNumber = &req.TrackingNumber
	order.Status = "shipped"

	if err := s.ap2Repo.UpdateOrder(ctx, order); err != nil {
		s.log.Error("failed to update shipping info", "error", err, "order_id", req.OrderID)
		return nil, fmt.Errorf("failed to update shipping info: %w", err)
	}

	s.log.Info("updated shipping info", "order_id", req.OrderID)
	return order, nil
}

func (s *MerchantAgentService) GetPendingCarts(ctx context.Context, merchantAgentID string) ([]*models.CartMandate, error) {
	return s.ap2Repo.GetPendingCartMandates(ctx, merchantAgentID)
}

func (s *MerchantAgentService) RespondToCart(ctx context.Context, cartMandateID, merchantAgentID, status, merchantSignature string) error {
	cartMandate, err := s.ap2Repo.GetCartMandateByMerchant(ctx, cartMandateID, merchantAgentID)
	if err != nil {
		return fmt.Errorf("cart mandate not found: %w", err)
	}

	if status == "signed" && merchantSignature != "" {
		if err := s.ap2Repo.SignCartMandate(ctx, cartMandateID, merchantSignature); err != nil {
			s.log.Error("failed to sign cart mandate", "error", err, "cart_mandate_id", cartMandateID)
			return fmt.Errorf("failed to sign cart: %w", err)
		}
	} else if status == "rejected" {
		if err := s.ap2Repo.UpdateCartMandate(ctx, cartMandate); err != nil {
			return fmt.Errorf("failed to reject cart: %w", err)
		}
	}

	s.log.Info("responded to cart mandate", "cart_mandate_id", cartMandateID, "status", status)
	return nil
}

func (s *MerchantAgentService) GetProductInventory(ctx context.Context, agentID string) (map[string]int, error) {
	products, _, err := s.ap2Repo.GetProductsByAgent(ctx, agentID, 1, 1000)
	if err != nil {
		return nil, err
	}

	inventory := make(map[string]int)
	for _, product := range products {
		inventory[product.ID] = product.InventoryCount
	}

	return inventory, nil
}
