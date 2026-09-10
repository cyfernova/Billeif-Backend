package services

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"

	"github.com/stretchr/testify/require"
)

func TestChatSearchRoutingIgnoresCapitalization(t *testing.T) {
	for _, query := range []string{"Hello. Please greet me.", "HELLO", "Thanks. Can you help me?", "Write A Short Greeting"} {
		require.False(t, shouldUseWebSearch(query), query)
	}
	for _, query := range []string{"Search the web for DeepSeek", "Who is Mira Murati?", "latest GST update today"} {
		require.True(t, shouldUseWebSearch(query), query)
	}
}

func TestLLMModelListAcceptsDeepSeekTransportHeaders(t *testing.T) {
	header := http.Header{
		"Content-Type":                     {"application/json"},
		"Access-Control-Allow-Credentials": {"true"},
		"X-Ds-Trace-Id":                    {"trace"},
		"X-Cache":                          {"Miss from cloudfront"},
		"Via":                              {"1.1 cloudfront"},
		"X-Amz-Cf-Pop":                     {"CCU50-P1"},
		"X-Amz-Cf-Id":                      {"request"},
	}
	require.True(t, llmModelListHeadersProveTerminalJSON(header))
	header.Set("Link", "</models?page=2>; rel=next")
	require.False(t, llmModelListHeadersProveTerminalJSON(header))
}

func TestBusinessChatRefreshesMissingHealthOnlyOnce(t *testing.T) {
	calls := 0
	cache := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{})
	service := NewLLMService(config.LLMConfig{APIKey: "test", APIURL: "https://api.deepseek.com/chat/completions", Model: "model", Timeout: 1}, logger.New()).WithHealthCache(cache).WithCapabilityGuard(&recordingCapabilityGuard{err: errors.New("denied")})
	service.client.Transport = llmProbeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		return llmProbeResponse(r, http.StatusOK, nil, `{"object":"list","data":[{"id":"model","object":"model","owned_by":"test"}]}`), nil
	})
	for i := 0; i < 2; i++ {
		_, err := service.ChatWithWebSearchForBusiness(context.Background(), "business", "user", []ChatMessage{{Role: "user", Content: "hello"}})
		require.Error(t, err)
	}
	fact, found := cache.CustomerFact(CapabilityAI)
	require.True(t, found)
	require.Equal(t, CapabilityProviderHealthy, fact.Status)
	require.Equal(t, 1, calls)
}

func TestLLMServiceChatSendsOpenAICompatibleRequest(t *testing.T) {
	t.Parallel()

	const apiKey = "test-llm-key"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("Authorization"), "Bearer "+apiKey; got != want {
			t.Fatalf("Authorization header = %q, want %q", got, want)
		}
		if got := r.Header.Get("x-api-key"); got != "" {
			t.Fatalf("x-api-key header = %q, want empty", got)
		}
		if got := r.Header.Get("anthropic-version"); got != "" {
			t.Fatalf("anthropic-version header = %q, want empty", got)
		}

		var req OpenAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode request: %v", err)
		}
		if req.Model != "test-llm-model" {
			t.Fatalf("model = %q, want test-llm-model", req.Model)
		}
		if len(req.Messages) != 1 || req.Messages[0].Role != "user" || req.Messages[0].Content != "hello" {
			t.Fatalf("messages = %#v, want one OpenAI user message", req.Messages)
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"ok"}}],"model":"test-llm-model"}`))
	}))
	defer server.Close()

	svc := NewLLMService(config.LLMConfig{
		APIKey:  apiKey,
		APIURL:  server.URL,
		Model:   "test-llm-model",
		Timeout: 5,
	}, logger.NewWithEnv("test"))

	got, err := svc.Chat(context.Background(), []ChatMessage{{Role: "user", Content: "hello"}})
	if err != nil {
		t.Fatalf("Chat returned error: %v", err)
	}
	if got != "ok" {
		t.Fatalf("Chat response = %q, want ok", got)
	}
}

func TestLLMCapabilityProbeUsesDeepSeekReadOnlyModelList(t *testing.T) {
	var calls int
	service := NewLLMService(config.LLMConfig{
		APIKey: "test-key", APIURL: "https://api.deepseek.com/chat/completions", Model: "deepseek-chat", Timeout: 1,
	}, logger.New())
	service.client.Transport = llmProbeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		calls++
		if r.Method != http.MethodGet || r.URL.Path != "/models" {
			t.Fatalf("probe request = %s %s", r.Method, r.URL.Path)
		}
		if got := r.Header.Get("Authorization"); got != "Bearer test-key" {
			t.Fatalf("authorization = %q", got)
		}
		return llmProbeResponse(r, http.StatusOK, nil, `{"object":"list","data":[{"id":"deepseek-chat","object":"model"}]}`), nil
	})

	outcome := service.ProbeGlobalCapability(context.Background())

	if outcome.Err != nil {
		t.Fatalf("probe error = %v", outcome.Err)
	}
	if calls != 1 {
		t.Fatalf("provider calls = %d, want 1", calls)
	}
}

func TestLLMCapabilityProbeMarksMissingConfiguredModelUnavailableWithoutRawBody(t *testing.T) {
	const rawBody = `{"object":"list","data":[{"id":"other-model","credential":"raw-secret"}]}`
	service := NewLLMService(config.LLMConfig{
		APIKey: "test-key", APIURL: "https://api.deepseek.com/v1/chat/completions", Model: "configured-model", Timeout: 1,
	}, logger.New())
	service.client.Transport = llmProbeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodGet || r.URL.Path != "/v1/models" {
			t.Fatalf("probe request = %s %s", r.Method, r.URL.Path)
		}
		return llmProbeResponse(r, http.StatusOK, nil, rawBody), nil
	})

	outcome := service.ProbeGlobalCapability(context.Background())

	if outcome.Err == nil {
		t.Fatal("missing configured model must be unavailable")
	}
	if strings.Contains(outcome.Err.Error(), "other-model") || strings.Contains(outcome.Err.Error(), "raw-secret") {
		t.Fatalf("probe error leaked model response: %v", outcome.Err)
	}
	cache := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{})
	if err := NewCapabilityGlobalHealthRecorder(cache, nil).RecordGlobalOutcome(CapabilityAI, outcome); err != nil {
		t.Fatalf("record outcome: %v", err)
	}
	fact, found := cache.CustomerFact(CapabilityAI)
	if !found || fact.Status != CapabilityProviderUnavailable {
		t.Fatalf("missing configured model fact = %#v, found=%v", fact, found)
	}
}

func TestLLMCapabilityProbeCustomEndpointCannotProveCompleteModelAbsence(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls++
		_, _ = w.Write([]byte(`{"object":"list","data":[{"id":"other-model"}]}`))
	}))
	defer server.Close()
	service := NewLLMService(config.LLMConfig{
		APIKey: "test-key", APIURL: server.URL + "/v1/chat/completions",
		Model: "configured-model", Timeout: 1,
	}, logger.New())

	outcome := service.ProbeGlobalCapability(context.Background())

	require.ErrorIs(t, outcome.Err, ErrCapabilityProbeUnsupported)
	require.Zero(t, calls, "custom endpoints must not be probed as complete-list providers")
}

func TestLLMCapabilityProbeUnrecognizedResponseHeaderCannotProveCompleteModelAbsence(t *testing.T) {
	service := NewLLMService(config.LLMConfig{
		APIKey: "test-key", APIURL: "https://api.deepseek.com/v1/chat/completions",
		Model: "configured-model", Timeout: 1,
	}, logger.New())
	service.client.Transport = roundTripFunc(func(request *http.Request) (*http.Response, error) {
		require.Equal(t, "api.deepseek.com", request.URL.Host)
		require.Equal(t, "/v1/models", request.URL.Path)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header: http.Header{
				"Content-Type": []string{"application/json"},
				"X-Next-Token": []string{"opaque-continuation"},
			},
			Body:    io.NopCloser(strings.NewReader(`{"object":"list","data":[{"id":"other-model"}]}`)),
			Request: request,
		}, nil
	})

	outcome := service.ProbeGlobalCapability(context.Background())

	require.ErrorIs(t, outcome.Err, ErrCapabilityProbeUnsupported)
	require.NotContains(t, outcome.Err.Error(), "opaque-continuation")
}

func TestLLMCapabilityProbeLeavesUnprovenModelListsUnknown(t *testing.T) {
	oversized := `{"object":"list","data":[]}` + strings.Repeat(" ", (1<<20)+1)
	tooManyModels := strings.Builder{}
	tooManyModels.WriteString(`{"object":"list","data":[{"id":"configured-model"}`)
	for index := 1; index <= 10_000; index++ {
		tooManyModels.WriteString(`,{"id":"other-`)
		tooManyModels.WriteString(strconv.Itoa(index))
		tooManyModels.WriteString(`"}`)
	}
	tooManyModels.WriteString(`]}`)

	for _, fixture := range []struct {
		name string
		body string
	}{
		{name: "missing required list marker", body: `{"data":[]}`},
		{name: "missing data", body: `{"object":"list"}`},
		{name: "empty object", body: `{}`},
		{name: "malformed JSON", body: `{"object":"list","data":[`},
		{name: "malformed model entry", body: `{"object":"list","data":[{}]}`},
		{name: "pagination marker makes completeness unproven", body: `{"object":"list","data":[],"has_more":true}`},
		{name: "unrecognized top-level field", body: `{"object":"list","data":[],"next_page":"secret-cursor"}`},
		{name: "response exceeds byte limit", body: oversized},
		{name: "response exceeds model scan limit", body: tooManyModels.String()},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			service := NewLLMService(config.LLMConfig{
				APIKey: "test-key", APIURL: "https://api.deepseek.com/v1/chat/completions",
				Model: "configured-model", Timeout: 1,
			}, logger.New())
			service.client.Transport = llmProbeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				return llmProbeResponse(r, http.StatusOK, nil, fixture.body), nil
			})

			outcome := service.ProbeGlobalCapability(context.Background())

			if !errors.Is(outcome.Err, ErrCapabilityProbeUnsupported) {
				t.Fatalf("probe error = %v, want unsupported/unknown", outcome.Err)
			}
		})
	}
}

func TestLLMCapabilityProbeRejectsHTTPResponsesThatDoNotProveACompleteList(t *testing.T) {
	for _, fixture := range []struct {
		name    string
		status  int
		headers map[string]string
	}{
		{name: "unrecognized successful status", status: http.StatusCreated},
		{name: "partial content", status: http.StatusPartialContent},
		{name: "link pagination", status: http.StatusOK, headers: map[string]string{"Link": `<https://provider.test/models?page=2>; rel="next"`}},
		{name: "content range", status: http.StatusOK, headers: map[string]string{"Content-Range": "items 0-0/2"}},
		{name: "next cursor", status: http.StatusOK, headers: map[string]string{"X-Next-Cursor": "raw-secret-cursor"}},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			service := NewLLMService(config.LLMConfig{
				APIKey: "test-key", APIURL: "https://api.deepseek.com/v1/chat/completions",
				Model: "configured-model", Timeout: 1,
			}, logger.New())
			service.client.Transport = llmProbeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				return llmProbeResponse(r, fixture.status, fixture.headers, `{"object":"list","data":[{"id":"other-model"}]}`), nil
			})

			outcome := service.ProbeGlobalCapability(context.Background())

			require.ErrorIs(t, outcome.Err, ErrCapabilityProbeUnsupported)
			require.NotContains(t, outcome.Err.Error(), "raw-secret-cursor")
		})
	}
}

func TestLLMUnsupportedProbeRouteLeavesHealthUnknownWithoutChatMutation(t *testing.T) {
	for _, status := range []int{http.StatusNotFound, http.StatusMethodNotAllowed} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			var methods []string
			service := NewLLMService(config.LLMConfig{
				APIKey: "test-key", APIURL: "https://api.deepseek.com/chat/completions", Model: "deepseek-chat", Timeout: 1,
			}, logger.New())
			service.client.Transport = llmProbeRoundTripFunc(func(r *http.Request) (*http.Response, error) {
				methods = append(methods, r.Method+" "+r.URL.Path)
				return llmProbeResponse(r, status, nil, `{"error":"raw secret unsupported response"}`), nil
			})
			cache := NewCapabilityGlobalHealthCache(CapabilityGlobalHealthCacheOptions{})
			observer := NewCapabilityGlobalHealthObserver(
				map[CapabilityKey]CapabilityGlobalProviderProber{CapabilityAI: service},
				NewCapabilityGlobalHealthRecorder(cache, nil), CapabilityGlobalHealthObserverOptions{MaxAttempts: 1},
			)

			if err := observer.ObserveOnce(context.Background()); err != nil {
				t.Fatalf("observe: %v", err)
			}
			if _, found := cache.CustomerFact(CapabilityAI); found {
				t.Fatal("unsupported model probe must leave health unknown")
			}
			if len(methods) != 1 || methods[0] != "GET /models" {
				t.Fatalf("provider calls = %v, want only GET /models", methods)
			}
		})
	}
}

type llmProbeRoundTripFunc func(*http.Request) (*http.Response, error)

func (f llmProbeRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func llmProbeResponse(request *http.Request, status int, headers map[string]string, body string) *http.Response {
	header := http.Header{"Content-Type": []string{"application/json"}}
	for name, value := range headers {
		header.Set(name, value)
	}
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
		Request:    request,
	}
}

func TestLLMUnrecognizedChatEndpointShapeDoesNotCallProvider(t *testing.T) {
	var calls int
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { calls++ }))
	defer server.Close()
	service := NewLLMService(config.LLMConfig{APIKey: "test-key", APIURL: server.URL + "/proxy/chat/completions", Model: "model", Timeout: 1}, logger.New())

	outcome := service.ProbeGlobalCapability(context.Background())

	if !errors.Is(outcome.Err, ErrCapabilityProbeUnsupported) {
		t.Fatalf("probe error = %v, want unsupported", outcome.Err)
	}
	if calls != 0 {
		t.Fatalf("provider calls = %d, want zero", calls)
	}
}

func TestLLMServiceBusinessChatRejectsUnavailableAICapabilityBeforeProvider(t *testing.T) {
	var providerCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	guard := &recordingCapabilityGuard{err: &CapabilityUnavailableError{
		Code: "capability_unavailable", Capability: CapabilityAI,
		State: CapabilityStateUnknown, ReasonCode: ReasonProviderHealthUnknown,
	}}
	service := NewLLMService(config.LLMConfig{APIKey: "test", APIURL: server.URL, Timeout: 1}, logger.New()).WithCapabilityGuard(guard)

	_, err := service.ChatWithWebSearchForBusiness(context.Background(), "biz-1", "user-1", []ChatMessage{{Role: "user", Content: "hello"}})

	var unavailable *CapabilityUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("business chat error = %T %v, want CapabilityUnavailableError", err, err)
	}
	if providerCalls != 0 {
		t.Fatalf("provider calls = %d, want zero", providerCalls)
	}
	if guard.request.Capability != CapabilityAI || guard.request.BusinessID != "biz-1" || guard.request.UserID != "user-1" {
		t.Fatalf("guard request = %#v", guard.request)
	}
}

func TestLLMServiceBusinessAgentAssistRejectsUnavailableAICapabilityBeforeProvider(t *testing.T) {
	var providerCalls int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		providerCalls++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	guard := &recordingCapabilityGuard{err: &CapabilityUnavailableError{
		Code: "capability_unavailable", Capability: CapabilityAI,
		State: CapabilityStateUnknown, ReasonCode: ReasonProviderHealthUnknown,
	}}
	service := NewLLMService(config.LLMConfig{APIKey: "test", APIURL: server.URL, Timeout: 1}, logger.New()).WithCapabilityGuard(guard)

	_, err := service.ProcessAgentIntentForBusiness(context.Background(), "biz-1", "user-1", "suggest an agent", "")

	var unavailable *CapabilityUnavailableError
	if !errors.As(err, &unavailable) {
		t.Fatalf("agent assist error = %T %v, want CapabilityUnavailableError", err, err)
	}
	if providerCalls != 0 {
		t.Fatalf("provider calls = %d, want zero", providerCalls)
	}
}

func TestLLMServiceChatWithWebSearchUsesExaContext(t *testing.T) {
	t.Parallel()

	const exaKey = "test-exa-key"
	exaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.Header.Get("x-api-key"), exaKey; got != want {
			t.Fatalf("x-api-key header = %q, want %q", got, want)
		}
		var req exaSearchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode Exa request: %v", err)
		}
		if req.Query != "What is the latest GST e-invoice update today?" {
			t.Fatalf("Exa query = %q", req.Query)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"GST update","url":"https://example.test/gst","publishedDate":"2026-05-15","author":"Example","highlights":["Latest GST e-invoice update for testing."],"image":"https://example.test/gst.png","favicon":"https://example.test/favicon.ico","extras":{"imageLinks":["https://example.test/gst-extra.png"]}}]}`))
	}))
	defer exaServer.Close()

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req OpenAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode LLM request: %v", err)
		}
		if len(req.Messages) < 3 {
			t.Fatalf("messages = %#v, want system prompt, Exa context, user message", req.Messages)
		}
		foundSearchContext := false
		for _, msg := range req.Messages {
			if msg.Role == "system" && strings.Contains(msg.Content, "Exa web search") && strings.Contains(msg.Content, "https://example.test/gst") {
				foundSearchContext = true
				break
			}
		}
		if !foundSearchContext {
			t.Fatalf("LLM request did not include Exa search context: %#v", req.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Use the cited GST update."}}],"model":"test-llm-model"}`))
	}))
	defer llmServer.Close()

	svc := NewLLMService(config.LLMConfig{
		APIKey:     "test-llm-key",
		APIURL:     llmServer.URL,
		Model:      "test-llm-model",
		Timeout:    5,
		ExaAPIKey:  exaKey,
		ExaBaseURL: exaServer.URL,
		ExaTimeout: 5,
	}, logger.NewWithEnv("test"))

	got, err := svc.ChatWithWebSearch(context.Background(), []ChatMessage{
		{Role: "system", Content: "system prompt"},
		{Role: "user", Content: "What is the latest GST e-invoice update today?"},
	})
	if err != nil {
		t.Fatalf("ChatWithWebSearch returned error: %v", err)
	}
	if got.Response != "Use the cited GST update." {
		t.Fatalf("response = %q", got.Response)
	}
	if got.WebSearch == nil || !got.WebSearch.Used || got.WebSearch.Query == "" || len(got.WebSearch.Results) != 1 {
		t.Fatalf("web search metadata = %#v", got.WebSearch)
	}
	result := got.WebSearch.Results[0]
	if result.ImageURL != "https://example.test/gst.png" || result.FaviconURL != "https://example.test/favicon.ico" || len(result.ImageURLs) != 1 {
		t.Fatalf("web search image metadata = %#v", result)
	}
}

func TestLLMServiceChatWithWebSearchRequiresExaKey(t *testing.T) {
	t.Parallel()

	svc := NewLLMService(config.LLMConfig{
		APIKey:  "test-llm-key",
		APIURL:  "https://llm.example.test/chat/completions",
		Model:   "test-llm-model",
		Timeout: 5,
	}, logger.NewWithEnv("test"))

	_, err := svc.ChatWithWebSearch(context.Background(), []ChatMessage{{Role: "user", Content: "latest GST update today"}})
	if err == nil || !strings.Contains(err.Error(), "EXA_API_KEY") {
		t.Fatalf("error = %v, want missing EXA_API_KEY error", err)
	}
}

func TestLLMServiceChatWithWebSearchUsesExaForExternalQuestion(t *testing.T) {
	t.Parallel()

	const exaKey = "test-exa-key"
	exaServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req exaSearchRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode Exa request: %v", err)
		}
		if req.Query != "Who is Mira Murati?" {
			t.Fatalf("Exa query = %q", req.Query)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"title":"Mira Murati profile","url":"https://example.test/mira","highlights":["Mira Murati is a technology executive."]}]}`))
	}))
	defer exaServer.Close()

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req OpenAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode LLM request: %v", err)
		}
		foundSearchContext := false
		for _, msg := range req.Messages {
			if msg.Role == "system" && strings.Contains(msg.Content, "Exa web search") && strings.Contains(msg.Content, "https://example.test/mira") {
				foundSearchContext = true
				break
			}
		}
		if !foundSearchContext {
			t.Fatalf("LLM request did not include autonomous Exa search context: %#v", req.Messages)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Answered with web context."}}],"model":"test-llm-model"}`))
	}))
	defer llmServer.Close()

	svc := NewLLMService(config.LLMConfig{
		APIKey:     "test-llm-key",
		APIURL:     llmServer.URL,
		Model:      "test-llm-model",
		Timeout:    5,
		ExaAPIKey:  exaKey,
		ExaBaseURL: exaServer.URL,
		ExaTimeout: 5,
	}, logger.NewWithEnv("test"))

	got, err := svc.ChatWithWebSearch(context.Background(), []ChatMessage{{Role: "user", Content: "Who is Mira Murati?"}})
	if err != nil {
		t.Fatalf("ChatWithWebSearch returned error: %v", err)
	}
	if got.WebSearch == nil || !got.WebSearch.Used {
		t.Fatalf("web search metadata = %#v, want used", got.WebSearch)
	}
}

func TestLLMServiceChatWithWebSearchDoesNotRequireExaForAppWorkflow(t *testing.T) {
	t.Parallel()

	llmServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req OpenAIChatRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			t.Fatalf("decode LLM request: %v", err)
		}
		for _, msg := range req.Messages {
			if msg.Role == "system" && strings.Contains(msg.Content, "Exa web search") {
				t.Fatalf("unexpected Exa context for local workflow query: %#v", req.Messages)
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"Create an invoice from the New Invoice screen."}}],"model":"test-llm-model"}`))
	}))
	defer llmServer.Close()

	svc := NewLLMService(config.LLMConfig{
		APIKey:  "test-llm-key",
		APIURL:  llmServer.URL,
		Model:   "test-llm-model",
		Timeout: 5,
	}, logger.NewWithEnv("test"))

	got, err := svc.ChatWithWebSearch(context.Background(), []ChatMessage{{Role: "user", Content: "How do I create an invoice?"}})
	if err != nil {
		t.Fatalf("ChatWithWebSearch returned error: %v", err)
	}
	if got.WebSearch == nil || got.WebSearch.Used {
		t.Fatalf("web search metadata = %#v, want not used", got.WebSearch)
	}
}
