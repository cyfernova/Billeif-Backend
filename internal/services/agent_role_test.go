package services

import (
	"testing"

	"invoice-backend/internal/models"
)

func TestNormalizeMarketplaceAgentType(t *testing.T) {
	tests := map[string]string{
		"buyer":    "shopping",
		"seller":   "merchant",
		"shopping": "shopping",
		"merchant": "merchant",
	}

	for input, expected := range tests {
		if got := NormalizeMarketplaceAgentType(input); got != expected {
			t.Fatalf("expected %s -> %s, got %s", input, expected, got)
		}
	}
}

func TestRoleConfigTypeForAgentType(t *testing.T) {
	if got := RoleConfigTypeForAgentType("shopping"); got != models.AgentTypeBuyer {
		t.Fatalf("expected shopping agents to map to buyer config, got %s", got)
	}
	if got := RoleConfigTypeForAgentType("merchant"); got != models.AgentTypeSeller {
		t.Fatalf("expected merchant agents to map to seller config, got %s", got)
	}
	if got := MarketplaceRoleForType("merchant"); got != MarketplaceRoleSeller {
		t.Fatalf("expected merchant agents to expose seller marketplace role, got %s", got)
	}
}
