package services

import (
	"context"
	"invoice-backend/internal/models"
	"testing"

	"github.com/stretchr/testify/require"
)

type preferenceFixture map[string]*models.WellKnownAgentConfig

func (p preferenceFixture) GetAgentConfig(_ context.Context, id string) (*models.WellKnownAgentConfig, error) {
	if value, ok := p[id]; ok {
		return value, nil
	}
	return nil, ErrAgentConfigNotFound
}

func TestNegotiationUsesLowerParticipantRoundLimit(t *testing.T) {
	a := (&A2AGovernanceAdapter{}).WithPreferences(preferenceFixture{
		"buyer":  {Config: &models.AgentConfig{Type: models.AgentTypeBuyer, BuyerConfig: &models.BuyerConfig{MaxRounds: 4}}},
		"seller": {Config: &models.AgentConfig{Type: models.AgentTypeSeller, SellerConfig: &models.SellerConfig{MaxRounds: 2}}},
	})
	rounds, err := a.negotiationRoundLimit(context.Background(), "buyer", "seller", 1200, 5)
	require.NoError(t, err)
	require.Equal(t, 2, rounds)
	rounds, err = a.negotiationRoundLimit(context.Background(), "missing", "missing", 1200, 5)
	require.NoError(t, err)
	require.Equal(t, 5, rounds)
}

func TestBargainingPreferencesConstrainOffersAndAcceptance(t *testing.T) {
	n := &models.BargainingNegotiation{InitialAmount: 1200, CurrentAmount: 1100, MaxRounds: 5}
	budget := 1000.0
	buyer := &models.WellKnownAgentConfig{Config: &models.AgentConfig{Type: models.AgentTypeBuyer, BuyerConfig: &models.BuyerConfig{MaxRounds: 3, BudgetLimit: &budget, TargetDiscount: 15}}}
	limits, err := resolveBargainingPreferenceLimits(n, "buyer", buyer)
	require.NoError(t, err)
	require.Equal(t, 3, limits.MaxRounds)
	require.Equal(t, 1000.0, limits.Maximum)
	require.Equal(t, 15.0, limits.TargetDiscount)
	require.True(t, limits.allows(&LLMBargainingResponse{Action: "counteroffer", ProposedAmount: 1000}))
	require.False(t, limits.allows(&LLMBargainingResponse{Action: "accept", ProposedAmount: 1100}))
	require.False(t, limits.allows(&LLMBargainingResponse{Action: "counteroffer", ProposedAmount: 1001}))
	require.True(t, limits.allows(&LLMBargainingResponse{Action: "reject", ProposedAmount: 1100}))
	seller := &models.WellKnownAgentConfig{Config: &models.AgentConfig{Type: models.AgentTypeSeller, SellerConfig: &models.SellerConfig{MaxRounds: 20, MinAcceptablePrice: 1150}}}
	limits, err = resolveBargainingPreferenceLimits(n, "seller", seller)
	require.NoError(t, err)
	require.Equal(t, 5, limits.MaxRounds)
	require.Equal(t, 1150.0, limits.Minimum)
	require.False(t, limits.allows(&LLMBargainingResponse{Action: "accept", ProposedAmount: 1100}))
	require.True(t, limits.allows(&LLMBargainingResponse{Action: "counteroffer", ProposedAmount: 1150}))
	seller.Config.SellerConfig.MinAcceptablePrice = 1300
	_, err = resolveBargainingPreferenceLimits(n, "seller", seller)
	require.Error(t, err)
	_, err = resolveBargainingPreferenceLimits(n, "buyer", seller)
	require.Error(t, err)
	defaults, err := resolveBargainingPreferenceLimits(n, "buyer", nil)
	require.NoError(t, err)
	require.Equal(t, 1100.0, defaults.Maximum)
	require.Equal(t, 5, defaults.MaxRounds)
}
