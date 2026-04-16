package postgres

import (
	"context"
	"fmt"
	"time"

	"invoice-backend/internal/models"

	"github.com/google/uuid"
	"gorm.io/datatypes"
	"gorm.io/gorm"
)

// Agent Registry methods implementation

// RegisterAgent registers a new agent in the discovery registry
func (r *ap2Repository) RegisterAgent(ctx context.Context, registry *models.AgentRegistry) error {
	if err := r.db.WithContext(ctx).Create(registry).Error; err != nil {
		r.log.Error("failed to register agent", "error", err, "agent_id", registry.AgentID)
		return fmt.Errorf("failed to register agent: %w", err)
	}

	r.log.Info("registered agent in discovery", "registry_id", registry.ID, "agent_id", registry.AgentID)

	// Create corresponding stats record
	stats := &models.AgentDiscoveryStats{
		ID:                 uuid.New(),
		AgentRegistryID:    registry.ID,
		TotalViews:         0,
		TotalSearchesFound: 0,
		TotalInquiries:     0,
		TotalIntegrations:  0,
		CreatedAt:          time.Now(),
		UpdatedAt:          time.Now(),
	}

	if err := r.db.WithContext(ctx).Create(stats).Error; err != nil {
		r.log.Warn("failed to create discovery stats", "error", err, "registry_id", registry.ID)
	}

	return nil
}

// GetAgentRegistry retrieves agent registry by agent ID
func (r *ap2Repository) GetAgentRegistry(ctx context.Context, agentID string) (*models.AgentRegistry, error) {
	var registry models.AgentRegistry

	if err := r.db.WithContext(ctx).
		Where("agent_id = ? AND deleted_at IS NULL", agentID).
		First(&registry).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("agent registry not found")
		}
		return nil, fmt.Errorf("failed to get agent registry: %w", err)
	}

	return &registry, nil
}

// GetAgentRegistryByID retrieves agent registry by registry ID
func (r *ap2Repository) GetAgentRegistryByID(ctx context.Context, registryID string) (*models.AgentRegistry, error) {
	var registry models.AgentRegistry

	if err := r.db.WithContext(ctx).
		Where("id = ? AND deleted_at IS NULL", registryID).
		First(&registry).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("agent registry not found")
		}
		return nil, fmt.Errorf("failed to get agent registry: %w", err)
	}

	return &registry, nil
}

// UpdateAgentRegistry updates an agent registry entry
func (r *ap2Repository) UpdateAgentRegistry(ctx context.Context, registry *models.AgentRegistry) error {
	if err := r.db.WithContext(ctx).Save(registry).Error; err != nil {
		r.log.Error("failed to update agent registry", "error", err, "registry_id", registry.ID)
		return fmt.Errorf("failed to update agent registry: %w", err)
	}

	return nil
}

// DeleteAgentRegistry soft-deletes an agent registry entry
func (r *ap2Repository) DeleteAgentRegistry(ctx context.Context, registryID string) error {
	now := time.Now()

	if err := r.db.WithContext(ctx).
		Model(&models.AgentRegistry{}).
		Where("id = ?", registryID).
		Update("deleted_at", now).Error; err != nil {
		return fmt.Errorf("failed to delete agent registry: %w", err)
	}

	return nil
}

// SearchAgents searches for agents based on discovery filters
func (r *ap2Repository) SearchAgents(ctx context.Context, filter *models.AgentDiscoveryFilter, page, limit int) ([]*models.AgentRegistry, int64, error) {
	var registries []*models.AgentRegistry
	var total int64

	query := r.db.WithContext(ctx)

	// Base query - join with agents table to access business_id
	query = query.Table("agent_registry ar").Where("ar.is_active = ? AND ar.deleted_at IS NULL", true)

	// Join with agents table if we need to filter by business_id
	if filter.BusinessID != "" {
		query = query.Joins("JOIN agents a ON a.id = ar.agent_id").
			Where("a.business_id = ?", filter.BusinessID)
	}

	// Apply filters
	if len(filter.AgentTypes) > 0 {
		query = query.Where("ar.agent_type IN ?", filter.AgentTypes)
	}

	if filter.IsVerified != nil {
		query = query.Where("ar.is_verified = ?", *filter.IsVerified)
	}

	if filter.IsPublic != nil {
		query = query.Where("ar.is_public = ?", *filter.IsPublic)
	}

	if len(filter.Capabilities) > 0 {
		for _, cap := range filter.Capabilities {
			query = query.Where("? = ANY(ar.capabilities)", cap)
		}
	}

	if len(filter.Tags) > 0 {
		for _, tag := range filter.Tags {
			query = query.Where("? = ANY(ar.tags)", tag)
		}
	}

	if len(filter.Jurisdictions) > 0 {
		query = query.Where("ar.jurisdictions && ?", datatypes.JSONQuery(fmt.Sprintf(`["%s"]`, filter.Jurisdictions[0])))
	}

	if len(filter.Currencies) > 0 {
		query = query.Where("ar.currencies && ?", datatypes.JSONQuery(fmt.Sprintf(`["%s"]`, filter.Currencies[0])))
	}

	if filter.MinAverageRating != nil {
		query = query.Where("ar.average_rating >= ?", *filter.MinAverageRating)
	}

	if filter.HealthCheckStatus != nil {
		query = query.Where("ar.health_check_status = ?", *filter.HealthCheckStatus)
	}

	// Get total count
	if err := query.Model(&models.AgentRegistry{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count agents: %w", err)
	}

	// Apply pagination
	offset := (page - 1) * limit
	if err := query.
		Order("ar.average_rating DESC, ar.total_reviews DESC, ar.created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&registries).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to search agents: %w", err)
	}

	return registries, total, nil
}

// DiscoverAgentsByCapability discovers agents with specific capabilities
func (r *ap2Repository) DiscoverAgentsByCapability(ctx context.Context, capabilities []string, page, limit int) ([]*models.AgentRegistry, int64, error) {
	var registries []*models.AgentRegistry
	var total int64

	query := r.db.WithContext(ctx).
		Where("is_active = ? AND is_verified = ? AND deleted_at IS NULL", true, true)

	// Filter by all capabilities
	for _, cap := range capabilities {
		query = query.Where("? = ANY(capabilities)", cap)
	}

	// Get total count
	if err := query.Model(&models.AgentRegistry{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count agents: %w", err)
	}

	// Apply pagination
	offset := (page - 1) * limit
	if err := query.
		Order("average_rating DESC, total_reviews DESC").
		Offset(offset).
		Limit(limit).
		Find(&registries).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to discover agents: %w", err)
	}

	return registries, total, nil
}

// GetPublicAgents retrieves all public agents
func (r *ap2Repository) GetPublicAgents(ctx context.Context, page, limit int) ([]*models.AgentRegistry, int64, error) {
	var registries []*models.AgentRegistry
	var total int64

	query := r.db.WithContext(ctx).
		Where("is_public = ? AND is_verified = ? AND is_active = ? AND deleted_at IS NULL", true, true, true)

	// Get total count
	if err := query.Model(&models.AgentRegistry{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count public agents: %w", err)
	}

	// Apply pagination
	offset := (page - 1) * limit
	if err := query.
		Order("average_rating DESC, total_reviews DESC, created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&registries).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get public agents: %w", err)
	}

	return registries, total, nil
}

// GetVerifiedAgents retrieves all verified agents
func (r *ap2Repository) GetVerifiedAgents(ctx context.Context, agentType string, page, limit int) ([]*models.AgentRegistry, int64, error) {
	var registries []*models.AgentRegistry
	var total int64

	query := r.db.WithContext(ctx).
		Where("is_verified = ? AND is_active = ? AND deleted_at IS NULL", true, true)

	if agentType != "" {
		query = query.Where("agent_type = ?", agentType)
	}

	// Get total count
	if err := query.Model(&models.AgentRegistry{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count verified agents: %w", err)
	}

	// Apply pagination
	offset := (page - 1) * limit
	if err := query.
		Order("average_rating DESC, total_reviews DESC, created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&registries).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get verified agents: %w", err)
	}

	return registries, total, nil
}

// VerifyAgentRegistry marks an agent as verified
func (r *ap2Repository) VerifyAgentRegistry(ctx context.Context, registryID string) error {
	now := time.Now()

	if err := r.db.WithContext(ctx).
		Model(&models.AgentRegistry{}).
		Where("id = ?", registryID).
		Updates(map[string]interface{}{
			"is_verified": true,
			"verified_at": now,
		}).Error; err != nil {
		return fmt.Errorf("failed to verify agent registry: %w", err)
	}

	return nil
}

// UnverifyAgentRegistry marks an agent as unverified
func (r *ap2Repository) UnverifyAgentRegistry(ctx context.Context, registryID string) error {
	if err := r.db.WithContext(ctx).
		Model(&models.AgentRegistry{}).
		Where("id = ?", registryID).
		Update("is_verified", false).Error; err != nil {
		return fmt.Errorf("failed to unverify agent registry: %w", err)
	}

	return nil
}

// DeactivateAgentRegistry deactivates an agent
func (r *ap2Repository) DeactivateAgentRegistry(ctx context.Context, registryID string) error {
	if err := r.db.WithContext(ctx).
		Model(&models.AgentRegistry{}).
		Where("id = ?", registryID).
		Update("is_active", false).Error; err != nil {
		return fmt.Errorf("failed to deactivate agent registry: %w", err)
	}

	return nil
}

// ActivateAgentRegistry activates a deactivated agent
func (r *ap2Repository) ActivateAgentRegistry(ctx context.Context, registryID string) error {
	if err := r.db.WithContext(ctx).
		Model(&models.AgentRegistry{}).
		Where("id = ?", registryID).
		Update("is_active", true).Error; err != nil {
		return fmt.Errorf("failed to activate agent registry: %w", err)
	}

	return nil
}

// UpdateAgentRegistryHealthCheck updates the health check status
func (r *ap2Repository) UpdateAgentRegistryHealthCheck(ctx context.Context, registryID, status, message string) error {
	now := time.Now()

	if err := r.db.WithContext(ctx).
		Model(&models.AgentRegistry{}).
		Where("id = ?", registryID).
		Updates(map[string]interface{}{
			"health_check_status":  status,
			"health_check_message": message,
			"last_health_check":    now,
		}).Error; err != nil {
		return fmt.Errorf("failed to update health check: %w", err)
	}

	return nil
}

// Agent Discovery Audit methods

// CreateDiscoveryAudit creates an audit log entry
func (r *ap2Repository) CreateDiscoveryAudit(ctx context.Context, audit *models.AgentDiscoveryAudit) error {
	if err := r.db.WithContext(ctx).Create(audit).Error; err != nil {
		r.log.Error("failed to create discovery audit", "error", err)
		return fmt.Errorf("failed to create discovery audit: %w", err)
	}

	return nil
}

// GetDiscoveryAuditByID retrieves an audit entry by ID
func (r *ap2Repository) GetDiscoveryAuditByID(ctx context.Context, id string) (*models.AgentDiscoveryAudit, error) {
	var audit models.AgentDiscoveryAudit

	if err := r.db.WithContext(ctx).Where("id = ?", id).First(&audit).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("audit entry not found")
		}
		return nil, fmt.Errorf("failed to get audit entry: %w", err)
	}

	return &audit, nil
}

// GetDiscoveryAuditByAgent retrieves audit entries for an agent
func (r *ap2Repository) GetDiscoveryAuditByAgent(ctx context.Context, agentRegistryID string, page, limit int) ([]*models.AgentDiscoveryAudit, int64, error) {
	var audits []*models.AgentDiscoveryAudit
	var total int64

	query := r.db.WithContext(ctx).Where("agent_registry_id = ?", agentRegistryID)

	// Get total count
	if err := query.Model(&models.AgentDiscoveryAudit{}).Count(&total).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to count audits: %w", err)
	}

	// Apply pagination
	offset := (page - 1) * limit
	if err := query.
		Order("created_at DESC").
		Offset(offset).
		Limit(limit).
		Find(&audits).Error; err != nil {
		return nil, 0, fmt.Errorf("failed to get audits: %w", err)
	}

	return audits, total, nil
}

// Agent Discovery Stats methods

// GetDiscoveryStats retrieves stats for an agent
func (r *ap2Repository) GetDiscoveryStats(ctx context.Context, agentRegistryID string) (*models.AgentDiscoveryStats, error) {
	var stats models.AgentDiscoveryStats

	if err := r.db.WithContext(ctx).
		Where("agent_registry_id = ?", agentRegistryID).
		First(&stats).Error; err != nil {
		if err == gorm.ErrRecordNotFound {
			return nil, fmt.Errorf("stats not found")
		}
		return nil, fmt.Errorf("failed to get stats: %w", err)
	}

	return &stats, nil
}

// UpdateDiscoveryStats updates discovery statistics
func (r *ap2Repository) UpdateDiscoveryStats(ctx context.Context, stats *models.AgentDiscoveryStats) error {
	stats.UpdatedAt = time.Now()

	if err := r.db.WithContext(ctx).Save(stats).Error; err != nil {
		return fmt.Errorf("failed to update stats: %w", err)
	}

	return nil
}

// IncrementAgentViews increments the view count
func (r *ap2Repository) IncrementAgentViews(ctx context.Context, agentRegistryID string) error {
	now := time.Now()

	if err := r.db.WithContext(ctx).
		Model(&models.AgentDiscoveryStats{}).
		Where("agent_registry_id = ?", agentRegistryID).
		Updates(map[string]interface{}{
			"total_views":     gorm.Expr("total_views + 1"),
			"views_this_week": gorm.Expr("views_this_week + 1"),
			"last_viewed_at":  now,
			"updated_at":      now,
		}).Error; err != nil {
		return fmt.Errorf("failed to increment views: %w", err)
	}

	return nil
}

// IncrementAgentSearchFound increments the search found count
func (r *ap2Repository) IncrementAgentSearchFound(ctx context.Context, agentRegistryID string) error {
	now := time.Now()

	if err := r.db.WithContext(ctx).
		Model(&models.AgentDiscoveryStats{}).
		Where("agent_registry_id = ?", agentRegistryID).
		Updates(map[string]interface{}{
			"total_searches_found": gorm.Expr("total_searches_found + 1"),
			"last_search_found_at": now,
			"updated_at":           now,
		}).Error; err != nil {
		return fmt.Errorf("failed to increment search found: %w", err)
	}

	return nil
}

// IncrementAgentInquiries increments the inquiry count
func (r *ap2Repository) IncrementAgentInquiries(ctx context.Context, agentRegistryID string) error {
	now := time.Now()

	if err := r.db.WithContext(ctx).
		Model(&models.AgentDiscoveryStats{}).
		Where("agent_registry_id = ?", agentRegistryID).
		Updates(map[string]interface{}{
			"total_inquiries":     gorm.Expr("total_inquiries + 1"),
			"inquiries_this_week": gorm.Expr("inquiries_this_week + 1"),
			"last_inquiry_at":     now,
			"updated_at":          now,
		}).Error; err != nil {
		return fmt.Errorf("failed to increment inquiries: %w", err)
	}

	return nil
}

// IncrementAgentIntegrations increments the integration count
func (r *ap2Repository) IncrementAgentIntegrations(ctx context.Context, agentRegistryID string) error {
	now := time.Now()

	if err := r.db.WithContext(ctx).
		Model(&models.AgentDiscoveryStats{}).
		Where("agent_registry_id = ?", agentRegistryID).
		Updates(map[string]interface{}{
			"total_integrations":     gorm.Expr("total_integrations + 1"),
			"integrations_this_week": gorm.Expr("integrations_this_week + 1"),
			"last_integration_at":    now,
			"updated_at":             now,
		}).Error; err != nil {
		return fmt.Errorf("failed to increment integrations: %w", err)
	}

	return nil
}
