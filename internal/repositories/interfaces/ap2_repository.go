package interfaces

import (
	"context"
	"time"

	"invoice-backend/internal/models"
)

type AP2Repository interface {
	// Intent Mandates
	CreateIntentMandate(ctx context.Context, mandate *models.IntentMandate) error
	GetIntentMandateByID(ctx context.Context, id, userID string) (*models.IntentMandate, error)
	GetActiveIntentMandatesByUser(ctx context.Context, userID string) ([]*models.IntentMandate, error)
	GetIntentMandatesByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.IntentMandate, int64, error)
	UpdateIntentMandateStatus(ctx context.Context, id, status string) error
	RevokeIntentMandate(ctx context.Context, id string) error

	// Cart Mandates
	CreateCartMandate(ctx context.Context, mandate *models.CartMandate) error
	GetCartMandateByID(ctx context.Context, id, userID string) (*models.CartMandate, error)
	GetCartMandateByMerchant(ctx context.Context, id, merchantID string) (*models.CartMandate, error)
	GetCartMandatesByUser(ctx context.Context, userID string, page, limit int) ([]*models.CartMandate, int64, error)
	UpdateCartMandate(ctx context.Context, mandate *models.CartMandate) error
	SignCartMandate(ctx context.Context, id, signature string) error
	GetPendingCartMandates(ctx context.Context, merchantID string) ([]*models.CartMandate, error)

	// Payment Mandates
	CreatePaymentMandate(ctx context.Context, mandate *models.PaymentMandate) error
	GetPaymentMandateByID(ctx context.Context, id string) (*models.PaymentMandate, error)
	GetPaymentMandatesByUser(ctx context.Context, userID string, page, limit int) ([]*models.PaymentMandate, int64, error)
	UpdatePaymentMandate(ctx context.Context, mandate *models.PaymentMandate) error
	UpdatePaymentMandateStatus(ctx context.Context, id, status string) error
	GetPaymentMandateByRazorpayOrder(ctx context.Context, orderID string) (*models.PaymentMandate, error)

	// Agents
	CreateAgent(ctx context.Context, agent *models.Agent) error
	GetAgentByID(ctx context.Context, id string) (*models.Agent, error)
	HasAgentOwnership(ctx context.Context, ownerID, agentID string) (bool, error)
	GetAgentsByUser(ctx context.Context, userID string, page, limit int) ([]*models.Agent, int64, error)
	GetAgentsByBusiness(ctx context.Context, businessID string, page, limit int) ([]*models.Agent, int64, error)
	GetAgentsByType(ctx context.Context, agentType string, page, limit int) ([]*models.Agent, int64, error)
	GetActiveAgentsByType(ctx context.Context, agentType string) ([]*models.Agent, error)
	UpdateAgent(ctx context.Context, agent *models.Agent) error
	DeleteAgent(ctx context.Context, id string) error

	CreateAgentWithCapabilities(ctx context.Context, agent *models.Agent, capabilities []*models.AgentCapability) error

	// Agent Capabilities
	CreateAgentCapability(ctx context.Context, capability *models.AgentCapability) error
	GetCapabilitiesByAgent(ctx context.Context, agentID string) ([]*models.AgentCapability, error)
	DeleteCapability(ctx context.Context, id string) error

	// Credentials
	CreatePaymentCredential(ctx context.Context, credential *models.PaymentCredential) error
	GetPaymentCredentialsByUser(ctx context.Context, userID string) ([]*models.PaymentCredential, error)
	GetPaymentCredentialByID(ctx context.Context, id string) (*models.PaymentCredential, error)
	GetDefaultCredential(ctx context.Context, userID string) (*models.PaymentCredential, error)
	UpdateCredential(ctx context.Context, id string, updates map[string]interface{}) error
	DeleteCredential(ctx context.Context, id string) error
	SetDefaultCredential(ctx context.Context, userID, credentialID string) error

	// Credential Tokens
	CreateCredentialToken(ctx context.Context, token *models.CredentialToken) error
	GetCredentialToken(ctx context.Context, token string) (*models.CredentialToken, error)
	MarkTokenAsUsed(ctx context.Context, tokenID string) error
	GetValidTokensByCredential(ctx context.Context, credentialID string) ([]*models.CredentialToken, error)

	// Marketplace
	CreateMarketplaceProduct(ctx context.Context, product *models.MarketplaceProduct) error
	GetMarketplaceProductByID(ctx context.Context, id string) (*models.MarketplaceProduct, error)
	GetMarketplaceProducts(ctx context.Context, filters map[string]interface{}, page, limit int) ([]*models.MarketplaceProduct, int64, error)
	SearchMarketplaceProducts(ctx context.Context, query string, page, limit int) ([]*models.MarketplaceProduct, int64, error)
	GetProductsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.MarketplaceProduct, int64, error)
	GetAvailableProducts(ctx context.Context, page, limit int) ([]*models.MarketplaceProduct, int64, error)
	UpdateMarketplaceProduct(ctx context.Context, product *models.MarketplaceProduct) error
	DeleteMarketplaceProduct(ctx context.Context, id string) error

	// Marketplace Orders
	CreateOrder(ctx context.Context, order *models.MarketplaceOrder) error
	GetOrderByID(ctx context.Context, id string) (*models.MarketplaceOrder, error)
	GetOrdersByUser(ctx context.Context, userID string, page, limit int) ([]*models.MarketplaceOrder, int64, error)
	GetOrdersByUserAndStatus(ctx context.Context, userID, status string, page, limit int) ([]*models.MarketplaceOrder, int64, error)
	GetOrdersByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.MarketplaceOrder, int64, error)
	UpdateOrder(ctx context.Context, order *models.MarketplaceOrder) error
	UpdateOrderStatus(ctx context.Context, orderID, status string) error
	GetOrderByCartMandate(ctx context.Context, cartMandateID string) (*models.MarketplaceOrder, error)

	// A2A Messages
	CreateA2AMessage(ctx context.Context, message *models.A2AMessage) error
	GetA2AMessageByID(ctx context.Context, id string) (*models.A2AMessage, error)
	GetMessagesBySender(ctx context.Context, senderAgentID string, page, limit int) ([]*models.A2AMessage, int64, error)
	GetMessagesByReceiver(ctx context.Context, receiverAgentID string, page, limit int) ([]*models.A2AMessage, int64, error)
	UpdateMessageStatus(ctx context.Context, id, status string) error
	UpdateMessageResponse(ctx context.Context, id string, response string) error

	// Agent Transactions
	CreateAgentTransaction(ctx context.Context, transaction *models.AgentTransaction) error
	GetTransactionsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.AgentTransaction, int64, error)
	GetTransactionsByUser(ctx context.Context, userID string, page, limit int) ([]*models.AgentTransaction, int64, error)

	// Agent Registry (Discovery)
	RegisterAgent(ctx context.Context, registry *models.AgentRegistry) error
	GetAgentRegistry(ctx context.Context, agentID string) (*models.AgentRegistry, error)
	GetAgentRegistryByID(ctx context.Context, registryID string) (*models.AgentRegistry, error)
	UpdateAgentRegistry(ctx context.Context, registry *models.AgentRegistry) error
	DeleteAgentRegistry(ctx context.Context, registryID string) error
	SearchAgents(ctx context.Context, filter *models.AgentDiscoveryFilter, page, limit int) ([]*models.AgentRegistry, int64, error)
	DiscoverAgentsByCapability(ctx context.Context, capabilities []string, page, limit int) ([]*models.AgentRegistry, int64, error)
	GetPublicAgents(ctx context.Context, page, limit int) ([]*models.AgentRegistry, int64, error)
	GetVerifiedAgents(ctx context.Context, agentType string, page, limit int) ([]*models.AgentRegistry, int64, error)
	VerifyAgentRegistry(ctx context.Context, registryID string) error
	UnverifyAgentRegistry(ctx context.Context, registryID string) error
	DeactivateAgentRegistry(ctx context.Context, registryID string) error
	ActivateAgentRegistry(ctx context.Context, registryID string) error
	UpdateAgentRegistryHealthCheck(ctx context.Context, registryID, status, message string) error

	// Agent Discovery Audit
	CreateDiscoveryAudit(ctx context.Context, audit *models.AgentDiscoveryAudit) error
	GetDiscoveryAuditByID(ctx context.Context, id string) (*models.AgentDiscoveryAudit, error)
	GetDiscoveryAuditByAgent(ctx context.Context, agentRegistryID string, page, limit int) ([]*models.AgentDiscoveryAudit, int64, error)

	// Agent Discovery Stats
	GetDiscoveryStats(ctx context.Context, agentRegistryID string) (*models.AgentDiscoveryStats, error)
	UpdateDiscoveryStats(ctx context.Context, stats *models.AgentDiscoveryStats) error
	IncrementAgentViews(ctx context.Context, agentRegistryID string) error
	IncrementAgentSearchFound(ctx context.Context, agentRegistryID string) error
	IncrementAgentInquiries(ctx context.Context, agentRegistryID string) error
	IncrementAgentIntegrations(ctx context.Context, agentRegistryID string) error

	// Bargaining Negotiations
	CreateBargainingNegotiation(ctx context.Context, negotiation *models.BargainingNegotiation) error
	GetBargainingNegotiationByID(ctx context.Context, id string) (*models.BargainingNegotiation, error)
	GetNegotiationsByUser(ctx context.Context, userID string, page, limit int) ([]*models.BargainingNegotiation, int64, error)
	GetNegotiationsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.BargainingNegotiation, int64, error)
	UpdateNegotiationStatus(ctx context.Context, id, status string) error
	UpdateNegotiationAmountAndRounds(ctx context.Context, id string, amount float64, rounds int, status string) error
	CompleteNegotiation(ctx context.Context, id, status string, finalAmount float64, completedAt *time.Time) error

	// Bargaining Rounds
	CreateBargainingRound(ctx context.Context, round *models.BargainingRound) error
	GetBargainingRounds(ctx context.Context, negotiationID string) ([]*models.BargainingRound, error)
	GetBargainingRoundsByAgent(ctx context.Context, agentID string, page, limit int) ([]*models.BargainingRound, int64, error)
}
