package services

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/logger"
)

var (
	ErrAgentConfigNotFound = fmt.Errorf("agent configuration not found")
	ErrInvalidConfig       = fmt.Errorf("invalid agent configuration")
)

type AgentConfigService struct {
	wellKnownDir string
	configs      map[string]*models.WellKnownAgentConfig
	mu           sync.RWMutex
	log          *logger.Logger
}

func NewAgentConfigService(wellKnownDir string, log *logger.Logger) *AgentConfigService {
	if os.Getenv("AWS_LAMBDA_FUNCTION_NAME") != "" && !filepath.IsAbs(wellKnownDir) {
		wellKnownDir = filepath.Join(os.TempDir(), wellKnownDir)
	}

	svc := &AgentConfigService{
		wellKnownDir: wellKnownDir,
		configs:      make(map[string]*models.WellKnownAgentConfig),
		log:          log,
	}

	if err := os.MkdirAll(wellKnownDir, 0755); err != nil {
		log.Error("failed to create .well-known directory", "error", err, "dir", wellKnownDir)
	}

	svc.loadFromFile()

	return svc
}

type UpdateBuyerConfigRequest struct {
	MaxDiscountPercent  float64  `json:"max_discount_percent" validate:"required,gte=0,lte=100"`
	MinDiscountPercent  float64  `json:"min_discount_percent" validate:"required,gte=0,lte=100"`
	TargetDiscount      float64  `json:"target_discount" validate:"required,gte=0,lte=100"`
	RiskTolerance       float64  `json:"risk_tolerance" validate:"required,gte=0,lte=1"`
	PatienceLevel       float64  `json:"patience_level" validate:"required,gte=0,lte=1"`
	MaxRounds           int      `json:"max_rounds" validate:"required,gte=1,lte=20"`
	PreferredProducts   []string `json:"preferred_products"`
	BlacklistedVendors  []string `json:"blacklisted_vendors"`
	BudgetLimit         *float64 `json:"budget_limit,omitempty"`
	PaymentTerms        []string `json:"payment_terms"`
	AcceptanceThreshold float64  `json:"acceptance_threshold" validate:"required,gte=0,lte=1"`
}

type UpdateSellerConfigRequest struct {
	MinAcceptablePrice    float64  `json:"min_acceptable_price" validate:"required,gte=0"`
	MaxMarkupPercent      float64  `json:"max_markup_percent" validate:"required,gte=0,lte=200"`
	InventoryPressure     float64  `json:"inventory_pressure" validate:"required,gte=0,lte=1"`
	SalesVolumeGoal       *float64 `json:"sales_volume_goal,omitempty"`
	CustomerLoyaltyFactor float64  `json:"customer_loyalty_factor" validate:"required,gte=0,lte=2"`
	MaxRounds             int      `json:"max_rounds" validate:"required,gte=1,lte=20"`
	PreferredCustomers    []string `json:"preferred_customers"`
	PaymentTerms          []string `json:"payment_terms"`
	AcceptanceThreshold   float64  `json:"acceptance_threshold" validate:"required,gte=0,lte=1"`
}

func (s *AgentConfigService) SaveAgentConfig(ctx context.Context, agent *models.Agent, config *models.AgentConfig) (*models.WellKnownAgentConfig, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if config == nil {
		return nil, fmt.Errorf("%w: config is required", ErrInvalidConfig)
	}

	roleType := RoleConfigTypeForAgentType(agent.Type)
	if roleType == "" {
		return nil, fmt.Errorf("%w: unsupported agent type %s", ErrInvalidConfig, agent.Type)
	}
	config.Type = roleType

	if err := s.validateConfig(config); err != nil {
		return nil, fmt.Errorf("%w: %v", ErrInvalidConfig, err)
	}

	var capabilities []string
	if agent.Capabilities != "" {
		var caps []struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(agent.Capabilities), &caps); err == nil {
			for _, cap := range caps {
				capabilities = append(capabilities, cap.Type)
			}
		}
	}

	var a2aEndpoint string
	if agent.A2AEndpoint != nil {
		a2aEndpoint = *agent.A2AEndpoint
	}

	wellKnownConfig := &models.WellKnownAgentConfig{
		AgentID:      agent.ID,
		Name:         agent.Name,
		Type:         roleType,
		Description:  "",
		Version:      "1.0",
		Config:       config,
		Capabilities: capabilities,
		A2AEndpoint:  a2aEndpoint,
		CreatedAt:    agent.CreatedAt,
		UpdatedAt:    agent.UpdatedAt,
		Metadata: map[string]interface{}{
			"marketplace_role": MarketplaceRoleForType(agent.Type),
			"marketplace_type": NormalizeMarketplaceAgentType(agent.Type),
		},
	}

	s.configs[agent.ID] = wellKnownConfig

	if err := s.saveToFile(); err != nil {
		s.log.Error("failed to save agent config to file", "error", err, "agent_id", agent.ID)
		return wellKnownConfig, nil
	}

	s.log.Info("agent configuration saved", "agent_id", agent.ID, "type", config.Type)

	return wellKnownConfig, nil
}

func (s *AgentConfigService) CreateDefaultConfigForAgentType(agentType string) (*models.AgentConfig, error) {
	switch RoleConfigTypeForAgentType(agentType) {
	case models.AgentTypeBuyer:
		return s.CreateDefaultBuyerConfig(), nil
	case models.AgentTypeSeller:
		return s.CreateDefaultSellerConfig(), nil
	default:
		return nil, fmt.Errorf("%w: unsupported agent type %s", ErrInvalidConfig, agentType)
	}
}

func (s *AgentConfigService) GetAgentConfig(ctx context.Context, agentID string) (*models.WellKnownAgentConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	config, exists := s.configs[agentID]
	if !exists {
		return nil, ErrAgentConfigNotFound
	}

	return config, nil
}

func (s *AgentConfigService) GetAllAgentConfigs(ctx context.Context) ([]*models.WellKnownAgentConfig, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	configs := make([]*models.WellKnownAgentConfig, 0, len(s.configs))
	for _, config := range s.configs {
		configs = append(configs, config)
	}

	return configs, nil
}

func (s *AgentConfigService) DeleteAgentConfig(ctx context.Context, agentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, exists := s.configs[agentID]; !exists {
		return ErrAgentConfigNotFound
	}

	delete(s.configs, agentID)

	if err := s.saveToFile(); err != nil {
		s.log.Error("failed to save after deleting agent config", "error", err, "agent_id", agentID)
		return err
	}

	s.log.Info("agent configuration deleted", "agent_id", agentID)

	return nil
}

func (s *AgentConfigService) CreateDefaultBuyerConfig() *models.AgentConfig {
	return &models.AgentConfig{
		Type:       models.AgentTypeBuyer,
		Volatility: 0.5,
		BuyerConfig: &models.BuyerConfig{
			MaxDiscountPercent:  25.0,
			MinDiscountPercent:  5.0,
			TargetDiscount:      15.0,
			RiskTolerance:       0.5,
			PatienceLevel:       0.5,
			MaxRounds:           5,
			PreferredProducts:   []string{},
			BlacklistedVendors:  []string{},
			PaymentTerms:        []string{"net30", "net60"},
			AcceptanceThreshold: 0.85,
		},
	}
}

func (s *AgentConfigService) CreateDefaultSellerConfig() *models.AgentConfig {
	return &models.AgentConfig{
		Type:       models.AgentTypeSeller,
		Volatility: 0.5,
		SellerConfig: &models.SellerConfig{
			MinAcceptablePrice:    0.0,
			MaxMarkupPercent:      30.0,
			InventoryPressure:     0.5,
			CustomerLoyaltyFactor: 1.0,
			MaxRounds:             5,
			PreferredCustomers:    []string{},
			PaymentTerms:          []string{"net30"},
			VolumeDiscountTiers:   []models.DiscountTier{},
			AcceptanceThreshold:   0.85,
		},
	}
}

func (s *AgentConfigService) saveToFile() error {
	agents := make([]models.WellKnownAgentConfig, 0, len(s.configs))
	for _, config := range s.configs {
		agents = append(agents, *config)
	}

	file := &models.WellKnownAgentsFile{
		Version:  "1.0",
		Agents:   agents,
		LastSync: s.now(),
		Metadata: make(map[string]interface{}),
	}

	data, err := json.MarshalIndent(file, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal agent configs: %w", err)
	}

	filePath := filepath.Join(s.wellKnownDir, "agents.json")
	if err := os.WriteFile(filePath, data, 0644); err != nil {
		return fmt.Errorf("failed to write agents.json: %w", err)
	}

	return nil
}

func (s *AgentConfigService) loadFromFile() {
	filePath := filepath.Join(s.wellKnownDir, "agents.json")

	data, err := os.ReadFile(filePath)
	if err != nil {
		if !os.IsNotExist(err) {
			s.log.Error("failed to read agents.json", "error", err)
		}
		return
	}

	var file models.WellKnownAgentsFile
	if err := json.Unmarshal(data, &file); err != nil {
		s.log.Error("failed to unmarshal agents.json", "error", err)
		return
	}

	s.configs = make(map[string]*models.WellKnownAgentConfig)
	for i := range file.Agents {
		agent := &file.Agents[i]
		s.configs[agent.AgentID] = agent
	}

	s.log.Info("loaded agent configurations", "count", len(s.configs))
}

func (s *AgentConfigService) validateConfig(config *models.AgentConfig) error {
	if config.Type != models.AgentTypeBuyer && config.Type != models.AgentTypeSeller {
		return fmt.Errorf("invalid agent type: %s", config.Type)
	}

	if config.Volatility < 0 || config.Volatility > 1 {
		return fmt.Errorf("volatility must be between 0 and 1")
	}

	if config.Type == models.AgentTypeBuyer {
		if config.BuyerConfig == nil {
			return fmt.Errorf("buyer config required for buyer agent")
		}
		buyer := config.BuyerConfig
		if buyer.MaxRounds < 1 || buyer.MaxRounds > 20 {
			return fmt.Errorf("max rounds must be between 1 and 20")
		}
		if buyer.MaxDiscountPercent < buyer.MinDiscountPercent {
			return fmt.Errorf("max discount must be greater than min discount")
		}
		if buyer.AcceptanceThreshold < 0 || buyer.AcceptanceThreshold > 1 {
			return fmt.Errorf("acceptance threshold must be between 0 and 1")
		}
	}

	if config.Type == models.AgentTypeSeller {
		if config.SellerConfig == nil {
			return fmt.Errorf("seller config required for seller agent")
		}
		seller := config.SellerConfig
		if seller.MaxRounds < 1 || seller.MaxRounds > 20 {
			return fmt.Errorf("max rounds must be between 1 and 20")
		}
		if seller.AcceptanceThreshold < 0 || seller.AcceptanceThreshold > 1 {
			return fmt.Errorf("acceptance threshold must be between 0 and 1")
		}
	}

	return nil
}

func (s *AgentConfigService) now() time.Time {
	return time.Now()
}
