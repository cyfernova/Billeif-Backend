package main

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/internal/providers/sarvam"
	"invoice-backend/internal/voice/audio"
	"invoice-backend/internal/voice/composition"
	voicesession "invoice-backend/internal/voice/session"
	voicetelemetry "invoice-backend/internal/voice/telemetry"
	"invoice-backend/internal/voice/webrtc"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

const sarvamTransportTimeout = 22 * time.Second

type providerConfigResolver interface {
	ResolveProvider(context.Context, *config.Config, config.SecretKind) (*config.Config, error)
}

// lazySarvamProviders retains only the secret identifier and secure transports.
// It resolves a fresh request-scoped configuration at each STT socket/chat/TTS
// generation, so the process does not pin a rotated API key.
type lazySarvamProviders struct {
	mu sync.RWMutex

	config    *config.Config
	resolver  providerConfigResolver
	http      *http.Client
	websocket *websocket.Dialer
	transport *http.Transport

	closeOnce sync.Once
	closed    bool
}

type sarvamProviderSnapshot struct {
	config    *config.Config
	resolver  providerConfigResolver
	http      *http.Client
	websocket *websocket.Dialer
	transport *http.Transport
}

func loadProductionRuntimeDependencies(ctx context.Context, environment runtimeEnvironment) (runtimeDependencies, error) {
	if ctx == nil || ctx.Err() != nil {
		return runtimeDependencies{}, ErrInvalidRuntimeEnvironment
	}
	telemetryEmitter, err := voicetelemetry.NewRuntimeEmitter(os.Stdout, os.LookupEnv)
	if err != nil {
		return runtimeDependencies{}, ErrInvalidRuntimeEnvironment
	}
	awsConfig, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(environment.region))
	if err != nil || awsConfig.Region != runtimeRegion {
		return runtimeDependencies{}, ErrInvalidRuntimeEnvironment
	}

	secretsClient := secretsmanager.NewFromConfig(awsConfig, func(options *secretsmanager.Options) {
		options.BaseEndpoint = aws.String("https://secretsmanager." + environment.region + ".amazonaws.com")
		options.EndpointResolverV2 = secretsmanager.NewDefaultEndpointResolverV2()
		options.ClientLogMode = 0
	})
	providerResolver, err := config.NewRuntimeResolver(config.RuntimeResolverOptions{
		Clients:           config.RuntimeResolvers{Secrets: secretsClient},
		SecretIdentifiers: []string{environment.sarvamSecretARN},
	})
	if err != nil {
		return runtimeDependencies{}, ErrInvalidRuntimeEnvironment
	}
	httpClient, websocketDialer, transport := newSarvamTransports()
	providers, err := newLazySarvamProviders(&config.Config{
		AWS:     config.AWSConfig{Region: environment.region},
		Secrets: config.SecretIdentifiers{Sarvam: environment.sarvamSecretARN},
		Sarvam:  config.SarvamConfig{BaseURL: sarvam.DefaultBaseURL, Timeout: int(sarvamTransportTimeout / time.Second)},
	}, providerResolver, httpClient, websocketDialer, transport)
	if err != nil {
		transport.CloseIdleConnections()
		return runtimeDependencies{}, err
	}
	observedProviders, err := newObservedSarvamProviders(providers, telemetryEmitter)
	if err != nil {
		_ = providers.Close()
		return runtimeDependencies{}, ErrInvalidRuntimeEnvironment
	}
	signalingMetrics, err := newRuntimeSignalingMetrics(telemetryEmitter)
	if err != nil {
		_ = observedProviders.Close()
		return runtimeDependencies{}, ErrInvalidRuntimeEnvironment
	}

	ice, err := webrtc.NewKVSICECredentialSource(awsConfig)
	if err != nil {
		_ = observedProviders.Close()
		return runtimeDependencies{}, ErrInvalidRuntimeEnvironment
	}
	authorization, err := composition.NewCognitoAuthorizationResolver(config.CognitoConfig{
		Phone: config.CognitoPhoneConfig{
			Region: environment.phoneRegion, UserPoolID: environment.phonePoolID, ClientID: environment.phoneClientID,
		},
		JWKSRefreshRate: 10 * time.Minute,
	})
	if err != nil {
		_ = observedProviders.Close()
		return runtimeDependencies{}, ErrInvalidRuntimeEnvironment
	}

	dynamoClient := dynamodb.NewFromConfig(awsConfig, func(options *dynamodb.Options) {
		options.BaseEndpoint = aws.String("https://dynamodb." + environment.region + ".amazonaws.com")
		options.EndpointResolverV2 = dynamodb.NewDefaultEndpointResolverV2()
		options.ClientLogMode = 0
	})
	sessions := voicesession.NewDynamoDBStore(dynamoClient, voicesession.DynamoDBStoreConfig{
		TableName: environment.sessionTable, LeaseIndexName: "gsi2",
		GlobalCapacityLimit: environment.globalLimit, PerUserCapacityLimit: environment.perUserLimit,
	})
	outputs, err := newProductionSpeechOutputFactory(ctx, observedProviders, sessions, telemetryEmitter)
	if err != nil {
		_ = observedProviders.Close()
		return runtimeDependencies{}, ErrInvalidRuntimeEnvironment
	}
	return runtimeDependencies{
		Authorization: authorization, Sessions: sessions, LeaseRenewer: sessions, ICE: ice, STT: observedProviders, Chat: observedProviders,
		Outputs: outputs,
		Decoders: composition.DecoderFactoryFunc(func() (audio.Decoder, error) {
			return audio.NewLibopusDecoder()
		}),
		Metrics: signalingMetrics,
		Closers: []io.Closer{observedProviders},
	}, nil
}

// newProductionSpeechOutputFactory binds TTS and final-turn durability without
// opening a provider socket or performing durable I/O during bootstrap. Each
// consenting peer owns a bounded writer that is drained by SpeechOutput.Close.
// The worker context deliberately outlives signal cancellation so orderly
// runtime shutdown gets its bounded drain window before the process exits.
func newProductionSpeechOutputFactory(
	ctx context.Context,
	tts sarvam.TTSOpener,
	writer voicesession.FinalTurnWriter,
	telemetryEmitter *voicetelemetry.Emitter,
) (composition.OutputFactory, error) {
	if ctx == nil || ctx.Err() != nil || nilRuntimeInterface(tts) || nilRuntimeInterface(writer) || telemetryEmitter == nil {
		return nil, ErrInvalidRuntimeEnvironment
	}
	workerContext := context.WithoutCancel(ctx)
	finalTurns := newProductionFinalTurnSinkFactory(workerContext, writer, telemetryEmitter)
	if nilRuntimeInterface(finalTurns) {
		return nil, ErrInvalidRuntimeEnvironment
	}
	return composition.SpeechOutputFactory{
		TTS: composition.SessionTTSFactoryFunc(func(voicesession.Session) (sarvam.TTSOpener, error) {
			return tts, nil
		}),
		Encoders: composition.LibopusSpeechEncoderFactory{},
		Telemetry: composition.TurnTelemetryFactoryFunc(func() (voicetelemetry.RuntimeTurnTelemetry, error) {
			correlationID, err := uuid.NewRandom()
			if err != nil {
				return nil, err
			}
			return voicetelemetry.NewInstrumentation(correlationID.String(), telemetryEmitter)
		}),
		FinalTurns: finalTurns,
	}, nil
}

func newProductionFinalTurnSinkFactory(
	ctx context.Context,
	writer voicesession.FinalTurnWriter,
	signals voicetelemetry.SignalRecorder,
) composition.FinalTurnSinkFactory {
	if ctx == nil || ctx.Err() != nil || nilRuntimeInterface(writer) || nilRuntimeInterface(signals) {
		return nil
	}
	return composition.FinalTurnSinkFactoryFunc(func(value voicesession.Session) (composition.FinalTurnSinkLease, error) {
		worker, err := voicesession.NewFinalTurnWorker(ctx, writer, voicesession.FinalTurnWorkerConfig{
			SessionID: value.ID, InitialSequence: value.TurnSequence, InitialGenerationID: value.GenerationID,
		})
		if err != nil {
			return composition.FinalTurnSinkLease{}, ErrInvalidRuntimeEnvironment
		}
		go observeFinalTurnWorkerFailures(worker.Errors(), signals)
		return composition.FinalTurnSinkLease{Sink: worker, Close: worker.Close}, nil
	})
}

func observeFinalTurnWorkerFailures(failures <-chan error, signals voicetelemetry.SignalRecorder) {
	if failures == nil || nilRuntimeInterface(signals) {
		return
	}
	for failure := range failures {
		if failure == nil {
			continue
		}
		func() {
			defer func() { _ = recover() }()
			_ = signals.RecordSignal(voicetelemetry.SignalDurabilityFailures, 1)
		}()
		return
	}
}

func newSarvamTransports() (*http.Client, *websocket.Dialer, *http.Transport) {
	networkDialer := &net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second}
	tlsConfig := &tls.Config{
		MinVersion: tls.VersionTLS12,
		ServerName: "api.sarvam.ai",
	}
	transport := &http.Transport{
		Proxy:                  nil,
		DialContext:            networkDialer.DialContext,
		ForceAttemptHTTP2:      true,
		MaxIdleConns:           8,
		MaxIdleConnsPerHost:    4,
		IdleConnTimeout:        30 * time.Second,
		TLSHandshakeTimeout:    5 * time.Second,
		ResponseHeaderTimeout:  5 * time.Second,
		ExpectContinueTimeout:  time.Second,
		MaxResponseHeaderBytes: 16 << 10,
		TLSClientConfig:        tlsConfig.Clone(),
	}
	httpClient := &http.Client{
		Transport: transport,
		Timeout:   sarvamTransportTimeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	websocketDialer := &websocket.Dialer{
		NetDialContext:    networkDialer.DialContext,
		Proxy:             nil,
		HandshakeTimeout:  5 * time.Second,
		ReadBufferSize:    16 << 10,
		WriteBufferSize:   16 << 10,
		EnableCompression: false,
		TLSClientConfig:   tlsConfig.Clone(),
	}
	return httpClient, websocketDialer, transport
}

func newLazySarvamProviders(
	appConfig *config.Config,
	resolver providerConfigResolver,
	httpClient *http.Client,
	websocketDialer *websocket.Dialer,
	transport *http.Transport,
) (*lazySarvamProviders, error) {
	if appConfig == nil || resolver == nil || httpClient == nil || websocketDialer == nil || transport == nil ||
		httpClient.Transport != transport || transport.Proxy != nil || websocketDialer.Proxy != nil ||
		transport.TLSClientConfig == nil || websocketDialer.TLSClientConfig == nil ||
		transport.TLSClientConfig.MinVersion < tls.VersionTLS12 || websocketDialer.TLSClientConfig.MinVersion < tls.VersionTLS12 ||
		transport.TLSClientConfig.ServerName != "api.sarvam.ai" || websocketDialer.TLSClientConfig.ServerName != "api.sarvam.ai" ||
		strings.TrimSpace(appConfig.Secrets.Sarvam) == "" || appConfig.Sarvam.BaseURL != sarvam.DefaultBaseURL {
		return nil, ErrInvalidRuntimeEnvironment
	}
	return &lazySarvamProviders{
		config: appConfig, resolver: resolver, http: httpClient, websocket: websocketDialer, transport: transport,
	}, nil
}

func (providers *lazySarvamProviders) OpenSTT(ctx context.Context) (sarvam.STTStream, error) {
	snapshot, err := providers.snapshot()
	if err != nil {
		return nil, err
	}
	client, err := resolveSarvamClient(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	stt, err := sarvam.NewSTTClient(client, snapshot.websocket)
	if err != nil {
		return nil, sarvam.ErrSTTInvalidStream
	}
	return stt.OpenSTT(ctx)
}

func (providers *lazySarvamProviders) StreamChat(ctx context.Context, request sarvam.ChatRequest, handler sarvam.ChatEventHandler) (sarvam.ChatResult, error) {
	snapshot, err := providers.snapshot()
	if err != nil {
		return sarvam.ChatResult{}, err
	}
	client, err := resolveSarvamClient(ctx, snapshot)
	if err != nil {
		return sarvam.ChatResult{}, err
	}
	chat, err := sarvam.NewChatClient(client)
	if err != nil {
		return sarvam.ChatResult{}, sarvam.ErrChatClientRequired
	}
	return chat.StreamChat(ctx, request, handler)
}

func (providers *lazySarvamProviders) OpenTTS(ctx context.Context, language string) (sarvam.TTSStream, error) {
	snapshot, err := providers.snapshot()
	if err != nil {
		return nil, err
	}
	client, err := resolveSarvamClient(ctx, snapshot)
	if err != nil {
		return nil, err
	}
	tts, err := sarvam.NewTTSClient(client, snapshot.websocket)
	if err != nil {
		return nil, sarvam.ErrTTSInvalidStream
	}
	return tts.OpenTTS(ctx, language)
}

func (providers *lazySarvamProviders) resolveClient(ctx context.Context) (*sarvam.Client, error) {
	snapshot, err := providers.snapshot()
	if err != nil {
		return nil, err
	}
	return resolveSarvamClient(ctx, snapshot)
}

func (providers *lazySarvamProviders) snapshot() (sarvamProviderSnapshot, error) {
	if providers == nil {
		return sarvamProviderSnapshot{}, ErrInvalidRuntimeEnvironment
	}
	providers.mu.RLock()
	defer providers.mu.RUnlock()
	if providers.closed || providers.config == nil || providers.resolver == nil || providers.http == nil ||
		providers.websocket == nil || providers.transport == nil {
		return sarvamProviderSnapshot{}, ErrInvalidRuntimeEnvironment
	}
	return sarvamProviderSnapshot{
		config: providers.config, resolver: providers.resolver, http: providers.http,
		websocket: providers.websocket, transport: providers.transport,
	}, nil
}

func resolveSarvamClient(ctx context.Context, snapshot sarvamProviderSnapshot) (*sarvam.Client, error) {
	if ctx == nil || snapshot.config == nil || snapshot.resolver == nil || snapshot.http == nil ||
		snapshot.websocket == nil || snapshot.transport == nil {
		return nil, ErrInvalidRuntimeEnvironment
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	resolved, err := snapshot.resolver.ResolveProvider(ctx, snapshot.config, config.SecretSarvam)
	if err != nil || resolved == nil || resolved.Sarvam.BaseURL != sarvam.DefaultBaseURL {
		return nil, ErrInvalidRuntimeEnvironment
	}
	providerConfig := sarvam.Config{APIKey: resolved.Sarvam.APIKey, BaseURL: sarvam.DefaultBaseURL}
	resolved.Sarvam.APIKey = ""
	client, err := sarvam.NewClient(providerConfig, snapshot.http)
	providerConfig.APIKey = ""
	if err != nil {
		return nil, ErrInvalidRuntimeEnvironment
	}
	return client, nil
}

func (providers *lazySarvamProviders) Close() error {
	if providers == nil {
		return nil
	}
	providers.closeOnce.Do(func() {
		providers.mu.Lock()
		transport := providers.transport
		providers.closed = true
		providers.http = nil
		providers.websocket = nil
		providers.transport = nil
		providers.resolver = nil
		providers.config = nil
		providers.mu.Unlock()
		if transport != nil {
			transport.CloseIdleConnections()
		}
	})
	return nil
}

func (*lazySarvamProviders) String() string   { return "Sarvam providers{redacted}" }
func (*lazySarvamProviders) GoString() string { return "Sarvam providers{redacted}" }
func (*lazySarvamProviders) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{}{})
}

var (
	_ sarvam.STTOpener    = (*lazySarvamProviders)(nil)
	_ sarvam.ChatStreamer = (*lazySarvamProviders)(nil)
	_ sarvam.TTSOpener    = (*lazySarvamProviders)(nil)
	_ io.Closer           = (*lazySarvamProviders)(nil)
)
