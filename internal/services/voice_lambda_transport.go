package services

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi"
	"github.com/aws/aws-sdk-go-v2/service/apigatewaymanagementapi/types"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamotypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/gorilla/websocket"
)

const (
	VoiceActionStart   = "voice.start"
	VoiceActionAudio   = "voice.audio"
	VoiceActionControl = "voice.control"

	VoiceOutboundAudioChunk     = "audio_chunk"
	VoiceOutboundSessionStarted = "session_started"
	VoiceOutboundSessionClosed  = "closed"
	VoiceOutboundSessionExpiry  = "session_expiring"

	VoiceSessionStatusStarting = "starting"
	VoiceSessionStatusRunning  = "running"
	VoiceSessionStatusStopping = "stopping"
	VoiceSessionStatusClosed   = "closed"
	VoiceSessionStatusError    = "error"

	VoiceEventKindAudio   = "audio"
	VoiceEventKindControl = "control"
)

type VoiceStartRequest struct {
	Action          string                `json:"action"`
	BusinessID      string                `json:"business_id"`
	ConversationID  string                `json:"conversation_id,omitempty"`
	Language        string                `json:"language,omitempty"`
	Voice           string                `json:"voice,omitempty"`
	VisibleMessages []RealtimeHistoryItem `json:"visible_messages,omitempty"`
}

type VoiceAudioRequest struct {
	Action     string `json:"action"`
	SessionID  string `json:"session_id,omitempty"`
	Sequence   int64  `json:"seq"`
	AudioB64   string `json:"audio_b64"`
	SampleRate int    `json:"sample_rate"`
}

type VoiceControlRequest struct {
	Action          string                 `json:"action"`
	SessionID       string                 `json:"session_id,omitempty"`
	Sequence        int64                  `json:"seq"`
	Type            string                 `json:"type"`
	BusinessID      string                 `json:"business_id,omitempty"`
	ConversationID  string                 `json:"conversation_id,omitempty"`
	VisibleMessages []RealtimeHistoryItem  `json:"visible_messages,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

type VoiceSessionWorkerRequest struct {
	SessionID string `json:"session_id"`
}

type VoiceLambdaSession struct {
	PK             string                `dynamodbav:"pk"`
	SK             string                `dynamodbav:"sk"`
	SessionID      string                `dynamodbav:"session_id"`
	ConnectionID   string                `dynamodbav:"connection_id"`
	UserID         string                `dynamodbav:"user_id"`
	BusinessID     string                `dynamodbav:"business_id"`
	ConversationID string                `dynamodbav:"conversation_id,omitempty"`
	Language       string                `dynamodbav:"language,omitempty"`
	Voice          string                `dynamodbav:"voice,omitempty"`
	History        []RealtimeHistoryItem `dynamodbav:"history,omitempty"`
	Status         string                `dynamodbav:"session_status"`
	CreatedAt      int64                 `dynamodbav:"created_at"`
	UpdatedAt      int64                 `dynamodbav:"updated_at"`
	ExpiresAt      int64                 `dynamodbav:"expires_at"`
}

type VoiceLambdaEvent struct {
	PK         string                    `dynamodbav:"pk"`
	SK         string                    `dynamodbav:"sk"`
	SessionID  string                    `dynamodbav:"session_id"`
	Sequence   int64                     `dynamodbav:"sequence"`
	Kind       string                    `dynamodbav:"kind"`
	AudioB64   string                    `dynamodbav:"audio_b64,omitempty"`
	SampleRate int                       `dynamodbav:"sample_rate,omitempty"`
	Control    RealtimeVoiceControlEvent `dynamodbav:"control,omitempty"`
	CreatedAt  int64                     `dynamodbav:"created_at"`
	ExpiresAt  int64                     `dynamodbav:"expires_at"`
}

type voiceConnectionSession struct {
	PK           string `dynamodbav:"pk"`
	SK           string `dynamodbav:"sk"`
	ConnectionID string `dynamodbav:"connection_id"`
	SessionID    string `dynamodbav:"session_id"`
	ExpiresAt    int64  `dynamodbav:"expires_at"`
}

type VoiceLambdaStore struct {
	db    *dynamodb.Client
	table string
}

func NewVoiceLambdaStore(db *dynamodb.Client, table string) (*VoiceLambdaStore, error) {
	if db == nil {
		return nil, fmt.Errorf("dynamodb client is required")
	}
	if strings.TrimSpace(table) == "" {
		return nil, fmt.Errorf("VOICE_SESSIONS_TABLE is required")
	}
	return &VoiceLambdaStore{db: db, table: strings.TrimSpace(table)}, nil
}

func NewVoiceLambdaSession(req VoiceStartRequest, connectionID, userID string, maxSessionSeconds int) VoiceLambdaSession {
	now := time.Now().UTC()
	sessionID := fmt.Sprintf("%s-%d", strings.TrimSpace(connectionID), now.UnixNano())
	expiresAt := now.Add(time.Duration(maxSessionSeconds) * time.Second).Unix()
	return VoiceLambdaSession{
		PK:             sessionPK(sessionID),
		SK:             "META",
		SessionID:      sessionID,
		ConnectionID:   strings.TrimSpace(connectionID),
		UserID:         strings.TrimSpace(userID),
		BusinessID:     strings.TrimSpace(req.BusinessID),
		ConversationID: strings.TrimSpace(req.ConversationID),
		Language:       strings.TrimSpace(req.Language),
		Voice:          strings.TrimSpace(req.Voice),
		History:        req.VisibleMessages,
		Status:         VoiceSessionStatusStarting,
		CreatedAt:      now.UnixMilli(),
		UpdatedAt:      now.UnixMilli(),
		ExpiresAt:      expiresAt,
	}
}

func (s *VoiceLambdaStore) CreateSession(ctx context.Context, session VoiceLambdaSession, maxConcurrent int) error {
	if strings.TrimSpace(session.SessionID) == "" || strings.TrimSpace(session.ConnectionID) == "" || strings.TrimSpace(session.UserID) == "" || strings.TrimSpace(session.BusinessID) == "" {
		return fmt.Errorf("session_id, connection_id, user_id, and business_id are required")
	}
	if maxConcurrent <= 0 {
		return fmt.Errorf("max concurrent sessions must be positive")
	}

	session.PK = sessionPK(session.SessionID)
	session.SK = "META"
	sessionItem, err := attributevalue.MarshalMap(session)
	if err != nil {
		return fmt.Errorf("marshal voice session: %w", err)
	}

	connection := voiceConnectionSession{
		PK:           connectionPK(session.ConnectionID),
		SK:           "ACTIVE",
		ConnectionID: session.ConnectionID,
		SessionID:    session.SessionID,
		ExpiresAt:    session.ExpiresAt,
	}
	connectionItem, err := attributevalue.MarshalMap(connection)
	if err != nil {
		return fmt.Errorf("marshal voice connection session: %w", err)
	}

	_, err = s.db.TransactWriteItems(ctx, &dynamodb.TransactWriteItemsInput{
		TransactItems: []dynamotypes.TransactWriteItem{
			{
				Put: &dynamotypes.Put{
					TableName:           aws.String(s.table),
					Item:                sessionItem,
					ConditionExpression: aws.String("attribute_not_exists(pk)"),
				},
			},
			{
				Put: &dynamotypes.Put{
					TableName:           aws.String(s.table),
					Item:                connectionItem,
					ConditionExpression: aws.String("attribute_not_exists(pk)"),
				},
			},
			{
				Update: &dynamotypes.Update{
					TableName: aws.String(s.table),
					Key: map[string]dynamotypes.AttributeValue{
						"pk": &dynamotypes.AttributeValueMemberS{Value: userPK(session.UserID)},
						"sk": &dynamotypes.AttributeValueMemberS{Value: "COUNTER"},
					},
					UpdateExpression:    aws.String("SET expires_at = :ttl ADD active_count :one"),
					ConditionExpression: aws.String("attribute_not_exists(active_count) OR active_count < :max"),
					ExpressionAttributeValues: map[string]dynamotypes.AttributeValue{
						":one": &dynamotypes.AttributeValueMemberN{Value: "1"},
						":max": &dynamotypes.AttributeValueMemberN{Value: fmt.Sprintf("%d", maxConcurrent)},
						":ttl": &dynamotypes.AttributeValueMemberN{Value: fmt.Sprintf("%d", session.ExpiresAt)},
					},
				},
			},
		},
	})
	if err != nil {
		return fmt.Errorf("create voice session: %w", err)
	}
	return nil
}

func (s *VoiceLambdaStore) GetSession(ctx context.Context, sessionID string) (*VoiceLambdaSession, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table),
		Key: map[string]dynamotypes.AttributeValue{
			"pk": &dynamotypes.AttributeValueMemberS{Value: sessionPK(sessionID)},
			"sk": &dynamotypes.AttributeValueMemberS{Value: "META"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get voice session: %w", err)
	}
	if len(out.Item) == 0 {
		return nil, nil
	}
	var session VoiceLambdaSession
	if err := attributevalue.UnmarshalMap(out.Item, &session); err != nil {
		return nil, fmt.Errorf("unmarshal voice session: %w", err)
	}
	return &session, nil
}

func (s *VoiceLambdaStore) FindActiveSessionByConnection(ctx context.Context, connectionID string) (*VoiceLambdaSession, error) {
	out, err := s.db.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: aws.String(s.table),
		Key: map[string]dynamotypes.AttributeValue{
			"pk": &dynamotypes.AttributeValueMemberS{Value: connectionPK(connectionID)},
			"sk": &dynamotypes.AttributeValueMemberS{Value: "ACTIVE"},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("get active voice connection: %w", err)
	}
	if len(out.Item) == 0 {
		return nil, nil
	}
	var active voiceConnectionSession
	if err := attributevalue.UnmarshalMap(out.Item, &active); err != nil {
		return nil, fmt.Errorf("unmarshal active voice connection: %w", err)
	}
	return s.GetSession(ctx, active.SessionID)
}

func (s *VoiceLambdaStore) UpdateSessionStatus(ctx context.Context, sessionID, status string) error {
	_, err := s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table),
		Key: map[string]dynamotypes.AttributeValue{
			"pk": &dynamotypes.AttributeValueMemberS{Value: sessionPK(sessionID)},
			"sk": &dynamotypes.AttributeValueMemberS{Value: "META"},
		},
		UpdateExpression: aws.String("SET session_status = :status, updated_at = :updated"),
		ExpressionAttributeValues: map[string]dynamotypes.AttributeValue{
			":status":  &dynamotypes.AttributeValueMemberS{Value: status},
			":updated": &dynamotypes.AttributeValueMemberN{Value: fmt.Sprintf("%d", time.Now().UTC().UnixMilli())},
		},
	})
	if err != nil {
		return fmt.Errorf("update voice session status: %w", err)
	}
	return nil
}

func (s *VoiceLambdaStore) CompleteSession(ctx context.Context, sessionID, status string) error {
	session, err := s.GetSession(ctx, sessionID)
	if err != nil || session == nil {
		return err
	}
	if session.Status == VoiceSessionStatusClosed || session.Status == VoiceSessionStatusError {
		return nil
	}

	now := time.Now().UTC().UnixMilli()
	_, err = s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table),
		Key: map[string]dynamotypes.AttributeValue{
			"pk": &dynamotypes.AttributeValueMemberS{Value: sessionPK(sessionID)},
			"sk": &dynamotypes.AttributeValueMemberS{Value: "META"},
		},
		UpdateExpression:    aws.String("SET session_status = :status, updated_at = :updated"),
		ConditionExpression: aws.String("session_status <> :closed AND session_status <> :error"),
		ExpressionAttributeValues: map[string]dynamotypes.AttributeValue{
			":status":  &dynamotypes.AttributeValueMemberS{Value: status},
			":updated": &dynamotypes.AttributeValueMemberN{Value: fmt.Sprintf("%d", now)},
			":closed":  &dynamotypes.AttributeValueMemberS{Value: VoiceSessionStatusClosed},
			":error":   &dynamotypes.AttributeValueMemberS{Value: VoiceSessionStatusError},
		},
	})
	if err != nil {
		var conditional *dynamotypes.ConditionalCheckFailedException
		if errors.As(err, &conditional) {
			return nil
		}
		return fmt.Errorf("complete voice session: %w", err)
	}

	_, _ = s.db.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: aws.String(s.table),
		Key: map[string]dynamotypes.AttributeValue{
			"pk": &dynamotypes.AttributeValueMemberS{Value: connectionPK(session.ConnectionID)},
			"sk": &dynamotypes.AttributeValueMemberS{Value: "ACTIVE"},
		},
	})

	_, _ = s.db.UpdateItem(ctx, &dynamodb.UpdateItemInput{
		TableName: aws.String(s.table),
		Key: map[string]dynamotypes.AttributeValue{
			"pk": &dynamotypes.AttributeValueMemberS{Value: userPK(session.UserID)},
			"sk": &dynamotypes.AttributeValueMemberS{Value: "COUNTER"},
		},
		UpdateExpression:    aws.String("ADD active_count :minus"),
		ConditionExpression: aws.String("active_count > :zero"),
		ExpressionAttributeValues: map[string]dynamotypes.AttributeValue{
			":minus": &dynamotypes.AttributeValueMemberN{Value: "-1"},
			":zero":  &dynamotypes.AttributeValueMemberN{Value: "0"},
		},
	})
	return nil
}

func (s *VoiceLambdaStore) EnqueueAudio(ctx context.Context, sessionID string, sequence int64, audioB64 string, sampleRate int, maxFrameBytes int, eventTTLSeconds int) error {
	if sequence <= 0 {
		return fmt.Errorf("audio sequence must be positive")
	}
	audio, err := base64.StdEncoding.DecodeString(audioB64)
	if err != nil {
		return fmt.Errorf("audio_b64 must be valid base64")
	}
	if len(audio) == 0 {
		return fmt.Errorf("audio frame is empty")
	}
	if maxFrameBytes <= 0 {
		return fmt.Errorf("max frame bytes must be positive")
	}
	if eventTTLSeconds <= 0 {
		return fmt.Errorf("event TTL seconds must be positive")
	}
	if len(audio) > maxFrameBytes {
		return fmt.Errorf("audio frame exceeds maximum size")
	}
	event := VoiceLambdaEvent{
		PK:         sessionPK(sessionID),
		SK:         eventSK(sequence),
		SessionID:  sessionID,
		Sequence:   sequence,
		Kind:       VoiceEventKindAudio,
		AudioB64:   audioB64,
		SampleRate: sampleRate,
		CreatedAt:  time.Now().UTC().UnixMilli(),
		ExpiresAt:  time.Now().UTC().Add(time.Duration(eventTTLSeconds) * time.Second).Unix(),
	}
	return s.putEvent(ctx, event)
}

func (s *VoiceLambdaStore) EnqueueControl(ctx context.Context, sessionID string, sequence int64, control RealtimeVoiceControlEvent, eventTTLSeconds int) error {
	if sequence <= 0 {
		return fmt.Errorf("control sequence must be positive")
	}
	if eventTTLSeconds <= 0 {
		return fmt.Errorf("event TTL seconds must be positive")
	}
	control.Type = strings.TrimSpace(control.Type)
	if _, err := ParseRealtimeVoiceControl(mustMarshal(control)); err != nil {
		return err
	}
	event := VoiceLambdaEvent{
		PK:        sessionPK(sessionID),
		SK:        eventSK(sequence),
		SessionID: sessionID,
		Sequence:  sequence,
		Kind:      VoiceEventKindControl,
		Control:   control,
		CreatedAt: time.Now().UTC().UnixMilli(),
		ExpiresAt: time.Now().UTC().Add(time.Duration(eventTTLSeconds) * time.Second).Unix(),
	}
	return s.putEvent(ctx, event)
}

func (s *VoiceLambdaStore) putEvent(ctx context.Context, event VoiceLambdaEvent) error {
	item, err := attributevalue.MarshalMap(event)
	if err != nil {
		return fmt.Errorf("marshal voice event: %w", err)
	}
	_, err = s.db.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(s.table),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(pk) AND attribute_not_exists(sk)"),
	})
	if err != nil {
		return fmt.Errorf("enqueue voice event: %w", err)
	}
	return nil
}

func (s *VoiceLambdaStore) ListEventsAfter(ctx context.Context, sessionID string, sequence int64) ([]VoiceLambdaEvent, error) {
	out, err := s.db.Query(ctx, &dynamodb.QueryInput{
		TableName:              aws.String(s.table),
		KeyConditionExpression: aws.String("pk = :pk AND sk > :sk"),
		ExpressionAttributeValues: map[string]dynamotypes.AttributeValue{
			":pk": &dynamotypes.AttributeValueMemberS{Value: sessionPK(sessionID)},
			":sk": &dynamotypes.AttributeValueMemberS{Value: eventSK(sequence)},
		},
	})
	if err != nil {
		return nil, fmt.Errorf("query voice events: %w", err)
	}
	events := make([]VoiceLambdaEvent, 0, len(out.Items))
	for _, item := range out.Items {
		var event VoiceLambdaEvent
		if err := attributevalue.UnmarshalMap(item, &event); err != nil {
			return nil, fmt.Errorf("unmarshal voice event: %w", err)
		}
		if event.Kind == VoiceEventKindAudio || event.Kind == VoiceEventKindControl {
			events = append(events, event)
		}
	}
	sort.Slice(events, func(i, j int) bool {
		return events[i].Sequence < events[j].Sequence
	})
	return events, nil
}

type VoicePoster interface {
	PostEvent(ctx context.Context, connectionID string, event RealtimeAppEvent) error
	PostAudio(ctx context.Context, connectionID string, audio []byte) error
}

type APIGatewayVoicePoster struct {
	mgmt             *apigatewaymanagementapi.Client
	maxOutboundBytes int
}

func NewAPIGatewayVoicePoster(awsCfg aws.Config, endpoint string, maxOutboundBytes int) (*APIGatewayVoicePoster, error) {
	if strings.TrimSpace(endpoint) == "" {
		return nil, fmt.Errorf("WEBSOCKET_API_ENDPOINT is required")
	}
	if maxOutboundBytes <= 0 {
		return nil, fmt.Errorf("VOICE_WS_MAX_OUTBOUND_CHUNK_BYTES must be positive")
	}
	mgmt := apigatewaymanagementapi.NewFromConfig(awsCfg, func(o *apigatewaymanagementapi.Options) {
		o.BaseEndpoint = aws.String(strings.TrimSpace(endpoint))
	})
	return &APIGatewayVoicePoster{mgmt: mgmt, maxOutboundBytes: maxOutboundBytes}, nil
}

func (p *APIGatewayVoicePoster) PostEvent(ctx context.Context, connectionID string, event RealtimeAppEvent) error {
	payload, err := json.Marshal(event)
	if err != nil {
		return err
	}
	return p.post(ctx, connectionID, payload)
}

func (p *APIGatewayVoicePoster) PostAudio(ctx context.Context, connectionID string, audio []byte) error {
	if len(audio) == 0 {
		return nil
	}
	for offset := 0; offset < len(audio); offset += p.maxOutboundBytes {
		end := offset + p.maxOutboundBytes
		if end > len(audio) {
			end = len(audio)
		}
		payload, err := json.Marshal(map[string]string{
			"type":      VoiceOutboundAudioChunk,
			"audio_b64": base64.StdEncoding.EncodeToString(audio[offset:end]),
		})
		if err != nil {
			return err
		}
		if err := p.post(ctx, connectionID, payload); err != nil {
			return err
		}
	}
	return nil
}

func (p *APIGatewayVoicePoster) post(ctx context.Context, connectionID string, payload []byte) error {
	_, err := p.mgmt.PostToConnection(ctx, &apigatewaymanagementapi.PostToConnectionInput{
		ConnectionId: aws.String(connectionID),
		Data:         payload,
	})
	if err != nil {
		var gone *types.GoneException
		if errors.As(err, &gone) {
			return fmt.Errorf("websocket connection is gone: %w", err)
		}
		return err
	}
	return nil
}

type VoiceDeepgramClient interface {
	Connect(ctx context.Context) error
	SendSettings(ctx context.Context, settings map[string]interface{}) error
	SendBinary(audio []byte) error
	SendRaw(messageType int, payload []byte) error
	ReadMessage() (int, []byte, error)
	Close() error
}

type VoiceDeepgramFactory func(cfg config.VoiceRealtimeConfig, log *logger.Logger) VoiceDeepgramClient

type VoiceLambdaSessionRunner struct {
	cfg                  config.VoiceRealtimeConfig
	store                *VoiceLambdaStore
	poster               VoicePoster
	pollInterval         time.Duration
	providerReadyTimeout time.Duration
	deepgram             VoiceDeepgramFactory
	log                  *logger.Logger
}

func NewVoiceLambdaSessionRunner(cfg config.VoiceRealtimeConfig, store *VoiceLambdaStore, poster VoicePoster, pollInterval time.Duration, providerReadyTimeout time.Duration, deepgram VoiceDeepgramFactory, log *logger.Logger) (*VoiceLambdaSessionRunner, error) {
	if err := cfg.ValidateForRuntime(); err != nil {
		return nil, err
	}
	if store == nil {
		return nil, fmt.Errorf("voice session store is required")
	}
	if poster == nil {
		return nil, fmt.Errorf("voice poster is required")
	}
	if pollInterval <= 0 {
		return nil, fmt.Errorf("voice event poll interval must be positive")
	}
	if providerReadyTimeout <= 0 {
		return nil, fmt.Errorf("voice provider ready timeout must be positive")
	}
	if deepgram == nil {
		deepgram = func(cfg config.VoiceRealtimeConfig, log *logger.Logger) VoiceDeepgramClient {
			return NewDeepgramVoiceAgentClient(cfg, log)
		}
	}
	if log == nil {
		log = logger.Global()
	}
	return &VoiceLambdaSessionRunner{
		cfg:                  cfg,
		store:                store,
		poster:               poster,
		pollInterval:         pollInterval,
		providerReadyTimeout: providerReadyTimeout,
		deepgram:             deepgram,
		log:                  log.Named("voice_lambda_session"),
	}, nil
}

func (r *VoiceLambdaSessionRunner) Run(ctx context.Context, sessionID string) error {
	session, err := r.store.GetSession(ctx, sessionID)
	if err != nil {
		return err
	}
	if session == nil {
		return fmt.Errorf("voice session %s not found", sessionID)
	}

	sessionCtx, cancel := context.WithTimeout(ctx, time.Duration(r.cfg.MaxSessionSeconds)*time.Second)
	defer cancel()
	defer func() {
		if err := r.store.CompleteSession(context.Background(), sessionID, VoiceSessionStatusClosed); err != nil {
			r.log.Warn("failed to complete voice session on close", "session_id", sessionID, "error", err)
		}
	}()

	log := r.log.With("session_id", sessionID, "connection_id", session.ConnectionID, "user_id", session.UserID, "business_id", session.BusinessID)
	dg := r.deepgram(r.cfg, log)
	if err := dg.Connect(sessionCtx); err != nil {
		_ = r.poster.PostEvent(ctx, session.ConnectionID, RealtimeAppEvent{Type: AppEventError, Code: "deepgram_connect_failed", Message: "Could not connect realtime voice agent"})
		_ = r.store.CompleteSession(context.Background(), sessionID, VoiceSessionStatusError)
		return err
	}
	defer dg.Close()

	if err := waitForLambdaDeepgramWelcome(sessionCtx, dg, r.providerReadyTimeout); err != nil {
		_ = r.poster.PostEvent(ctx, session.ConnectionID, RealtimeAppEvent{Type: AppEventError, Code: "deepgram_welcome_failed", Message: "Deepgram voice agent did not become ready"})
		_ = r.store.CompleteSession(context.Background(), sessionID, VoiceSessionStatusError)
		return err
	}

	settings := BuildDeepgramVoiceAgentSettings(r.cfg, DeepgramVoiceAgentSettingsOptions{
		SessionID:      session.SessionID,
		UserID:         session.UserID,
		BusinessID:     session.BusinessID,
		ConversationID: session.ConversationID,
		Language:       session.Language,
		Voice:          session.Voice,
		History:        session.History,
	})
	if err := dg.SendSettings(sessionCtx, settings); err != nil {
		_ = r.poster.PostEvent(ctx, session.ConnectionID, RealtimeAppEvent{Type: AppEventError, Code: "deepgram_settings_failed", Message: "Could not configure realtime voice agent"})
		_ = r.store.CompleteSession(context.Background(), sessionID, VoiceSessionStatusError)
		return err
	}

	if err := r.store.UpdateSessionStatus(ctx, sessionID, VoiceSessionStatusRunning); err != nil {
		return err
	}
	if err := r.poster.PostEvent(ctx, session.ConnectionID, RealtimeAppEvent{Type: VoiceOutboundSessionStarted, Data: map[string]string{"session_id": sessionID}}); err != nil {
		return err
	}
	if err := r.poster.PostEvent(ctx, session.ConnectionID, RealtimeAppEvent{Type: AppEventReady}); err != nil {
		return err
	}

	deepgramMessages := make(chan RealtimeDeepgramOutbound, 16)
	deepgramErrors := make(chan error, 1)
	go func() {
		for {
			messageType, payload, err := dg.ReadMessage()
			if err != nil {
				deepgramErrors <- err
				return
			}
			deepgramMessages <- RealtimeDeepgramOutbound{MessageType: messageType, Payload: payload}
		}
	}()

	pollTicker := time.NewTicker(r.pollInterval)
	defer pollTicker.Stop()
	keepAliveTicker := time.NewTicker(time.Duration(r.cfg.PingIntervalSeconds) * time.Second)
	defer keepAliveTicker.Stop()

	var lastSequence int64
	for {
		select {
		case <-sessionCtx.Done():
			_ = r.poster.PostEvent(context.Background(), session.ConnectionID, RealtimeAppEvent{Type: VoiceOutboundSessionExpiry})
			return nil
		case err := <-deepgramErrors:
			return err
		case message := <-deepgramMessages:
			if err := r.handleDeepgramMessage(sessionCtx, session, dg, message); err != nil {
				return err
			}
		case <-keepAliveTicker.C:
			_ = dg.SendRaw(websocket.TextMessage, mustMarshal(map[string]string{"type": "KeepAlive"}))
		case <-pollTicker.C:
			events, err := r.store.ListEventsAfter(sessionCtx, sessionID, lastSequence)
			if err != nil {
				return err
			}
			for _, event := range events {
				if event.Sequence > lastSequence {
					lastSequence = event.Sequence
				}
				stop, err := r.handleAppEvent(sessionCtx, session, dg, event)
				if err != nil {
					return err
				}
				if stop {
					_ = r.poster.PostEvent(context.Background(), session.ConnectionID, RealtimeAppEvent{Type: VoiceOutboundSessionClosed})
					return nil
				}
			}
		}
	}
}

func (r *VoiceLambdaSessionRunner) handleAppEvent(ctx context.Context, session *VoiceLambdaSession, dg VoiceDeepgramClient, event VoiceLambdaEvent) (bool, error) {
	switch event.Kind {
	case VoiceEventKindAudio:
		audio, err := base64.StdEncoding.DecodeString(event.AudioB64)
		if err != nil {
			return false, err
		}
		return false, dg.SendBinary(audio)
	case VoiceEventKindControl:
		switch event.Control.Type {
		case "start":
			return false, nil
		case "stop":
			return true, nil
		case "ping":
			return false, r.poster.PostEvent(ctx, session.ConnectionID, RealtimeAppEvent{Type: AppEventPong})
		case "interrupt":
			return false, r.poster.PostEvent(ctx, session.ConnectionID, RealtimeAppEvent{Type: AppEventInterrupted})
		case "client_context":
			if event.Control.BusinessID != "" && event.Control.BusinessID != session.BusinessID {
				_ = r.poster.PostEvent(ctx, session.ConnectionID, RealtimeAppEvent{Type: AppEventError, Code: "business_id_mismatch", Message: "client_context business_id does not match session"})
				return true, nil
			}
			return false, nil
		default:
			return false, fmt.Errorf("unsupported voice control %q", event.Control.Type)
		}
	default:
		return false, nil
	}
}

func (r *VoiceLambdaSessionRunner) handleDeepgramMessage(ctx context.Context, session *VoiceLambdaSession, dg VoiceDeepgramClient, message RealtimeDeepgramOutbound) error {
	switch message.MessageType {
	case websocket.BinaryMessage:
		return r.poster.PostAudio(ctx, session.ConnectionID, message.Payload)
	case websocket.TextMessage:
		event, ok, err := MapDeepgramJSONEvent(message.Payload)
		if err != nil {
			return err
		}
		if !ok {
			var envelope struct {
				Type string `json:"type"`
			}
			_ = json.Unmarshal(message.Payload, &envelope)
			if envelope.Type == "FunctionCallRequest" {
				return r.respondUnsupportedFunctionCall(ctx, dg, message.Payload)
			}
			return nil
		}
		if event.Type == "function_call_request" {
			return r.respondUnsupportedFunctionCall(ctx, dg, message.Payload)
		}
		return r.poster.PostEvent(ctx, session.ConnectionID, event)
	default:
		return nil
	}
}

func (r *VoiceLambdaSessionRunner) respondUnsupportedFunctionCall(ctx context.Context, dg VoiceDeepgramClient, payload []byte) error {
	var req DeepgramFunctionCallRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return err
	}
	for _, fn := range req.Functions {
		if err := dg.SendRaw(websocket.TextMessage, mustMarshal(map[string]interface{}{
			"type":        "FunctionCallResponse",
			"id":          fn.ID,
			"name":        fn.Name,
			"content":     `{"error":"unsupported_function"}`,
			"client_side": false,
		})); err != nil {
			return err
		}
	}
	return nil
}

func waitForLambdaDeepgramWelcome(ctx context.Context, dg VoiceDeepgramClient, timeout time.Duration) error {
	type readResult struct {
		messageType int
		payload     []byte
		err         error
	}
	results := make(chan readResult, 1)
	go func() {
		messageType, payload, err := dg.ReadMessage()
		results <- readResult{messageType: messageType, payload: payload, err: err}
	}()

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	var result readResult
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return fmt.Errorf("Deepgram voice agent did not become ready before timeout")
	case result = <-results:
		if result.err != nil {
			return result.err
		}
	}
	if result.messageType != websocket.TextMessage {
		return fmt.Errorf("unexpected Deepgram binary message before settings")
	}
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(result.payload, &envelope); err != nil {
		return err
	}
	if envelope.Type == "Welcome" {
		return nil
	}
	if envelope.Type == "Error" {
		event, _, _ := MapDeepgramJSONEvent(result.payload)
		return fmt.Errorf("Deepgram error before settings: %s", event.Message)
	}
	return fmt.Errorf("unexpected Deepgram event before settings: %s", envelope.Type)
}

func sessionPK(sessionID string) string {
	return "SESSION#" + strings.TrimSpace(sessionID)
}

func connectionPK(connectionID string) string {
	return "CONNECTION#" + strings.TrimSpace(connectionID)
}

func userPK(userID string) string {
	return "USER#" + strings.TrimSpace(userID)
}

func eventSK(sequence int64) string {
	return fmt.Sprintf("EVENT#%020d", sequence)
}

func mustMarshal(value interface{}) []byte {
	payload, _ := json.Marshal(value)
	return payload
}
