package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
)

var (
	ErrAgentNotFound    = errors.New("agent not found")
	ErrInvalidAgentType = errors.New("invalid agent type")
)

type AgentService struct {
	ap2Repo     interfaces.AP2Repository
	productRepo interfaces.ProductRepository
	signer      *ap2.SignatureService
	log         *logger.Logger
}

func NewAgentService(ap2Repo interfaces.AP2Repository, productRepo interfaces.ProductRepository, signer *ap2.SignatureService, log *logger.Logger) *AgentService {
	return &AgentService{
		ap2Repo:     ap2Repo,
		productRepo: productRepo,
		signer:      signer,
		log:         log,
	}
}

type CreatePersonalAgentRequest struct {
	UserID      string
	BusinessID  string
	Name        string
	Description string
	Config      map[string]interface{}
}

type CreateMerchantAgentRequest struct {
	BusinessID  string
	Name        string
	Description string
	ProductIDs  []string
}

func (s *AgentService) CreatePersonalAgent(ctx context.Context, req *CreatePersonalAgentRequest) (*models.Agent, error) {
	capabilities := s.getDefaultShoppingCapabilities()
	config := req.Config
	if config == nil {
		config = make(map[string]interface{})
	}
	config["type"] = "shopping"

	agent := &models.Agent{
		OwnerID:      req.UserID,
		BusinessID:   req.BusinessID,
		Name:         req.Name,
		Type:         "shopping",
		Description:  &req.Description,
		Capabilities: capabilities,
		Config:       s.marshalConfig(config),
		IsPublic:     false,
		IsActive:     true,
	}

	agentCapabilities := s.buildCapabilities(s.getShoppingCapabilityList())

	if err := s.ap2Repo.CreateAgentWithCapabilities(ctx, agent, agentCapabilities); err != nil {
		s.log.Error("failed to create agent with capabilities", "error", err, "user_id", req.UserID)
		return nil, fmt.Errorf("failed to create agent: %w", err)
	}

	ApplyMarketplaceRoleToAgent(agent)
	s.log.Info("created personal agent", "agent_id", agent.ID, "user_id", req.UserID)
	return agent, nil
}

func (s *AgentService) CreateMerchantAgent(ctx context.Context, req *CreateMerchantAgentRequest) (*models.Agent, error) {
	capabilities := s.getDefaultMerchantCapabilities()
	config := map[string]interface{}{
		"type":        "merchant",
		"product_ids": req.ProductIDs,
	}

	agent := &models.Agent{
		OwnerID:      req.BusinessID,
		BusinessID:   req.BusinessID,
		Name:         req.Name,
		Type:         "merchant",
		Description:  &req.Description,
		Capabilities: capabilities,
		Config:       s.marshalConfig(config),
		IsPublic:     true,
		IsActive:     true,
	}

	agentCapabilities := s.buildCapabilities(s.getMerchantCapabilityList())

	// Use transaction to create agent and register in discovery
	if err := s.ap2Repo.CreateAgentWithCapabilities(ctx, agent, agentCapabilities); err != nil {
		s.log.Error("failed to create merchant agent with capabilities", "error", err, "business_id", req.BusinessID)
		return nil, fmt.Errorf("failed to create merchant agent: %w", err)
	}

	// Register in discovery agent_registry
	if err := s.registerAgentInDiscovery(ctx, agent); err != nil {
		s.log.Warn("failed to register merchant agent in discovery", "error", err, "agent_id", agent.ID)
		// Don't fail the creation, just log warning
	}

	// Populate product_ids for response
	agent.ProductIDs = extractProductIDs(agent.Config)

	ApplyMarketplaceRoleToAgent(agent)
	s.log.Info("created merchant agent", "agent_id", agent.ID, "business_id", req.BusinessID)
	return agent, nil
}

// extractProductIDs extracts product_ids from agent config
func extractProductIDs(config string) []string {
	var productIDs []string
	if config == "" {
		return productIDs
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return productIDs
	}
	if ids, ok := cfg["product_ids"].([]interface{}); ok {
		for _, id := range ids {
			if str, ok := id.(string); ok {
				productIDs = append(productIDs, str)
			}
		}
	}
	return productIDs
}

// registerAgentInDiscovery creates an AgentRegistry entry for an existing agent
func (s *AgentService) registerAgentInDiscovery(ctx context.Context, agent *models.Agent) error {
	productIDs := extractProductIDs(agent.Config)

	// Fetch product categories from product IDs
	var tags []string
	uniqueTags := make(map[string]struct{})
	for _, pid := range productIDs {
		product, err := s.productRepo.GetByID(ctx, pid, agent.BusinessID)
		if err != nil {
			s.log.Warn("failed to get product for category", "product_id", pid, "error", err)
			continue
		}
		// Add category name as tag
		if product.Category != nil && product.Category.Name != "" {
			uniqueTags[product.Category.Name] = struct{}{}
		}
		// Add product name as tag
		if product.Name != "" {
			uniqueTags[product.Name] = struct{}{}
		}
	}
	for tag := range uniqueTags {
		tags = append(tags, tag)
	}

	// If no tags from products, add default merchant tag
	if len(tags) == 0 {
		tags = []string{"merchant"}
	}

	registry := &models.AgentRegistry{
		ID:                uuid.New(),
		AgentID:           uuid.MustParse(agent.ID),
		AgentName:         agent.Name,
		AgentDescription:  agent.Description,
		AgentType:         NormalizeMarketplaceAgentType(agent.Type),
		Tags:              tags,
		ProductIDs:        productIDs,
		IsPublic:          agent.IsPublic,
		IsActive:          agent.IsActive,
		IsVerified:        false,
		HealthCheckStatus: stringPtr("healthy"),
		CreatedAt:         time.Now(),
		UpdatedAt:         time.Now(),
	}

	if err := s.ap2Repo.RegisterAgent(ctx, registry); err != nil {
		return fmt.Errorf("failed to register agent in discovery: %w", err)
	}

	ApplyMarketplaceRoleToRegistry(registry)
	s.log.Info("agent registered in discovery", "agent_id", agent.ID, "registry_id", registry.ID.String(), "tags", tags)
	return nil
}

func (s *AgentService) GetAgentByID(ctx context.Context, agentID string) (*models.Agent, error) {
	agent, err := s.ap2Repo.GetAgentByID(ctx, agentID)
	if err != nil {
		s.log.Error("failed to get agent", "error", err, "agent_id", agentID)
		return nil, ErrAgentNotFound
	}
	agent.ProductIDs = extractProductIDs(agent.Config)
	ApplyMarketplaceRoleToAgent(agent)
	return agent, nil
}

func (s *AgentService) GetAgentsByBusiness(ctx context.Context, businessID string, page, limit int) ([]*models.Agent, int64, error) {
	agents, total, err := s.ap2Repo.GetAgentsByBusiness(ctx, businessID, page, limit)
	if err != nil {
		return nil, 0, err
	}
	for _, agent := range agents {
		agent.ProductIDs = extractProductIDs(agent.Config)
	}
	ApplyMarketplaceRoleToAgents(agents)
	return agents, total, nil
}

func (s *AgentService) GetAgentsByUser(ctx context.Context, userID string, page, limit int) ([]*models.Agent, int64, error) {
	agents, total, err := s.ap2Repo.GetAgentsByUser(ctx, userID, page, limit)
	if err != nil {
		return nil, 0, err
	}
	for _, agent := range agents {
		agent.ProductIDs = extractProductIDs(agent.Config)
	}
	ApplyMarketplaceRoleToAgents(agents)
	return agents, total, nil
}

func (s *AgentService) GetActiveAgentsByType(ctx context.Context, agentType string) ([]*models.Agent, error) {
	agents, err := s.ap2Repo.GetActiveAgentsByType(ctx, NormalizeMarketplaceAgentType(agentType))
	if err != nil {
		return nil, err
	}
	ApplyMarketplaceRoleToAgents(agents)
	return agents, nil
}

func (s *AgentService) UpdateAgent(ctx context.Context, agentID string, updates map[string]interface{}) error {
	agent, err := s.ap2Repo.GetAgentByID(ctx, agentID)
	if err != nil {
		return ErrAgentNotFound
	}

	if name, ok := updates["name"].(string); ok {
		agent.Name = name
	}
	if description, ok := updates["description"].(string); ok {
		agent.Description = &description
	}
	if isActive, ok := updates["is_active"].(bool); ok {
		agent.IsActive = isActive
	}
	if config, ok := updates["config"].(map[string]interface{}); ok {
		agent.Config = s.marshalConfig(config)
	}

	return s.ap2Repo.UpdateAgent(ctx, agent)
}

func (s *AgentService) DeleteAgent(ctx context.Context, agentID string) error {
	return s.ap2Repo.DeleteAgent(ctx, agentID)
}

func (s *AgentService) GetAgentCapabilities(ctx context.Context, agentID string) ([]*models.AgentCapability, error) {
	return s.ap2Repo.GetCapabilitiesByAgent(ctx, agentID)
}

func (s *AgentService) AddCapability(ctx context.Context, agentID, capabilityType, description string, config map[string]interface{}) error {
	agentCapability := &models.AgentCapability{
		AgentID:        agentID,
		CapabilityType: capabilityType,
		Description:    &description,
		Config:         s.marshalConfig(config),
	}
	return s.ap2Repo.CreateAgentCapability(ctx, agentCapability)
}

func (s *AgentService) RemoveCapability(ctx context.Context, capabilityID string) error {
	return s.ap2Repo.DeleteCapability(ctx, capabilityID)
}

type AgentCapability struct {
	Type        string
	Description string
	Config      map[string]interface{}
}

func (s *AgentService) getDefaultShoppingCapabilities() string {
	capabilities := []AgentCapability{
		{
			Type:        "search_products",
			Description: "Search and find products in marketplace",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "create_cart",
			Description: "Create shopping cart with selected products",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "process_payment",
			Description: "Process payment through Razorpay",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "track_orders",
			Description: "Track order status and delivery",
			Config:      map[string]interface{}{},
		},
	}
	return s.marshalCapabilities(capabilities)
}

func (s *AgentService) getShoppingCapabilityList() []AgentCapability {
	return []AgentCapability{
		{
			Type:        "search_products",
			Description: "Search and find products in marketplace",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "create_cart",
			Description: "Create shopping cart with selected products",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "process_payment",
			Description: "Process payment through Razorpay",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "track_orders",
			Description: "Track order status and delivery",
			Config:      map[string]interface{}{},
		},
	}
}

func (s *AgentService) getDefaultMerchantCapabilities() string {
	capabilities := []AgentCapability{
		{
			Type:        "list_products",
			Description: "List merchant products in marketplace",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "manage_inventory",
			Description: "Manage product inventory",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "process_orders",
			Description: "Process and fulfill customer orders",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "update_order_status",
			Description: "Update order shipping and delivery status",
			Config:      map[string]interface{}{},
		},
	}
	return s.marshalCapabilities(capabilities)
}

func (s *AgentService) getMerchantCapabilityList() []AgentCapability {
	return []AgentCapability{
		{
			Type:        "list_products",
			Description: "List merchant products in marketplace",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "manage_inventory",
			Description: "Manage product inventory",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "process_orders",
			Description: "Process and fulfill customer orders",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "update_order_status",
			Description: "Update order shipping and delivery status",
			Config:      map[string]interface{}{},
		},
	}
}

func (s *AgentService) buildCapabilities(caps []AgentCapability) []*models.AgentCapability {
	result := make([]*models.AgentCapability, len(caps))
	for i, cap := range caps {
		desc := cap.Description
		result[i] = &models.AgentCapability{
			CapabilityType: cap.Type,
			Description:    &desc,
			Config:         s.marshalConfig(cap.Config),
		}
	}
	return result
}

func (s *AgentService) marshalCapabilities(capabilities []AgentCapability) string {
	data, err := json.Marshal(capabilities)
	if err != nil {
		return "[]"
	}
	return string(data)
}

func (s *AgentService) marshalConfig(config map[string]interface{}) string {
	if config == nil {
		return "{}"
	}
	data, err := json.Marshal(config)
	if err != nil {
		return "{}"
	}
	return string(data)
}

func (s *AgentService) CreateCredentialProviderAgent(ctx context.Context, businessID, name, description string) (*models.Agent, error) {
	capabilities := s.getDefaultCredentialProviderCapabilities()
	config := map[string]interface{}{
		"type": "credential_provider",
	}

	agent := &models.Agent{
		OwnerID:      businessID,
		BusinessID:   businessID,
		Name:         name,
		Type:         "credential_provider",
		Description:  &description,
		Capabilities: capabilities,
		Config:       s.marshalConfig(config),
		IsPublic:     false,
		IsActive:     true,
	}

	if err := s.ap2Repo.CreateAgent(ctx, agent); err != nil {
		s.log.Error("failed to create credential provider agent", "error", err, "business_id", businessID)
		return nil, fmt.Errorf("failed to create credential provider agent: %w", err)
	}

	s.log.Info("created credential provider agent", "agent_id", agent.ID, "business_id", businessID)
	return agent, nil
}

func (s *AgentService) getDefaultCredentialProviderCapabilities() string {
	capabilities := []AgentCapability{
		{
			Type:        "store_payment_methods",
			Description: "Securely store payment methods",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "generate_tokens",
			Description: "Generate one-time payment tokens",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "verify_credentials",
			Description: "Verify payment credentials",
			Config:      map[string]interface{}{},
		},
	}
	return s.marshalCapabilities(capabilities)
}

func (s *AgentService) CreatePaymentProcessorAgent(ctx context.Context, businessID, name, description string) (*models.Agent, error) {
	capabilities := s.getDefaultPaymentProcessorCapabilities()
	config := map[string]interface{}{
		"type": "payment_processor",
	}

	agent := &models.Agent{
		OwnerID:      businessID,
		BusinessID:   businessID,
		Name:         name,
		Type:         "payment_processor",
		Description:  &description,
		Capabilities: capabilities,
		Config:       s.marshalConfig(config),
		IsPublic:     false,
		IsActive:     true,
	}

	if err := s.ap2Repo.CreateAgent(ctx, agent); err != nil {
		s.log.Error("failed to create payment processor agent", "error", err, "business_id", businessID)
		return nil, fmt.Errorf("failed to create payment processor agent: %w", err)
	}

	s.log.Info("created payment processor agent", "agent_id", agent.ID, "business_id", businessID)
	return agent, nil
}

func (s *AgentService) getDefaultPaymentProcessorCapabilities() string {
	capabilities := []AgentCapability{
		{
			Type:        "process_payment",
			Description: "Process payments through Razorpay",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "capture_payment",
			Description: "Capture authorized payments",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "refund_payment",
			Description: "Process refunds",
			Config:      map[string]interface{}{},
		},
		{
			Type:        "verify_webhook",
			Description: "Verify and process payment webhooks",
			Config:      map[string]interface{}{},
		},
	}
	return s.marshalCapabilities(capabilities)
}

func (s *AgentService) ValidateAgentPermission(userID, agentID string) (bool, error) {
	return s.ap2Repo.HasAgentOwnership(context.Background(), userID, agentID)
}
