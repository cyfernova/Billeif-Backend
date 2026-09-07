package postgres_test

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/postgres"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/require"
	gormpostgres "gorm.io/driver/postgres"
	"gorm.io/gorm"
)

func TestAgentConfigPersistsAcrossServiceInstances(t *testing.T) {
	dsn := os.Getenv("MIGRATION_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("MIGRATION_TEST_DATABASE_URL required")
	}
	db, err := gorm.Open(gormpostgres.Open(dsn), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	defer sqlDB.Close()
	tx := db.Begin()
	require.NoError(t, tx.Error)
	defer tx.Rollback()
	schema := fmt.Sprintf("agent_config_qa_%d", time.Now().UnixNano())
	require.NoError(t, tx.Exec("CREATE SCHEMA "+schema).Error)
	require.NoError(t, tx.Exec("SET LOCAL search_path TO "+schema).Error)
	require.NoError(t, tx.Exec("CREATE TABLE agents (id UUID PRIMARY KEY, business_id UUID NOT NULL, UNIQUE(id, business_id))").Error)
	up, err := os.ReadFile("../../../migrations/000063_agent_bargaining_configs.up.sql")
	require.NoError(t, err)
	require.NoError(t, tx.Exec(string(up)).Error)
	agent := &models.Agent{ID: "11111111-1111-4111-8111-111111111111", BusinessID: "22222222-2222-4222-8222-222222222222", Name: "Test buyer", Type: "shopping"}
	require.NoError(t, tx.Exec("INSERT INTO agents VALUES (?, ?)", agent.ID, agent.BusinessID).Error)
	ctx := context.Background()
	first := services.NewAgentConfigService(t.TempDir(), logger.New()).WithRepository(postgres.NewAgentConfigRepository(tx))
	config := first.CreateDefaultBuyerConfig()
	config.BuyerConfig.MaxRounds = 3
	_, err = first.SaveAgentConfig(ctx, agent, config)
	require.NoError(t, err)
	second := services.NewAgentConfigService(t.TempDir(), logger.New()).WithRepository(postgres.NewAgentConfigRepository(tx))
	saved, err := second.GetAgentConfig(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, 3, saved.Config.BuyerConfig.MaxRounds)
	saved.Config.BuyerConfig.MaxRounds = 4
	_, err = second.SaveAgentConfig(ctx, agent, saved.Config)
	require.NoError(t, err)
	reread, err := first.GetAgentConfig(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, 4, reread.Config.BuyerConfig.MaxRounds)
	err = postgres.NewAgentConfigRepository(tx).Save(ctx, "33333333-3333-4333-8333-333333333333", reread)
	require.ErrorContains(t, err, "business mismatch")
	unchanged, err := first.GetAgentConfig(ctx, agent.ID)
	require.NoError(t, err)
	require.Equal(t, 4, unchanged.Config.BuyerConfig.MaxRounds)
	all, err := second.GetAllAgentConfigs(ctx)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.NoError(t, second.DeleteAgentConfig(ctx, agent.ID))
	_, err = first.GetAgentConfig(ctx, agent.ID)
	require.ErrorIs(t, err, services.ErrAgentConfigNotFound)
	down, err := os.ReadFile("../../../migrations/000063_agent_bargaining_configs.down.sql")
	require.NoError(t, err)
	require.NoError(t, tx.Exec(string(down)).Error)
	require.NoError(t, tx.Exec(string(up)).Error)
}
