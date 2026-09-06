package services

import (
	"context"
	"invoice-backend/internal/models"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestGovernedBargainingRejectsInvalidModelOutputWithoutFallback(t *testing.T) {
	for _, raw := range []string{`not JSON`, `{"action":"purchase","proposed_amount":100}`, `{"action":"counteroffer","proposed_amount":-1}`, `{"action":"counteroffer","proposed_amount":1201}`, `{"action":"counteroffer","proposed_amount":1e999}`, "```json\n{}\n```"} {
		decision, err := parseGovernedBargainingDecision(raw, 1200, 1100)
		require.Error(t, err, raw)
		require.Nil(t, decision)
	}
	decision, err := parseGovernedBargainingDecision(`{"action":"counteroffer","proposed_amount":1050,"reason":"Offer based on current price"}`, 1200, 1100)
	require.NoError(t, err)
	require.Equal(t, 1050.0, decision.ProposedAmount)
	accepted, err := parseGovernedBargainingDecision(`{"action":"accept","proposed_amount":1150}`, 1200, 1100)
	require.NoError(t, err)
	require.Equal(t, 1100.0, accepted.ProposedAmount)
}

func TestGovernedBargainingUnconfiguredAdapterDeniesExecution(t *testing.T) {
	var adapter *A2AGovernanceAdapter
	require.False(t, adapter.Ready(context.Background(), "business"))
	_, err := adapter.Decide(context.Background(), nil, "agent", "buyer", 1)
	require.ErrorIs(t, err, ErrA2AGovernanceRequired)
}

func TestGovernedNegotiationsDoNotDispatchLegacyExternalNotifications(t *testing.T) {
	svc := &BargainingService{}
	require.True(t, svc.skipA2ANotifications(&models.BargainingNegotiation{Metadata: `{"governed_internal_negotiation":true}`}))
	require.True(t, svc.skipA2ANotifications(&models.BargainingNegotiation{Metadata: `{"procurement_managed_a2a":true}`}))
	require.False(t, svc.skipA2ANotifications(&models.BargainingNegotiation{Metadata: `{}`}))
}
