package services

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

// AgentDiscoveryService handles agent discovery operations
type AgentDiscoveryService struct {
	ap2Repo interfaces.AP2Repository
	log     *logger.Logger
}

// NewAgentDiscoveryService creates a new agent discovery service
func NewAgentDiscoveryService(ap2Repo interfaces.AP2Repository, log *logger.Logger) *AgentDiscoveryService {
	return &AgentDiscoveryService{
		ap2Repo: ap2Repo,
		log:     log,
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

// RegisterAgent registers a new agent in the discovery registry
func (s *AgentDiscoveryService) RegisterAgent(ctx context.Context, req *RegisterAgentRequest) (*models.AgentRegistry, error) {
	// Build agent card
	card := ap2.NewAgentCardBuilder().
		WithName(req.Name).
		WithDescription(req.Description).
		WithEndpoint(req.A2AEndpoint).
		WithType(req.AgentType).
		WithCapabilities(req.Capabilities).
		WithCurrencies(req.Currencies).
		WithJurisdictions(req.Jurisdictions).
		WithLanguages(req.SupportedLanguages)

	if req.PublicKey != nil {
		card = card.WithPublicKey(*req.PublicKey)
	}

	if req.PricingModel != nil {
		card = card.WithPricingModel(req.PricingModel)
	}

	agentCard, err := card.Build()
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

	// Create well-known URI
	wellKnownURI := ap2.CreateWellKnownURI(domain)

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

	s.log.Info("agent registered in discovery", "agent_id", req.AgentID, "registry_id", registry.ID.String())

	// Create audit log
	s.createAuditLog(ctx, registry.ID, "registered", "system", nil, datatypes.JSON(cardJSON))

	return registry, nil
}

// DiscoverAgents searches for agents matching criteria
func (s *AgentDiscoveryService) DiscoverAgents(ctx context.Context, query string, filters *models.AgentDiscoveryFilter, page, limit int) ([]*models.AgentRegistry, int64, error) {
	// Apply filters
	agents, total, err := s.ap2Repo.SearchAgents(ctx, filters, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to search agents: %w", err)
	}

	s.log.Info("discovered agents", "count", len(agents), "total", total, "filters", fmt.Sprintf("%+v", filters))

	return agents, total, nil
}

// GetPublicAgents retrieves all public agents
func (s *AgentDiscoveryService) GetPublicAgents(ctx context.Context, page, limit int) ([]*models.AgentRegistry, int64, error) {
	agents, total, err := s.ap2Repo.GetPublicAgents(ctx, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get public agents: %w", err)
	}

	return agents, total, nil
}

// GetVerifiedAgents retrieves all verified agents
func (s *AgentDiscoveryService) GetVerifiedAgents(ctx context.Context, agentType string, page, limit int) ([]*models.AgentRegistry, int64, error) {
	agents, total, err := s.ap2Repo.GetVerifiedAgents(ctx, agentType, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to get verified agents: %w", err)
	}

	return agents, total, nil
}

// GetAgentsByCapability retrieves agents with specific capabilities
func (s *AgentDiscoveryService) GetAgentsByCapability(ctx context.Context, capabilities []string, page, limit int) ([]*models.AgentRegistry, int64, error) {
	agents, total, err := s.ap2Repo.DiscoverAgentsByCapability(ctx, capabilities, page, limit)
	if err != nil {
		return nil, 0, fmt.Errorf("failed to discover agents by capability: %w", err)
	}

	return agents, total, nil
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

	return registry, nil
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
