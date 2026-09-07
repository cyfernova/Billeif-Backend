package postgres

import (
	"context"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
)

func (r *AgentGovernanceRepository) GovernanceOverview(ctx context.Context, businessID string) ([]models.AIGovernanceControl, []models.AIAgentRun, error) {
	if r == nil || r.db == nil || !validUUID(businessID) {
		return nil, nil, interfaces.ErrAgentGovernanceInvalidScope
	}
	var controls []models.AIGovernanceControl
	if err := r.db.WithContext(ctx).Where("scope_kind = 'global' OR (scope_kind = 'business' AND business_id = ?)", businessID).Find(&controls).Error; err != nil {
		return nil, nil, err
	}
	var runs []models.AIAgentRun
	// Never return prompts, tool arguments, provider configuration or other businesses' runs.
	err := r.db.WithContext(ctx).Select("id", "agent_id", "status", "steps_used", "max_steps", "cost_micros", "cost_currency", "created_at", "cancel_requested_at").Where("business_id = ?", businessID).Order("created_at DESC, id DESC").Limit(50).Find(&runs).Error
	return controls, runs, err
}
