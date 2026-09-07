package postgres

import (
	"context"
	"encoding/json"
	"fmt"
	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"

	"gorm.io/gorm"
)

type AgentConfigRepository struct{ db *gorm.DB }

func NewAgentConfigRepository(db *gorm.DB) *AgentConfigRepository {
	return &AgentConfigRepository{db: db}
}

func (r *AgentConfigRepository) Save(ctx context.Context, businessID string, config *models.WellKnownAgentConfig) error {
	data, err := json.Marshal(config)
	if err != nil {
		return err
	}
	result := r.db.WithContext(ctx).Exec(`INSERT INTO agent_bargaining_configs (agent_id, business_id, configuration)
 VALUES (?, ?, ?::jsonb) ON CONFLICT (agent_id) DO UPDATE SET configuration = EXCLUDED.configuration, updated_at = NOW()
	 WHERE agent_bargaining_configs.business_id = EXCLUDED.business_id`, config.AgentID, businessID, string(data))
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected != 1 {
		return fmt.Errorf("agent configuration business mismatch")
	}
	return nil
}

func (r *AgentConfigRepository) Get(ctx context.Context, agentID string) (*models.WellKnownAgentConfig, error) {
	var row struct{ Configuration string }
	result := r.db.WithContext(ctx).Raw(`SELECT configuration FROM agent_bargaining_configs WHERE agent_id = ?`, agentID).Scan(&row)
	if result.Error != nil {
		return nil, result.Error
	}
	if result.RowsAffected == 0 {
		return nil, interfaces.ErrAgentConfigNotFound
	}
	var config models.WellKnownAgentConfig
	if err := json.Unmarshal([]byte(row.Configuration), &config); err != nil {
		return nil, fmt.Errorf("decode agent configuration: %w", err)
	}
	return &config, nil
}

func (r *AgentConfigRepository) List(ctx context.Context) ([]*models.WellKnownAgentConfig, error) {
	var rows []struct{ Configuration string }
	if err := r.db.WithContext(ctx).Raw(`SELECT configuration FROM agent_bargaining_configs ORDER BY agent_id`).Scan(&rows).Error; err != nil {
		return nil, err
	}
	result := make([]*models.WellKnownAgentConfig, 0, len(rows))
	for _, row := range rows {
		var config models.WellKnownAgentConfig
		if err := json.Unmarshal([]byte(row.Configuration), &config); err != nil {
			return nil, err
		}
		result = append(result, &config)
	}
	return result, nil
}

func (r *AgentConfigRepository) Delete(ctx context.Context, agentID string) error {
	result := r.db.WithContext(ctx).Exec(`DELETE FROM agent_bargaining_configs WHERE agent_id = ?`, agentID)
	if result.Error != nil {
		return result.Error
	}
	if result.RowsAffected == 0 {
		return interfaces.ErrAgentConfigNotFound
	}
	return nil
}
