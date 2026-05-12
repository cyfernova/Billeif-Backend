package services

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"invoice-backend/internal/models"
	"invoice-backend/pkg/a2a"
	"invoice-backend/pkg/ap2"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"gorm.io/datatypes"
)

type sellerNegotiationRepoStub struct {
	agents      map[string]*models.Agent
	products    map[string]*models.MarketplaceProduct
	registries  map[string]*models.AgentRegistry
	reserveErr  error
	reserveCall []struct {
		productID string
		quantity  int
	}
}

func (s *sellerNegotiationRepoStub) GetAgentByID(ctx context.Context, id string) (*models.Agent, error) {
	agent, ok := s.agents[id]
	if !ok {
		return nil, ErrAgentNotFound
	}
	return agent, nil
}

func (s *sellerNegotiationRepoStub) GetMarketplaceProductByID(ctx context.Context, id string) (*models.MarketplaceProduct, error) {
	product, ok := s.products[id]
	if !ok {
		return nil, errNotFound("product")
	}
	return product, nil
}

func (s *sellerNegotiationRepoStub) GetAgentRegistry(ctx context.Context, agentID string) (*models.AgentRegistry, error) {
	registry, ok := s.registries[agentID]
	if !ok {
		return nil, errNotFound("registry")
	}
	return registry, nil
}

func (s *sellerNegotiationRepoStub) ReserveMarketplaceInventory(ctx context.Context, productID string, quantity int) error {
	if s.reserveErr != nil {
		return s.reserveErr
	}
	s.reserveCall = append(s.reserveCall, struct {
		productID string
		quantity  int
	}{productID: productID, quantity: quantity})
	return nil
}

func TestParseRegistryAgentCardMapsLegacyMerchantCapabilitiesToA2ASkills(t *testing.T) {
	legacyCard := &ap2.AgentCard{
		Name:         "Legacy Merchant",
		Description:  "Legacy seller card",
		Endpoint:     "https://seller.example/api/v1/a2a",
		Type:         "merchant",
		Capabilities: []string{"merchant.process_cart", "inventory.reserve"},
		Version:      "legacy-1",
	}

	registry := &models.AgentRegistry{
		AgentType:  "merchant",
		IsActive:   true,
		IsVerified: true,
		AgentCard:  mustJSON(t, legacyCard),
	}

	card, err := parseRegistryAgentCard(registry)
	if err != nil {
		t.Fatalf("parse legacy registry agent card: %v", err)
	}
	if !cardSupportsProcurement(card) {
		t.Fatal("expected legacy merchant card to expose procurement skills after normalization")
	}
	if endpoint := resolveRegistryA2AEndpoint(card, registry); endpoint != legacyCard.Endpoint {
		t.Fatalf("expected endpoint %q, got %q", legacyCard.Endpoint, endpoint)
	}
}

func TestResolveProcurementSellerInterfaceRefreshesFromWellKnownAgentCard(t *testing.T) {
	card, err := buildA2AAgentCardFromRegistration(&RegisterAgentRequest{
		Name:        "Seller",
		Description: "Test seller",
		A2AEndpoint: "https://fresh.example/api/v1/a2a",
		AgentType:   "merchant",
	})
	if err != nil {
		t.Fatalf("build a2a card: %v", err)
	}

	oldClient := a2aAgentCardHTTPClient
	a2aAgentCardHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		if req.URL.String() != "https://seller.example/.well-known/agent-card.json" {
			t.Fatalf("unexpected agent card URL %q", req.URL.String())
		}
		body, err := json.Marshal(card)
		if err != nil {
			t.Fatalf("encode card: %v", err)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(string(body))),
			Request:    req,
		}, nil
	})}
	defer func() { a2aAgentCardHTTPClient = oldClient }()

	registry := &models.AgentRegistry{
		AgentID:      uuid.New(),
		AgentType:    "merchant",
		IsActive:     true,
		IsVerified:   true,
		WellKnownURI: stringPtr("https://seller.example/.well-known/agent-card.json"),
		AgentCard:    datatypes.JSON([]byte(`{"name":"stale"}`)),
	}

	service := &ProcurementService{log: logger.New()}
	resolvedCard, endpoint, err := service.resolveProcurementSellerInterface(context.Background(), registry)
	if err != nil {
		t.Fatalf("resolve procurement seller interface: %v", err)
	}
	if resolvedCard == nil {
		t.Fatal("expected resolved card")
	}
	if endpoint != "https://fresh.example/api/v1/a2a" {
		t.Fatalf("expected refreshed endpoint, got %q", endpoint)
	}
	if !cardSupportsProcurement(resolvedCard) {
		t.Fatal("expected refreshed card to support procurement")
	}
}

func TestBuildA2AAgentCardFromRegistrationRejectsLocalEndpoint(t *testing.T) {
	_, err := buildA2AAgentCardFromRegistration(&RegisterAgentRequest{
		Name:        "Seller",
		Description: "Test seller",
		A2AEndpoint: "http://127.0.0.1:8080/api/v1/a2a",
		AgentType:   "merchant",
	})
	if err == nil {
		t.Fatal("expected local A2A endpoint to be rejected")
	}
}

func TestResolveRegistryA2AEndpointRejectsPrivateCardEndpoint(t *testing.T) {
	card := a2a.NewAgentCard("Seller", "private endpoint", registryAgentCardVersion)
	card.SupportedInterfaces = []a2a.AgentInterface{{
		URL:             "https://10.0.0.2/api/v1/a2a",
		ProtocolBinding: a2a.ProtocolBindingHTTPJSON,
		ProtocolVersion: a2a.SupportedVersion,
	}}

	if endpoint := resolveRegistryA2AEndpoint(card, nil); endpoint != "" {
		t.Fatalf("expected private endpoint to be filtered, got %q", endpoint)
	}
}

func TestFetchA2AAgentCardRejectsPrivateWellKnownURLWithoutRequest(t *testing.T) {
	oldClient := a2aAgentCardHTTPClient
	a2aAgentCardHTTPClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
		t.Fatalf("HTTP client should not be called for private well-known URL %q", req.URL.String())
		return nil, nil
	})}
	defer func() { a2aAgentCardHTTPClient = oldClient }()

	_, err := fetchA2AAgentCard(context.Background(), "https://169.254.169.254/.well-known/agent-card.json")
	if err == nil {
		t.Fatal("expected private well-known URL to be rejected")
	}
}

func TestNegotiationEvaluationHonorsExplicitRequestMaxRounds(t *testing.T) {
	service := &ProcurementService{}

	buyerCfg := &models.AgentConfig{
		Type: models.AgentTypeBuyer,
		BuyerConfig: &models.BuyerConfig{
			MaxDiscountPercent: 20,
			MinDiscountPercent: 5,
			TargetDiscount:     10,
			MaxRounds:          5,
		},
	}
	sellerCfg := &models.AgentConfig{
		Type: models.AgentTypeSeller,
		SellerConfig: &models.SellerConfig{
			MinAcceptablePrice: 85,
			MaxRounds:          5,
		},
	}

	eval := service.newNegotiationEvaluation(
		&CreateProcurementRunRequest{
			Quantity:   1,
			MaxBudget:  98,
			MaxRounds:  2,
			Currency:   "INR",
			AutoBuy:    true,
			MaxSellers: 5,
		},
		&procurementCandidateContext{
			product:      &models.MarketplaceProduct{Price: 100},
			sellerConfig: sellerCfg,
		},
		buyerCfg,
		100,
	)

	if eval.cycles != 2 {
		t.Fatalf("expected 2 negotiation cycles, got %d", eval.cycles)
	}
	if eval.negotiationMaxRounds != 4 {
		t.Fatalf("expected 4 total bargaining actions, got %d", eval.negotiationMaxRounds)
	}
}

func TestSellerNegotiationServiceReturnsQuoteAndAcceptsWithinFloor(t *testing.T) {
	log := logger.New()
	agentConfig := NewAgentConfigService(t.TempDir(), log)

	sellerID := "11111111-1111-1111-1111-111111111111"
	productID := "22222222-2222-2222-2222-222222222222"
	endpoint := "https://seller.example/api/v1/a2a"
	seller := &models.Agent{
		ID:          sellerID,
		OwnerID:     "33333333-3333-3333-3333-333333333333",
		BusinessID:  "33333333-3333-3333-3333-333333333333",
		Name:        "Seller",
		Type:        "merchant",
		IsActive:    true,
		A2AEndpoint: &endpoint,
		CreatedAt:   time.Now().UTC(),
		UpdatedAt:   time.Now().UTC(),
	}

	config := agentConfig.CreateDefaultSellerConfig()
	config.SellerConfig.MinAcceptablePrice = 90
	config.SellerConfig.MaxRounds = 4
	config.SellerConfig.PaymentTerms = []string{"net30"}
	config.SellerConfig.VolumeDiscountTiers = []models.DiscountTier{
		{MinQuantity: 1, DiscountPercent: 10},
	}
	if _, err := agentConfig.SaveAgentConfig(context.Background(), seller, config); err != nil {
		t.Fatalf("save seller config: %v", err)
	}

	registryCard, err := buildA2AAgentCardFromRegistration(&RegisterAgentRequest{
		Name:        "Seller",
		Description: "Negotiating merchant",
		A2AEndpoint: endpoint,
		AgentType:   "merchant",
	})
	if err != nil {
		t.Fatalf("build registry card: %v", err)
	}

	repo := &sellerNegotiationRepoStub{
		agents: map[string]*models.Agent{
			sellerID: seller,
		},
		products: map[string]*models.MarketplaceProduct{
			productID: {
				ID:                     productID,
				AgentID:                sellerID,
				Name:                   "Widget",
				Price:                  100,
				Currency:               "INR",
				IsAvailable:            true,
				InventoryCount:         10,
				ReservedInventoryCount: 1,
			},
		},
		registries: map[string]*models.AgentRegistry{
			sellerID: {
				AgentID:      uuid.MustParse(sellerID),
				AgentType:    "merchant",
				IsActive:     true,
				IsVerified:   true,
				A2AEndpoint:  &endpoint,
				AgentCard:    mustJSON(t, registryCard),
				WellKnownURI: stringPtr("https://seller.example/.well-known/agent-card.json"),
			},
		},
	}

	service := NewSellerNegotiationService(repo, agentConfig, log)

	quoteMetadata := map[string]interface{}{
		"procurementRunId": "run-1",
		"candidateId":      "candidate-1",
		"buyerAgentId":     "44444444-4444-4444-4444-444444444444",
		"sellerAgentId":    sellerID,
		"productId":        productID,
		"quantity":         1,
		"currency":         "INR",
		"paymentTerms":     []string{"net30"},
		"roundNumber":      1,
		"proposedAmount":   80.0,
		"previousAmount":   100.0,
		"maxBudget":        100.0,
		"maxRounds":        4,
		"deadlineAt":       time.Now().UTC().Add(time.Minute),
	}

	_, quoteArtifact, err := service.HandleTask(context.Background(), taskTypeProcurementQuoteRequest, quoteMetadata)
	if err != nil {
		t.Fatalf("handle quote request: %v", err)
	}
	quotePayload := artifactPayload(t, quoteArtifact)
	if got := quotePayload["status"]; got != procurementResponseQuote {
		t.Fatalf("expected quote response, got %v", got)
	}
	gotQuantity, err := interfaceToInt(quotePayload["availableQuantity"])
	if err != nil {
		t.Fatalf("parse available quantity: %v", err)
	}
	if got := gotQuantity; got != 9 {
		t.Fatalf("expected available quantity 9, got %d", got)
	}

	acceptMetadata := map[string]interface{}{
		"procurementRunId": "run-1",
		"candidateId":      "candidate-1",
		"buyerAgentId":     "44444444-4444-4444-4444-444444444444",
		"sellerAgentId":    sellerID,
		"productId":        productID,
		"quantity":         1,
		"currency":         "INR",
		"paymentTerms":     []string{"net30"},
		"roundNumber":      2,
		"proposedAmount":   95.0,
		"previousAmount":   quotePayload["amount"],
		"maxBudget":        100.0,
		"maxRounds":        4,
		"deadlineAt":       time.Now().UTC().Add(time.Minute),
	}

	_, acceptArtifact, err := service.HandleTask(context.Background(), taskTypeProcurementNegotiationAccept, acceptMetadata)
	if err != nil {
		t.Fatalf("handle accept request: %v", err)
	}
	acceptPayload := artifactPayload(t, acceptArtifact)
	if got := acceptPayload["status"]; got != procurementResponseAccept {
		t.Fatalf("expected accept response, got %v", got)
	}
	if len(repo.reserveCall) != 1 {
		t.Fatalf("expected one inventory reservation, got %d", len(repo.reserveCall))
	}
	if repo.reserveCall[0].productID != productID || repo.reserveCall[0].quantity != 1 {
		t.Fatalf("unexpected reservation %+v", repo.reserveCall[0])
	}
}

func mustJSON(t *testing.T, value interface{}) datatypes.JSON {
	t.Helper()

	data, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("marshal json: %v", err)
	}
	return datatypes.JSON(data)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func artifactPayload(t *testing.T, artifact *a2a.Artifact) map[string]interface{} {
	t.Helper()

	if artifact == nil || len(artifact.Parts) == 0 || len(artifact.Parts[0].Data) == 0 {
		t.Fatal("expected response artifact payload")
	}
	return artifact.Parts[0].Data
}

func errNotFound(entity string) error {
	return &notFoundError{entity: entity}
}

type notFoundError struct {
	entity string
}

func (e *notFoundError) Error() string {
	return e.entity + " not found"
}
