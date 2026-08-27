package handlers

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

type discoveryRegistrationRepository struct {
	interfaces.AP2Repository
	agents     map[string]*models.Agent
	registries []*models.AgentRegistry
	lookupErr  error
}

func (r *discoveryRegistrationRepository) GetAgentByID(_ context.Context, id string) (*models.Agent, error) {
	if r.lookupErr != nil {
		return nil, r.lookupErr
	}
	agent, ok := r.agents[id]
	if !ok {
		return nil, interfaces.ErrAgentNotFound
	}
	return agent, nil
}

func TestAgentDiscoveryRegistrationReturnsInternalServerErrorForRepositoryFailure(t *testing.T) {
	gin.SetMode(gin.TestMode)
	agentID := uuid.New().String()
	repo := &discoveryRegistrationRepository{lookupErr: errors.New("database unavailable")}
	handler := newAgentDiscoveryRegistrationHandler(repo)

	for _, tt := range []struct {
		name string
		path string
		body string
		call gin.HandlerFunc
	}{
		{
			name: "direct registration",
			path: "/discovery/agents/register",
			body: `{"agent_id":"` + agentID + `","name":"merchant","agent_type":"merchant","a2a_endpoint":"https://agent.example.com/a2a"}`,
			call: handler.RegisterAgent,
		},
		{
			name: "agents table registration",
			path: "/discovery/agents/register-from-agents?agent_id=" + agentID,
			call: handler.RegisterAgentFromAgents,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recorder := invokeAgentDiscoveryRegistration(t, tt.path, tt.body, tt.call)
			if recorder.Code != http.StatusInternalServerError {
				t.Fatalf("expected status %d, got %d: %s", http.StatusInternalServerError, recorder.Code, recorder.Body.String())
			}
		})
	}

	if len(repo.registries) != 0 {
		t.Fatalf("repository failure published %d records", len(repo.registries))
	}
}

func (r *discoveryRegistrationRepository) RegisterAgent(_ context.Context, registry *models.AgentRegistry) error {
	r.registries = append(r.registries, registry)
	return nil
}

func (r *discoveryRegistrationRepository) CreateDiscoveryAudit(_ context.Context, _ *models.AgentDiscoveryAudit) error {
	return nil
}

func TestAgentDiscoveryRegistrationForwardsEffectiveBusinessScope(t *testing.T) {
	gin.SetMode(gin.TestMode)
	agentID := uuid.New().String()
	repo := &discoveryRegistrationRepository{agents: map[string]*models.Agent{
		agentID: {ID: agentID, OwnerID: "other-user", BusinessID: "validated-business", Name: "merchant", Type: "merchant"},
	}}
	handler := newAgentDiscoveryRegistrationHandler(repo)

	for _, tt := range []struct {
		name   string
		path   string
		body   string
		call   gin.HandlerFunc
		status int
	}{
		{
			name:   "direct registration",
			path:   "/discovery/agents/register",
			body:   `{"agent_id":"` + agentID + `","name":"merchant","agent_type":"merchant","a2a_endpoint":"https://agent.example.com/a2a"}`,
			call:   handler.RegisterAgent,
			status: http.StatusCreated,
		},
		{
			name:   "agents table registration",
			path:   "/discovery/agents/register-from-agents?agent_id=" + agentID,
			call:   handler.RegisterAgentFromAgents,
			status: http.StatusOK,
		},
	} {
		t.Run(tt.name, func(t *testing.T) {
			recorder := invokeAgentDiscoveryRegistration(t, tt.path, tt.body, tt.call)
			if recorder.Code != tt.status {
				t.Fatalf("expected status %d, got %d: %s", tt.status, recorder.Code, recorder.Body.String())
			}
		})
	}

	if len(repo.registries) != 2 {
		t.Fatalf("expected both registration paths to publish, got %d records", len(repo.registries))
	}
	for _, registry := range repo.registries {
		if registry.AgentID.String() != agentID {
			t.Fatalf("registry is not bound to source agent %s: %s", agentID, registry.AgentID)
		}
	}
}

func TestAgentDiscoveryRegistrationHidesForeignAndMissingAgents(t *testing.T) {
	gin.SetMode(gin.TestMode)
	foreignAgentID := uuid.New().String()
	repo := &discoveryRegistrationRepository{agents: map[string]*models.Agent{
		foreignAgentID: {ID: foreignAgentID, OwnerID: "other-user", BusinessID: "other-business", Name: "foreign", Type: "shopping"},
	}}
	handler := newAgentDiscoveryRegistrationHandler(repo)

	foreign := invokeAgentDiscoveryRegistration(t, "/discovery/agents/register-from-agents?agent_id="+foreignAgentID, "", handler.RegisterAgentFromAgents)
	missing := invokeAgentDiscoveryRegistration(t, "/discovery/agents/register-from-agents?agent_id="+uuid.New().String(), "", handler.RegisterAgentFromAgents)
	if foreign.Code != http.StatusNotFound || missing.Code != http.StatusNotFound {
		t.Fatalf("foreign and missing agents must both return 404, got %d and %d", foreign.Code, missing.Code)
	}
	if foreign.Body.String() != missing.Body.String() {
		t.Fatalf("foreign and missing agent responses differ: %q vs %q", foreign.Body.String(), missing.Body.String())
	}
	if len(repo.registries) != 0 {
		t.Fatalf("foreign or missing agents were published: %d records", len(repo.registries))
	}
}

func newAgentDiscoveryRegistrationHandler(repo interfaces.AP2Repository) *AgentDiscoveryHandler {
	return NewAgentDiscoveryHandler(services.NewAgentDiscoveryService(repo, nil, nil, logger.New()), logger.New())
}

func invokeAgentDiscoveryRegistration(t *testing.T, path, body string, handler gin.HandlerFunc) *httptest.ResponseRecorder {
	t.Helper()
	recorder := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, path, strings.NewReader(body))
	if body != "" {
		request.Header.Set("Content-Type", "application/json")
	}
	ctx, _ := gin.CreateTestContext(recorder)
	ctx.Request = request
	ctx.Set("user_id", "user-1")
	ctx.Set("business_id", "unvalidated-business")
	ctx.Set("validated_business_id", "validated-business")
	handler(ctx)
	return recorder
}
