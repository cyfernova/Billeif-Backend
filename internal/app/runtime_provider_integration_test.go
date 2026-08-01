package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

type providerIntegrationSecrets struct {
	mu     sync.Mutex
	values map[string][]string
	calls  map[string]int
}

func (f *providerIntegrationSecrets) GetSecretValue(_ context.Context, input *secretsmanager.GetSecretValueInput, _ ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	identifier := aws.ToString(input.SecretId)
	f.calls[identifier]++
	values := f.values[identifier]
	value := values[0]
	if len(values) > 1 {
		f.values[identifier] = values[1:]
	}
	return &secretsmanager.GetSecretValueOutput{SecretString: aws.String(value)}, nil
}

func TestProductionRuntimeLazilyRefreshesProviderWithoutReplacingContainerOrScheduler(t *testing.T) {
	var requestsMu sync.Mutex
	var authorizations []string
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestsMu.Lock()
		authorizations = append(authorizations, r.Header.Get("Authorization"))
		requestsMu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"choices": []map[string]interface{}{
				{"message": map[string]string{"role": "assistant", "content": r.Header.Get("Authorization")}},
			},
		})
	}))
	defer provider.Close()

	now := time.Unix(1_700_000_000, 0)
	secretsClient := &providerIntegrationSecrets{
		values: map[string][]string{
			"llm-secret": {`{"api_key":"llm-v1"}`, `{"api_key":"llm-v2"}`},
			"exa-secret": {`{"api_key":"exa-key"}`},
		},
		calls: map[string]int{},
	}
	resolver, err := config.NewRuntimeResolver(config.RuntimeResolverOptions{
		Clients:           config.RuntimeResolvers{Secrets: secretsClient},
		SecretIdentifiers: []string{"llm-secret", "exa-secret"},
		TTL:               time.Minute,
		Now:               func() time.Time { return now },
	})
	if err != nil {
		t.Fatalf("new runtime resolver: %v", err)
	}
	cfg := &config.Config{
		Environment: "production",
		LLM: config.LLMConfig{
			APIURL:  provider.URL,
			Model:   "production-model",
			Timeout: 2,
		},
		Secrets: config.SecretIdentifiers{LLM: "llm-secret", Exa: "exa-secret"},
	}
	db, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", strings.ReplaceAll(t.Name(), "/", "_"))), &gorm.Config{})
	if err != nil {
		t.Fatalf("open runtime test database: %v", err)
	}
	workflow := services.NewWorkflowService(db, logger.New(), nil, nil)
	t.Cleanup(workflow.Stop)
	container := &services.Container{
		LLM:      services.NewLLMServiceWithResolver(cfg, resolver, logger.New()),
		Workflow: workflow,
	}
	rt := &Runtime{Config: cfg, Secrets: resolver, Svcs: container}
	originalContainer := rt.Svcs
	originalLLM := rt.Svcs.LLM
	originalWorkflow := rt.Svcs.Workflow

	if len(secretsClient.calls) != 0 {
		t.Fatalf("production bootstrap fetched providers: %#v", secretsClient.calls)
	}
	if got := workflow.SchedulerStartCount(); got != 1 {
		t.Fatalf("scheduler start count after bootstrap = %d", got)
	}

	for i, want := range []string{"Bearer llm-v1", "Bearer llm-v1"} {
		got, err := rt.Svcs.LLM.Chat(context.Background(), []services.ChatMessage{{Role: "user", Content: "hello"}})
		if err != nil {
			t.Fatalf("LLM use %d: %v", i+1, err)
		}
		if got != want {
			t.Fatalf("LLM use %d credential = %q, want %q", i+1, got, want)
		}
	}
	if secretsClient.calls["llm-secret"] != 1 || secretsClient.calls["exa-secret"] != 0 {
		t.Fatalf("warm or unrelated fetch count = %#v", secretsClient.calls)
	}

	now = now.Add(61 * time.Second)
	got, err := rt.Svcs.LLM.Chat(context.Background(), []services.ChatMessage{{Role: "user", Content: "hello again"}})
	if err != nil {
		t.Fatalf("LLM use after TTL: %v", err)
	}
	if got != "Bearer llm-v2" || secretsClient.calls["llm-secret"] != 2 || secretsClient.calls["exa-secret"] != 0 {
		t.Fatalf("TTL refresh result = %q, calls = %#v", got, secretsClient.calls)
	}
	if rt.Svcs != originalContainer || rt.Svcs.LLM != originalLLM || rt.Svcs.Workflow != originalWorkflow {
		t.Fatal("provider use replaced runtime, service, or workflow identity")
	}
	if got := workflow.SchedulerStartCount(); got != 1 {
		t.Fatalf("scheduler restarted during provider use: %d", got)
	}

	requestsMu.Lock()
	defer requestsMu.Unlock()
	if len(authorizations) != 3 {
		t.Fatalf("real provider request count = %d", len(authorizations))
	}
}
