package postgres

import (
	"context"
	"encoding/json"
	"invoice-backend/internal/repositories/interfaces"
)

func (r *AgentGovernanceRepository) SaveBargainingProposal(ctx context.Context, business, negotiation, agent string, round int, proposal json.RawMessage) error {
	if !validUUID(business) || !validUUID(negotiation) || !validUUID(agent) || round < 1 || round > 20 || !json.Valid(proposal) {
		return interfaces.ErrAgentGovernanceInvalidScope
	}
	result := r.db.WithContext(ctx).Exec(`INSERT INTO ai_bargaining_proposals (business_id, negotiation_id, agent_id, round_number, proposal)
SELECT ?, id, ?, ?, ?::jsonb FROM bargaining_negotiations WHERE id = ? AND business_id = ? AND (buyer_agent_id = ? OR seller_agent_id = ?)`, business, agent, round, string(proposal), negotiation, business, agent, agent)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return interfaces.ErrAgentGovernanceNotFound
	}
	return nil
}

func (r *AgentGovernanceRepository) ReadBargainingProposal(ctx context.Context, business, negotiation, agent string, round int) (json.RawMessage, error) {
	var row struct{ Proposal string }
	result := r.db.WithContext(ctx).Table("ai_bargaining_proposals").Select("proposal::text AS proposal").Where("business_id = ? AND negotiation_id = ? AND agent_id = ? AND round_number = ?", business, negotiation, agent, round).Take(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	return json.RawMessage(row.Proposal), nil
}
