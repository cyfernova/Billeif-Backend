package unit

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockMarketplaceAP2Repository mocks the AP2Repository interface for marketplace tests
type MockMarketplaceAP2Repository struct {
	mock.Mock
}

func (m *MockMarketplaceAP2Repository) RegisterAgent(ctx context.Context, registry *models.AgentRegistry) error {
	args := m.Called(ctx, registry)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetAgentByID(ctx context.Context, id string) (*models.Agent, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Agent), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) HasAgentOwnership(ctx context.Context, ownerID, agentID string) (bool, error) {
	args := m.Called(ctx, ownerID, agentID)
	return args.Bool(0), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetAgentsByUser(ctx context.Context, userID string, page, limit int) ([]*models.Agent, int64, error) {
	args := m.Called(ctx, userID, page, limit)
	return args.Get(0).([]*models.Agent), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetAgentsByBusiness(ctx context.Context, businessID string, page, limit int) ([]*models.Agent, int64, error) {
	args := m.Called(ctx, businessID, page, limit)
	return args.Get(0).([]*models.Agent), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetAgentsByType(ctx context.Context, agentType string, page, limit int) ([]*models.Agent, int64, error) {
	args := m.Called(ctx, agentType, page, limit)
	return args.Get(0).([]*models.Agent), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetAgents(ctx context.Context, page, limit int) ([]*models.Agent, int64, error) {
	args := m.Called(ctx, page, limit)
	return args.Get(0).([]*models.Agent), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetActiveAgentsByType(ctx context.Context, agentType string) ([]*models.Agent, error) {
	args := m.Called(ctx, agentType)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.Agent), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) SearchAgentsByCategories(ctx context.Context, categories []string, budget *float64, page, limit int) ([]*models.Agent, int64, error) {
	args := m.Called(ctx, categories, budget, page, limit)
	return args.Get(0).([]*models.Agent), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) SearchAgentsByBudget(ctx context.Context, budget float64, page, limit int) ([]*models.Agent, int64, error) {
	args := m.Called(ctx, budget, page, limit)
	return args.Get(0).([]*models.Agent), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) UpdateAgent(ctx context.Context, agent *models.Agent) error {
	args := m.Called(ctx, agent)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) DeleteAgent(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) CreateAgent(ctx context.Context, agent *models.Agent) error {
	args := m.Called(ctx, agent)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) CreateAgentWithCapabilities(ctx context.Context, agent *models.Agent, capabilities []*models.AgentCapability) error {
	args := m.Called(ctx, agent, capabilities)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) CreateAgentWithDiscovery(ctx context.Context, agent *models.Agent, capabilities []*models.AgentCapability, registry *models.AgentRegistry) error {
	args := m.Called(ctx, agent, capabilities, registry)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) CreateAgentCapability(ctx context.Context, capability *models.AgentCapability) error {
	args := m.Called(ctx, capability)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetCapabilitiesByAgent(ctx context.Context, agentID string) ([]*models.AgentCapability, error) {
	args := m.Called(ctx, agentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.AgentCapability), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) DeleteCapability(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetAgentRegistry(ctx context.Context, agentID string) (*models.AgentRegistry, error) {
	args := m.Called(ctx, agentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.AgentRegistry), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetAgentRegistryByID(ctx context.Context, registryID string) (*models.AgentRegistry, error) {
	args := m.Called(ctx, registryID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.AgentRegistry), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) UpdateAgentRegistry(ctx context.Context, registry *models.AgentRegistry) error {
	args := m.Called(ctx, registry)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) DeleteAgentRegistry(ctx context.Context, registryID string) error {
	args := m.Called(ctx, registryID)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) SearchAgents(ctx context.Context, filter *models.AgentDiscoveryFilter, page, limit int) ([]*models.AgentRegistry, int64, error) {
	args := m.Called(ctx, filter, page, limit)
	return args.Get(0).([]*models.AgentRegistry), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) DiscoverAgentsByCapability(ctx context.Context, capabilities []string, page, limit int) ([]*models.AgentRegistry, int64, error) {
	args := m.Called(ctx, capabilities, page, limit)
	return args.Get(0).([]*models.AgentRegistry), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetPublicAgents(ctx context.Context, page, limit int) ([]*models.AgentRegistry, int64, error) {
	args := m.Called(ctx, page, limit)
	return args.Get(0).([]*models.AgentRegistry), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetVerifiedAgents(ctx context.Context, agentType string, page, limit int) ([]*models.AgentRegistry, int64, error) {
	args := m.Called(ctx, agentType, page, limit)
	return args.Get(0).([]*models.AgentRegistry), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) VerifyAgentRegistry(ctx context.Context, registryID string) error {
	args := m.Called(ctx, registryID)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) UnverifyAgentRegistry(ctx context.Context, registryID string) error {
	args := m.Called(ctx, registryID)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) DeactivateAgentRegistry(ctx context.Context, registryID string) error {
	args := m.Called(ctx, registryID)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) ActivateAgentRegistry(ctx context.Context, registryID string) error {
	args := m.Called(ctx, registryID)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) UpdateAgentRegistryHealthCheck(ctx context.Context, registryID, status, message string) error {
	args := m.Called(ctx, registryID, status, message)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) CreateDiscoveryAudit(ctx context.Context, audit *models.AgentDiscoveryAudit) error {
	args := m.Called(ctx, audit)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetDiscoveryAuditByID(ctx context.Context, id string) (*models.AgentDiscoveryAudit, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.AgentDiscoveryAudit), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetDiscoveryAuditByAgent(ctx context.Context, agentRegistryID string, page, limit int) ([]*models.AgentDiscoveryAudit, int64, error) {
	args := m.Called(ctx, agentRegistryID, page, limit)
	return args.Get(0).([]*models.AgentDiscoveryAudit), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetDiscoveryStats(ctx context.Context, agentRegistryID string) (*models.AgentDiscoveryStats, error) {
	args := m.Called(ctx, agentRegistryID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.AgentDiscoveryStats), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) UpdateDiscoveryStats(ctx context.Context, stats *models.AgentDiscoveryStats) error {
	args := m.Called(ctx, stats)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) IncrementAgentViews(ctx context.Context, agentRegistryID string) error {
	args := m.Called(ctx, agentRegistryID)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) IncrementAgentSearchFound(ctx context.Context, agentRegistryID string) error {
	args := m.Called(ctx, agentRegistryID)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) IncrementAgentInquiries(ctx context.Context, agentRegistryID string) error {
	args := m.Called(ctx, agentRegistryID)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) IncrementAgentIntegrations(ctx context.Context, agentRegistryID string) error {
	args := m.Called(ctx, agentRegistryID)
	return args.Error(0)
}

// Marketplace methods with proper mock implementation
func (m *MockMarketplaceAP2Repository) GetMarketplaceProductByID(ctx context.Context, id string) (*models.MarketplaceProduct, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.MarketplaceProduct), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetMarketplaceProducts(ctx context.Context, filters map[string]interface{}, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	args := m.Called(ctx, filters, page, limit)
	var products []*models.MarketplaceProduct
	if args.Get(0) != nil {
		products = args.Get(0).([]*models.MarketplaceProduct)
	}
	return products, args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) SearchMarketplaceProducts(ctx context.Context, query string, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	args := m.Called(ctx, query, page, limit)
	var products []*models.MarketplaceProduct
	if args.Get(0) != nil {
		products = args.Get(0).([]*models.MarketplaceProduct)
	}
	return products, args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetProductsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	args := m.Called(ctx, agentID, page, limit)
	var products []*models.MarketplaceProduct
	if args.Get(0) != nil {
		products = args.Get(0).([]*models.MarketplaceProduct)
	}
	return products, args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetAvailableProducts(ctx context.Context, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	args := m.Called(ctx, page, limit)
	var products []*models.MarketplaceProduct
	if args.Get(0) != nil {
		products = args.Get(0).([]*models.MarketplaceProduct)
	}
	return products, args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) UpdateMarketplaceProduct(ctx context.Context, product *models.MarketplaceProduct) error {
	args := m.Called(ctx, product)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) DeleteMarketplaceProduct(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) ReserveMarketplaceInventory(ctx context.Context, productID string, quantity int) error {
	args := m.Called(ctx, productID, quantity)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) ReleaseMarketplaceInventory(ctx context.Context, productID string, quantity int) error {
	args := m.Called(ctx, productID, quantity)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) CommitMarketplaceInventory(ctx context.Context, productID string, quantity int) error {
	args := m.Called(ctx, productID, quantity)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetOrderByID(ctx context.Context, id string) (*models.MarketplaceOrder, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.MarketplaceOrder), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetOrdersByUser(ctx context.Context, userID string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	args := m.Called(ctx, userID, page, limit)
	var orders []*models.MarketplaceOrder
	if args.Get(0) != nil {
		orders = args.Get(0).([]*models.MarketplaceOrder)
	}
	return orders, args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetOrdersByUserAndStatus(ctx context.Context, userID, status string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	args := m.Called(ctx, userID, status, page, limit)
	var orders []*models.MarketplaceOrder
	if args.Get(0) != nil {
		orders = args.Get(0).([]*models.MarketplaceOrder)
	}
	return orders, args.Get(1).(int64), args.Error(2)
}

// Intent Mandates
func (m *MockMarketplaceAP2Repository) CreateIntentMandate(ctx context.Context, mandate *models.IntentMandate) error {
	args := m.Called(ctx, mandate)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetIntentMandateByID(ctx context.Context, id, userID string) (*models.IntentMandate, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.IntentMandate), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetActiveIntentMandatesByUser(ctx context.Context, userID string) ([]*models.IntentMandate, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.IntentMandate), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetIntentMandatesByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.IntentMandate, int64, error) {
	args := m.Called(ctx, agentID, page, limit)
	return args.Get(0).([]*models.IntentMandate), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) UpdateIntentMandateStatus(ctx context.Context, id, status string) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) RevokeIntentMandate(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

// Cart Mandates
func (m *MockMarketplaceAP2Repository) CreateCartMandate(ctx context.Context, mandate *models.CartMandate) error {
	args := m.Called(ctx, mandate)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetCartMandateByID(ctx context.Context, id, userID string) (*models.CartMandate, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.CartMandate), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetCartMandateByMerchant(ctx context.Context, id, merchantID string) (*models.CartMandate, error) {
	args := m.Called(ctx, id, merchantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.CartMandate), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetCartMandatesByUser(ctx context.Context, userID string, page, limit int) ([]*models.CartMandate, int64, error) {
	args := m.Called(ctx, userID, page, limit)
	return args.Get(0).([]*models.CartMandate), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) UpdateCartMandate(ctx context.Context, mandate *models.CartMandate) error {
	args := m.Called(ctx, mandate)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) SignCartMandate(ctx context.Context, id, signature string) error {
	args := m.Called(ctx, id, signature)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetPendingCartMandates(ctx context.Context, merchantID string) ([]*models.CartMandate, error) {
	args := m.Called(ctx, merchantID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.CartMandate), args.Error(1)
}

// Payment Mandates
func (m *MockMarketplaceAP2Repository) CreatePaymentMandate(ctx context.Context, mandate *models.PaymentMandate) error {
	args := m.Called(ctx, mandate)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetPaymentMandateByID(ctx context.Context, id string) (*models.PaymentMandate, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.PaymentMandate), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetPaymentMandatesByUser(ctx context.Context, userID string, page, limit int) ([]*models.PaymentMandate, int64, error) {
	args := m.Called(ctx, userID, page, limit)
	return args.Get(0).([]*models.PaymentMandate), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) UpdatePaymentMandate(ctx context.Context, mandate *models.PaymentMandate) error {
	args := m.Called(ctx, mandate)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) UpdatePaymentMandateStatus(ctx context.Context, id, status string) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetPaymentMandateByRazorpayOrder(ctx context.Context, orderID string) (*models.PaymentMandate, error) {
	args := m.Called(ctx, orderID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.PaymentMandate), args.Error(1)
}

// Credentials
func (m *MockMarketplaceAP2Repository) CreatePaymentCredential(ctx context.Context, credential *models.PaymentCredential) error {
	args := m.Called(ctx, credential)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetPaymentCredentialsByUser(ctx context.Context, userID string) ([]*models.PaymentCredential, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.PaymentCredential), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetPaymentCredentialByID(ctx context.Context, id string) (*models.PaymentCredential, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.PaymentCredential), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetDefaultCredential(ctx context.Context, userID string) (*models.PaymentCredential, error) {
	args := m.Called(ctx, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.PaymentCredential), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) UpdateCredential(ctx context.Context, id string, updates map[string]interface{}) error {
	args := m.Called(ctx, id, updates)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) DeleteCredential(ctx context.Context, id string) error {
	args := m.Called(ctx, id)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) SetDefaultCredential(ctx context.Context, userID, credentialID string) error {
	args := m.Called(ctx, userID, credentialID)
	return args.Error(0)
}

// Credential Tokens
func (m *MockMarketplaceAP2Repository) CreateCredentialToken(ctx context.Context, token *models.CredentialToken) error {
	args := m.Called(ctx, token)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetCredentialToken(ctx context.Context, token string) (*models.CredentialToken, error) {
	args := m.Called(ctx, token)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.CredentialToken), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) MarkTokenAsUsed(ctx context.Context, tokenID string) error {
	args := m.Called(ctx, tokenID)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetValidTokensByCredential(ctx context.Context, credentialID string) ([]*models.CredentialToken, error) {
	args := m.Called(ctx, credentialID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.CredentialToken), args.Error(1)
}

// Marketplace
func (m *MockMarketplaceAP2Repository) CreateMarketplaceProduct(ctx context.Context, product *models.MarketplaceProduct) error {
	args := m.Called(ctx, product)
	return args.Error(0)
}

// Procurement
func (m *MockMarketplaceAP2Repository) CreateProcurementRun(ctx context.Context, run *models.ProcurementRun) error {
	args := m.Called(ctx, run)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetProcurementRunByID(ctx context.Context, id, userID string) (*models.ProcurementRun, error) {
	args := m.Called(ctx, id, userID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.ProcurementRun), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetProcurementRunByIdempotencyKey(ctx context.Context, userID, shoppingAgentID, idempotencyKey string) (*models.ProcurementRun, error) {
	args := m.Called(ctx, userID, shoppingAgentID, idempotencyKey)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.ProcurementRun), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) UpdateProcurementRun(ctx context.Context, run *models.ProcurementRun) error {
	args := m.Called(ctx, run)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) CreateProcurementCandidate(ctx context.Context, candidate *models.ProcurementCandidate) error {
	args := m.Called(ctx, candidate)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) UpdateProcurementCandidate(ctx context.Context, candidate *models.ProcurementCandidate) error {
	args := m.Called(ctx, candidate)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetProcurementCandidatesByRun(ctx context.Context, runID string) ([]*models.ProcurementCandidate, error) {
	args := m.Called(ctx, runID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.ProcurementCandidate), args.Error(1)
}

// Marketplace Orders
func (m *MockMarketplaceAP2Repository) CreateOrder(ctx context.Context, order *models.MarketplaceOrder) error {
	args := m.Called(ctx, order)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetOrdersByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	args := m.Called(ctx, agentID, page, limit)
	return args.Get(0).([]*models.MarketplaceOrder), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) UpdateOrder(ctx context.Context, order *models.MarketplaceOrder) error {
	args := m.Called(ctx, order)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) UpdateOrderStatus(ctx context.Context, orderID, status string) error {
	args := m.Called(ctx, orderID, status)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetOrderByCartMandate(ctx context.Context, cartMandateID string) (*models.MarketplaceOrder, error) {
	args := m.Called(ctx, cartMandateID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.MarketplaceOrder), args.Error(1)
}

// A2A Messages
func (m *MockMarketplaceAP2Repository) CreateA2AMessage(ctx context.Context, message *models.A2AMessage) error {
	args := m.Called(ctx, message)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetA2AMessageByID(ctx context.Context, id string) (*models.A2AMessage, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.A2AMessage), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetMessagesBySender(ctx context.Context, senderAgentID string, page, limit int) ([]*models.A2AMessage, int64, error) {
	args := m.Called(ctx, senderAgentID, page, limit)
	return args.Get(0).([]*models.A2AMessage), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetMessagesByReceiver(ctx context.Context, receiverAgentID string, page, limit int) ([]*models.A2AMessage, int64, error) {
	args := m.Called(ctx, receiverAgentID, page, limit)
	return args.Get(0).([]*models.A2AMessage), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) UpdateMessageStatus(ctx context.Context, id, status string) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) UpdateMessageResponse(ctx context.Context, id string, response string) error {
	args := m.Called(ctx, id, response)
	return args.Error(0)
}

// Agent Transactions
func (m *MockMarketplaceAP2Repository) CreateAgentTransaction(ctx context.Context, transaction *models.AgentTransaction) error {
	args := m.Called(ctx, transaction)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetTransactionsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.AgentTransaction, int64, error) {
	args := m.Called(ctx, agentID, page, limit)
	return args.Get(0).([]*models.AgentTransaction), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetTransactionsByUser(ctx context.Context, userID string, page, limit int) ([]*models.AgentTransaction, int64, error) {
	args := m.Called(ctx, userID, page, limit)
	return args.Get(0).([]*models.AgentTransaction), args.Get(1).(int64), args.Error(2)
}

// Bargaining Negotiations
func (m *MockMarketplaceAP2Repository) CreateBargainingNegotiation(ctx context.Context, negotiation *models.BargainingNegotiation) error {
	args := m.Called(ctx, negotiation)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetBargainingNegotiationByID(ctx context.Context, id string) (*models.BargainingNegotiation, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.BargainingNegotiation), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetBargainingNegotiationBySessionID(ctx context.Context, sessionID string) (*models.BargainingNegotiation, error) {
	args := m.Called(ctx, sessionID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.BargainingNegotiation), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetNegotiationsByUser(ctx context.Context, userID string, page, limit int) ([]*models.BargainingNegotiation, int64, error) {
	args := m.Called(ctx, userID, page, limit)
	return args.Get(0).([]*models.BargainingNegotiation), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetNegotiationsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.BargainingNegotiation, int64, error) {
	args := m.Called(ctx, agentID, page, limit)
	return args.Get(0).([]*models.BargainingNegotiation), args.Get(1).(int64), args.Error(2)
}

func (m *MockMarketplaceAP2Repository) GetNegotiationsInProgress(ctx context.Context, limit int) ([]*models.BargainingNegotiation, error) {
	args := m.Called(ctx, limit)
	return args.Get(0).([]*models.BargainingNegotiation), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) UpdateNegotiationStatus(ctx context.Context, id, status string) error {
	args := m.Called(ctx, id, status)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) UpdateNegotiationAmountAndRounds(ctx context.Context, id string, amount float64, rounds int, status string) error {
	args := m.Called(ctx, id, amount, rounds, status)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) CompleteNegotiation(ctx context.Context, id, status string, finalAmount float64, completedAt *time.Time) error {
	args := m.Called(ctx, id, status, finalAmount, completedAt)
	return args.Error(0)
}

// Bargaining Rounds
func (m *MockMarketplaceAP2Repository) CreateBargainingRound(ctx context.Context, round *models.BargainingRound) error {
	args := m.Called(ctx, round)
	return args.Error(0)
}

func (m *MockMarketplaceAP2Repository) GetBargainingRounds(ctx context.Context, negotiationID string) ([]*models.BargainingRound, error) {
	args := m.Called(ctx, negotiationID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]*models.BargainingRound), args.Error(1)
}

func (m *MockMarketplaceAP2Repository) GetBargainingRoundsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.BargainingRound, int64, error) {
	args := m.Called(ctx, agentID, page, limit)
	return args.Get(0).([]*models.BargainingRound), args.Get(1).(int64), args.Error(2)
}

// TestListProducts_Success tests successful product listing
func TestMarketplaceService_ListProducts_Success(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	filters := map[string]interface{}{"category": "electronics"}
	page := 1
	limit := 10

	expectedProducts := []*models.MarketplaceProduct{
		{ID: "prod-1", Name: "Product 1", Price: 100.00},
		{ID: "prod-2", Name: "Product 2", Price: 200.00},
	}

	mockAP2.On("GetMarketplaceProducts", ctx, filters, page, limit).Return(expectedProducts, int64(2), nil)

	products, total, err := svc.ListProducts(ctx, filters, page, limit)

	assert.NoError(t, err)
	assert.Len(t, products, 2)
	assert.Equal(t, int64(2), total)
	mockAP2.AssertExpectations(t)
}

// TestListProducts_EmptyResult tests product listing with no results
func TestMarketplaceService_ListProducts_EmptyResult(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	filters := map[string]interface{}{"category": "nonexistent"}
	page := 1
	limit := 10

	mockAP2.On("GetMarketplaceProducts", ctx, filters, page, limit).Return([]*models.MarketplaceProduct{}, int64(0), nil)

	products, total, err := svc.ListProducts(ctx, filters, page, limit)

	assert.NoError(t, err)
	assert.Len(t, products, 0)
	assert.Equal(t, int64(0), total)
	mockAP2.AssertExpectations(t)
}

// TestListProducts_RepositoryError tests product listing with error
func TestMarketplaceService_ListProducts_RepositoryError(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	filters := map[string]interface{}{}
	page := 1
	limit := 10

	mockAP2.On("GetMarketplaceProducts", ctx, filters, page, limit).Return(nil, int64(0), errors.New("database error"))

	products, total, err := svc.ListProducts(ctx, filters, page, limit)

	assert.Error(t, err)
	assert.Nil(t, products)
	assert.Equal(t, int64(0), total)
	assert.Contains(t, err.Error(), "failed to list products")
	mockAP2.AssertExpectations(t)
}

// TestSearchProducts_Success tests successful product search
func TestMarketplaceService_SearchProducts_Success(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	query := "laptop"
	page := 1
	limit := 10

	expectedProducts := []*models.MarketplaceProduct{
		{ID: "prod-1", Name: "Gaming Laptop", Price: 1500.00},
		{ID: "prod-2", Name: "Business Laptop", Price: 1200.00},
	}

	mockAP2.On("SearchMarketplaceProducts", ctx, query, page, limit).Return(expectedProducts, int64(2), nil)

	products, total, err := svc.SearchProducts(ctx, query, page, limit)

	assert.NoError(t, err)
	assert.Len(t, products, 2)
	assert.Equal(t, int64(2), total)
	mockAP2.AssertExpectations(t)
}

// TestSearchProducts_EmptyResult tests product search with no results
func TestMarketplaceService_SearchProducts_EmptyResult(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	query := "nonexistentproduct"
	page := 1
	limit := 10

	mockAP2.On("SearchMarketplaceProducts", ctx, query, page, limit).Return([]*models.MarketplaceProduct{}, int64(0), nil)

	products, total, err := svc.SearchProducts(ctx, query, page, limit)

	assert.NoError(t, err)
	assert.Len(t, products, 0)
	assert.Equal(t, int64(0), total)
	mockAP2.AssertExpectations(t)
}

// TestSearchProducts_RepositoryError tests product search with error
func TestMarketplaceService_SearchProducts_RepositoryError(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	query := "laptop"
	page := 1
	limit := 10

	mockAP2.On("SearchMarketplaceProducts", ctx, query, page, limit).Return(nil, int64(0), errors.New("search error"))

	products, _, err := svc.SearchProducts(ctx, query, page, limit)

	assert.Error(t, err)
	assert.Nil(t, products)
	assert.Contains(t, err.Error(), "failed to search products")
	mockAP2.AssertExpectations(t)
}

// TestGetProduct_Success tests successful product retrieval
func TestMarketplaceService_GetProduct_Success(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	productID := "prod-123"

	expected := &models.MarketplaceProduct{
		ID:    productID,
		Name:  "Test Product",
		Price: 299.99,
	}

	mockAP2.On("GetMarketplaceProductByID", ctx, productID).Return(expected, nil)

	product, err := svc.GetProduct(ctx, productID)

	assert.NoError(t, err)
	assert.NotNil(t, product)
	assert.Equal(t, productID, product.ID)
	mockAP2.AssertExpectations(t)
}

// TestGetProduct_NotFound tests product not found
func TestMarketplaceService_GetProduct_NotFound(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	productID := "nonexistent"

	mockAP2.On("GetMarketplaceProductByID", ctx, productID).Return(nil, errors.New("product not found"))

	product, err := svc.GetProduct(ctx, productID)

	assert.Error(t, err)
	assert.Nil(t, product)
	assert.Contains(t, err.Error(), "failed to get product")
	mockAP2.AssertExpectations(t)
}

// TestGetAvailableProducts_Success tests successful available products retrieval
func TestMarketplaceService_GetAvailableProducts_Success(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	page := 1
	limit := 10

	expectedProducts := []*models.MarketplaceProduct{
		{ID: "prod-1", Name: "Available 1", IsAvailable: true},
		{ID: "prod-2", Name: "Available 2", IsAvailable: true},
	}

	mockAP2.On("GetAvailableProducts", ctx, page, limit).Return(expectedProducts, int64(2), nil)

	products, total, err := svc.GetAvailableProducts(ctx, page, limit)

	assert.NoError(t, err)
	assert.Len(t, products, 2)
	assert.Equal(t, int64(2), total)
	mockAP2.AssertExpectations(t)
}

// TestGetAvailableProducts_RepositoryError tests available products retrieval with error
func TestMarketplaceService_GetAvailableProducts_RepositoryError(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	page := 1
	limit := 10

	mockAP2.On("GetAvailableProducts", ctx, page, limit).Return(nil, int64(0), errors.New("database error"))

	products, _, err := svc.GetAvailableProducts(ctx, page, limit)

	assert.Error(t, err)
	assert.Nil(t, products)
	assert.Contains(t, err.Error(), "failed to get available products")
	mockAP2.AssertExpectations(t)
}

// TestGetProductsByCategory_Success tests successful products by category retrieval
func TestMarketplaceService_GetProductsByCategory_Success(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	category := "electronics"
	page := 1
	limit := 10

	expectedProducts := []*models.MarketplaceProduct{
		{ID: "prod-1", Name: "TV", Categories: `["electronics"]`},
	}

	mockAP2.On("GetMarketplaceProducts", ctx, map[string]interface{}{"categories": category}, page, limit).Return(expectedProducts, int64(1), nil)

	products, total, err := svc.GetProductsByCategory(ctx, category, page, limit)

	assert.NoError(t, err)
	assert.Len(t, products, 1)
	assert.Equal(t, int64(1), total)
	mockAP2.AssertExpectations(t)
}

// TestGetMerchantProducts_Success tests successful merchant products retrieval
func TestMarketplaceService_GetMerchantProducts_Success(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	agentID := "agent-123"
	page := 1
	limit := 10

	expectedProducts := []*models.MarketplaceProduct{
		{ID: "prod-1", AgentID: agentID, Name: "Merchant Product 1"},
		{ID: "prod-2", AgentID: agentID, Name: "Merchant Product 2"},
	}

	mockAP2.On("GetProductsByAgent", ctx, agentID, page, limit).Return(expectedProducts, int64(2), nil)

	products, total, err := svc.GetMerchantProducts(ctx, agentID, page, limit)

	assert.NoError(t, err)
	assert.Len(t, products, 2)
	assert.Equal(t, int64(2), total)
	mockAP2.AssertExpectations(t)
}

// TestGetMerchantProducts_RepositoryError tests merchant products retrieval with error
func TestMarketplaceService_GetMerchantProducts_RepositoryError(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	agentID := "agent-123"
	page := 1
	limit := 10

	mockAP2.On("GetProductsByAgent", ctx, agentID, page, limit).Return(nil, int64(0), errors.New("database error"))

	products, _, err := svc.GetMerchantProducts(ctx, agentID, page, limit)

	assert.Error(t, err)
	assert.Nil(t, products)
	assert.Contains(t, err.Error(), "failed to get merchant products")
	mockAP2.AssertExpectations(t)
}

// TestGetOrderByID_Success tests successful order retrieval
func TestMarketplaceService_GetOrderByID_Success(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	orderID := "order-123"

	expected := &models.MarketplaceOrder{
		ID:          orderID,
		UserID:      "user-456",
		TotalAmount: 299.99,
		Status:      "pending",
	}

	mockAP2.On("GetOrderByID", ctx, orderID).Return(expected, nil)

	order, err := svc.GetOrderByID(ctx, orderID)

	assert.NoError(t, err)
	assert.NotNil(t, order)
	assert.Equal(t, orderID, order.ID)
	assert.Equal(t, 299.99, order.TotalAmount)
	mockAP2.AssertExpectations(t)
}

// TestGetOrderByID_NotFound tests order not found
func TestMarketplaceService_GetOrderByID_NotFound(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	orderID := "nonexistent"

	mockAP2.On("GetOrderByID", ctx, orderID).Return(nil, errors.New("order not found"))

	order, err := svc.GetOrderByID(ctx, orderID)

	assert.Error(t, err)
	assert.Nil(t, order)
	assert.Contains(t, err.Error(), "failed to get order")
	mockAP2.AssertExpectations(t)
}

// TestGetUserOrders_Success tests successful user orders retrieval
func TestMarketplaceService_GetUserOrders_Success(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	userID := "user-123"
	page := 1
	limit := 10

	expectedOrders := []*models.MarketplaceOrder{
		{ID: "order-1", UserID: userID, TotalAmount: 100.00},
		{ID: "order-2", UserID: userID, TotalAmount: 200.00},
	}

	mockAP2.On("GetOrdersByUser", ctx, userID, page, limit).Return(expectedOrders, int64(2), nil)

	orders, total, err := svc.GetUserOrders(ctx, userID, page, limit)

	assert.NoError(t, err)
	assert.Len(t, orders, 2)
	assert.Equal(t, int64(2), total)
	mockAP2.AssertExpectations(t)
}

// TestGetUserOrders_EmptyResult tests user orders with no results
func TestMarketplaceService_GetUserOrders_EmptyResult(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	userID := "user-123"

	mockAP2.On("GetOrdersByUser", ctx, userID, 1, 10).Return([]*models.MarketplaceOrder{}, int64(0), nil)

	orders, total, err := svc.GetUserOrders(ctx, userID, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, orders, 0)
	assert.Equal(t, int64(0), total)
	mockAP2.AssertExpectations(t)
}

// TestGetUserOrders_RepositoryError tests user orders retrieval with error
func TestMarketplaceService_GetUserOrders_RepositoryError(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	userID := "user-123"

	mockAP2.On("GetOrdersByUser", ctx, userID, 1, 10).Return(nil, int64(0), errors.New("database error"))

	orders, _, err := svc.GetUserOrders(ctx, userID, 1, 10)

	assert.Error(t, err)
	assert.Nil(t, orders)
	assert.Contains(t, err.Error(), "failed to get user orders")
	mockAP2.AssertExpectations(t)
}

// TestGetOrdersByStatus_Success tests successful orders by status retrieval
func TestMarketplaceService_GetOrdersByStatus_Success(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	userID := "user-123"
	status := "pending"
	page := 1
	limit := 10

	expectedOrders := []*models.MarketplaceOrder{
		{ID: "order-1", UserID: userID, Status: "pending"},
	}

	mockAP2.On("GetOrdersByUserAndStatus", ctx, userID, status, page, limit).Return(expectedOrders, int64(1), nil)

	orders, total, err := svc.GetOrdersByStatus(ctx, userID, status, page, limit)

	assert.NoError(t, err)
	assert.Len(t, orders, 1)
	assert.Equal(t, int64(1), total)
	mockAP2.AssertExpectations(t)
}

// TestGetMarketplaceStats_Success tests successful marketplace stats retrieval
func TestMarketplaceService_GetMarketplaceStats_Success(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()

	products := []*models.MarketplaceProduct{
		{ID: "prod-1"},
	}
	shoppingAgents := []*models.Agent{
		{ID: "shop-1"},
	}
	merchantAgents := []*models.Agent{
		{ID: "merch-1"},
		{ID: "merch-2"},
	}

	mockAP2.On("GetAvailableProducts", ctx, 1, 1).Return(products, int64(3), nil)
	mockAP2.On("GetActiveAgentsByType", ctx, "shopping").Return(shoppingAgents, nil)
	mockAP2.On("GetActiveAgentsByType", ctx, "merchant").Return(merchantAgents, nil)

	stats, err := svc.GetMarketplaceStats(ctx)

	assert.NoError(t, err)
	assert.NotNil(t, stats)
	assert.Equal(t, int64(3), stats["available_products"])
	assert.Equal(t, 1, stats["shopping_agents"])
	assert.Equal(t, 2, stats["merchant_agents"])
	mockAP2.AssertExpectations(t)
}

// TestGetMarketplaceStats_ProductsError tests marketplace stats with products error
func TestMarketplaceService_GetMarketplaceStats_ProductsError(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()

	mockAP2.On("GetAvailableProducts", ctx, 1, 1).Return(nil, int64(0), errors.New("database error"))

	stats, err := svc.GetMarketplaceStats(ctx)

	assert.Error(t, err)
	assert.Nil(t, stats)
	mockAP2.AssertExpectations(t)
}

// TestGetMarketplaceStats_ShoppingAgentsError tests marketplace stats with shopping agents error
func TestMarketplaceService_GetMarketplaceStats_ShoppingAgentsError(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()

	products := []*models.MarketplaceProduct{{ID: "prod-1"}}

	mockAP2.On("GetAvailableProducts", ctx, 1, 1).Return(products, int64(1), nil)
	mockAP2.On("GetActiveAgentsByType", ctx, "shopping").Return(nil, errors.New("agents error"))

	stats, err := svc.GetMarketplaceStats(ctx)

	assert.Error(t, err)
	assert.Nil(t, stats)
	mockAP2.AssertExpectations(t)
}

// TestGetMarketplaceStats_MerchantAgentsError tests marketplace stats with merchant agents error
func TestMarketplaceService_GetMarketplaceStats_MerchantAgentsError(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()

	products := []*models.MarketplaceProduct{{ID: "prod-1"}}
	shoppingAgents := []*models.Agent{{ID: "shop-1"}}

	mockAP2.On("GetAvailableProducts", ctx, 1, 1).Return(products, int64(1), nil)
	mockAP2.On("GetActiveAgentsByType", ctx, "shopping").Return(shoppingAgents, nil)
	mockAP2.On("GetActiveAgentsByType", ctx, "merchant").Return(nil, errors.New("agents error"))

	stats, err := svc.GetMarketplaceStats(ctx)

	assert.Error(t, err)
	assert.Nil(t, stats)
	mockAP2.AssertExpectations(t)
}

// TestListProducts_Pagination tests product listing with different pagination values
func TestMarketplaceService_ListProducts_Pagination(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	filters := map[string]interface{}{}

	testCases := []struct {
		page     int
		limit    int
		products []*models.MarketplaceProduct
		total    int64
	}{
		{1, 10, []*models.MarketplaceProduct{{ID: "p1"}}, 1},
		{2, 10, []*models.MarketplaceProduct{}, 15},
		{1, 5, []*models.MarketplaceProduct{{ID: "p1"}, {ID: "p2"}}, 2},
	}

	for _, tc := range testCases {
		mockAP2.On("GetMarketplaceProducts", ctx, filters, tc.page, tc.limit).Return(tc.products, tc.total, nil).Once()

		products, total, err := svc.ListProducts(ctx, filters, tc.page, tc.limit)

		assert.NoError(t, err)
		assert.Equal(t, tc.products, products)
		assert.Equal(t, tc.total, total)
	}

	mockAP2.AssertExpectations(t)
}

// TestGetUserOrders_Pagination tests user orders with different pagination values
func TestMarketplaceService_GetUserOrders_Pagination(t *testing.T) {
	mockAP2 := new(MockMarketplaceAP2Repository)
	log := logger.New()

	svc := services.NewMarketplaceService(mockAP2, log)

	ctx := context.Background()
	userID := "user-123"

	testCases := []struct {
		page   int
		limit  int
		orders []*models.MarketplaceOrder
		total  int64
	}{
		{1, 10, []*models.MarketplaceOrder{{ID: "o1"}}, 1},
		{2, 10, []*models.MarketplaceOrder{}, 15},
		{1, 5, []*models.MarketplaceOrder{{ID: "o1"}, {ID: "o2"}}, 2},
	}

	for _, tc := range testCases {
		mockAP2.On("GetOrdersByUser", ctx, userID, tc.page, tc.limit).Return(tc.orders, tc.total, nil).Once()

		orders, total, err := svc.GetUserOrders(ctx, userID, tc.page, tc.limit)

		assert.NoError(t, err)
		assert.Equal(t, tc.orders, orders)
		assert.Equal(t, tc.total, total)
	}

	mockAP2.AssertExpectations(t)
}
