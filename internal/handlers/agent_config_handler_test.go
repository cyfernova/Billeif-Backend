package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"invoice-backend/internal/models"
	"invoice-backend/internal/repositories/interfaces"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/gin-gonic/gin"
	"go.uber.org/zap"
)

type agentConfigAP2Repo struct {
	interfaces.AP2Repository
	agents map[string]*models.Agent
}

func (r *agentConfigAP2Repo) GetAgentByID(ctx context.Context, id string) (*models.Agent, error) {
	agent, ok := r.agents[id]
	if !ok {
		return nil, errors.New("agent not found")
	}
	return agent, nil
}

func TestAgentConfigRequiresOwnedAgent(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tests := []struct {
		name      string
		method    string
		path      string
		agentID   string
		body      map[string]interface{}
		call      func(*AgentConfigHandler, *gin.Context)
		assertion func(*testing.T, *services.AgentConfigService, string)
	}{
		{
			name:    "get foreign config",
			method:  http.MethodGet,
			path:    "/agents/config/foreign-agent",
			agentID: "foreign-agent",
			call:    (*AgentConfigHandler).GetAgentConfig,
			assertion: func(t *testing.T, configService *services.AgentConfigService, agentID string) {
				if _, err := configService.GetAgentConfig(context.Background(), agentID); err != nil {
					t.Fatalf("foreign config should remain readable to service: %v", err)
				}
			},
		},
		{
			name:    "update foreign config",
			method:  http.MethodPut,
			path:    "/agents/config/foreign-agent",
			agentID: "foreign-agent",
			body: map[string]interface{}{
				"volatility": 0.9,
			},
			call: (*AgentConfigHandler).UpdateAgentConfig,
			assertion: func(t *testing.T, configService *services.AgentConfigService, agentID string) {
				config, err := configService.GetAgentConfig(context.Background(), agentID)
				if err != nil {
					t.Fatalf("foreign config should not be removed: %v", err)
				}
				if config.Config.Volatility == 0.9 {
					t.Fatal("foreign config was updated despite failed authorization")
				}
			},
		},
		{
			name:    "delete foreign config",
			method:  http.MethodDelete,
			path:    "/agents/config/foreign-agent",
			agentID: "foreign-agent",
			call:    (*AgentConfigHandler).DeleteAgentConfig,
			assertion: func(t *testing.T, configService *services.AgentConfigService, agentID string) {
				if _, err := configService.GetAgentConfig(context.Background(), agentID); err != nil {
					t.Fatalf("foreign config should not be deleted: %v", err)
				}
			},
		},
		{
			name:    "create foreign config",
			method:  http.MethodPost,
			path:    "/agents/config",
			agentID: "foreign-agent",
			body: map[string]interface{}{
				"agent_id": "foreign-agent",
				"config": map[string]interface{}{
					"type":       "buyer",
					"volatility": 0.2,
					"buyer_config": map[string]interface{}{
						"max_discount_percent": 25,
						"min_discount_percent": 5,
						"target_discount":      15,
						"risk_tolerance":       0.5,
						"patience_level":       0.5,
						"max_rounds":           5,
						"preferred_products":   []string{},
						"blacklisted_vendors":  []string{},
						"payment_terms":        []string{"net30"},
						"acceptance_threshold": 0.85,
					},
				},
			},
			call: (*AgentConfigHandler).CreateAgentConfig,
			assertion: func(t *testing.T, configService *services.AgentConfigService, agentID string) {
				config, err := configService.GetAgentConfig(context.Background(), agentID)
				if err != nil {
					t.Fatalf("foreign config should not be removed: %v", err)
				}
				if config.Config.Volatility == 0.2 {
					t.Fatal("foreign config was overwritten despite failed authorization")
				}
			},
		},
		{
			name:    "create foreign default config",
			method:  http.MethodPost,
			path:    "/agents/config/default",
			agentID: "foreign-agent",
			body: map[string]interface{}{
				"agent_id": "foreign-agent",
				"type":     "shopping",
			},
			call: (*AgentConfigHandler).CreateDefaultConfig,
			assertion: func(t *testing.T, configService *services.AgentConfigService, agentID string) {
				config, err := configService.GetAgentConfig(context.Background(), agentID)
				if err != nil {
					t.Fatalf("foreign config should not be removed: %v", err)
				}
				if config.Config.Volatility != 0.5 {
					t.Fatal("foreign default config was recreated despite failed authorization")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			handler, configService := newAgentConfigTestHandler(t, map[string]*models.Agent{
				"foreign-agent": testAgent("foreign-agent", "other-user", "other-business"),
			})
			saveDefaultConfig(t, configService, testAgent("foreign-agent", "other-user", "other-business"))

			recorder := invokeAgentConfigHandler(t, tt.method, tt.path, tt.body, "user-1", "business-1", tt.agentID, func(c *gin.Context) {
				tt.call(handler, c)
			})

			if recorder.Code != http.StatusNotFound {
				t.Fatalf("expected status %d, got %d. Body: %s", http.StatusNotFound, recorder.Code, recorder.Body.String())
			}

			tt.assertion(t, configService, tt.agentID)
		})
	}
}

func TestGetAllAgentConfigsFiltersToVisibleAgents(t *testing.T) {
	gin.SetMode(gin.TestMode)

	ownedAgent := testAgent("owned-agent", "user-1", "owned-business")
	businessAgent := testAgent("business-agent", "other-user", "business-1")
	foreignAgent := testAgent("foreign-agent", "other-user", "other-business")
	missingAgent := testAgent("missing-agent", "other-user", "other-business")

	handler, configService := newAgentConfigTestHandler(t, map[string]*models.Agent{
		ownedAgent.ID:    ownedAgent,
		businessAgent.ID: businessAgent,
		foreignAgent.ID:  foreignAgent,
	})
	saveDefaultConfig(t, configService, ownedAgent)
	saveDefaultConfig(t, configService, businessAgent)
	saveDefaultConfig(t, configService, foreignAgent)
	saveDefaultConfig(t, configService, missingAgent)

	recorder := invokeAgentConfigHandler(t, http.MethodGet, "/agents/config", nil, "user-1", "business-1", "", handler.GetAllAgentConfigs)
	if recorder.Code != http.StatusOK {
		t.Fatalf("expected status %d, got %d. Body: %s", http.StatusOK, recorder.Code, recorder.Body.String())
	}

	var response struct {
		Configs []models.WellKnownAgentConfig `json:"configs"`
	}
	if err := json.Unmarshal(recorder.Body.Bytes(), &response); err != nil {
		t.Fatalf("decode response: %v", err)
	}

	got := map[string]bool{}
	for _, config := range response.Configs {
		got[config.AgentID] = true
	}

	if !got[ownedAgent.ID] {
		t.Fatal("owned agent config was filtered out")
	}
	if !got[businessAgent.ID] {
		t.Fatal("business-scoped agent config was filtered out")
	}
	if got[foreignAgent.ID] {
		t.Fatal("foreign agent config was returned")
	}
	if got[missingAgent.ID] {
		t.Fatal("config for missing agent was returned")
	}
}

func newAgentConfigTestHandler(t *testing.T, agents map[string]*models.Agent) (*AgentConfigHandler, *services.AgentConfigService) {
	t.Helper()

	log := logger.FromZap(zap.NewNop())
	configService := services.NewAgentConfigService(t.TempDir(), log)
	repo := &agentConfigAP2Repo{agents: agents}

	return NewAgentConfigHandler(configService, repo, nil, nil, nil, log), configService
}

func invokeAgentConfigHandler(
	t *testing.T,
	method string,
	path string,
	body interface{},
	userID string,
	businessID string,
	agentID string,
	handler func(*gin.Context),
) *httptest.ResponseRecorder {
	t.Helper()

	var requestBody bytes.Buffer
	if body != nil {
		if err := json.NewEncoder(&requestBody).Encode(body); err != nil {
			t.Fatalf("encode body: %v", err)
		}
	}

	req := httptest.NewRequest(method, path, &requestBody)
	req.Header.Set("Content-Type", "application/json")
	recorder := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(recorder)
	c.Request = req
	c.Set("user_id", userID)
	c.Set("business_id", businessID)
	if agentID != "" {
		c.Params = gin.Params{{Key: "agent_id", Value: agentID}}
	}

	handler(c)

	return recorder
}

func saveDefaultConfig(t *testing.T, configService *services.AgentConfigService, agent *models.Agent) {
	t.Helper()

	config, err := configService.CreateDefaultConfigForAgentType(agent.Type)
	if err != nil {
		t.Fatalf("create default config: %v", err)
	}
	if _, err := configService.SaveAgentConfig(context.Background(), agent, config); err != nil {
		t.Fatalf("save default config: %v", err)
	}
}

func testAgent(id, ownerID, businessID string) *models.Agent {
	return &models.Agent{
		ID:         id,
		OwnerID:    ownerID,
		BusinessID: businessID,
		Name:       id,
		Type:       "shopping",
	}
}
