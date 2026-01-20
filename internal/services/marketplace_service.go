package services

import (
	"context"
	"fmt"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"
)

type MarketplaceService struct {
	ap2Repo interfaces.AP2Repository
	log     *logger.Logger
}

func NewMarketplaceService(ap2Repo interfaces.AP2Repository, log *logger.Logger) *MarketplaceService {
	return &MarketplaceService{
		ap2Repo: ap2Repo,
		log:     log,
	}
}

func (s *MarketplaceService) ListProducts(ctx context.Context, filters map[string]interface{}, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	products, total, err := s.ap2Repo.GetMarketplaceProducts(ctx, filters, page, limit)
	if err != nil {
		s.log.Error("failed to list products", "error", err)
		return nil, 0, fmt.Errorf("failed to list products: %w", err)
	}
	return products, total, nil
}

func (s *MarketplaceService) SearchProducts(ctx context.Context, query string, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	products, total, err := s.ap2Repo.SearchMarketplaceProducts(ctx, query, page, limit)
	if err != nil {
		s.log.Error("failed to search products", "error", err, "query", query)
		return nil, 0, fmt.Errorf("failed to search products: %w", err)
	}
	return products, total, nil
}

func (s *MarketplaceService) GetProduct(ctx context.Context, productID string) (*models.MarketplaceProduct, error) {
	product, err := s.ap2Repo.GetMarketplaceProductByID(ctx, productID)
	if err != nil {
		s.log.Error("failed to get product", "error", err, "product_id", productID)
		return nil, fmt.Errorf("failed to get product: %w", err)
	}
	return product, nil
}

func (s *MarketplaceService) GetAvailableProducts(ctx context.Context, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	products, total, err := s.ap2Repo.GetAvailableProducts(ctx, page, limit)
	if err != nil {
		s.log.Error("failed to get available products", "error", err)
		return nil, 0, fmt.Errorf("failed to get available products: %w", err)
	}
	return products, total, nil
}

func (s *MarketplaceService) GetProductsByCategory(ctx context.Context, category string, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	filters := map[string]interface{}{"categories": category}
	return s.ListProducts(ctx, filters, page, limit)
}

func (s *MarketplaceService) GetMerchantProducts(ctx context.Context, agentID string, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	products, total, err := s.ap2Repo.GetProductsByAgent(ctx, agentID, page, limit)
	if err != nil {
		s.log.Error("failed to get merchant products", "error", err, "agent_id", agentID)
		return nil, 0, fmt.Errorf("failed to get merchant products: %w", err)
	}
	return products, total, nil
}

func (s *MarketplaceService) GetOrderByID(ctx context.Context, orderID string) (*models.MarketplaceOrder, error) {
	order, err := s.ap2Repo.GetOrderByID(ctx, orderID)
	if err != nil {
		s.log.Error("failed to get order", "error", err, "order_id", orderID)
		return nil, fmt.Errorf("failed to get order: %w", err)
	}
	return order, nil
}

func (s *MarketplaceService) GetUserOrders(ctx context.Context, userID string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	orders, total, err := s.ap2Repo.GetOrdersByUser(ctx, userID, page, limit)
	if err != nil {
		s.log.Error("failed to get user orders", "error", err, "user_id", userID)
		return nil, 0, fmt.Errorf("failed to get user orders: %w", err)
	}
	return orders, total, nil
}

func (s *MarketplaceService) GetOrdersByStatus(ctx context.Context, userID, status string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	orders, total, err := s.ap2Repo.GetOrdersByUser(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, err
	}

	filteredOrders := make([]*models.MarketplaceOrder, 0)
	for _, order := range orders {
		if order.Status == status {
			filteredOrders = append(filteredOrders, order)
		}
	}

	return filteredOrders, total, nil
}

func (s *MarketplaceService) GetMarketplaceStats(ctx context.Context) (map[string]interface{}, error) {
	availableProducts, _, err := s.ap2Repo.GetAvailableProducts(ctx, 1, 1)
	if err != nil {
		return nil, err
	}

	shoppingAgents, err := s.ap2Repo.GetActiveAgentsByType(ctx, "shopping")
	if err != nil {
		return nil, err
	}

	merchantAgents, err := s.ap2Repo.GetActiveAgentsByType(ctx, "merchant")
	if err != nil {
		return nil, err
	}

	stats := map[string]interface{}{
		"available_products": len(availableProducts),
		"shopping_agents":    len(shoppingAgents),
		"merchant_agents":    len(merchantAgents),
	}

	return stats, nil
}
