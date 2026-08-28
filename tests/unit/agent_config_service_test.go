package unit

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/assert"
)

func setupTestDir(t *testing.T) string {
	tmpDir := t.TempDir()
	return tmpDir
}

func TestAgentConfigServiceUsesLambdaWritableStorage(t *testing.T) {
	tmpDir := t.TempDir()
	t.Setenv("AWS_LAMBDA_FUNCTION_NAME", "billeif-test-api-http")
	t.Setenv("TMPDIR", tmpDir)

	services.NewAgentConfigService(".well-known", logger.New())

	if _, err := os.Stat(filepath.Join(tmpDir, ".well-known")); err != nil {
		t.Fatalf("expected Lambda agent config directory under temporary storage: %v", err)
	}
}

func TestAgentConfigService_CreateDefaultBuyerConfig(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	config := svc.CreateDefaultBuyerConfig()

	assert.NotNil(t, config)
	assert.Equal(t, models.AgentTypeBuyer, config.Type)
	assert.Equal(t, 0.5, config.Volatility)
	assert.NotNil(t, config.BuyerConfig)
	assert.Equal(t, 25.0, config.BuyerConfig.MaxDiscountPercent)
	assert.Equal(t, 5.0, config.BuyerConfig.MinDiscountPercent)
	assert.Equal(t, 15.0, config.BuyerConfig.TargetDiscount)
	assert.Equal(t, 0.5, config.BuyerConfig.RiskTolerance)
	assert.Equal(t, 0.5, config.BuyerConfig.PatienceLevel)
	assert.Equal(t, 5, config.BuyerConfig.MaxRounds)
	assert.Equal(t, 0.85, config.BuyerConfig.AcceptanceThreshold)
	assert.Contains(t, config.BuyerConfig.PaymentTerms, "net30")
	assert.Contains(t, config.BuyerConfig.PaymentTerms, "net60")
}

func TestAgentConfigService_CreateDefaultSellerConfig(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	config := svc.CreateDefaultSellerConfig()

	assert.NotNil(t, config)
	assert.Equal(t, models.AgentTypeSeller, config.Type)
	assert.Equal(t, 0.5, config.Volatility)
	assert.NotNil(t, config.SellerConfig)
	assert.Equal(t, 0.0, config.SellerConfig.MinAcceptablePrice)
	assert.Equal(t, 30.0, config.SellerConfig.MaxMarkupPercent)
	assert.Equal(t, 0.5, config.SellerConfig.InventoryPressure)
	assert.Equal(t, 1.0, config.SellerConfig.CustomerLoyaltyFactor)
	assert.Equal(t, 5, config.SellerConfig.MaxRounds)
	assert.Equal(t, 0.85, config.SellerConfig.AcceptanceThreshold)
	assert.Contains(t, config.SellerConfig.PaymentTerms, "net30")
}

func TestAgentConfigService_CreateDefaultConfigForAgentType_Buyer(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	config, err := svc.CreateDefaultConfigForAgentType("shopping")

	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, models.AgentTypeBuyer, config.Type)
}

func TestAgentConfigService_CreateDefaultConfigForAgentType_Seller(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	config, err := svc.CreateDefaultConfigForAgentType("merchant")

	assert.NoError(t, err)
	assert.NotNil(t, config)
	assert.Equal(t, models.AgentTypeSeller, config.Type)
}

func TestAgentConfigService_CreateDefaultConfigForAgentType_Unsupported(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	config, err := svc.CreateDefaultConfigForAgentType("unsupported")

	assert.Error(t, err)
	assert.Nil(t, config)
	assert.Contains(t, err.Error(), "unsupported agent type")
}

func TestAgentConfigService_SaveAgentConfig_Buyer(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	now := time.Now()
	agent := &models.Agent{
		ID:           "agent-123",
		Name:         "Test Buyer",
		Type:         "shopping",
		Capabilities: `[{"type":"search"}]`,
		A2AEndpoint:  strPtr("https://example.com/a2a"),
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	config := &models.AgentConfig{
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
			PaymentTerms:        []string{"net30"},
			AcceptanceThreshold: 0.85,
		},
	}

	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "agent-123", result.AgentID)
	assert.Equal(t, "Test Buyer", result.Name)
	assert.Equal(t, models.AgentTypeBuyer, result.Type)
	assert.Equal(t, "1.0", result.Version)
	assert.Contains(t, result.Capabilities, "search")
	assert.Equal(t, "https://example.com/a2a", result.A2AEndpoint)
}

func TestAgentConfigService_SaveAgentConfig_Seller(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	now := time.Now()
	agent := &models.Agent{
		ID:           "agent-456",
		Name:         "Test Seller",
		Type:         "merchant",
		Capabilities: `[{"type":"sell"}]`,
		CreatedAt:    now,
		UpdatedAt:    now,
	}

	config := &models.AgentConfig{
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
			AcceptanceThreshold:   0.85,
		},
	}

	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "agent-456", result.AgentID)
	assert.Equal(t, models.AgentTypeSeller, result.Type)
}

func TestAgentConfigService_SaveAgentConfig_NilConfig(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	agent := &models.Agent{
		ID:   "agent-123",
		Name: "Test Agent",
		Type: "shopping",
	}

	result, err := svc.SaveAgentConfig(context.Background(), agent, nil)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "config is required")
}

func TestAgentConfigService_SaveAgentConfig_UnsupportedAgentType(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	agent := &models.Agent{
		ID:   "agent-123",
		Name: "Test Agent",
		Type: "unsupported",
	}

	config := &models.AgentConfig{
		Type: models.AgentTypeBuyer,
	}

	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "unsupported agent type")
}

func TestAgentConfigService_SaveAgentConfig_InvalidVolatility(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	agent := &models.Agent{
		ID:   "agent-123",
		Name: "Test Agent",
		Type: "shopping",
	}

	config := &models.AgentConfig{
		Type:       models.AgentTypeBuyer,
		Volatility: 1.5, // Invalid - must be between 0 and 1
		BuyerConfig: &models.BuyerConfig{
			MaxDiscountPercent:  25.0,
			MinDiscountPercent:  5.0,
			MaxRounds:           5,
			AcceptanceThreshold: 0.85,
		},
	}

	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "volatility must be between 0 and 1")
}

func TestAgentConfigService_SaveAgentConfig_MaxDiscountLessThanMin(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	agent := &models.Agent{
		ID:   "agent-123",
		Name: "Test Agent",
		Type: "shopping",
	}

	config := &models.AgentConfig{
		Type:       models.AgentTypeBuyer,
		Volatility: 0.5,
		BuyerConfig: &models.BuyerConfig{
			MaxDiscountPercent:  5.0, // Less than MinDiscountPercent
			MinDiscountPercent:  25.0,
			MaxRounds:           5,
			AcceptanceThreshold: 0.85,
		},
	}

	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "max discount must be greater than min discount")
}

func TestAgentConfigService_GetAgentConfig_Success(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	// First save a config
	now := time.Now()
	agent := &models.Agent{
		ID:        "agent-123",
		Name:      "Test Agent",
		Type:      "shopping",
		CreatedAt: now,
		UpdatedAt: now,
	}

	config := &models.AgentConfig{
		Type:       models.AgentTypeBuyer,
		Volatility: 0.5,
		BuyerConfig: &models.BuyerConfig{
			MaxDiscountPercent:  25.0,
			MinDiscountPercent:  5.0,
			MaxRounds:           5,
			AcceptanceThreshold: 0.85,
		},
	}

	_, err := svc.SaveAgentConfig(context.Background(), agent, config)
	assert.NoError(t, err)

	// Then retrieve it
	result, err := svc.GetAgentConfig(context.Background(), "agent-123")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, "agent-123", result.AgentID)
}

func TestAgentConfigService_GetAgentConfig_NotFound(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	result, err := svc.GetAgentConfig(context.Background(), "nonexistent")

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Equal(t, services.ErrAgentConfigNotFound, err)
}

func TestAgentConfigService_GetAllAgentConfigs(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	// Save multiple configs
	now := time.Now()
	for i := 1; i <= 3; i++ {
		agent := &models.Agent{
			ID:        "agent-" + string(rune('0'+i)),
			Name:      "Agent " + string(rune('0'+i)),
			Type:      "shopping",
			CreatedAt: now,
			UpdatedAt: now,
		}
		config := &models.AgentConfig{
			Type:       models.AgentTypeBuyer,
			Volatility: 0.5,
			BuyerConfig: &models.BuyerConfig{
				MaxDiscountPercent:  25.0,
				MinDiscountPercent:  5.0,
				MaxRounds:           5,
				AcceptanceThreshold: 0.85,
			},
		}
		_, err := svc.SaveAgentConfig(context.Background(), agent, config)
		assert.NoError(t, err)
	}

	// Retrieve all
	results, err := svc.GetAllAgentConfigs(context.Background())

	assert.NoError(t, err)
	assert.Len(t, results, 3)
}

func TestAgentConfigService_GetAllAgentConfigs_Empty(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	results, err := svc.GetAllAgentConfigs(context.Background())

	assert.NoError(t, err)
	assert.Len(t, results, 0)
}

func TestAgentConfigService_DeleteAgentConfig_Success(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	// First save a config
	now := time.Now()
	agent := &models.Agent{
		ID:        "agent-123",
		Name:      "Test Agent",
		Type:      "shopping",
		CreatedAt: now,
		UpdatedAt: now,
	}

	config := &models.AgentConfig{
		Type:       models.AgentTypeBuyer,
		Volatility: 0.5,
		BuyerConfig: &models.BuyerConfig{
			MaxDiscountPercent:  25.0,
			MinDiscountPercent:  5.0,
			MaxRounds:           5,
			AcceptanceThreshold: 0.85,
		},
	}

	_, err := svc.SaveAgentConfig(context.Background(), agent, config)
	assert.NoError(t, err)

	// Delete it
	err = svc.DeleteAgentConfig(context.Background(), "agent-123")

	assert.NoError(t, err)

	// Verify it's gone
	result, err := svc.GetAgentConfig(context.Background(), "agent-123")
	assert.Error(t, err)
	assert.Nil(t, result)
}

func TestAgentConfigService_DeleteAgentConfig_NotFound(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	err := svc.DeleteAgentConfig(context.Background(), "nonexistent")

	assert.Error(t, err)
	assert.Equal(t, services.ErrAgentConfigNotFound, err)
}

func TestAgentConfigService_ValidateConfig_InvalidBuyerConfig(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	// Missing BuyerConfig for buyer type
	config := &models.AgentConfig{
		Type:       models.AgentTypeBuyer,
		Volatility: 0.5,
		// BuyerConfig is nil
	}

	// We need to use reflection or save directly to test validation
	// Since SaveAgentConfig calls validateConfig internally, let's test via SaveAgentConfig
	agent := &models.Agent{
		ID:   "agent-123",
		Name: "Test Agent",
		Type: "shopping",
	}

	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "buyer config required")
}

func TestAgentConfigService_ValidateConfig_InvalidSellerConfig(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	// Missing SellerConfig for seller type
	config := &models.AgentConfig{
		Type:       models.AgentTypeSeller,
		Volatility: 0.5,
		// SellerConfig is nil
	}

	agent := &models.Agent{
		ID:   "agent-123",
		Name: "Test Agent",
		Type: "merchant",
	}

	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "seller config required")
}

func TestAgentConfigService_ValidateConfig_BuyerMaxRoundsOutOfRange(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	config := &models.AgentConfig{
		Type:       models.AgentTypeBuyer,
		Volatility: 0.5,
		BuyerConfig: &models.BuyerConfig{
			MaxDiscountPercent:  25.0,
			MinDiscountPercent:  5.0,
			MaxRounds:           25, // Invalid - must be 1-20
			AcceptanceThreshold: 0.85,
		},
	}

	agent := &models.Agent{
		ID:   "agent-123",
		Name: "Test Agent",
		Type: "shopping",
	}

	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "max rounds must be between 1 and 20")
}

func TestAgentConfigService_ValidateConfig_SellerAcceptanceThresholdOutOfRange(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	config := &models.AgentConfig{
		Type:       models.AgentTypeSeller,
		Volatility: 0.5,
		SellerConfig: &models.SellerConfig{
			MinAcceptablePrice:  0.0,
			MaxMarkupPercent:    30.0,
			MaxRounds:           5,
			AcceptanceThreshold: 1.5, // Invalid - must be 0-1
		},
	}

	agent := &models.Agent{
		ID:   "agent-123",
		Name: "Test Agent",
		Type: "merchant",
	}

	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	assert.Error(t, err)
	assert.Nil(t, result)
	assert.Contains(t, err.Error(), "acceptance threshold must be between 0 and 1")
}

func TestAgentConfigService_Persistence(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()

	// Create service and save a config
	svc1 := services.NewAgentConfigService(tmpDir, log)
	now := time.Now()
	agent := &models.Agent{
		ID:        "agent-persist",
		Name:      "Persistence Test",
		Type:      "shopping",
		CreatedAt: now,
		UpdatedAt: now,
	}
	config := &models.AgentConfig{
		Type:       models.AgentTypeBuyer,
		Volatility: 0.7,
		BuyerConfig: &models.BuyerConfig{
			MaxDiscountPercent:  30.0,
			MinDiscountPercent:  10.0,
			MaxRounds:           10,
			AcceptanceThreshold: 0.90,
		},
	}
	_, err := svc1.SaveAgentConfig(context.Background(), agent, config)
	assert.NoError(t, err)

	// Create a new service with the same directory - config should be loaded
	svc2 := services.NewAgentConfigService(tmpDir, log)
	result, err := svc2.GetAgentConfig(context.Background(), "agent-persist")

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, 0.7, result.Config.Volatility)
	assert.Equal(t, 30.0, result.Config.BuyerConfig.MaxDiscountPercent)
}

func TestAgentConfigService_FilePersistenceFailure(t *testing.T) {
	// Use an invalid directory path to trigger file write error
	tmpDir := "/nonexistent/path/that/cannot/be/written/to"
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	now := time.Now()
	agent := &models.Agent{
		ID:        "agent-fail",
		Name:      "Fail Test",
		Type:      "shopping",
		CreatedAt: now,
		UpdatedAt: now,
	}
	config := &models.AgentConfig{
		Type:       models.AgentTypeBuyer,
		Volatility: 0.5,
		BuyerConfig: &models.BuyerConfig{
			MaxDiscountPercent:  25.0,
			MinDiscountPercent:  5.0,
			MaxRounds:           5,
			AcceptanceThreshold: 0.85,
		},
	}

	// Save should still work in memory even if file write fails
	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	// The implementation returns the config even if file write fails
	assert.NoError(t, err)
	assert.NotNil(t, result)
}

func TestAgentConfigService_Metadata(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	now := time.Now()
	agent := &models.Agent{
		ID:        "agent-meta",
		Name:      "Metadata Test",
		Type:      "shopping",
		CreatedAt: now,
		UpdatedAt: now,
	}
	config := &models.AgentConfig{
		Type:       models.AgentTypeBuyer,
		Volatility: 0.5,
		BuyerConfig: &models.BuyerConfig{
			MaxDiscountPercent:  25.0,
			MinDiscountPercent:  5.0,
			MaxRounds:           5,
			AcceptanceThreshold: 0.85,
		},
	}

	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.NotNil(t, result.Metadata)
	assert.Equal(t, "buyer", result.Metadata["marketplace_role"])
	assert.Equal(t, "shopping", result.Metadata["marketplace_type"])
}

func strPtr(s string) *string {
	return &s
}

// Helper to create a properly typed test
func createBuyerAgent(id, name string) *models.Agent {
	now := time.Now()
	return &models.Agent{
		ID:        id,
		Name:      name,
		Type:      "shopping",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func createSellerAgent(id, name string) *models.Agent {
	now := time.Now()
	return &models.Agent{
		ID:        id,
		Name:      name,
		Type:      "merchant",
		CreatedAt: now,
		UpdatedAt: now,
	}
}

func createBuyerConfig() *models.AgentConfig {
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
			PaymentTerms:        []string{"net30"},
			AcceptanceThreshold: 0.85,
		},
	}
}

func createSellerConfig() *models.AgentConfig {
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
			AcceptanceThreshold:   0.85,
		},
	}
}

func TestAgentConfigService_UpdateAndRetrieve(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	// Create
	agent := createBuyerAgent("agent-update", "Update Test")
	config := createBuyerConfig()
	_, err := svc.SaveAgentConfig(context.Background(), agent, config)
	assert.NoError(t, err)

	// Retrieve
	result, err := svc.GetAgentConfig(context.Background(), "agent-update")
	assert.NoError(t, err)
	assert.Equal(t, "Update Test", result.Name)

	// Verify default values
	assert.Equal(t, 0.5, result.Config.Volatility)
	assert.Equal(t, 25.0, result.Config.BuyerConfig.MaxDiscountPercent)
}

func TestAgentConfigService_ListConfigs(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	// Create buyer and seller agents
	buyerAgent := createBuyerAgent("buyer-1", "Buyer One")
	_, err := svc.SaveAgentConfig(context.Background(), buyerAgent, createBuyerConfig())
	assert.NoError(t, err)

	sellerAgent := createSellerAgent("seller-1", "Seller One")
	_, err = svc.SaveAgentConfig(context.Background(), sellerAgent, createSellerConfig())
	assert.NoError(t, err)

	// List all
	results, err := svc.GetAllAgentConfigs(context.Background())
	assert.NoError(t, err)
	assert.Len(t, results, 2)

	// Verify types
	types := make(map[models.AgentType]bool)
	for _, r := range results {
		types[r.Type] = true
	}
	assert.True(t, types[models.AgentTypeBuyer])
	assert.True(t, types[models.AgentTypeSeller])
}

func TestAgentConfigService_DeleteNonExistent(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	err := svc.DeleteAgentConfig(context.Background(), "does-not-exist")

	assert.Error(t, err)
	assert.Equal(t, services.ErrAgentConfigNotFound, err)
}

func TestAgentConfigService_GetAfterDelete(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	// Create and delete
	agent := createBuyerAgent("temp-agent", "Temp Agent")
	_, err := svc.SaveAgentConfig(context.Background(), agent, createBuyerConfig())
	assert.NoError(t, err)

	err = svc.DeleteAgentConfig(context.Background(), "temp-agent")
	assert.NoError(t, err)

	// Try to get deleted config
	_, err = svc.GetAgentConfig(context.Background(), "temp-agent")
	assert.Error(t, err)
	assert.Equal(t, services.ErrAgentConfigNotFound, err)
}

func TestAgentConfigService_CapabilitiesParsing(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	agent := &models.Agent{
		ID:           "agent-caps",
		Name:         "Capabilities Test",
		Type:         "shopping",
		Capabilities: `[{"type":"search"},{"type":"compare"},{"type":"negotiate"}]`,
		CreatedAt:    time.Now(),
		UpdatedAt:    time.Now(),
	}

	config := createBuyerConfig()
	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Len(t, result.Capabilities, 3)
	assert.Contains(t, result.Capabilities, "search")
	assert.Contains(t, result.Capabilities, "compare")
	assert.Contains(t, result.Capabilities, "negotiate")
}

func TestAgentConfigService_A2AEndpoint(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	a2aEndpoint := "https://agent.example.com/a2a/v0.3"
	agent := &models.Agent{
		ID:          "agent-a2a",
		Name:        "A2A Test",
		Type:        "shopping",
		A2AEndpoint: &a2aEndpoint,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	config := createBuyerConfig()
	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, a2aEndpoint, result.A2AEndpoint)
}

func TestAgentConfigService_A2AEndpointNil(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	agent := &models.Agent{
		ID:          "agent-no-a2a",
		Name:        "No A2A Test",
		Type:        "shopping",
		A2AEndpoint: nil,
		CreatedAt:   time.Now(),
		UpdatedAt:   time.Now(),
	}

	config := createBuyerConfig()
	result, err := svc.SaveAgentConfig(context.Background(), agent, config)

	assert.NoError(t, err)
	assert.NotNil(t, result)
	assert.Empty(t, result.A2AEndpoint)
}

func TestAgentConfigService_SaveMultipleAgents(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	// Save multiple agents
	for i := 1; i <= 5; i++ {
		agent := createBuyerAgent("multi-agent-"+string(rune('0'+i)), "Multi Agent")
		_, err := svc.SaveAgentConfig(context.Background(), agent, createBuyerConfig())
		assert.NoError(t, err)
	}

	// Verify all saved
	results, err := svc.GetAllAgentConfigs(context.Background())
	assert.NoError(t, err)
	assert.Len(t, results, 5)

	// Verify each can be retrieved individually
	for i := 1; i <= 5; i++ {
		result, err := svc.GetAgentConfig(context.Background(), "multi-agent-"+string(rune('0'+i)))
		assert.NoError(t, err)
		assert.NotNil(t, result)
	}
}

func TestAgentConfigService_BuyerAgentTypes(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	// Test with different buyer-related types
	buyerTypes := []string{"shopping", "buyer", "BUYER", "Shopping"}

	for _, agentType := range buyerTypes {
		agent := &models.Agent{
			ID:   "agent-" + agentType,
			Name: "Test " + agentType,
			Type: agentType,
		}
		config := createBuyerConfig()
		result, err := svc.SaveAgentConfig(context.Background(), agent, config)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, models.AgentTypeBuyer, result.Type, "Failed for type: "+agentType)
	}
}

func TestAgentConfigService_SellerAgentTypes(t *testing.T) {
	tmpDir := setupTestDir(t)
	log := logger.New()
	svc := services.NewAgentConfigService(tmpDir, log)

	// Test with different seller-related types
	sellerTypes := []string{"merchant", "seller", "SELLER", "Merchant"}

	for _, agentType := range sellerTypes {
		agent := &models.Agent{
			ID:   "agent-" + agentType,
			Name: "Test " + agentType,
			Type: agentType,
		}
		config := createSellerConfig()
		result, err := svc.SaveAgentConfig(context.Background(), agent, config)

		assert.NoError(t, err)
		assert.NotNil(t, result)
		assert.Equal(t, models.AgentTypeSeller, result.Type, "Failed for type: "+agentType)
	}
}
