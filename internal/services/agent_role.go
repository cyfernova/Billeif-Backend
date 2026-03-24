package services

import (
	"strings"

	"invoice-backend/internal/models"
)

const (
	MarketplaceRoleBuyer  = "buyer"
	MarketplaceRoleSeller = "seller"
)

func NormalizeMarketplaceAgentType(agentType string) string {
	switch strings.TrimSpace(strings.ToLower(agentType)) {
	case MarketplaceRoleBuyer:
		return "shopping"
	case MarketplaceRoleSeller:
		return "merchant"
	default:
		return strings.TrimSpace(strings.ToLower(agentType))
	}
}

func MarketplaceRoleForType(agentType string) string {
	switch NormalizeMarketplaceAgentType(agentType) {
	case "shopping":
		return MarketplaceRoleBuyer
	case "merchant":
		return MarketplaceRoleSeller
	default:
		return ""
	}
}

func RoleConfigTypeForAgentType(agentType string) models.AgentType {
	switch NormalizeMarketplaceAgentType(agentType) {
	case "shopping":
		return models.AgentTypeBuyer
	case "merchant":
		return models.AgentTypeSeller
	default:
		return ""
	}
}

func ApplyMarketplaceRoleToAgent(agent *models.Agent) {
	if agent == nil {
		return
	}
	agent.MarketplaceRole = MarketplaceRoleForType(agent.Type)
}

func ApplyMarketplaceRoleToAgents(agents []*models.Agent) {
	for _, agent := range agents {
		ApplyMarketplaceRoleToAgent(agent)
	}
}

func ApplyMarketplaceRoleToRegistry(registry *models.AgentRegistry) {
	if registry == nil {
		return
	}
	registry.MarketplaceRole = MarketplaceRoleForType(registry.AgentType)
}

func ApplyMarketplaceRoleToRegistries(registries []*models.AgentRegistry) {
	for _, registry := range registries {
		ApplyMarketplaceRoleToRegistry(registry)
	}
}
