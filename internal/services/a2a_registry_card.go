package services

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/ap2"
)

const (
	taskTypeProcurementQuoteRequest       = "procurement.quote.request"
	taskTypeProcurementNegotiationCounter = "procurement.negotiation.counteroffer"
	taskTypeProcurementNegotiationAccept  = "procurement.negotiation.accept"
	taskTypeProcurementNegotiationReject  = "procurement.negotiation.reject"
	taskTypeMerchantProcessCart           = "merchant.process_cart"
	skillIDShoppingProcurement            = "shopping.procurement"
	registryAgentCardVersion              = "1.0.0"
)

var merchantProcurementSkills = []string{
	taskTypeProcurementQuoteRequest,
	taskTypeProcurementNegotiationCounter,
	taskTypeProcurementNegotiationAccept,
	taskTypeProcurementNegotiationReject,
	taskTypeMerchantProcessCart,
}

func buildA2AAgentCardFromRegistration(req *RegisterAgentRequest) (*a2a.AgentCard, error) {
	if strings.TrimSpace(req.A2AEndpoint) == "" {
		return nil, fmt.Errorf("a2a endpoint is required")
	}

	card := a2a.NewAgentCard(req.Name, req.Description, registryAgentCardVersion)
	card.Provider = &a2a.AgentProvider{
		Organization: req.Name,
		URL:          strings.TrimSpace(req.A2AEndpoint),
	}
	card.SupportedInterfaces = []a2a.AgentInterface{
		{
			URL:             strings.TrimSpace(req.A2AEndpoint),
			ProtocolBinding: a2a.ProtocolBindingHTTPJSON,
			ProtocolVersion: a2a.SupportedVersion,
		},
	}
	card.Capabilities = a2a.AgentCapabilities{
		Streaming:         true,
		PushNotifications: true,
	}
	card.DefaultInputModes = []string{"text/plain", "application/json"}
	card.DefaultOutputModes = []string{"text/plain", "application/json"}
	card.Skills = buildAgentSkills(req.AgentType, req.Capabilities)
	if len(card.Skills) == 0 {
		return nil, fmt.Errorf("agent card must expose at least one skill")
	}
	return card, nil
}

func buildAgentSkills(agentType string, capabilities []string) []a2a.AgentSkill {
	skillIDs := make([]string, 0, len(capabilities)+4)
	skillIDs = append(skillIDs, capabilities...)

	switch NormalizeMarketplaceAgentType(agentType) {
	case "shopping":
		skillIDs = append(skillIDs, skillIDShoppingProcurement)
	case "merchant":
		skillIDs = append(skillIDs, merchantProcurementSkills...)
	}

	seen := make(map[string]struct{}, len(skillIDs))
	skills := make([]a2a.AgentSkill, 0, len(skillIDs))
	for _, skillID := range skillIDs {
		skillID = strings.TrimSpace(skillID)
		if skillID == "" {
			continue
		}
		if _, ok := seen[skillID]; ok {
			continue
		}
		seen[skillID] = struct{}{}
		skills = append(skills, agentSkillDefinition(skillID))
	}
	return skills
}

func agentSkillDefinition(skillID string) a2a.AgentSkill {
	skill := a2a.AgentSkill{
		ID:          skillID,
		Name:        skillID,
		Description: "Agent skill: " + skillID,
		Tags:        []string{"marketplace"},
		InputModes:  []string{"text/plain", "application/json"},
		OutputModes: []string{"text/plain", "application/json"},
	}

	switch skillID {
	case skillIDShoppingProcurement:
		skill.Name = "Shopping Procurement"
		skill.Description = "Match listings, negotiate with seller agents, and execute procurement runs."
		skill.Tags = []string{"shopping", "procurement", "marketplace"}
	case taskTypeProcurementQuoteRequest:
		skill.Name = "Procurement Quote Request"
		skill.Description = "Review a buyer procurement request and return a quote, counteroffer, acceptance, rejection, or availability response."
		skill.Tags = []string{"merchant", "procurement", "quote"}
	case taskTypeProcurementNegotiationCounter:
		skill.Name = "Procurement Counteroffer"
		skill.Description = "Process a buyer counteroffer for an in-progress procurement negotiation."
		skill.Tags = []string{"merchant", "procurement", "negotiation"}
	case taskTypeProcurementNegotiationAccept:
		skill.Name = "Procurement Accept"
		skill.Description = "Finalize acceptance of a procurement negotiation and reserve inventory."
		skill.Tags = []string{"merchant", "procurement", "negotiation"}
	case taskTypeProcurementNegotiationReject:
		skill.Name = "Procurement Reject"
		skill.Description = "Acknowledge buyer rejection of a procurement negotiation."
		skill.Tags = []string{"merchant", "procurement", "negotiation"}
	case taskTypeMerchantProcessCart:
		skill.Name = "Merchant Process Cart"
		skill.Description = "Review and sign cart mandates for merchant-side checkout handoff."
		skill.Tags = []string{"merchant", "checkout", "cart"}
	default:
		skill.Tags = []string{"marketplace", strings.ReplaceAll(skillID, ".", "_")}
	}

	return skill
}

func parseRegistryAgentCard(registry *models.AgentRegistry) (*a2a.AgentCard, error) {
	if registry == nil || len(registry.AgentCard) == 0 {
		return nil, fmt.Errorf("agent card is missing")
	}

	var modern a2a.AgentCard
	if err := json.Unmarshal(registry.AgentCard, &modern); err == nil && modern.Name != "" {
		card := normalizeParsedAgentCard(&modern, registry)
		if len(card.SupportedInterfaces) > 0 || len(card.Skills) > 0 {
			return card, nil
		}
	}

	var legacy ap2.AgentCard
	if err := json.Unmarshal(registry.AgentCard, &legacy); err == nil && legacy.Name != "" {
		return convertLegacyAgentCard(&legacy, registry), nil
	}

	var legacyModel models.AgentCard
	if err := json.Unmarshal(registry.AgentCard, &legacyModel); err == nil && legacyModel.Name != "" {
		return convertModelAgentCard(&legacyModel, registry), nil
	}

	return nil, fmt.Errorf("unsupported agent card format")
}

func normalizeParsedAgentCard(card *a2a.AgentCard, registry *models.AgentRegistry) *a2a.AgentCard {
	if card == nil {
		return nil
	}

	clone := *card
	if len(card.SupportedInterfaces) > 0 {
		clone.SupportedInterfaces = append([]a2a.AgentInterface(nil), card.SupportedInterfaces...)
	}
	if len(card.DefaultInputModes) == 0 {
		clone.DefaultInputModes = []string{"text/plain", "application/json"}
	}
	if len(card.DefaultOutputModes) == 0 {
		clone.DefaultOutputModes = []string{"text/plain", "application/json"}
	}
	if len(card.Skills) == 0 && registry != nil {
		clone.Skills = buildAgentSkills(registry.AgentType, registry.Capabilities)
	}
	if len(clone.SupportedInterfaces) == 0 && registry != nil && registry.A2AEndpoint != nil && strings.TrimSpace(*registry.A2AEndpoint) != "" {
		clone.SupportedInterfaces = []a2a.AgentInterface{
			{
				URL:             strings.TrimSpace(*registry.A2AEndpoint),
				ProtocolBinding: a2a.ProtocolBindingHTTPJSON,
				ProtocolVersion: a2a.SupportedVersion,
			},
		}
	}
	if strings.TrimSpace(clone.Version) == "" {
		clone.Version = registryAgentCardVersion
	}
	return &clone
}

func convertLegacyAgentCard(card *ap2.AgentCard, registry *models.AgentRegistry) *a2a.AgentCard {
	converted := a2a.NewAgentCard(card.Name, card.Description, card.Version)
	converted.Provider = &a2a.AgentProvider{
		Organization: card.Name,
		URL:          strings.TrimSpace(card.Endpoint),
	}
	if strings.TrimSpace(card.Endpoint) != "" {
		converted.SupportedInterfaces = []a2a.AgentInterface{
			{
				URL:             strings.TrimSpace(card.Endpoint),
				ProtocolBinding: a2a.ProtocolBindingHTTPJSON,
				ProtocolVersion: a2a.SupportedVersion,
			},
		}
	}
	converted.Capabilities = a2a.AgentCapabilities{
		Streaming:         true,
		PushNotifications: true,
	}
	converted.DefaultInputModes = []string{"text/plain", "application/json"}
	converted.DefaultOutputModes = []string{"text/plain", "application/json"}
	converted.Skills = buildAgentSkills(card.Type, card.Capabilities)
	return normalizeParsedAgentCard(converted, registry)
}

func convertModelAgentCard(card *models.AgentCard, registry *models.AgentRegistry) *a2a.AgentCard {
	converted := a2a.NewAgentCard(card.Name, card.Description, card.Version)
	converted.Provider = &a2a.AgentProvider{
		Organization: card.Name,
		URL:          strings.TrimSpace(card.Endpoint),
	}
	if strings.TrimSpace(card.Endpoint) != "" {
		converted.SupportedInterfaces = []a2a.AgentInterface{
			{
				URL:             strings.TrimSpace(card.Endpoint),
				ProtocolBinding: a2a.ProtocolBindingHTTPJSON,
				ProtocolVersion: a2a.SupportedVersion,
			},
		}
	}
	converted.Capabilities = a2a.AgentCapabilities{
		Streaming:         true,
		PushNotifications: true,
	}
	converted.DefaultInputModes = []string{"text/plain", "application/json"}
	converted.DefaultOutputModes = []string{"text/plain", "application/json"}
	converted.Skills = buildAgentSkills(card.Type, card.Capabilities)
	return normalizeParsedAgentCard(converted, registry)
}

func resolveRegistryA2AEndpoint(card *a2a.AgentCard, registry *models.AgentRegistry) string {
	if card != nil {
		for _, iface := range card.SupportedInterfaces {
			if strings.TrimSpace(iface.URL) == "" {
				continue
			}
			if iface.ProtocolBinding == "" || iface.ProtocolBinding == a2a.ProtocolBindingHTTPJSON || iface.ProtocolBinding == a2a.ProtocolBindingJSONRPC {
				return strings.TrimSpace(iface.URL)
			}
		}
	}
	if registry != nil && registry.A2AEndpoint != nil {
		return strings.TrimSpace(*registry.A2AEndpoint)
	}
	return ""
}

func cardSupportsProcurement(card *a2a.AgentCard) bool {
	if card == nil {
		return false
	}

	required := map[string]struct{}{
		taskTypeProcurementQuoteRequest:       {},
		taskTypeProcurementNegotiationCounter: {},
		taskTypeProcurementNegotiationAccept:  {},
		taskTypeProcurementNegotiationReject:  {},
	}

	for _, skill := range card.Skills {
		if _, ok := required[strings.TrimSpace(skill.ID)]; ok {
			delete(required, strings.TrimSpace(skill.ID))
		}
	}

	return len(required) == 0
}

func fetchA2AAgentCard(ctx context.Context, wellKnownURI string) (*a2a.AgentCard, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimSpace(wellKnownURI), nil)
	if err != nil {
		return nil, fmt.Errorf("build agent card request: %w", err)
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch agent card: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, fmt.Errorf("fetch agent card: unexpected status %d", resp.StatusCode)
	}

	var card a2a.AgentCard
	if err := json.NewDecoder(resp.Body).Decode(&card); err == nil && card.Name != "" {
		return normalizeParsedAgentCard(&card, nil), nil
	}

	return nil, fmt.Errorf("decode agent card: unsupported format")
}
