package unit

import (
	"context"
	"errors"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
)

// MockAP2Repository mocks the AP2Repository interface
type MockAP2Repository struct {
	mock.Mock
}

func (m *MockAP2Repository) RegisterAgent(ctx context.Context, registry *models.AgentRegistry) error {
	args := m.Called(ctx, registry)
	return args.Error(0)
}

func (m *MockAP2Repository) GetAgentRegistry(ctx context.Context, agentID string) (*models.AgentRegistry, error) {
	args := m.Called(ctx, agentID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.AgentRegistry), args.Error(1)
}

func (m *MockAP2Repository) GetAgentRegistryByID(ctx context.Context, registryID string) (*models.AgentRegistry, error) {
	args := m.Called(ctx, registryID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.AgentRegistry), args.Error(1)
}

func (m *MockAP2Repository) UpdateAgentRegistry(ctx context.Context, registry *models.AgentRegistry) error {
	args := m.Called(ctx, registry)
	return args.Error(0)
}

func (m *MockAP2Repository) DeleteAgentRegistry(ctx context.Context, registryID string) error {
	args := m.Called(ctx, registryID)
	return args.Error(0)
}

func (m *MockAP2Repository) SearchAgents(ctx context.Context, filter *models.AgentDiscoveryFilter, page, limit int) ([]*models.AgentRegistry, int64, error) {
	args := m.Called(ctx, filter, page, limit)
	return args.Get(0).([]*models.AgentRegistry), args.Get(1).(int64), args.Error(2)
}

func (m *MockAP2Repository) DiscoverAgentsByCapability(ctx context.Context, capabilities []string, page, limit int) ([]*models.AgentRegistry, int64, error) {
	args := m.Called(ctx, capabilities, page, limit)
	return args.Get(0).([]*models.AgentRegistry), args.Get(1).(int64), args.Error(2)
}

func (m *MockAP2Repository) GetPublicAgents(ctx context.Context, page, limit int) ([]*models.AgentRegistry, int64, error) {
	args := m.Called(ctx, page, limit)
	return args.Get(0).([]*models.AgentRegistry), args.Get(1).(int64), args.Error(2)
}

func (m *MockAP2Repository) GetVerifiedAgents(ctx context.Context, agentType string, page, limit int) ([]*models.AgentRegistry, int64, error) {
	args := m.Called(ctx, agentType, page, limit)
	return args.Get(0).([]*models.AgentRegistry), args.Get(1).(int64), args.Error(2)
}

func (m *MockAP2Repository) VerifyAgentRegistry(ctx context.Context, registryID string) error {
	args := m.Called(ctx, registryID)
	return args.Error(0)
}

func (m *MockAP2Repository) UnverifyAgentRegistry(ctx context.Context, registryID string) error {
	args := m.Called(ctx, registryID)
	return args.Error(0)
}

func (m *MockAP2Repository) DeactivateAgentRegistry(ctx context.Context, registryID string) error {
	args := m.Called(ctx, registryID)
	return args.Error(0)
}

func (m *MockAP2Repository) ActivateAgentRegistry(ctx context.Context, registryID string) error {
	args := m.Called(ctx, registryID)
	return args.Error(0)
}

func (m *MockAP2Repository) UpdateAgentRegistryHealthCheck(ctx context.Context, registryID, status, message string) error {
	args := m.Called(ctx, registryID, status, message)
	return args.Error(0)
}

func (m *MockAP2Repository) CreateDiscoveryAudit(ctx context.Context, audit *models.AgentDiscoveryAudit) error {
	args := m.Called(ctx, audit)
	return args.Error(0)
}

func (m *MockAP2Repository) GetDiscoveryAuditByID(ctx context.Context, id string) (*models.AgentDiscoveryAudit, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.AgentDiscoveryAudit), args.Error(1)
}

func (m *MockAP2Repository) GetDiscoveryAuditByAgent(ctx context.Context, agentRegistryID string, page, limit int) ([]*models.AgentDiscoveryAudit, int64, error) {
	args := m.Called(ctx, agentRegistryID, page, limit)
	return args.Get(0).([]*models.AgentDiscoveryAudit), args.Get(1).(int64), args.Error(2)
}

func (m *MockAP2Repository) GetDiscoveryStats(ctx context.Context, agentRegistryID string) (*models.AgentDiscoveryStats, error) {
	args := m.Called(ctx, agentRegistryID)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.AgentDiscoveryStats), args.Error(1)
}

func (m *MockAP2Repository) UpdateDiscoveryStats(ctx context.Context, stats *models.AgentDiscoveryStats) error {
	args := m.Called(ctx, stats)
	return args.Error(0)
}

func (m *MockAP2Repository) IncrementAgentViews(ctx context.Context, agentRegistryID string) error {
	args := m.Called(ctx, agentRegistryID)
	return args.Error(0)
}

func (m *MockAP2Repository) IncrementAgentSearchFound(ctx context.Context, agentRegistryID string) error {
	args := m.Called(ctx, agentRegistryID)
	return args.Error(0)
}

func (m *MockAP2Repository) IncrementAgentInquiries(ctx context.Context, agentRegistryID string) error {
	args := m.Called(ctx, agentRegistryID)
	return args.Error(0)
}

func (m *MockAP2Repository) IncrementAgentIntegrations(ctx context.Context, agentRegistryID string) error {
	args := m.Called(ctx, agentRegistryID)
	return args.Error(0)
}

// Unused methods for interface compliance - these won't be called in our tests
func (m *MockAP2Repository) CreateIntentMandate(ctx context.Context, mandate *models.IntentMandate) error {
	return nil
}
func (m *MockAP2Repository) GetIntentMandateByID(ctx context.Context, id, userID string) (*models.IntentMandate, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetActiveIntentMandatesByUser(ctx context.Context, userID string) ([]*models.IntentMandate, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetIntentMandatesByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.IntentMandate, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) UpdateIntentMandateStatus(ctx context.Context, id, status string) error {
	return nil
}
func (m *MockAP2Repository) RevokeIntentMandate(ctx context.Context, id string) error {
	return nil
}
func (m *MockAP2Repository) CreateCartMandate(ctx context.Context, mandate *models.CartMandate) error {
	return nil
}
func (m *MockAP2Repository) GetCartMandateByID(ctx context.Context, id, userID string) (*models.CartMandate, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetCartMandateByMerchant(ctx context.Context, id, merchantID string) (*models.CartMandate, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetCartMandatesByUser(ctx context.Context, userID string, page, limit int) ([]*models.CartMandate, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) UpdateCartMandate(ctx context.Context, mandate *models.CartMandate) error {
	return nil
}
func (m *MockAP2Repository) SignCartMandate(ctx context.Context, id, signature string) error {
	return nil
}
func (m *MockAP2Repository) GetPendingCartMandates(ctx context.Context, merchantID string) ([]*models.CartMandate, error) {
	return nil, nil
}
func (m *MockAP2Repository) CreatePaymentMandate(ctx context.Context, mandate *models.PaymentMandate) error {
	return nil
}
func (m *MockAP2Repository) GetPaymentMandateByID(ctx context.Context, id string) (*models.PaymentMandate, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetPaymentMandatesByUser(ctx context.Context, userID string, page, limit int) ([]*models.PaymentMandate, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) UpdatePaymentMandate(ctx context.Context, mandate *models.PaymentMandate) error {
	return nil
}
func (m *MockAP2Repository) UpdatePaymentMandateStatus(ctx context.Context, id, status string) error {
	return nil
}
func (m *MockAP2Repository) GetPaymentMandateByRazorpayOrder(ctx context.Context, orderID string) (*models.PaymentMandate, error) {
	return nil, nil
}
func (m *MockAP2Repository) CreateAgent(ctx context.Context, agent *models.Agent) error {
	return nil
}
func (m *MockAP2Repository) GetAgentByID(ctx context.Context, id string) (*models.Agent, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*models.Agent), args.Error(1)
}
func (m *MockAP2Repository) HasAgentOwnership(ctx context.Context, ownerID, agentID string) (bool, error) {
	return false, nil
}
func (m *MockAP2Repository) GetAgentsByUser(ctx context.Context, userID string, page, limit int) ([]*models.Agent, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) GetAgentsByBusiness(ctx context.Context, businessID string, page, limit int) ([]*models.Agent, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) GetAgentsByType(ctx context.Context, agentType string, page, limit int) ([]*models.Agent, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) GetAgents(ctx context.Context, page, limit int) ([]*models.Agent, int64, error) {
	args := m.Called(ctx, page, limit)
	return args.Get(0).([]*models.Agent), args.Get(1).(int64), args.Error(2)
}
func (m *MockAP2Repository) SearchAgentsByCategories(ctx context.Context, categories []string, budget *float64, page, limit int) ([]*models.Agent, int64, error) {
	args := m.Called(ctx, categories, budget, page, limit)
	return args.Get(0).([]*models.Agent), args.Get(1).(int64), args.Error(2)
}
func (m *MockAP2Repository) SearchAgentsByBudget(ctx context.Context, budget float64, page, limit int) ([]*models.Agent, int64, error) {
	args := m.Called(ctx, budget, page, limit)
	return args.Get(0).([]*models.Agent), args.Get(1).(int64), args.Error(2)
}
func (m *MockAP2Repository) GetActiveAgentsByType(ctx context.Context, agentType string) ([]*models.Agent, error) {
	return nil, nil
}
func (m *MockAP2Repository) UpdateAgent(ctx context.Context, agent *models.Agent) error {
	return nil
}
func (m *MockAP2Repository) DeleteAgent(ctx context.Context, id string) error {
	return nil
}
func (m *MockAP2Repository) CreateAgentWithCapabilities(ctx context.Context, agent *models.Agent, capabilities []*models.AgentCapability) error {
	return nil
}
func (m *MockAP2Repository) CreateAgentWithDiscovery(ctx context.Context, agent *models.Agent, capabilities []*models.AgentCapability, registry *models.AgentRegistry) error {
	return nil
}
func (m *MockAP2Repository) CreateAgentCapability(ctx context.Context, capability *models.AgentCapability) error {
	return nil
}
func (m *MockAP2Repository) GetCapabilitiesByAgent(ctx context.Context, agentID string) ([]*models.AgentCapability, error) {
	return nil, nil
}
func (m *MockAP2Repository) DeleteCapability(ctx context.Context, id string) error {
	return nil
}
func (m *MockAP2Repository) CreatePaymentCredential(ctx context.Context, credential *models.PaymentCredential) error {
	return nil
}
func (m *MockAP2Repository) GetPaymentCredentialsByUser(ctx context.Context, userID string) ([]*models.PaymentCredential, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetPaymentCredentialByID(ctx context.Context, id string) (*models.PaymentCredential, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetDefaultCredential(ctx context.Context, userID string) (*models.PaymentCredential, error) {
	return nil, nil
}
func (m *MockAP2Repository) UpdateCredential(ctx context.Context, id string, updates map[string]interface{}) error {
	return nil
}
func (m *MockAP2Repository) DeleteCredential(ctx context.Context, id string) error {
	return nil
}
func (m *MockAP2Repository) SetDefaultCredential(ctx context.Context, userID, credentialID string) error {
	return nil
}
func (m *MockAP2Repository) CreateCredentialToken(ctx context.Context, token *models.CredentialToken) error {
	return nil
}
func (m *MockAP2Repository) GetCredentialToken(ctx context.Context, token string) (*models.CredentialToken, error) {
	return nil, nil
}
func (m *MockAP2Repository) MarkTokenAsUsed(ctx context.Context, tokenID string) error {
	return nil
}
func (m *MockAP2Repository) GetValidTokensByCredential(ctx context.Context, credentialID string) ([]*models.CredentialToken, error) {
	return nil, nil
}
func (m *MockAP2Repository) CreateMarketplaceProduct(ctx context.Context, product *models.MarketplaceProduct) error {
	return nil
}
func (m *MockAP2Repository) GetMarketplaceProductByID(ctx context.Context, id string) (*models.MarketplaceProduct, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetMarketplaceProducts(ctx context.Context, filters map[string]interface{}, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) SearchMarketplaceProducts(ctx context.Context, query string, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) GetProductsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) GetAvailableProducts(ctx context.Context, page, limit int) ([]*models.MarketplaceProduct, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) UpdateMarketplaceProduct(ctx context.Context, product *models.MarketplaceProduct) error {
	return nil
}
func (m *MockAP2Repository) DeleteMarketplaceProduct(ctx context.Context, id string) error {
	return nil
}
func (m *MockAP2Repository) ReserveMarketplaceInventory(ctx context.Context, productID string, quantity int) error {
	return nil
}
func (m *MockAP2Repository) ReleaseMarketplaceInventory(ctx context.Context, productID string, quantity int) error {
	return nil
}
func (m *MockAP2Repository) CommitMarketplaceInventory(ctx context.Context, productID string, quantity int) error {
	return nil
}
func (m *MockAP2Repository) CreateProcurementRun(ctx context.Context, run *models.ProcurementRun) error {
	return nil
}
func (m *MockAP2Repository) GetProcurementRunByID(ctx context.Context, id, userID string) (*models.ProcurementRun, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetProcurementRunByIdempotencyKey(ctx context.Context, userID, shoppingAgentID, idempotencyKey string) (*models.ProcurementRun, error) {
	return nil, nil
}
func (m *MockAP2Repository) UpdateProcurementRun(ctx context.Context, run *models.ProcurementRun) error {
	return nil
}
func (m *MockAP2Repository) CreateProcurementCandidate(ctx context.Context, candidate *models.ProcurementCandidate) error {
	return nil
}
func (m *MockAP2Repository) UpdateProcurementCandidate(ctx context.Context, candidate *models.ProcurementCandidate) error {
	return nil
}
func (m *MockAP2Repository) GetProcurementCandidatesByRun(ctx context.Context, runID string) ([]*models.ProcurementCandidate, error) {
	return nil, nil
}
func (m *MockAP2Repository) CreateOrder(ctx context.Context, order *models.MarketplaceOrder) error {
	return nil
}
func (m *MockAP2Repository) GetOrderByID(ctx context.Context, id string) (*models.MarketplaceOrder, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetOrdersByUser(ctx context.Context, userID string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) GetOrdersByUserAndStatus(ctx context.Context, userID, status string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) GetOrdersByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.MarketplaceOrder, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) UpdateOrder(ctx context.Context, order *models.MarketplaceOrder) error {
	return nil
}
func (m *MockAP2Repository) UpdateOrderStatus(ctx context.Context, orderID, status string) error {
	return nil
}
func (m *MockAP2Repository) GetOrderByCartMandate(ctx context.Context, cartMandateID string) (*models.MarketplaceOrder, error) {
	return nil, nil
}
func (m *MockAP2Repository) CreateA2AMessage(ctx context.Context, message *models.A2AMessage) error {
	return nil
}
func (m *MockAP2Repository) GetA2AMessageByID(ctx context.Context, id string) (*models.A2AMessage, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetMessagesBySender(ctx context.Context, senderAgentID string, page, limit int) ([]*models.A2AMessage, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) GetMessagesByReceiver(ctx context.Context, receiverAgentID string, page, limit int) ([]*models.A2AMessage, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) UpdateMessageStatus(ctx context.Context, id, status string) error {
	return nil
}
func (m *MockAP2Repository) UpdateMessageResponse(ctx context.Context, id string, response string) error {
	return nil
}
func (m *MockAP2Repository) CreateAgentTransaction(ctx context.Context, transaction *models.AgentTransaction) error {
	return nil
}
func (m *MockAP2Repository) GetTransactionsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.AgentTransaction, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) GetTransactionsByUser(ctx context.Context, userID string, page, limit int) ([]*models.AgentTransaction, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) CreateBargainingNegotiation(ctx context.Context, negotiation *models.BargainingNegotiation) error {
	return nil
}
func (m *MockAP2Repository) GetBargainingNegotiationByID(ctx context.Context, id string) (*models.BargainingNegotiation, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetBargainingNegotiationBySessionID(ctx context.Context, sessionID string) (*models.BargainingNegotiation, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetNegotiationsByUser(ctx context.Context, userID string, page, limit int) ([]*models.BargainingNegotiation, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) GetNegotiationsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.BargainingNegotiation, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) GetNegotiationsInProgress(ctx context.Context, limit int) ([]*models.BargainingNegotiation, error) {
	return nil, nil
}
func (m *MockAP2Repository) UpdateNegotiationStatus(ctx context.Context, id, status string) error {
	return nil
}
func (m *MockAP2Repository) UpdateNegotiationAmountAndRounds(ctx context.Context, id string, amount float64, rounds int, status string) error {
	return nil
}
func (m *MockAP2Repository) CompleteNegotiation(ctx context.Context, id, status string, finalAmount float64, completedAt *time.Time) error {
	return nil
}
func (m *MockAP2Repository) CreateBargainingRound(ctx context.Context, round *models.BargainingRound) error {
	return nil
}
func (m *MockAP2Repository) GetBargainingRounds(ctx context.Context, negotiationID string) ([]*models.BargainingRound, error) {
	return nil, nil
}
func (m *MockAP2Repository) GetBargainingRoundsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.BargainingRound, int64, error) {
	return nil, 0, nil
}
func (m *MockAP2Repository) ClaimBargainingRound(ctx context.Context, negotiationID string, roundNumber int, leaseOwner string, now, leaseExpiresAt time.Time) (bool, error) {
	return false, nil
}
func (m *MockAP2Repository) CompleteBargainingRoundClaim(ctx context.Context, negotiationID string, roundNumber int, leaseOwner string, completedAt time.Time) (bool, error) {
	return false, nil
}

// TestAgentDiscoveryService_RegisterAgent tests successful agent registration
func TestAgentDiscoveryService_RegisterAgent(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	agentID := uuid.New().String()
	req := &services.RegisterAgentRequest{
		AgentID:       agentID,
		Name:          "Test Shopping Agent",
		Description:   "A test shopping agent",
		Domain:        "test-agent.example.com",
		A2AEndpoint:   "https://test-agent.example.com/a2a",
		AgentType:     "shopping",
		Capabilities:  []string{"shopping.procurement"},
		Tags:          []string{"shopping", "test"},
		Jurisdictions: []string{"US"},
		Currencies:    []string{"USD"},
		IsPublic:      true,
	}

	mockRepo.On("RegisterAgent", ctx, mock.AnythingOfType("*models.AgentRegistry")).Return(nil)
	mockRepo.On("CreateDiscoveryAudit", ctx, mock.AnythingOfType("*models.AgentDiscoveryAudit")).Return(nil)
	mockRepo.On("GetAgentByID", ctx, agentID).Return(&models.Agent{ID: agentID, OwnerID: "owner-1"}, nil)

	registry, err := svc.RegisterAgent(ctx, services.AgentRegistrationActor{UserID: "owner-1"}, req)

	assert.NoError(t, err)
	assert.NotNil(t, registry)
	assert.Equal(t, req.Name, registry.AgentName)
	assert.Equal(t, "shopping", registry.AgentType)
	assert.True(t, registry.IsPublic)
	assert.True(t, registry.IsActive)
	mockRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_RegisterAgent_MissingEndpoint tests registration fails without A2A endpoint
func TestAgentDiscoveryService_RegisterAgent_MissingEndpoint(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	req := &services.RegisterAgentRequest{
		AgentID:   uuid.New().String(),
		Name:      "Test Agent",
		AgentType: "shopping",
		// Missing A2AEndpoint
	}
	mockRepo.On("GetAgentByID", ctx, req.AgentID).Return(&models.Agent{ID: req.AgentID, OwnerID: "owner-1"}, nil)

	registry, err := svc.RegisterAgent(ctx, services.AgentRegistrationActor{UserID: "owner-1"}, req)

	assert.Error(t, err)
	assert.Nil(t, registry)
	assert.Contains(t, err.Error(), "a2a endpoint is required")
}

func TestAgentDiscoveryService_RegistrationAuthorizesSourceAgent(t *testing.T) {
	ctx := context.Background()
	agentID := uuid.New().String()
	repositoryFailure := errors.New("repository unavailable")

	tests := []struct {
		name           string
		register       func(*services.AgentDiscoveryService, services.AgentRegistrationActor, string) (*models.AgentRegistry, error)
		agent          *models.Agent
		actor          services.AgentRegistrationActor
		wantRegistered bool
		lookupErr      error
		wantErr        error
	}{
		{
			name: "direct registration allows personal owner",
			register: func(svc *services.AgentDiscoveryService, actor services.AgentRegistrationActor, id string) (*models.AgentRegistry, error) {
				return svc.RegisterAgent(ctx, actor, &services.RegisterAgentRequest{AgentID: id, Name: "source agent", A2AEndpoint: "https://agent.example.com/a2a", AgentType: "shopping"})
			},
			agent:          &models.Agent{ID: agentID, OwnerID: "owner-1", BusinessID: "business-2"},
			actor:          services.AgentRegistrationActor{UserID: "owner-1", BusinessID: "business-1"},
			wantRegistered: true,
		},
		{
			name: "direct registration allows effective business",
			register: func(svc *services.AgentDiscoveryService, actor services.AgentRegistrationActor, id string) (*models.AgentRegistry, error) {
				return svc.RegisterAgent(ctx, actor, &services.RegisterAgentRequest{AgentID: id, Name: "source agent", A2AEndpoint: "https://agent.example.com/a2a", AgentType: "merchant"})
			},
			agent:          &models.Agent{ID: agentID, OwnerID: "other-owner", BusinessID: "business-1"},
			actor:          services.AgentRegistrationActor{UserID: "owner-1", BusinessID: "business-1"},
			wantRegistered: true,
		},
		{
			name: "agents table registration allows personal owner",
			register: func(svc *services.AgentDiscoveryService, actor services.AgentRegistrationActor, id string) (*models.AgentRegistry, error) {
				return svc.RegisterAgentFromAgentsTable(ctx, actor, id)
			},
			agent:          &models.Agent{ID: agentID, OwnerID: "owner-1", BusinessID: "business-2", Name: "source agent", Type: "shopping"},
			actor:          services.AgentRegistrationActor{UserID: "owner-1", BusinessID: "business-1"},
			wantRegistered: true,
		},
		{
			name: "agents table registration allows effective business",
			register: func(svc *services.AgentDiscoveryService, actor services.AgentRegistrationActor, id string) (*models.AgentRegistry, error) {
				return svc.RegisterAgentFromAgentsTable(ctx, actor, id)
			},
			agent:          &models.Agent{ID: agentID, OwnerID: "other-owner", BusinessID: "business-1", Name: "merchant agent", Type: "merchant"},
			actor:          services.AgentRegistrationActor{UserID: "owner-1", BusinessID: "business-1"},
			wantRegistered: true,
		},
		{
			name: "direct registration hides cross user agent",
			register: func(svc *services.AgentDiscoveryService, actor services.AgentRegistrationActor, id string) (*models.AgentRegistry, error) {
				return svc.RegisterAgent(ctx, actor, &services.RegisterAgentRequest{AgentID: id, Name: "source agent", A2AEndpoint: "https://agent.example.com/a2a", AgentType: "shopping"})
			},
			agent:          &models.Agent{ID: agentID, OwnerID: "other-owner", BusinessID: "other-business"},
			actor:          services.AgentRegistrationActor{UserID: "owner-1", BusinessID: "business-1"},
			wantRegistered: false,
			wantErr:        services.ErrAgentRegistrationNotFound,
		},
		{
			name: "agents table registration hides cross business agent",
			register: func(svc *services.AgentDiscoveryService, actor services.AgentRegistrationActor, id string) (*models.AgentRegistry, error) {
				return svc.RegisterAgentFromAgentsTable(ctx, actor, id)
			},
			agent:          &models.Agent{ID: agentID, OwnerID: "other-owner", BusinessID: "other-business", Name: "foreign agent", Type: "merchant"},
			actor:          services.AgentRegistrationActor{UserID: "owner-1", BusinessID: "business-1"},
			wantRegistered: false,
			wantErr:        services.ErrAgentRegistrationNotFound,
		},
		{
			name: "direct registration returns repository failure",
			register: func(svc *services.AgentDiscoveryService, actor services.AgentRegistrationActor, id string) (*models.AgentRegistry, error) {
				return svc.RegisterAgent(ctx, actor, &services.RegisterAgentRequest{AgentID: id, Name: "source agent", A2AEndpoint: "https://agent.example.com/a2a", AgentType: "shopping"})
			},
			actor:          services.AgentRegistrationActor{UserID: "owner-1", BusinessID: "business-1"},
			wantRegistered: false,
			lookupErr:      repositoryFailure,
			wantErr:        repositoryFailure,
		},
		{
			name: "agents table registration returns repository failure",
			register: func(svc *services.AgentDiscoveryService, actor services.AgentRegistrationActor, id string) (*models.AgentRegistry, error) {
				return svc.RegisterAgentFromAgentsTable(ctx, actor, id)
			},
			actor:          services.AgentRegistrationActor{UserID: "owner-1", BusinessID: "business-1"},
			wantRegistered: false,
			lookupErr:      repositoryFailure,
			wantErr:        repositoryFailure,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockRepo := new(MockAP2Repository)
			svc := services.NewAgentDiscoveryService(mockRepo, new(MockProductRepository), nil, logger.New())
			mockRepo.On("GetAgentByID", ctx, agentID).Return(tt.agent, tt.lookupErr)
			if tt.wantRegistered {
				mockRepo.On("RegisterAgent", ctx, mock.AnythingOfType("*models.AgentRegistry")).Return(nil)
				mockRepo.On("CreateDiscoveryAudit", ctx, mock.AnythingOfType("*models.AgentDiscoveryAudit")).Return(nil).Maybe()
			}

			registry, err := tt.register(svc, tt.actor, agentID)

			if tt.wantRegistered {
				assert.NoError(t, err)
				assert.NotNil(t, registry)
			} else {
				assert.ErrorIs(t, err, tt.wantErr)
				assert.Nil(t, registry)
				mockRepo.AssertNotCalled(t, "RegisterAgent", mock.Anything, mock.Anything)
			}
			mockRepo.AssertExpectations(t)
		})
	}
}

// TestAgentDiscoveryService_DiscoverAgents tests agent discovery with filters
func TestAgentDiscoveryService_DiscoverAgents(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	expectedAgents := []*models.AgentRegistry{
		{ID: uuid.New(), AgentName: "Agent 1", AgentType: "shopping"},
		{ID: uuid.New(), AgentName: "Agent 2", AgentType: "shopping"},
	}

	filters := &models.AgentDiscoveryFilter{
		AgentTypes: []string{"shopping"},
	}

	mockRepo.On("SearchAgents", ctx, mock.AnythingOfType("*models.AgentDiscoveryFilter"), 1, 10).Return(expectedAgents, int64(2), nil)

	agents, total, err := svc.DiscoverAgents(ctx, "", filters, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, agents, 2)
	assert.Equal(t, int64(2), total)
	mockRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_GetPublicAgents tests retrieving public agents
func TestAgentDiscoveryService_GetPublicAgents(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	expectedAgents := []*models.AgentRegistry{
		{ID: uuid.New(), AgentName: "Public Agent 1", IsPublic: true},
		{ID: uuid.New(), AgentName: "Public Agent 2", IsPublic: true},
	}

	mockRepo.On("GetPublicAgents", ctx, 1, 10).Return(expectedAgents, int64(2), nil)

	agents, total, err := svc.GetPublicAgents(ctx, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, agents, 2)
	assert.Equal(t, int64(2), total)
	mockRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_GetVerifiedAgents tests retrieving verified agents
func TestAgentDiscoveryService_GetVerifiedAgents(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	expectedAgents := []*models.AgentRegistry{
		{ID: uuid.New(), AgentName: "Verified Merchant", IsVerified: true, AgentType: "merchant"},
	}

	mockRepo.On("GetVerifiedAgents", ctx, "merchant", 1, 10).Return(expectedAgents, int64(1), nil)

	agents, total, err := svc.GetVerifiedAgents(ctx, "merchant", 1, 10)

	assert.NoError(t, err)
	assert.Len(t, agents, 1)
	assert.Equal(t, int64(1), total)
	assert.True(t, agents[0].IsVerified)
	mockRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_GetAgentsByCapability tests retrieving agents by product category
func TestAgentDiscoveryService_GetAgentsByCapability(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	productID := uuid.New().String()
	expectedAgents := []*models.Agent{
		{ID: uuid.New().String(), Name: "Laptop Seller", Type: "merchant", ProductIDs: []string{productID}},
	}

	// Mock product with matching category
	mockProduct := &models.Product{
		ID:         productID,
		Name:       "Gaming Laptop",
		Price:      1599.99,
		Categories: []string{"electronics", "laptops", "gamings"},
	}

	mockRepo.On("GetAgents", ctx, 1, 10).Return(expectedAgents, int64(1), nil)
	mockProductRepo.On("GetByID", ctx, productID, "").Return(mockProduct, nil)

	agents, total, err := svc.GetAgentsByCapability(ctx, []string{"laptops"}, 1, 10)

	assert.NoError(t, err)
	assert.Len(t, agents, 1)
	assert.Equal(t, int64(1), total)
	assert.Equal(t, "Laptop Seller", agents[0].Name)
	mockRepo.AssertExpectations(t)
	mockProductRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_GetAgentRegistry tests retrieving a specific agent registry
func TestAgentDiscoveryService_GetAgentRegistry(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	agentID := uuid.New()
	expectedRegistry := &models.AgentRegistry{
		ID:        uuid.New(),
		AgentID:   agentID,
		AgentName: "Test Agent",
		IsActive:  true,
	}

	mockRepo.On("GetAgentRegistry", ctx, agentID.String()).Return(expectedRegistry, nil)
	mockRepo.On("IncrementAgentViews", ctx, expectedRegistry.ID.String()).Return(nil)

	registry, err := svc.GetAgentRegistry(ctx, agentID.String())

	assert.NoError(t, err)
	assert.Equal(t, expectedRegistry.AgentName, registry.AgentName)
	mockRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_VerifyAgent tests marking an agent as verified
func TestAgentDiscoveryService_VerifyAgent(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	registryID := uuid.New().String()

	mockRepo.On("VerifyAgentRegistry", ctx, registryID).Return(nil)
	mockRepo.On("CreateDiscoveryAudit", ctx, mock.AnythingOfType("*models.AgentDiscoveryAudit")).Return(nil)

	err := svc.VerifyAgent(ctx, registryID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_UnverifyAgent tests removing verification from an agent
func TestAgentDiscoveryService_UnverifyAgent(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	registryID := uuid.New().String()

	mockRepo.On("UnverifyAgentRegistry", ctx, registryID).Return(nil)
	mockRepo.On("CreateDiscoveryAudit", ctx, mock.AnythingOfType("*models.AgentDiscoveryAudit")).Return(nil)

	err := svc.UnverifyAgent(ctx, registryID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_DeactivateAgent tests deactivating an agent
func TestAgentDiscoveryService_DeactivateAgent(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	registryID := uuid.New().String()

	mockRepo.On("DeactivateAgentRegistry", ctx, registryID).Return(nil)
	mockRepo.On("CreateDiscoveryAudit", ctx, mock.AnythingOfType("*models.AgentDiscoveryAudit")).Return(nil)

	err := svc.DeactivateAgent(ctx, registryID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_ActivateAgent tests activating a deactivated agent
func TestAgentDiscoveryService_ActivateAgent(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	registryID := uuid.New().String()

	mockRepo.On("ActivateAgentRegistry", ctx, registryID).Return(nil)
	mockRepo.On("CreateDiscoveryAudit", ctx, mock.AnythingOfType("*models.AgentDiscoveryAudit")).Return(nil)

	err := svc.ActivateAgent(ctx, registryID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_UpdateAgentRating tests updating agent rating
func TestAgentDiscoveryService_UpdateAgentRating(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	registryID := uuid.New().String()
	existingRegistry := &models.AgentRegistry{
		ID:            uuid.MustParse(registryID),
		AgentName:     "Test Agent",
		AverageRating: 4.0,
		TotalReviews:  10,
	}

	mockRepo.On("GetAgentRegistryByID", ctx, registryID).Return(existingRegistry, nil)
	mockRepo.On("UpdateAgentRegistry", ctx, mock.AnythingOfType("*models.AgentRegistry")).Return(nil)

	err := svc.UpdateAgentRating(ctx, registryID, 5.0, "Great agent!")

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_RecordAgentInquiry tests recording an inquiry
func TestAgentDiscoveryService_RecordAgentInquiry(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	registryID := uuid.New().String()

	mockRepo.On("IncrementAgentInquiries", ctx, registryID).Return(nil)

	err := svc.RecordAgentInquiry(ctx, registryID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_RecordAgentIntegration tests recording an integration
func TestAgentDiscoveryService_RecordAgentIntegration(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	registryID := uuid.New().String()

	mockRepo.On("IncrementAgentIntegrations", ctx, registryID).Return(nil)

	err := svc.RecordAgentIntegration(ctx, registryID)

	assert.NoError(t, err)
	mockRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_DiscoveryCapabilitiesForType_Merchant tests capabilities added for merchant agents
func TestAgentDiscoveryService_DiscoveryCapabilitiesForType_Merchant(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	agentID := uuid.New().String()

	// Merchant agents should get additional procurement capabilities auto-added
	req := &services.RegisterAgentRequest{
		AgentID:      agentID,
		Name:         "Test Merchant",
		A2AEndpoint:  "https://merchant.example.com/a2a",
		AgentType:    "merchant",
		Capabilities: []string{"bargaining"},
	}

	mockRepo.On("RegisterAgent", ctx, mock.AnythingOfType("*models.AgentRegistry")).Return(nil)
	mockRepo.On("CreateDiscoveryAudit", ctx, mock.AnythingOfType("*models.AgentDiscoveryAudit")).Return(nil)
	mockRepo.On("GetAgentByID", ctx, agentID).Return(&models.Agent{ID: agentID, OwnerID: "owner-1"}, nil)

	registry, err := svc.RegisterAgent(ctx, services.AgentRegistrationActor{UserID: "owner-1"}, req)

	assert.NoError(t, err)
	assert.NotNil(t, registry)
	// Verify merchant-specific capabilities were added
	assert.Contains(t, registry.Capabilities, "bargaining")
	assert.Contains(t, registry.Capabilities, "merchant.process_cart")
	assert.Contains(t, registry.Capabilities, "inventory.reserve")
	assert.Contains(t, registry.Capabilities, "cart.sign")
	mockRepo.AssertExpectations(t)
}

// TestAgentDiscoveryService_DiscoveryCapabilitiesForType_Shopping tests capabilities added for shopping agents
func TestAgentDiscoveryService_DiscoveryCapabilitiesForType_Shopping(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	agentID := uuid.New().String()

	req := &services.RegisterAgentRequest{
		AgentID:      agentID,
		Name:         "Test Shopping Agent",
		A2AEndpoint:  "https://shopping.example.com/a2a",
		AgentType:    "shopping",
		Capabilities: []string{},
	}

	mockRepo.On("RegisterAgent", ctx, mock.AnythingOfType("*models.AgentRegistry")).Return(nil)
	mockRepo.On("CreateDiscoveryAudit", ctx, mock.AnythingOfType("*models.AgentDiscoveryAudit")).Return(nil)
	mockRepo.On("GetAgentByID", ctx, agentID).Return(&models.Agent{ID: agentID, OwnerID: "owner-1"}, nil)

	registry, err := svc.RegisterAgent(ctx, services.AgentRegistrationActor{UserID: "owner-1"}, req)

	assert.NoError(t, err)
	assert.NotNil(t, registry)
	// Verify shopping-specific capabilities were added
	assert.Contains(t, registry.Capabilities, "shopping.procurement")
	assert.Contains(t, registry.Capabilities, "shopping.search")
	mockRepo.AssertExpectations(t)
}

// TestNormalizeMarketplaceAgentType_BuyerToShopping tests that "buyer" type is normalized to "shopping"
func TestNormalizeMarketplaceAgentType_BuyerToShopping(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	agentID := uuid.New().String()

	req := &services.RegisterAgentRequest{
		AgentID:      agentID,
		Name:         "Test Buyer Agent",
		A2AEndpoint:  "https://buyer.example.com/a2a",
		AgentType:    "buyer",
		Capabilities: []string{"test"},
	}

	mockRepo.On("RegisterAgent", ctx, mock.AnythingOfType("*models.AgentRegistry")).Return(nil)
	mockRepo.On("CreateDiscoveryAudit", ctx, mock.AnythingOfType("*models.AgentDiscoveryAudit")).Return(nil)
	mockRepo.On("GetAgentByID", ctx, agentID).Return(&models.Agent{ID: agentID, OwnerID: "owner-1"}, nil)

	registry, err := svc.RegisterAgent(ctx, services.AgentRegistrationActor{UserID: "owner-1"}, req)

	assert.NoError(t, err)
	assert.Equal(t, "shopping", registry.AgentType)
	assert.Equal(t, "buyer", registry.MarketplaceRole)
	mockRepo.AssertExpectations(t)
}

// TestNormalizeMarketplaceAgentType_SellerToMerchant tests that "seller" type is normalized to "merchant"
func TestNormalizeMarketplaceAgentType_SellerToMerchant(t *testing.T) {
	mockRepo := new(MockAP2Repository)
	mockProductRepo := new(MockProductRepository)
	log := logger.New()
	svc := services.NewAgentDiscoveryService(mockRepo, mockProductRepo, nil, log)

	ctx := context.Background()
	agentID := uuid.New().String()

	req := &services.RegisterAgentRequest{
		AgentID:      agentID,
		Name:         "Test Seller Agent",
		A2AEndpoint:  "https://seller.example.com/a2a",
		AgentType:    "seller",
		Capabilities: []string{"test"},
	}

	mockRepo.On("RegisterAgent", ctx, mock.AnythingOfType("*models.AgentRegistry")).Return(nil)
	mockRepo.On("CreateDiscoveryAudit", ctx, mock.AnythingOfType("*models.AgentDiscoveryAudit")).Return(nil)
	mockRepo.On("GetAgentByID", ctx, agentID).Return(&models.Agent{ID: agentID, OwnerID: "owner-1"}, nil)

	registry, err := svc.RegisterAgent(ctx, services.AgentRegistrationActor{UserID: "owner-1"}, req)

	assert.NoError(t, err)
	assert.Equal(t, "merchant", registry.AgentType)
	assert.Equal(t, "seller", registry.MarketplaceRole)
	mockRepo.AssertExpectations(t)
}
