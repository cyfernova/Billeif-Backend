package services

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// AgentDiscoveryService handles agent discovery operations
type AgentDiscoveryService struct {
	ap2Repo     interfaces.AP2Repository
	productRepo interfaces.ProductRepository
	llmSvc      *LLMService
	log         *logger.Logger
}

// LLMDiscoveryResult represents the parsed result from LLM query analysis
type LLMDiscoveryResult struct {
	Query         string   `json:"query"`
	BusinessID    string   `json:"business_id"`
	AgentTypes    []string `json:"agent_types"`
	Capabilities  []string `json:"capabilities"`
	Tags          []string `json:"tags"`
	Jurisdictions []string `json:"jurisdictions"`
	Currencies    []string `json:"currencies"`
	Category      string   `json:"category"`
	IsVerified    *bool    `json:"is_verified"`
	IsPublic      *bool    `json:"is_public"`
	MinRating     *float64 `json:"min_rating"`
	Explanation   string   `json:"explanation"`
	ProductQuery  string   `json:"product_query"` // LLM-extracted product search query
	ProductNames  []string `json:"product_names"` // Specific product names if mentioned
	ProductTags   []string `json:"product_tags"`  // Product categories/tags to search
}

// AgentProductMatch represents LLM matching result for an agent's products
type AgentProductMatch struct {
	AgentID         uuid.UUID       `json:"agent_id"`
	AgentName       string          `json:"agent_name"`
	MatchScore      float64         `json:"match_score"`      // 0.0 - 1.0
	MatchReason     string          `json:"match_reason"`     // LLM explanation
	MatchedProducts []*ProductMatch `json:"matched_products"` // Which products matched
}

// ProductMatch represents a single product's match result
type ProductMatch struct {
	ProductID   string  `json:"product_id"`
	ProductName string  `json:"product_name"`
	Description string  `json:"description"`
	MatchScore  float64 `json:"match_score"` // 0.0 - 1.0
	MatchReason string  `json:"match_reason"`
}

// NewAgentDiscoveryService creates a new agent discovery service
func NewAgentDiscoveryService(ap2Repo interfaces.AP2Repository, productRepo interfaces.ProductRepository, llmSvc *LLMService, log *logger.Logger) *AgentDiscoveryService {
	return &AgentDiscoveryService{
		ap2Repo:     ap2Repo,
		productRepo: productRepo,
		llmSvc:      llmSvc,
		log:         log,
	}
}

// RegisterAgentRequest represents a request to register an agent
type RegisterAgentRequest struct {
	AgentID            string                 `json:"agent_id"`
	Name               string                 `json:"name"`
	Description        string                 `json:"description"`
	Domain             string                 `json:"domain"`
	A2AEndpoint        string                 `json:"a2a_endpoint"`
	AgentType          string                 `json:"agent_type"`
	Capabilities       []string               `json:"capabilities"`
	Tags               []string               `json:"tags"`
	Jurisdictions      []string               `json:"jurisdictions"`
	Currencies         []string               `json:"currencies"`
	SupportedLanguages []string               `json:"supported_languages"`
	PricingModel       map[string]interface{} `json:"pricing_model"`
	PublicKey          *string                `json:"public_key"`
	IsPublic           bool                   `json:"is_public"`
}

// BuildAgentRegistryFromAgent creates an AgentRegistry entry from an existing Agent
func (s *AgentDiscoveryService) BuildAgentRegistryFromAgent(ctx context.Context, agent *models.Agent, domain string, a2aEndpoint string) (*models.AgentRegistry, error) {
	agentType := NormalizeMarketplaceAgentType(agent.Type)

	// Parse capabilities from JSON string
	var capabilities []string
	if agent.Capabilities != "" {
		if err := json.Unmarshal([]byte(agent.Capabilities), &capabilities); err != nil {
			s.log.Warn("failed to parse agent capabilities", "error", err)
			capabilities = []string{}
		}
	} else {
		capabilities = []string{}
	}

	// Build agent card from agent data
	agentCard := map[string]interface{}{
		"name":        agent.Name,
		"description": agent.Description,
		"type":        agent.Type,
	}
	cardJSON, err := json.Marshal(agentCard)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize agent card: %w", err)
	}

	if domain == "" {
		domain = fmt.Sprintf("agent-%s.internal", agent.ID)
	}
	wellKnownURI := fmt.Sprintf("https://%s/.well-known/agent-card.json", domain)

	registry := &models.AgentRegistry{
		ID:                 uuid.New(),
		AgentID:            uuid.MustParse(agent.ID),
		AgentName:          agent.Name,
		AgentDescription:   agent.Description,
		AgentType:          agentType,
		AgentCard:          datatypes.JSON(cardJSON),
		Domain:             &domain,
		WellKnownURI:       &wellKnownURI,
		A2AEndpoint:        &a2aEndpoint,
		Capabilities:       capabilities,
		Tags:               []string{},
		Jurisdictions:      []string{},
		Currencies:         []string{},
		SupportedLanguages: []string{},
		PricingModel:       datatypes.JSON("{}"),
		IsPublic:           agent.IsPublic,
		IsActive:           agent.IsActive,
		IsVerified:         false,
		HealthCheckStatus:  stringPtr("healthy"),
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	ApplyMarketplaceRoleToRegistry(registry)
	return registry, nil
}

// RegisterAgentFromAgentsTable looks up an agent by ID from the agents table and registers it in discovery
func (s *AgentDiscoveryService) RegisterAgentFromAgentsTable(ctx context.Context, agentID string) (*models.AgentRegistry, error) {
	// Look up the agent in the agents table
	agent, err := s.ap2Repo.GetAgentByID(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("agent not found in agents table: %w", err)
	}

	// Build registry entry from agent
	registry, err := s.BuildAgentRegistryFromAgent(ctx, agent, "", "")
	if err != nil {
		return nil, fmt.Errorf("failed to build registry entry: %w", err)
	}

	// Register in discovery
	if err := s.ap2Repo.RegisterAgent(ctx, registry); err != nil {
		return nil, fmt.Errorf("failed to register agent in discovery: %w", err)
	}

	s.log.Info("agent registered in discovery from agents table", "agent_id", agentID, "registry_id", registry.ID.String())
	return registry, nil
}

// RegisterAgent registers a new agent in the discovery registry
func (s *AgentDiscoveryService) RegisterAgent(ctx context.Context, req *RegisterAgentRequest) (*models.AgentRegistry, error) {
	req.AgentType = NormalizeMarketplaceAgentType(req.AgentType)
	req.Capabilities = discoveryCapabilitiesForType(req.AgentType, req.Capabilities)

	agentCard, err := buildA2AAgentCardFromRegistration(req)
	if err != nil {
		return nil, fmt.Errorf("failed to build agent card: %w", err)
	}

	// Serialize agent card to JSON
	cardJSON, err := json.Marshal(agentCard)
	if err != nil {
		return nil, fmt.Errorf("failed to serialize agent card: %w", err)
	}

	// Handle optional domain
	domain := req.Domain
	if domain == "" {
		domain = fmt.Sprintf("agent-%s.internal", req.AgentID)
	}

	wellKnownURI := fmt.Sprintf("https://%s/.well-known/agent-card.json", domain)

	// Create registry entry
	agentCardJSON := datatypes.JSON(cardJSON)

	pricingJSON := datatypes.JSON("{}")
	if req.PricingModel != nil {
		pricingData, _ := json.Marshal(req.PricingModel)
		pricingJSON = datatypes.JSON(pricingData)
	}

	a2aEndpoint := req.A2AEndpoint

	registry := &models.AgentRegistry{
		ID:                 uuid.New(),
		AgentID:            uuid.MustParse(req.AgentID),
		AgentName:          req.Name,
		AgentDescription:   &req.Description,
		AgentType:          req.AgentType,
		AgentCard:          agentCardJSON,
		Domain:             &domain,
		WellKnownURI:       &wellKnownURI,
		A2AEndpoint:        &a2aEndpoint,
		Capabilities:       req.Capabilities,
		Tags:               req.Tags,
		Jurisdictions:      req.Jurisdictions,
		Currencies:         req.Currencies,
		SupportedLanguages: req.SupportedLanguages,
		PricingModel:       pricingJSON,
		IsPublic:           req.IsPublic,
		IsActive:           true,
		IsVerified:         false, // Require manual verification
		HealthCheckStatus:  stringPtr("healthy"),
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	// Save registry
	if err := s.ap2Repo.RegisterAgent(ctx, registry); err != nil {
		return nil, fmt.Errorf("failed to register agent: %w", err)
	}

	ApplyMarketplaceRoleToRegistry(registry)

	s.log.Info("agent registered in discovery", "agent_id", req.AgentID, "registry_id", registry.ID.String())

	// Create audit log
	s.createAuditLog(ctx, registry.ID, "registered", "system", nil, datatypes.JSON(cardJSON))

	return registry, nil
}

// DiscoverAgents searches for agents matching criteria
func (s *AgentDiscoveryService) DiscoverAgents(ctx context.Context, query string, filters *models.AgentDiscoveryFilter, page, limit int) ([]*models.AgentRegistry, int64, error) {
	if filters != nil && len(filters.AgentTypes) > 0 {
		for i := range filters.AgentTypes {
			filters.AgentTypes[i] = NormalizeMarketplaceAgentType(filters.AgentTypes[i])
		}
	}

	// Apply filters
	agents, total, err := s.ap2Repo.SearchAgents(ctx, filters, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to search agents: %w", err)
	}

	ApplyMarketplaceRoleToRegistries(agents)
	s.loadProductsForAgents(ctx, agents)

	s.log.Info("discovered agents", "count", len(agents), "total", total, "filters", fmt.Sprintf("%+v", filters))

	return agents, total, nil
}

// DiscoverSellersByProduct finds seller/merchant agents that sell the given product
func (s *AgentDiscoveryService) DiscoverSellersByProduct(ctx context.Context, productID string, page, limit int) ([]*models.AgentRegistry, int64, error) {
	// Build filter for seller agents that have this product in their product_ids
	filters := &models.AgentDiscoveryFilter{
		AgentTypes:     []string{"seller", "merchant"},
		IsPublic:       boolPtr(true),
		IsActive:       boolPtr(true),
		ExcludeDeleted: true,
	}

	agents, total, err := s.ap2Repo.SearchAgents(ctx, filters, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to search seller agents: %w", err)
	}

	ApplyMarketplaceRoleToRegistries(agents)
	s.loadProductsForAgents(ctx, agents)

	// Filter to agents that actually have this product
	var matchingAgents []*models.AgentRegistry
	for _, agent := range agents {
		for _, pid := range agent.ProductIDs {
			if pid == productID {
				matchingAgents = append(matchingAgents, agent)
				break
			}
		}
	}

	s.log.Info("discovered sellers for product",
		"product_id", productID,
		"agent_count", len(matchingAgents),
		"total", total,
	)

	return matchingAgents, total, nil
}

// DiscoverSellersByCategory finds seller/merchant agents that sell products in a given category
// It searches through product categories, not agent tags
func (s *AgentDiscoveryService) DiscoverSellersByCategory(ctx context.Context, category string, page, limit int) ([]*models.AgentRegistry, int64, error) {
	// Get all agents that could be sellers (we'll filter by product categories)
	filter := &models.AgentDiscoveryFilter{
		ExcludeDeleted: true,
	}

	agents, total, err := s.ap2Repo.SearchAgents(ctx, filter, 1, 1000)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to search agents: %w", err)
	}

	ApplyMarketplaceRoleToRegistries(agents)
	s.loadProductsForAgents(ctx, agents)

	// Filter to merchant/shopping agents with products in the given category
	var matchingAgents []*models.AgentRegistry
	for _, agent := range agents {
		if agent.AgentType != "merchant" && agent.AgentType != "shopping" {
			continue
		}
		if s.agentHasMatchingCategories(agent, []string{category}) {
			matchingAgents = append(matchingAgents, agent)
			// Log product names for this agent
			var productNames []string
			for _, p := range agent.Products {
				for _, cat := range p.Categories {
					if strings.EqualFold(strings.TrimSpace(cat), strings.TrimSpace(category)) {
						productNames = append(productNames, p.Name)
						break
					}
				}
			}
			s.log.Info("agent matched for category", "agent_name", agent.AgentName, "category", category, "products", productNames)
		}
	}

	// Update total
	total = int64(len(matchingAgents))

	// Apply pagination
	start := (page - 1) * limit
	end := start + limit
	if start >= len(matchingAgents) {
		return []*models.AgentRegistry{}, total, nil
	}
	if end > len(matchingAgents) {
		end = len(matchingAgents)
	}

	s.log.Info("discovered sellers for category",
		"category", category,
		"agent_count", len(matchingAgents),
		"total", total,
	)

	return matchingAgents[start:end], total, nil
}

// DiscoverAgentsByProductCategories finds agents whose products have matching categories and are within budget
// This searches through the product_ids of each agent and matches against product categories
// budget parameter filters agents with at least one product priced at or below the budget
func (s *AgentDiscoveryService) DiscoverAgentsByProductCategories(ctx context.Context, categories []string, agentTypes []string, budget *float64, page, limit int) ([]*models.AgentRegistry, int64, error) {
	// Normalize agent types if provided
	var normalizedTypes []string
	for _, t := range agentTypes {
		normalizedTypes = append(normalizedTypes, NormalizeMarketplaceAgentType(t))
	}

	// Build filter - get agents that could match
	filter := &models.AgentDiscoveryFilter{
		IsPublic:       boolPtr(true),
		IsActive:       boolPtr(true),
		ExcludeDeleted: true,
	}
	if len(normalizedTypes) > 0 {
		filter.AgentTypes = normalizedTypes
	}

	// Search for potential agents
	agents, total, err := s.ap2Repo.SearchAgents(ctx, filter, 1, 1000) // Get more agents initially for filtering
	if err != nil {
		return nil, 0, fmt.Errorf("failed to search agents: %w", err)
	}

	// Load products for each agent and filter by categories and budget
	ApplyMarketplaceRoleToRegistries(agents)
	s.loadProductsForAgents(ctx, agents)

	// Filter agents whose products have matching categories and are within budget
	var matchingAgents []*models.AgentRegistry
	for _, agent := range agents {
		if s.agentHasMatchingCategories(agent, categories) {
			// If budget is specified, check if any product is within budget
			if budget != nil && *budget > 0 {
				if !s.agentHasProductsWithinBudget(agent, *budget) {
					continue
				}
			}
			matchingAgents = append(matchingAgents, agent)
		}
	}

	// Apply pagination
	total = int64(len(matchingAgents))
	start := (page - 1) * limit
	end := start + limit
	if start >= len(matchingAgents) {
		return []*models.AgentRegistry{}, total, nil
	}
	if end > len(matchingAgents) {
		end = len(matchingAgents)
	}

	s.log.Info("discovered agents by product categories",
		"categories", categories,
		"agent_types", agentTypes,
		"budget", budget,
		"matched_count", len(matchingAgents),
		"total", total,
	)

	return matchingAgents[start:end], total, nil
}

// agentHasMatchingCategories checks if an agent has any product with matching categories
func (s *AgentDiscoveryService) agentHasMatchingCategories(agent *models.AgentRegistry, categories []string) bool {
	if len(categories) == 0 {
		return true
	}

	for _, product := range agent.Products {
		for _, productCategory := range product.Categories {
			for _, searchCategory := range categories {
				if strings.EqualFold(strings.TrimSpace(productCategory), strings.TrimSpace(searchCategory)) {
					return true
				}
			}
		}
	}

	return false
}

// agentHasProductsWithinBudget checks if an agent has at least one product within the budget
func (s *AgentDiscoveryService) agentHasProductsWithinBudget(agent *models.AgentRegistry, budget float64) bool {
	for _, product := range agent.Products {
		if product.Price <= budget {
			return true
		}
	}
	return false
}

// GetPublicAgents retrieves all public agents
func (s *AgentDiscoveryService) GetPublicAgents(ctx context.Context, page, limit int) ([]*models.AgentRegistry, int64, error) {
	agents, total, err := s.ap2Repo.GetPublicAgents(ctx, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get public agents: %w", err)
	}

	ApplyMarketplaceRoleToRegistries(agents)
	s.loadProductsForAgents(ctx, agents)
	return agents, total, nil
}

// GetVerifiedAgents retrieves all verified agents
func (s *AgentDiscoveryService) GetVerifiedAgents(ctx context.Context, agentType string, page, limit int) ([]*models.AgentRegistry, int64, error) {
	agents, total, err := s.ap2Repo.GetVerifiedAgents(ctx, NormalizeMarketplaceAgentType(agentType), page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get verified agents: %w", err)
	}

	ApplyMarketplaceRoleToRegistries(agents)
	s.loadProductsForAgents(ctx, agents)
	return agents, total, nil
}

// GetAgentsByCapability retrieves agents from the agents table filtered by product categories
// If capabilities include product category names, it finds agents whose products have matching categories
func (s *AgentDiscoveryService) GetAgentsByCapability(ctx context.Context, capabilities []string, page, limit int) ([]*models.Agent, int64, error) {
	// Get all agents from the agents table
	agents, total, err := s.ap2Repo.GetAgents(ctx, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get agents: %w", err)
	}

	// Populate product_ids for all agents (only if not already set)
	for _, agent := range agents {
		if len(agent.ProductIDs) == 0 && agent.Config != "" {
			agent.ProductIDs = extractProductIDsFromConfig(agent.Config)
		}
	}

	// If no capabilities filter specified, return all agents
	if len(capabilities) == 0 {
		return agents, total, nil
	}

	// Filter agents by product categories
	var filtered []*models.Agent
	for _, agent := range agents {
		if agentHasMatchingProductCategories(ctx, agent, capabilities, s.productRepo) {
			filtered = append(filtered, agent)
		}
	}

	// Update total to reflect filtered count
	total = int64(len(filtered))

	// Apply pagination to filtered results
	start := (page - 1) * limit
	end := start + limit
	if start >= len(filtered) {
		return []*models.Agent{}, total, nil
	}
	if end > len(filtered) {
		end = len(filtered)
	}

	return filtered[start:end], total, nil
}

// DiscoverAgentsByBudget searches the agents table for merchant agents with price within budget.
func (s *AgentDiscoveryService) DiscoverAgentsByBudget(ctx context.Context, budget float64, page, limit int) ([]*models.Agent, int64, error) {
	agents, total, err := s.ap2Repo.SearchAgentsByBudget(ctx, budget, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to search agents by budget: %w", err)
	}

	// Populate product_ids from config for each agent
	for _, agent := range agents {
		agent.ProductIDs = extractProductIDsFromConfig(agent.Config)
	}

	s.log.Info("discovered agents by budget",
		"budget", budget,
		"found", len(agents),
		"total", total,
	)

	return agents, total, nil
}

// DiscoverAgentsByCategoriesAndBudget searches the agents table for agents with matching categories and price within budget.
// It filters based on product categories and product names, not agent categories.
func (s *AgentDiscoveryService) DiscoverAgentsByCategoriesAndBudget(ctx context.Context, categories []string, budget float64, page, limit int) ([]*models.Agent, int64, error) {
	// First get agents within budget (search by budget, not agent categories)
	agents, total, err := s.ap2Repo.SearchAgentsByBudget(ctx, budget, 1, 1000) // Get more to filter
	if err != nil {
		return nil, 0, fmt.Errorf("failed to search agents by budget: %w", err)
	}

	// Populate product_ids and products for each agent
	for _, agent := range agents {
		agent.ProductIDs = extractProductIDsFromConfig(agent.Config)
	}

	// Load product details for each agent
	s.loadProductsForAgentsFromAgents(ctx, agents)

	// Filter agents based on product categories and product names (not agent categories)
	var filtered []*models.Agent
	for _, agent := range agents {
		if s.agentMatchesSearchTerms(agent, categories) {
			filtered = append(filtered, agent)
		}
	}

	// Apply pagination to filtered results
	total = int64(len(filtered))
	start := (page - 1) * limit
	end := start + limit
	if start >= len(filtered) {
		return []*models.Agent{}, total, nil
	}
	if end > len(filtered) {
		end = len(filtered)
	}

	s.log.Info("discovered agents by categories and budget",
		"categories", categories,
		"budget", budget,
		"found", len(filtered),
		"total", total,
	)

	return filtered[start:end], total, nil
}

// agentMatchesSearchTerms checks if an agent has any product matching the search terms via category OR product name
func (s *AgentDiscoveryService) agentMatchesSearchTerms(agent *models.Agent, searchTerms []string) bool {
	if len(searchTerms) == 0 {
		return true
	}

	for _, product := range agent.Products {
		// Check product name match
		for _, term := range searchTerms {
			if strings.Contains(strings.ToLower(product.Name), strings.ToLower(strings.TrimSpace(term))) {
				return true
			}
		}

		// Check product categories match
		for _, productCategory := range product.Categories {
			for _, searchTerm := range searchTerms {
				if strings.EqualFold(strings.TrimSpace(productCategory), strings.TrimSpace(searchTerm)) {
					return true
				}
			}
		}
	}

	return false
}

// agentHasMatchingProductCategories checks if an agent has any product with matching categories
func agentHasMatchingProductCategories(ctx context.Context, agent *models.Agent, categories []string, productRepo interfaces.ProductRepository) bool {
	productIDs := agent.ProductIDs
	if len(productIDs) == 0 {
		// Parse product_ids from config if not already populated
		productIDs = extractProductIDsFromConfig(agent.Config)
	}

	if len(categories) == 0 || len(productIDs) == 0 {
		return false
	}

	for _, productID := range productIDs {
		product, err := productRepo.GetByID(ctx, productID, "")
		if err != nil {
			continue
		}
		for _, cat := range categories {
			for _, productCat := range product.Categories {
				if strings.EqualFold(strings.TrimSpace(cat), strings.TrimSpace(productCat)) {
					return true
				}
			}
		}
	}
	return false
}

// extractProductIDsFromConfig extracts product_ids from agent config JSON
func extractProductIDsFromConfig(config string) []string {
	if config == "" {
		return nil
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal([]byte(config), &cfg); err != nil {
		return nil
	}
	if ids, ok := cfg["product_ids"].([]interface{}); ok {
		result := make([]string, 0, len(ids))
		for _, id := range ids {
			if s, ok := id.(string); ok {
				result = append(result, s)
			}
		}
		return result
	}
	return nil
}

// GetAgentRegistry retrieves an agent's registry entry
func (s *AgentDiscoveryService) GetAgentRegistry(ctx context.Context, agentID string) (*models.AgentRegistry, error) {
	registry, err := s.ap2Repo.GetAgentRegistry(ctx, agentID)
	if err != nil {
		return nil, fmt.Errorf("failed to get agent registry: %w", err)
	}

	// Increment view count
	if err := s.ap2Repo.IncrementAgentViews(ctx, registry.ID.String()); err != nil {
		s.log.Warn("failed to increment agent views", "agent_id", agentID, "error", err)
	}

	ApplyMarketplaceRoleToRegistry(registry)

	// Load product details
	s.loadProductsForAgents(ctx, []*models.AgentRegistry{registry})

	return registry, nil
}

func discoveryCapabilitiesForType(agentType string, capabilities []string) []string {
	existing := make(map[string]struct{}, len(capabilities))
	for _, capability := range capabilities {
		existing[capability] = struct{}{}
	}

	add := func(value string) {
		if _, ok := existing[value]; ok {
			return
		}
		capabilities = append(capabilities, value)
		existing[value] = struct{}{}
	}

	switch agentType {
	case "merchant":
		add("bargaining")
		add("merchant.process_cart")
		add("inventory.reserve")
		add("cart.sign")
		add(taskTypeProcurementQuoteRequest)
		add(taskTypeProcurementNegotiationCounter)
		add(taskTypeProcurementNegotiationAccept)
		add(taskTypeProcurementNegotiationReject)
	case "shopping":
		add("shopping.procurement")
		add("shopping.search")
	}

	return capabilities
}

// VerifyAgent marks an agent as verified
func (s *AgentDiscoveryService) VerifyAgent(ctx context.Context, registryID string) error {
	// Verify the agent
	if err := s.ap2Repo.VerifyAgentRegistry(ctx, registryID); err != nil {
		return fmt.Errorf("failed to verify agent: %w", err)
	}

	s.log.Info("agent verified", "registry_id", registryID)

	// Create audit log
	s.createAuditLog(ctx, uuid.MustParse(registryID), "verified", "admin", stringPtr("agent verified by admin"), nil)

	return nil
}

// UnverifyAgent removes verification from an agent
func (s *AgentDiscoveryService) UnverifyAgent(ctx context.Context, registryID string) error {
	if err := s.ap2Repo.UnverifyAgentRegistry(ctx, registryID); err != nil {
		return fmt.Errorf("failed to unverify agent: %w", err)
	}

	s.log.Info("agent unverified", "registry_id", registryID)

	// Create audit log
	s.createAuditLog(ctx, uuid.MustParse(registryID), "unverified", "admin", stringPtr("agent unverified"), nil)

	return nil
}

// DeactivateAgent deactivates an agent
func (s *AgentDiscoveryService) DeactivateAgent(ctx context.Context, registryID string) error {
	if err := s.ap2Repo.DeactivateAgentRegistry(ctx, registryID); err != nil {
		return fmt.Errorf("failed to deactivate agent: %w", err)
	}

	s.log.Info("agent deactivated", "registry_id", registryID)

	// Create audit log
	s.createAuditLog(ctx, uuid.MustParse(registryID), "deactivated", "admin", nil, nil)

	return nil
}

// ActivateAgent activates a deactivated agent
func (s *AgentDiscoveryService) ActivateAgent(ctx context.Context, registryID string) error {
	if err := s.ap2Repo.ActivateAgentRegistry(ctx, registryID); err != nil {
		return fmt.Errorf("failed to activate agent: %w", err)
	}

	s.log.Info("agent activated", "registry_id", registryID)

	// Create audit log
	s.createAuditLog(ctx, uuid.MustParse(registryID), "activated", "admin", nil, nil)

	return nil
}

// PerformHealthCheck performs a health check on an agent
func (s *AgentDiscoveryService) PerformHealthCheck(ctx context.Context, registryID string) error {
	// Retrieve agent registry to verify it exists
	_, err := s.ap2Repo.GetAgentRegistryByID(ctx, registryID)
	if err != nil {
		return fmt.Errorf("failed to get agent registry: %w", err)
	}

	// In a real implementation, this would make an HTTP call to the agent's endpoint
	// For now, we'll just update the health check status
	status := "healthy"
	message := "agent is responsive"

	if err := s.ap2Repo.UpdateAgentRegistryHealthCheck(ctx, registryID, status, message); err != nil {
		return fmt.Errorf("failed to update health check: %w", err)
	}

	s.log.Info("health check completed", "registry_id", registryID, "status", status)

	return nil
}

// UpdateAgentRating updates the agent's rating
func (s *AgentDiscoveryService) UpdateAgentRating(ctx context.Context, registryID string, rating float64, review string) error {
	registry, err := s.ap2Repo.GetAgentRegistryByID(ctx, registryID)
	if err != nil {
		return fmt.Errorf("failed to get agent registry: %w", err)
	}

	// Update rating (simple average calculation)
	totalRating := (registry.AverageRating * float64(registry.TotalReviews)) + rating
	registry.TotalReviews++
	registry.AverageRating = totalRating / float64(registry.TotalReviews)

	if err := s.ap2Repo.UpdateAgentRegistry(ctx, registry); err != nil {
		return fmt.Errorf("failed to update agent rating: %w", err)
	}

	s.log.Info("agent rating updated", "registry_id", registryID, "new_rating", registry.AverageRating, "total_reviews", registry.TotalReviews)

	return nil
}

// RecordAgentInquiry records that an agent was inquired about
func (s *AgentDiscoveryService) RecordAgentInquiry(ctx context.Context, registryID string) error {
	if err := s.ap2Repo.IncrementAgentInquiries(ctx, registryID); err != nil {
		return fmt.Errorf("failed to record inquiry: %w", err)
	}

	return nil
}

// RecordAgentIntegration records that an agent was integrated
func (s *AgentDiscoveryService) RecordAgentIntegration(ctx context.Context, registryID string) error {
	if err := s.ap2Repo.IncrementAgentIntegrations(ctx, registryID); err != nil {
		return fmt.Errorf("failed to record integration: %w", err)
	}

	return nil
}

// Helper functions

func (s *AgentDiscoveryService) createAuditLog(ctx context.Context, registryID uuid.UUID, action string, performedBy string, reason *string, stateJSON []byte) {
	audit := &models.AgentDiscoveryAudit{
		ID:              uuid.New(),
		AgentRegistryID: registryID,
		Action:          action,
		PerformedBy:     &performedBy,
		Reason:          reason,
		CurrentState:    datatypes.JSON(stateJSON),
		CreatedAt:       time.Now(),
	}

	if err := s.ap2Repo.CreateDiscoveryAudit(ctx, audit); err != nil {
		s.log.Warn("failed to create discovery audit", "error", err)
	}
}

func stringPtr(s string) *string {
	return &s
}

func boolPtr(b bool) *bool {
	return &b
}

// DiscoverAgentsByLLMSearch uses LLM to parse natural language query and search agents
func (s *AgentDiscoveryService) DiscoverAgentsByLLMSearch(ctx context.Context, naturalQuery string, businessID string, page, limit int) ([]*models.AgentRegistry, int64, *LLMDiscoveryResult, error) {
	if s.llmSvc == nil {
		return nil, 0, nil, fmt.Errorf("LLM service not available")
	}

	result, err := s.parseQueryWithLLM(ctx, naturalQuery)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to parse query with LLM: %w", err)
	}

	// Override business_id if provided directly
	if businessID != "" {
		result.BusinessID = businessID
	}

	// If we have a product query, search marketplace products first
	var matchingAgentIDs []string
	if result.ProductQuery != "" || len(result.ProductNames) > 0 || len(result.ProductTags) > 0 {
		matchingAgentIDs, err = s.findAgentsByProductSearch(ctx, result)
		if err != nil {
			s.log.Warn("failed to search products, falling back to regular search", "error", err)
		}
	}

	filter := &models.AgentDiscoveryFilter{
		ExcludeDeleted: true,
	}

	// If we found agents via product search, filter to only those agents
	if len(matchingAgentIDs) > 0 {
		// We need to filter by agent IDs - we'll handle this specially in the repo
		filter.IDs = make([]uuid.UUID, len(matchingAgentIDs))
		for i, id := range matchingAgentIDs {
			filter.IDs[i] = uuid.MustParse(id)
		}
	}

	if result.BusinessID != "" {
		filter.BusinessID = result.BusinessID
	}

	if len(result.AgentTypes) > 0 {
		normalized := make([]string, len(result.AgentTypes))
		for i, t := range result.AgentTypes {
			normalized[i] = NormalizeMarketplaceAgentType(t)
		}
		filter.AgentTypes = normalized
	}

	if len(result.Capabilities) > 0 {
		filter.Capabilities = result.Capabilities
	}

	if len(result.Tags) > 0 {
		filter.Tags = result.Tags
	}

	if len(result.Jurisdictions) > 0 {
		filter.Jurisdictions = result.Jurisdictions
	}

	if len(result.Currencies) > 0 {
		filter.Currencies = result.Currencies
	}

	if result.Category != "" {
		if filter.Tags == nil {
			filter.Tags = []string{}
		}
		filter.Tags = append(filter.Tags, result.Category)
	}

	if result.IsVerified != nil {
		filter.IsVerified = result.IsVerified
	}

	if result.IsPublic != nil {
		filter.IsPublic = result.IsPublic
	}

	if result.MinRating != nil {
		filter.MinAverageRating = result.MinRating
	}

	agents, total, err := s.ap2Repo.SearchAgents(ctx, filter, page, limit)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("failed to search agents: %w", err)
	}

	ApplyMarketplaceRoleToRegistries(agents)

	// Load product details for each agent from their product_ids
	s.loadProductsForAgents(ctx, agents)

	// Use LLM to analyze query against agent products for better matching
	var productMatches []*AgentProductMatch
	if len(result.ProductQuery) > 0 || result.ProductNames != nil || result.ProductTags != nil {
		productMatches, err = s.matchAgentsProductsWithLLM(ctx, naturalQuery, agents)
		if err != nil {
			s.log.Warn("failed to match products with LLM, using fallback", "error", err)
		}
	}

	// If we have LLM product matches, rank agents by match score
	if len(productMatches) > 0 {
		// Sort agents by LLM match score
		s.rankAgentsByProductMatch(agents, productMatches)
	}

	s.log.Info("LLM-driven agent discovery completed",
		"query", naturalQuery,
		"found_count", len(agents),
		"total", total,
		"explanation", result.Explanation,
		"product_match_count", len(matchingAgentIDs),
		"llm_product_match_count", len(productMatches),
	)

	return agents, total, result, nil
}

// findAgentsByProductSearch searches marketplace products and returns agent IDs that have matching products
func (s *AgentDiscoveryService) findAgentsByProductSearch(ctx context.Context, result *LLMDiscoveryResult) ([]string, error) {
	var allAgentIDs []string
	uniqueAgents := make(map[string]struct{})

	// If specific product names are mentioned, search by exact names
	if len(result.ProductNames) > 0 {
		for _, name := range result.ProductNames {
			products, _, err := s.ap2Repo.SearchMarketplaceProducts(ctx, name, 1, 50)
			if err != nil {
				s.log.Warn("failed to search marketplace product by name", "name", name, "error", err)
				continue
			}
			for _, p := range products {
				uniqueAgents[p.AgentID] = struct{}{}
			}
		}
	}

	// If product tags/categories are mentioned, search by tags
	if len(result.ProductTags) > 0 {
		for _, tag := range result.ProductTags {
			products, _, err := s.ap2Repo.GetMarketplaceProducts(ctx, map[string]interface{}{
				"category": tag,
			}, 1, 50)
			if err != nil {
				s.log.Warn("failed to search marketplace products by tag", "tag", tag, "error", err)
				continue
			}
			for _, p := range products {
				uniqueAgents[p.AgentID] = struct{}{}
			}
		}
	}

	// If there's a general product query, search by that
	if result.ProductQuery != "" {
		products, _, err := s.ap2Repo.SearchMarketplaceProducts(ctx, result.ProductQuery, 1, 100)
		if err != nil {
			s.log.Warn("failed to search marketplace products", "query", result.ProductQuery, "error", err)
		} else {
			for _, p := range products {
				uniqueAgents[p.AgentID] = struct{}{}
			}
		}
	}

	for id := range uniqueAgents {
		allAgentIDs = append(allAgentIDs, id)
	}

	s.log.Info("found agents by product search", "count", len(allAgentIDs), "query", result.ProductQuery)
	return allAgentIDs, nil
}

// loadProductsForAgents loads product details for each agent's product_ids
func (s *AgentDiscoveryService) loadProductsForAgents(ctx context.Context, agents []*models.AgentRegistry) {
	for _, agent := range agents {
		if len(agent.ProductIDs) == 0 {
			continue
		}

		for _, productID := range agent.ProductIDs {
			product, err := s.productRepo.GetByIDWithoutTenant(ctx, productID)
			if err != nil {
				s.log.Warn("failed to load product for agent", "product_id", productID, "agent_id", agent.AgentID.String(), "error", err)
				continue
			}
			agent.Products = append(agent.Products, product)
		}
	}
}

// loadProductsForAgentsFromAgents loads product details for each agent's product_ids (for Agent models)
func (s *AgentDiscoveryService) loadProductsForAgentsFromAgents(ctx context.Context, agents []*models.Agent) {
	for _, agent := range agents {
		if len(agent.ProductIDs) == 0 {
			continue
		}

		for _, productID := range agent.ProductIDs {
			product, err := s.productRepo.GetByIDWithoutTenant(ctx, productID)
			if err != nil {
				s.log.Warn("failed to load product for agent", "product_id", productID, "agent_id", agent.ID, "error", err)
				continue
			}
			agent.Products = append(agent.Products, product)
		}
	}
}

// matchAgentsProductsWithLLM uses LLM to match the query against agent products and returns match results
func (s *AgentDiscoveryService) matchAgentsProductsWithLLM(ctx context.Context, query string, agents []*models.AgentRegistry) ([]*AgentProductMatch, error) {
	if s.llmSvc == nil {
		return nil, fmt.Errorf("LLM service not available")
	}

	var matches []*AgentProductMatch

	for _, agent := range agents {
		if len(agent.ProductIDs) == 0 || len(agent.Products) == 0 {
			continue
		}

		// Call LLM to match query against products
		match, err := s.evaluateProductMatchWithLLM(ctx, query, agent.AgentName, agent.Products)
		if err != nil {
			s.log.Warn("failed to evaluate product match with LLM", "agent_id", agent.AgentID.String(), "error", err)
			continue
		}

		// Only include agents with some match (score > 0)
		if match.MatchScore > 0 {
			match.AgentID = agent.AgentID
			match.AgentName = agent.AgentName
			matches = append(matches, match)
		}
	}

	return matches, nil
}

// rankAgentsByProductMatch reorders agents based on LLM product match scores
// Agents with higher match scores appear first; agents without matches go last
func (s *AgentDiscoveryService) rankAgentsByProductMatch(agents []*models.AgentRegistry, matches []*AgentProductMatch) {
	// Build a map of agent ID to match score
	matchScores := make(map[string]float64)
	for _, m := range matches {
		matchScores[m.AgentID.String()] = m.MatchScore
	}

	// Sort agents: matched ones by score (desc), then unmatched
	sorted := make([]*models.AgentRegistry, 0, len(agents))
	unmatched := make([]*models.AgentRegistry, 0)

	for _, agent := range agents {
		if score, ok := matchScores[agent.AgentID.String()]; ok && score > 0 {
			sorted = append(sorted, agent)
		} else {
			unmatched = append(unmatched, agent)
		}
	}

	// Sort matched agents by score descending (simple bubble sort for small lists)
	for i := 0; i < len(sorted)-1; i++ {
		for j := 0; j < len(sorted)-i-1; j++ {
			scoreA := matchScores[sorted[j].AgentID.String()]
			scoreB := matchScores[sorted[j+1].AgentID.String()]
			if scoreA < scoreB {
				sorted[j], sorted[j+1] = sorted[j+1], sorted[j]
			}
		}
	}

	// Append unmatched agents at the end
	sorted = append(sorted, unmatched...)

	// Copy back to original slice
	copy(agents, sorted)
}

// buildProductContext builds a readable context string from products for LLM analysis
func (s *AgentDiscoveryService) buildProductContext(products []*models.Product) string {
	if len(products) == 0 {
		return "No products available"
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "Products available (%d items):\n", len(products))

	for i, p := range products {
		fmt.Fprintf(&sb, "\n[%d] %s", i+1, p.Name)
		if p.Description != "" {
			fmt.Fprintf(&sb, " - %s", p.Description)
		}
		if p.SKU != "" {
			fmt.Fprintf(&sb, " (SKU: %s)", p.SKU)
		}
		if p.Category != nil && p.Category.Name != "" {
			fmt.Fprintf(&sb, " - Category: %s", p.Category.Name)
		}
		if p.Price > 0 {
			fmt.Fprintf(&sb, " - Price: %.2f %s", p.Price, p.Currency)
		}
	}

	return sb.String()
}

// evaluateProductMatchWithLLM uses LLM to evaluate how well products match a query
func (s *AgentDiscoveryService) evaluateProductMatchWithLLM(ctx context.Context, query, agentName string, products []*models.Product) (*AgentProductMatch, error) {
	if s.llmSvc == nil {
		return nil, fmt.Errorf("LLM service not available")
	}

	productContext := s.buildProductContext(products)

	systemPrompt := `You are a product matching expert. Your job is to evaluate how well a user's search query matches a set of products.

## Your Task:
Given a user's search query and a list of products, determine:
1. Which products match the query (partial matches count)
2. A match score from 0.0 (no match) to 1.0 (perfect match)
3. A brief explanation of why products do or don't match

## Matching Guidelines:
- Consider product name, description, category, SKU, and any other relevant attributes
- Partial matches count (e.g., "laptop" matches "gaming laptop")
- Price range mentions should be considered if relevant
- Generic queries like "electronics" should match most tech products
- If no products match at all, return score 0.0

## Output Format:
Return a JSON object with these fields:
- match_score: number between 0.0 and 1.0 (overall match for the agent)
- match_reason: brief sentence explaining the match
- matched_products: array of products that matched with individual scores (use exact product names from the input)

Example output:
{"match_score": 0.85, "match_reason": "Agent has 3 laptops that match the query", "matched_products": [{"product_name": "Gaming Laptop", "score": 0.9, "reason": "Matches 'gaming laptop' query"}, {"product_name": "Business Laptop", "score": 0.8, "reason": "Matches 'laptop' requirement"}]}`

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: fmt.Sprintf("User Query: %s\n\nAgent Name: %s\n\n%s", query, agentName, productContext)},
	}

	response, err := s.llmSvc.Chat(ctx, messages)
	if err != nil {
		return nil, fmt.Errorf("failed to call LLM for product matching: %w", err)
	}

	// Parse LLM response
	var matchResult struct {
		MatchScore      float64 `json:"match_score"`
		MatchReason     string  `json:"match_reason"`
		MatchedProducts []struct {
			ProductName string  `json:"product_name"`
			Score       float64 `json:"score"`
			Reason      string  `json:"reason"`
		} `json:"matched_products"`
	}

	trimmed := strings.TrimSpace(response)
	start := strings.Index(trimmed, "{")
	end := strings.LastIndex(trimmed, "}")
	if start == -1 || end == -1 {
		return nil, fmt.Errorf("no JSON found in LLM response")
	}
	trimmed = trimmed[start : end+1]

	if err := json.Unmarshal([]byte(trimmed), &matchResult); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}

	// Build matched products - try to find product ID by name
	productNameToID := make(map[string]string)
	for _, p := range products {
		productNameToID[strings.ToLower(p.Name)] = p.ID
	}

	var matchedProducts []*ProductMatch
	for _, mp := range matchResult.MatchedProducts {
		productID := ""
		if pid, ok := productNameToID[strings.ToLower(mp.ProductName)]; ok {
			productID = pid
		}
		matchedProducts = append(matchedProducts, &ProductMatch{
			ProductID:   productID,
			ProductName: mp.ProductName,
			Description: mp.Reason,
			MatchScore:  mp.Score,
			MatchReason: mp.Reason,
		})
	}

	return &AgentProductMatch{
		MatchScore:      matchResult.MatchScore,
		MatchReason:     matchResult.MatchReason,
		MatchedProducts: matchedProducts,
	}, nil
}

func (s *AgentDiscoveryService) parseQueryWithLLM(ctx context.Context, query string) (*LLMDiscoveryResult, error) {
	systemPrompt := `You are an AI agent discovery query parser. Your job is to analyze natural language queries and extract structured filter criteria for finding agents and their products.

## Available Agent Types:
- buyer, seller: bargaining/negotiation agents
- shopping, merchant: marketplace agents
- credential_provider: authentication agents
- payment_processor: payment handling agents

## Capabilities (examples):
- bargaining, shopping.procurement, shopping.search
- merchant.process_cart, inventory.reserve, cart.sign
- payment.process, payment.refund
- credential.issue, credential.verify

## Categories (for products):
Common product categories like electronics, clothing, food, furniture, etc.

## Your Task:
Parse the user's natural language query and extract relevant filters. Be generous with matching - if someone says "electronics sellers" that implies both agent type "seller" and tag "electronics".

## Product Search:
When the user describes a product they want (e.g., "laptops", "cheap phones", "furniture for office"), extract:
- product_query: A search string to find products matching the description
- product_names: Specific product names mentioned (if any)
- product_tags: Product category tags (e.g., "electronics", "clothing")

## Output Format:
Return a JSON object with these fields:
- query: the original query (sanitized)
- business_id: UUID of the business if mentioned (only include if user explicitly provides a business ID)
- agent_types: array of agent types to search (e.g., ["seller", "merchant"])
- capabilities: array of required capabilities
- tags: array of category/keyword tags
- jurisdictions: array of geographic jurisdictions if mentioned
- currencies: array of currencies if specified (e.g., ["USD", "EUR"])
- category: primary product category if identifiable
- is_verified: boolean (true if user wants only verified agents)
- is_public: boolean (true if user wants only public agents)
- min_rating: minimum rating if specified (1.0-5.0)
- product_query: Natural language search query for products (e.g., "laptops with 16GB RAM" -> "laptops")
- product_names: Array of specific product names mentioned (empty if none)
- product_tags: Array of product category tags extracted from query (e.g., ["electronics", "phone"])
- explanation: brief sentence explaining what the search is doing

## Examples:
Input: "find me verified electronics sellers in the US"
Output: {"agent_types":["seller","merchant"],"tags":["electronics"],"jurisdictions":["US"],"is_verified":true,"explanation":"Finding verified electronics sellers in the US"}

Input: "who sells furniture"
Output: {"agent_types":["seller","merchant"],"tags":["furniture"],"is_public":true,"explanation":"Finding public agents that sell furniture"}

Input: "i need a shopping agent for clothing"
Output: {"agent_types":["shopping"],"tags":["clothing"],"explanation":"Finding shopping agents for clothing"}

Input: "find sellers that have laptops"
Output: {"agent_types":["seller","merchant"],"product_query":"laptops","tags":["electronics"],"explanation":"Finding seller agents that have laptops in their marketplace"}`

	messages := []ChatMessage{
		{Role: "system", Content: systemPrompt},
		{Role: "user", Content: query},
	}

	response, err := s.llmSvc.Chat(ctx, messages)
	if err != nil {
		s.log.Error("failed to call LLM for query parsing", "error", err)
		return nil, err
	}

	var result LLMDiscoveryResult
	if err := json.Unmarshal([]byte(response), &result); err != nil {
		s.log.Warn("failed to parse LLM response as JSON, attempting cleanup", "error", err, "response", response)
		// Try to extract JSON from response
		trimmed := strings.TrimSpace(response)
		start := strings.Index(trimmed, "{")
		end := strings.LastIndex(trimmed, "}")
		if start != -1 && end != -1 {
			trimmed = trimmed[start : end+1]
			if err2 := json.Unmarshal([]byte(trimmed), &result); err2 != nil {
				return nil, fmt.Errorf("failed to parse LLM response: %w", err2)
			}
		} else {
			return nil, fmt.Errorf("failed to parse LLM response: no JSON found in response")
		}
	}

	return &result, nil
}
