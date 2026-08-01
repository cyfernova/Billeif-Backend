package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	"invoice-backend/internal/app"
	"invoice-backend/internal/config"
	"invoice-backend/internal/middleware"
	postgresrepo "invoice-backend/internal/repositories/postgres"
	"invoice-backend/internal/services"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-lambda-go/events"
	"github.com/aws/aws-lambda-go/lambda"
	"github.com/aws/aws-sdk-go-v2/aws"
	awslambda "github.com/aws/aws-sdk-go-v2/service/lambda"
	lambdatypes "github.com/aws/aws-sdk-go-v2/service/lambda/types"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

var (
	wsInitOnce  sync.Once
	wsCfg       *config.Config
	wsSvc       *services.WebSocketConnectionService
	wsAuthSvc   businessAccessChecker
	voiceCfg    *config.LambdaVoiceConfig
	voiceStore  *services.VoiceLambdaStore
	voicePoster services.VoicePoster
	voiceLambda *awslambda.Client
	wsLog       *logger.Logger
	wsInitErr   error
)

type businessAccessChecker interface {
	UserHasBusinessAccess(ctx context.Context, userID, businessID string) bool
}

func initWSRuntime() {
	lambdaVoiceCfg, err := config.LoadLambdaVoiceConfig(true)
	if err != nil {
		wsInitErr = fmt.Errorf("load lambda voice config: %w", err)
		return
	}
	cfg := loadWSConfig(lambdaVoiceCfg)

	log := logger.NewWithConfig(logger.Config{
		Environment: cfg.Environment,
		Level:       cfg.Logging.Level,
		Format:      cfg.Logging.Format,
	}).Named("ws_lambda")

	appCfg, err := config.LoadForProfile(config.ProfileWebSocket)
	if err != nil {
		wsInitErr = fmt.Errorf("load app config for websocket auth: %w", err)
		return
	}
	awsCfg, err := awsclients.New(context.Background(), cfg.AWS, log)
	if err != nil {
		wsInitErr = fmt.Errorf("init aws clients: %w", err)
		return
	}
	resolver, err := config.NewRuntimeResolver(config.RuntimeResolverOptions{
		Clients: config.RuntimeResolvers{
			Secrets: secretsmanager.NewFromConfig(awsCfg.SDKConfig),
			SSM:     awsCfg.SSM,
		},
		SecretIdentifiers: []string{appCfg.Secrets.Database},
		ParameterNames:    []string{appCfg.SSM.DatabaseHostParam},
	})
	if err != nil {
		wsInitErr = fmt.Errorf("initialize websocket credential resolver: %w", err)
		return
	}
	authSvc, err := initWebSocketBusinessAuth(appCfg, resolver, log)
	if err != nil {
		wsInitErr = fmt.Errorf("initialize websocket business auth: %w", err)
		return
	}

	svc := services.NewWebSocketConnectionService(cfg, awsCfg, log)
	if svc == nil {
		wsInitErr = fmt.Errorf("initialize websocket connection service")
		return
	}

	store, err := services.NewVoiceLambdaStore(awsCfg.DynamoDB, lambdaVoiceCfg.VoiceSessionsTable)
	if err != nil {
		wsInitErr = fmt.Errorf("initialize voice session store: %w", err)
		return
	}
	poster, err := services.NewAPIGatewayVoicePoster(awsCfg.SDKConfig, lambdaVoiceCfg.WebSocket.APIEndpoint, lambdaVoiceCfg.MaxOutboundChunkBytes)
	if err != nil {
		wsInitErr = fmt.Errorf("initialize voice websocket poster: %w", err)
		return
	}

	wsCfg = cfg
	wsSvc = svc
	wsAuthSvc = authSvc
	voiceCfg = lambdaVoiceCfg
	voiceStore = store
	voicePoster = poster
	voiceLambda = awslambda.NewFromConfig(awsCfg.SDKConfig)
	wsLog = log
}

func handleWebSocket(ctx context.Context, req events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	wsInitOnce.Do(initWSRuntime)
	if wsInitErr != nil {
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: wsInitErr.Error()}, nil
	}

	connectionID := req.RequestContext.ConnectionID
	switch req.RequestContext.RouteKey {
	case "$connect":
		authToken := extractAuthToken(req)
		if authToken == "" {
			return events.APIGatewayProxyResponse{StatusCode: 401, Body: "missing auth token"}, nil
		}

		claims, err := parseClaims(authToken)
		if err != nil {
			wsLog.Warn("websocket connect auth failed", "error", err)
			return events.APIGatewayProxyResponse{StatusCode: 401, Body: "invalid auth token"}, nil
		}

		businessID, response, ok := authorizeWebSocketBusinessScope(ctx, req, claims, connectionID)
		if !ok {
			return response, nil
		}

		if err := wsSvc.RegisterConnectionWithBusiness(ctx, connectionID, claims.Subject, businessID); err != nil {
			wsLog.Error("failed to register websocket connection", "connection_id", connectionID, "error", err)
			return events.APIGatewayProxyResponse{StatusCode: 500, Body: "failed to register connection"}, nil
		}

		return events.APIGatewayProxyResponse{StatusCode: 200, Body: "connected"}, nil

	case "$disconnect":
		if err := stopVoiceSessionForConnection(ctx, connectionID); err != nil {
			wsLog.Warn("failed to stop voice session during websocket disconnect", "connection_id", connectionID, "error", err)
		}
		if err := wsSvc.UnregisterConnection(ctx, connectionID); err != nil {
			wsLog.Warn("failed to unregister websocket connection", "connection_id", connectionID, "error", err)
		}
		return events.APIGatewayProxyResponse{StatusCode: 200, Body: "disconnected"}, nil

	case services.VoiceActionStart:
		if response, disabled := voicePilotDisabledResponse(voiceCfg); disabled {
			return response, nil
		}
		return handleVoiceStart(ctx, connectionID, req)

	case services.VoiceActionAudio:
		if response, disabled := voicePilotDisabledResponse(voiceCfg); disabled {
			return response, nil
		}
		return handleVoiceAudio(ctx, connectionID, req)

	case services.VoiceActionControl:
		if response, disabled := voicePilotDisabledResponse(voiceCfg); disabled {
			return response, nil
		}
		return handleVoiceControl(ctx, connectionID, req)

	case "$default":
		// Incoming messages can be routed here for custom handling if needed.
		return events.APIGatewayProxyResponse{StatusCode: 200, Body: "ok"}, nil

	default:
		return events.APIGatewayProxyResponse{StatusCode: 200, Body: "ok"}, nil
	}
}

func voicePilotDisabledResponse(cfg *config.LambdaVoiceConfig) (events.APIGatewayProxyResponse, bool) {
	if cfg != nil && cfg.Enabled {
		return events.APIGatewayProxyResponse{}, false
	}
	return events.APIGatewayProxyResponse{StatusCode: 503, Body: "voice pilot is disabled"}, true
}

func parseClaims(token string) (*middleware.CognitoClaims, error) {
	if strings.Contains(strings.ToLower(token), "bearer ") {
		return middleware.ValidateCognitoAuthorization(wsCfg.Cognito, token)
	}
	return middleware.ValidateCognitoToken(wsCfg.Cognito, token)
}

func initWebSocketBusinessAuth(cfg *config.Config, resolver *config.RuntimeResolver, log *logger.Logger) (*services.BusinessAuthService, error) {
	db, err := app.OpenDatabase(cfg, resolver, log)
	if err != nil {
		return nil, fmt.Errorf("connect database: %w", err)
	}

	return services.NewBusinessAuthService(
		db,
		postgresrepo.NewBusinessRepository(db),
		postgresrepo.NewTeamMemberRepository(db),
		log,
	), nil
}

func authorizeWebSocketBusinessScope(ctx context.Context, req events.APIGatewayWebsocketProxyRequest, claims *middleware.CognitoClaims, connectionID string) (string, events.APIGatewayProxyResponse, bool) {
	if claims == nil {
		return "", events.APIGatewayProxyResponse{StatusCode: 401, Body: "invalid auth token"}, false
	}

	requestedBusinessID := firstNonEmpty(req.QueryStringParameters["business_id"], req.Headers["business_id"], req.Headers["x-business-id"], claims.BusinessID)
	if requestedBusinessID == "" {
		wsLog.Warn("websocket connect missing business scope", "connection_id", connectionID, "user_id", claims.Subject)
		return "", events.APIGatewayProxyResponse{StatusCode: 403, Body: "business scope required"}, false
	}
	if claims.BusinessID != "" && requestedBusinessID != claims.BusinessID {
		wsLog.Warn("websocket connect business mismatch", "connection_id", connectionID, "user_id", claims.Subject)
		return "", events.APIGatewayProxyResponse{StatusCode: 403, Body: "business_id does not match authenticated scope"}, false
	}
	if wsAuthSvc == nil {
		wsLog.Error("websocket business auth is not configured", "connection_id", connectionID, "user_id", claims.Subject)
		return "", events.APIGatewayProxyResponse{StatusCode: 500, Body: "business authorization is not configured"}, false
	}
	if !wsAuthSvc.UserHasBusinessAccess(ctx, claims.Subject, requestedBusinessID) {
		wsLog.Warn("websocket connect business access denied", "connection_id", connectionID, "user_id", claims.Subject, "business_id", requestedBusinessID)
		return "", events.APIGatewayProxyResponse{StatusCode: 403, Body: "access denied to this business"}, false
	}
	return requestedBusinessID, events.APIGatewayProxyResponse{}, true
}

func extractAuthToken(req events.APIGatewayWebsocketProxyRequest) string {
	if h, ok := req.Headers["Authorization"]; ok && h != "" {
		return h
	}
	if h, ok := req.Headers["authorization"]; ok && h != "" {
		return h
	}
	if t, ok := req.QueryStringParameters["access_token"]; ok && t != "" {
		return t
	}
	if t, ok := req.QueryStringParameters["authorization"]; ok && t != "" {
		return t
	}
	if t, ok := req.QueryStringParameters["token"]; ok && t != "" {
		return t
	}
	return ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func stripBearerPrefix(token string) string {
	trimmed := strings.TrimSpace(token)
	parts := strings.SplitN(trimmed, " ", 2)
	if len(parts) == 2 && strings.EqualFold(parts[0], "bearer") {
		return strings.TrimSpace(parts[1])
	}
	return trimmed
}

func handleVoiceStart(ctx context.Context, connectionID string, req events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	connection, err := wsSvc.GetConnection(ctx, connectionID)
	if err != nil {
		wsLog.Error("failed to load websocket connection for voice start", "connection_id", connectionID, "error", err)
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "failed to load connection"}, nil
	}
	if connection == nil {
		return events.APIGatewayProxyResponse{StatusCode: 401, Body: "connection is not registered"}, nil
	}

	var start services.VoiceStartRequest
	if err := decodeWebSocketBody(req, &start); err != nil {
		return events.APIGatewayProxyResponse{StatusCode: 400, Body: err.Error()}, nil
	}
	start.BusinessID = strings.TrimSpace(start.BusinessID)
	if start.BusinessID == "" {
		return events.APIGatewayProxyResponse{StatusCode: 400, Body: "business_id is required"}, nil
	}
	if connection.BusinessID == "" || start.BusinessID != connection.BusinessID {
		return events.APIGatewayProxyResponse{StatusCode: 403, Body: "business_id does not match authenticated scope"}, nil
	}
	workerAccessToken := firstNonEmpty(start.AccessToken, extractAuthToken(req))
	if workerAccessToken != "" {
		workerClaims, err := parseClaims(workerAccessToken)
		if err != nil || workerClaims.Subject != connection.UserID {
			wsLog.Warn("voice start worker token rejected", "connection_id", connectionID, "user_id", connection.UserID)
			return events.APIGatewayProxyResponse{StatusCode: 401, Body: "invalid voice access token"}, nil
		}
	}

	if active, err := activeVoiceSessionForStart(ctx, connectionID, start.BusinessID); err != nil {
		wsLog.Error("failed to check active voice session", "connection_id", connectionID, "error", err)
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "failed to check active voice session"}, nil
	} else if active != nil {
		return respondToActiveVoiceSessionStart(ctx, connectionID, active), nil
	}

	session := services.NewVoiceLambdaSession(start, connectionID, connection.UserID, voiceCfg.VoiceRealtime.MaxSessionSeconds)
	if err := voiceStore.CreateSession(ctx, session, voiceCfg.VoiceRealtime.MaxConcurrentSessionsPerUser); err != nil {
		if active, activeErr := activeVoiceSessionForStart(ctx, connectionID, start.BusinessID); activeErr == nil && active != nil {
			return respondToActiveVoiceSessionStart(ctx, connectionID, active), nil
		}
		_ = postVoiceError(ctx, connectionID, "voice_session_limit", err.Error())
		return events.APIGatewayProxyResponse{StatusCode: 409, Body: "voice session could not be started"}, nil
	}

	payload, _ := json.Marshal(services.VoiceSessionWorkerRequest{
		SessionID:   session.SessionID,
		AccessToken: stripBearerPrefix(workerAccessToken),
	})
	_, err = voiceLambda.Invoke(ctx, &awslambda.InvokeInput{
		FunctionName:   aws.String(voiceCfg.SessionWorkerFunctionName),
		InvocationType: lambdatypes.InvocationTypeEvent,
		Payload:        payload,
	})
	if err != nil {
		_ = voiceStore.CompleteSession(ctx, session.SessionID, services.VoiceSessionStatusError)
		_ = postVoiceError(ctx, connectionID, "voice_worker_start_failed", "Could not start realtime voice worker")
		wsLog.Error("failed to invoke voice session worker", "session_id", session.SessionID, "error", err)
		return events.APIGatewayProxyResponse{StatusCode: 500, Body: "failed to start voice worker"}, nil
	}

	_ = postVoiceEvent(ctx, connectionID, services.RealtimeAppEvent{
		Type: services.VoiceOutboundSessionStarted,
		Data: map[string]string{"session_id": session.SessionID},
	})
	return events.APIGatewayProxyResponse{StatusCode: 200, Body: "voice session starting"}, nil
}

func activeVoiceSessionForStart(ctx context.Context, connectionID, businessID string) (*services.VoiceLambdaSession, error) {
	session, err := voiceStore.FindActiveSessionByConnection(ctx, connectionID)
	if err != nil || session == nil {
		return nil, err
	}
	if session.ConnectionID != strings.TrimSpace(connectionID) || session.BusinessID != strings.TrimSpace(businessID) {
		return nil, nil
	}
	if session.Status == services.VoiceSessionStatusClosed || session.Status == services.VoiceSessionStatusError {
		_ = voiceStore.CompleteSession(ctx, session.SessionID, session.Status)
		return nil, nil
	}
	return session, nil
}

func respondToActiveVoiceSessionStart(ctx context.Context, connectionID string, session *services.VoiceLambdaSession) events.APIGatewayProxyResponse {
	if canReuseVoiceSessionForStart(session, connectionID, session.BusinessID) {
		acknowledgeVoiceSessionStart(ctx, connectionID, session)
		return events.APIGatewayProxyResponse{StatusCode: 200, Body: "voice session already active"}
	}
	if session.Status == services.VoiceSessionStatusStopping {
		_ = postVoiceEvent(ctx, connectionID, services.RealtimeAppEvent{Type: services.VoiceOutboundSessionClosed})
		return events.APIGatewayProxyResponse{StatusCode: 200, Body: "voice session stopping"}
	}
	return events.APIGatewayProxyResponse{StatusCode: 409, Body: "voice session is not reusable"}
}

func canReuseVoiceSessionForStart(session *services.VoiceLambdaSession, connectionID, businessID string) bool {
	if session == nil {
		return false
	}
	if session.ConnectionID != strings.TrimSpace(connectionID) || session.BusinessID != strings.TrimSpace(businessID) {
		return false
	}
	return session.Status == services.VoiceSessionStatusStarting || session.Status == services.VoiceSessionStatusRunning
}

func acknowledgeVoiceSessionStart(ctx context.Context, connectionID string, session *services.VoiceLambdaSession) {
	_ = postVoiceEvent(ctx, connectionID, services.RealtimeAppEvent{
		Type: services.VoiceOutboundSessionStarted,
		Data: map[string]string{"session_id": session.SessionID},
	})
	if session.Status == services.VoiceSessionStatusRunning {
		_ = postVoiceEvent(ctx, connectionID, services.RealtimeAppEvent{Type: services.AppEventReady})
	}
}

func handleVoiceAudio(ctx context.Context, connectionID string, req events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	var audio services.VoiceAudioRequest
	if err := decodeWebSocketBody(req, &audio); err != nil {
		_ = postVoiceError(ctx, connectionID, "invalid_voice_audio", err.Error())
		return events.APIGatewayProxyResponse{StatusCode: 400, Body: err.Error()}, nil
	}
	session, response := resolveVoiceSession(ctx, connectionID, audio.SessionID)
	if response != nil {
		return *response, nil
	}
	if audio.SampleRate != voiceCfg.VoiceRealtime.InputSampleRate {
		_ = postVoiceError(ctx, connectionID, "voice_sample_rate_mismatch", "sample_rate does not match configured voice input sample rate")
		return events.APIGatewayProxyResponse{StatusCode: 400, Body: "sample_rate does not match configured voice input sample rate"}, nil
	}
	if err := voiceStore.EnqueueAudio(ctx, session.SessionID, audio.Sequence, audio.AudioB64, audio.SampleRate, voiceCfg.VoiceRealtime.MaxFrameBytes, voiceCfg.EventTTLSeconds); err != nil {
		_ = postVoiceError(ctx, connectionID, "invalid_voice_audio", err.Error())
		return events.APIGatewayProxyResponse{StatusCode: 400, Body: err.Error()}, nil
	}
	return events.APIGatewayProxyResponse{StatusCode: 200, Body: "queued"}, nil
}

func handleVoiceControl(ctx context.Context, connectionID string, req events.APIGatewayWebsocketProxyRequest) (events.APIGatewayProxyResponse, error) {
	var control services.VoiceControlRequest
	if err := decodeWebSocketBody(req, &control); err != nil {
		_ = postVoiceError(ctx, connectionID, "invalid_voice_control", err.Error())
		return events.APIGatewayProxyResponse{StatusCode: 400, Body: err.Error()}, nil
	}
	session, response := resolveVoiceSession(ctx, connectionID, control.SessionID)
	if response != nil {
		return *response, nil
	}
	event := services.RealtimeVoiceControlEvent{
		Type:            strings.TrimSpace(control.Type),
		BusinessID:      strings.TrimSpace(control.BusinessID),
		ConversationID:  strings.TrimSpace(control.ConversationID),
		VisibleMessages: control.VisibleMessages,
		Metadata:        control.Metadata,
	}
	if err := voiceStore.EnqueueControl(ctx, session.SessionID, control.Sequence, event, voiceCfg.EventTTLSeconds); err != nil {
		_ = postVoiceError(ctx, connectionID, "invalid_voice_control", err.Error())
		return events.APIGatewayProxyResponse{StatusCode: 400, Body: err.Error()}, nil
	}
	if event.Type == "stop" {
		_ = voiceStore.UpdateSessionStatus(ctx, session.SessionID, services.VoiceSessionStatusStopping)
	}
	return events.APIGatewayProxyResponse{StatusCode: 200, Body: "queued"}, nil
}

func stopVoiceSessionForConnection(ctx context.Context, connectionID string) error {
	if voiceStore == nil {
		return nil
	}
	session, err := voiceStore.FindActiveSessionByConnection(ctx, connectionID)
	if err != nil || session == nil {
		return err
	}
	return voiceStore.EnqueueControl(ctx, session.SessionID, time.Now().UnixNano(), services.RealtimeVoiceControlEvent{Type: "stop"}, voiceCfg.EventTTLSeconds)
}

func resolveVoiceSession(ctx context.Context, connectionID, requestedSessionID string) (*services.VoiceLambdaSession, *events.APIGatewayProxyResponse) {
	var session *services.VoiceLambdaSession
	var err error
	if strings.TrimSpace(requestedSessionID) != "" {
		session, err = voiceStore.GetSession(ctx, requestedSessionID)
	} else {
		session, err = voiceStore.FindActiveSessionByConnection(ctx, connectionID)
	}
	if err != nil {
		wsLog.Error("failed to resolve voice session", "connection_id", connectionID, "error", err)
		response := events.APIGatewayProxyResponse{StatusCode: 500, Body: "failed to resolve voice session"}
		return nil, &response
	}
	if session == nil || session.ConnectionID != connectionID {
		response := events.APIGatewayProxyResponse{StatusCode: 404, Body: "active voice session not found"}
		return nil, &response
	}
	return session, nil
}

func decodeWebSocketBody(req events.APIGatewayWebsocketProxyRequest, target interface{}) error {
	body := []byte(req.Body)
	if req.IsBase64Encoded {
		decoded, err := base64.StdEncoding.DecodeString(req.Body)
		if err != nil {
			return fmt.Errorf("request body is not valid base64")
		}
		body = decoded
	}
	if len(body) == 0 {
		return fmt.Errorf("request body is required")
	}
	if err := json.Unmarshal(body, target); err != nil {
		return fmt.Errorf("request body must be valid JSON")
	}
	return nil
}

func postVoiceError(ctx context.Context, connectionID, code, message string) error {
	return postVoiceEvent(ctx, connectionID, services.RealtimeAppEvent{Type: services.AppEventError, Code: code, Message: message})
}

func postVoiceEvent(ctx context.Context, connectionID string, event services.RealtimeAppEvent) error {
	if voicePoster == nil {
		return fmt.Errorf("voice poster is not configured")
	}
	return voicePoster.PostEvent(ctx, connectionID, event)
}

func loadWSConfig(voice *config.LambdaVoiceConfig) *config.Config {
	c := &config.Config{
		Environment: voice.Environment,
		Logging: config.LoggingConfig{
			Level:  voice.LogLevel,
			Format: voice.LogFormat,
		},
		AWS:       voice.AWS,
		Cognito:   voice.Cognito,
		WebSocket: voice.WebSocket,
	}

	return c
}

func main() {
	lambda.Start(handleWebSocket)
}
