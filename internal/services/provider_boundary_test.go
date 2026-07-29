package services

import (
	"context"
	"encoding/base64"
	"sync"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"
)

type recordingProviderResolver struct {
	mu    sync.Mutex
	kinds []config.SecretKind
}

func (r *recordingProviderResolver) ResolveProvider(_ context.Context, cfg *config.Config, kind config.SecretKind) (*config.Config, error) {
	r.mu.Lock()
	r.kinds = append(r.kinds, kind)
	r.mu.Unlock()
	resolved := *cfg
	switch kind {
	case config.SecretCredentialEncryption:
		resolved.Credentials.EncryptionKey = base64.StdEncoding.EncodeToString(make([]byte, 32))
	case config.SecretRazorpay:
		resolved.Razorpay.KeyID = "key-id"
		resolved.Razorpay.KeySecret = "key-secret"
		resolved.Razorpay.WebhookSecret = "webhook-secret"
	case config.SecretDeepgram:
		resolved.VoiceRealtime.DeepgramAPIKey = "deepgram-key"
	case config.SecretDeepSeek:
		resolved.VoiceRealtime.DeepSeekAPIKey = "deepseek-key"
	case config.SecretGSTProvider:
		resolved.GST.APIToken = "gst-token"
	}
	return &resolved, nil
}

func (r *recordingProviderResolver) calls() []config.SecretKind {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]config.SecretKind(nil), r.kinds...)
}

func TestProviderServicesResolveOnlyAtConcreteUseBoundaries(t *testing.T) {
	t.Parallel()

	cfg := &config.Config{}
	cfg.Razorpay.BaseURL = "https://api.razorpay.com"
	cfg.Razorpay.Timeout = 1
	cfg.GST.BaseURL = "https://gst.example.test"
	cfg.Secrets.Deepgram = "deepgram-secret"
	cfg.Secrets.DeepSeek = "deepseek-secret"
	cfg.VoiceRealtime = config.VoiceRealtimeConfig{
		DeepgramVoiceAgentURL:        "wss://agent.deepgram.com/v1/agent/converse",
		InputEncoding:                "linear16",
		InputSampleRate:              16000,
		OutputEncoding:               "linear16",
		OutputSampleRate:             16000,
		ListenModel:                  "nova-3",
		SpeakModel:                   "aura-2",
		DeepSeekBaseURL:              "https://api.deepseek.com/v1",
		DeepSeekModel:                "deepseek-chat",
		MaxSessionSeconds:            60,
		PingIntervalSeconds:          10,
		WriteTimeoutSeconds:          5,
		MaxFrameBytes:                8192,
		MaxConcurrentSessionsPerUser: 1,
	}
	resolver := &recordingProviderResolver{}
	log := logger.New()

	credentials := NewCredentialProviderServiceWithResolver(nil, cfg, resolver, log)
	payments := NewRazorpayPaymentService(cfg, nil, log, resolver)
	voice := NewRealtimeVoiceServiceWithResolver(cfg, resolver, log)
	gstProvider := NewLazyConfiguredGSTProvider(cfg, resolver, log)
	if got := resolver.calls(); len(got) != 0 {
		t.Fatalf("constructors fetched provider credentials: %v", got)
	}
	if err := voice.ConfigError(); err != nil {
		t.Fatalf("identifier-backed voice config rejected before upgrade: %v", err)
	}
	if got := resolver.calls(); len(got) != 0 {
		t.Fatalf("pre-upgrade validation fetched provider credentials: %v", got)
	}

	if _, err := credentials.encryptCredential(context.Background(), "token"); err != nil {
		t.Fatalf("encrypt credential: %v", err)
	}
	if _, err := payments.clientFor(context.Background()); err != nil {
		t.Fatalf("construct Razorpay client: %v", err)
	}
	if _, err := voice.runtimeConfig(context.Background()); err != nil {
		t.Fatalf("resolve voice config: %v", err)
	}
	if err := gstProvider.ValidateCredentials(context.Background(), &GSTIntegrationAccountCredentials{}); err != nil {
		t.Fatalf("validate GST credentials: %v", err)
	}

	want := []config.SecretKind{
		config.SecretCredentialEncryption,
		config.SecretRazorpay,
		config.SecretDeepgram,
		config.SecretDeepSeek,
		config.SecretGSTProvider,
	}
	got := resolver.calls()
	if len(got) != len(want) {
		t.Fatalf("resolved kinds = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("resolved kinds = %v, want %v", got, want)
		}
	}
}
