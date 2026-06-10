package services

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"

	"github.com/google/uuid"
	"github.com/gorilla/websocket"
)

type RealtimeVoiceSession struct {
	ID             string
	UserID         string
	BusinessID     string
	ConversationID string
	AccessToken    string

	AppConn      *websocket.Conn
	DeepgramConn *websocket.Conn

	Ctx    context.Context
	Cancel context.CancelFunc

	AppWrite      chan RealtimeAppOutbound
	DeepgramWrite chan RealtimeDeepgramOutbound

	StartedAt time.Time

	log *logger.Logger

	firstAppAudioLogged       atomic.Bool
	firstDeepgramAudioLogged  atomic.Bool
	firstAssistantAudioLogged atomic.Bool
	bytesAppToBackend         atomic.Int64
	bytesBackendToApp         atomic.Int64
}

type RealtimeVoiceSessionRequest struct {
	UserID         string
	BusinessID     string
	ConversationID string
	AccessToken    string
	Voice          string
	Language       string
}

type RealtimeVoiceService struct {
	cfg config.VoiceRealtimeConfig
	log *logger.Logger

	mu           sync.Mutex
	sessions     map[string]*RealtimeVoiceSession
	userSessions map[string]int
	metrics      *realtimeVoiceMetrics
	mcpBridge    *VoiceMCPBridge
}

type realtimeVoiceMetrics struct {
	activeSessions atomic.Int64
	totalSessions  atomic.Int64
	errorCount     atomic.Int64
}

func NewRealtimeVoiceService(cfg config.VoiceRealtimeConfig, log *logger.Logger, mcpBridge ...*VoiceMCPBridge) *RealtimeVoiceService {
	return &RealtimeVoiceService{
		cfg:          cfg,
		log:          log.Named("realtime_voice"),
		sessions:     make(map[string]*RealtimeVoiceSession),
		userSessions: make(map[string]int),
		metrics:      &realtimeVoiceMetrics{},
		mcpBridge:    firstVoiceMCPBridge(mcpBridge),
	}
}

func (s *RealtimeVoiceService) ConfigError() error {
	return s.cfg.ValidateForRuntime()
}

func (s *RealtimeVoiceService) ActiveSessions() int64 {
	return s.metrics.activeSessions.Load()
}

func (s *RealtimeVoiceService) Serve(ctx context.Context, appConn *websocket.Conn, req RealtimeVoiceSessionRequest) {
	sessionCtx, cancel := context.WithCancel(ctx)
	if s.cfg.MaxSessionSeconds > 0 {
		sessionCtx, cancel = context.WithTimeout(ctx, time.Duration(s.cfg.MaxSessionSeconds)*time.Second)
	}

	session := &RealtimeVoiceSession{
		ID:             uuid.NewString(),
		UserID:         strings.TrimSpace(req.UserID),
		BusinessID:     strings.TrimSpace(req.BusinessID),
		ConversationID: strings.TrimSpace(req.ConversationID),
		AccessToken:    strings.TrimSpace(req.AccessToken),
		AppConn:        appConn,
		Ctx:            sessionCtx,
		Cancel:         cancel,
		AppWrite:       make(chan RealtimeAppOutbound, 128),
		DeepgramWrite:  make(chan RealtimeDeepgramOutbound, 128),
		StartedAt:      time.Now(),
	}
	session.log = s.log.With(
		"session_id", session.ID,
		"user_id", session.UserID,
		"business_id", session.BusinessID,
		"conversation_id", session.ConversationID,
	)

	if err := s.register(session); err != nil {
		session.log.Warn("realtime voice session rejected", "error", err)
		_ = appConn.WriteJSON(RealtimeAppEvent{
			Type:    AppEventError,
			Code:    "voice_session_limit",
			Message: err.Error(),
		})
		_ = appConn.Close()
		cancel()
		return
	}
	defer s.unregister(session)

	session.log.Info("voice realtime session started")
	defer func() {
		session.Cancel()
		_ = session.AppConn.Close()
		if session.DeepgramConn != nil {
			_ = session.DeepgramConn.Close()
		}
		session.log.Info("voice realtime session ended",
			"duration_ms", time.Since(session.StartedAt).Milliseconds(),
			"bytes_app_to_backend", session.bytesAppToBackend.Load(),
			"bytes_backend_to_app", session.bytesBackendToApp.Load(),
		)
	}()

	session.AppConn.SetReadLimit(int64(s.cfg.MaxFrameBytes))
	session.AppConn.SetCloseHandler(func(code int, text string) error {
		session.log.Info("app websocket closed", "code", code, "reason", safeCloseReason(text))
		session.Cancel()
		return nil
	})

	dg := NewDeepgramVoiceAgentClient(s.cfg, session.log)
	connectStart := time.Now()
	if err := dg.Connect(session.Ctx); err != nil {
		s.metrics.errorCount.Add(1)
		session.log.Error("Deepgram connection failed", "error", err)
		s.sendApp(session, NewAppError("deepgram_connect_failed", "Could not connect realtime voice agent"))
		return
	}
	session.DeepgramConn = dg.Conn()
	session.log.Info("Deepgram connected", "duration_ms", time.Since(connectStart).Milliseconds())

	if err := s.waitForDeepgramWelcome(session, dg); err != nil {
		s.metrics.errorCount.Add(1)
		session.log.Error("Deepgram welcome failed", "error", err)
		s.sendApp(session, NewAppError("deepgram_welcome_failed", "Deepgram voice agent did not become ready"))
		return
	}

	settings := BuildDeepgramVoiceAgentSettings(s.cfg, DeepgramVoiceAgentSettingsOptions{
		SessionID:       session.ID,
		UserID:          session.UserID,
		BusinessID:      session.BusinessID,
		ConversationID:  session.ConversationID,
		Language:        req.Language,
		Voice:           req.Voice,
		MCPToolsEnabled: s.mcpBridge.Enabled(),
	})

	var wg sync.WaitGroup
	wg.Add(5)
	go func() {
		defer wg.Done()
		s.writeToAppLoop(session)
	}()
	go func() {
		defer wg.Done()
		s.readFromAppLoop(session)
	}()
	go func() {
		defer wg.Done()
		s.readFromDeepgramLoop(session, dg)
	}()
	go func() {
		defer wg.Done()
		s.writeToDeepgramLoop(session, dg, settings)
	}()
	go func() {
		defer wg.Done()
		s.keepAliveLoop(session)
	}()

	s.sendApp(session, NewAppJSONEvent(RealtimeAppEvent{Type: AppEventReady}))
	<-session.Ctx.Done()
	_ = session.AppConn.Close()
	if session.DeepgramConn != nil {
		_ = session.DeepgramConn.Close()
	}
	wg.Wait()
}

func (s *RealtimeVoiceService) register(session *RealtimeVoiceSession) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	limit := s.cfg.MaxConcurrentSessionsPerUser
	if limit <= 0 {
		limit = 1
	}
	if s.userSessions[session.UserID] >= limit {
		return fmt.Errorf("maximum realtime voice sessions reached")
	}
	s.sessions[session.ID] = session
	s.userSessions[session.UserID]++
	s.metrics.activeSessions.Add(1)
	s.metrics.totalSessions.Add(1)
	return nil
}

func (s *RealtimeVoiceService) unregister(session *RealtimeVoiceSession) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if _, ok := s.sessions[session.ID]; ok {
		delete(s.sessions, session.ID)
		s.metrics.activeSessions.Add(-1)
	}
	if count := s.userSessions[session.UserID]; count <= 1 {
		delete(s.userSessions, session.UserID)
	} else {
		s.userSessions[session.UserID] = count - 1
	}
}

func (s *RealtimeVoiceService) waitForDeepgramWelcome(session *RealtimeVoiceSession, dg *DeepgramVoiceAgentClient) error {
	if session.DeepgramConn != nil {
		_ = session.DeepgramConn.SetReadDeadline(time.Now().Add(10 * time.Second))
		defer func() {
			_ = session.DeepgramConn.SetReadDeadline(time.Time{})
		}()
	}
	messageType, payload, err := dg.ReadMessage()
	if err != nil {
		return err
	}
	if messageType == websocket.TextMessage {
		var envelope struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(payload, &envelope); err != nil {
			return err
		}
		if envelope.Type == "Welcome" {
			return nil
		}
		if envelope.Type == "Error" {
			event, _, _ := MapDeepgramJSONEvent(payload)
			return fmt.Errorf("Deepgram error before settings: %s", event.Message)
		}
		return fmt.Errorf("unexpected Deepgram event before settings: %s", envelope.Type)
	}
	return fmt.Errorf("unexpected Deepgram binary message before settings")
}

func (s *RealtimeVoiceService) readFromAppLoop(session *RealtimeVoiceSession) {
	for {
		messageType, payload, err := session.AppConn.ReadMessage()
		if err != nil {
			if !errors.Is(err, context.Canceled) {
				session.log.Info("app websocket read stopped", "error", safeWebSocketError(err))
			}
			session.Cancel()
			return
		}

		switch messageType {
		case websocket.BinaryMessage:
			if len(payload) > s.cfg.MaxFrameBytes {
				session.log.Warn("oversized app audio frame rejected", "size", len(payload), "max_frame_bytes", s.cfg.MaxFrameBytes)
				s.sendApp(session, NewAppError("audio_frame_too_large", "Audio frame exceeds maximum size"))
				session.Cancel()
				return
			}
			if !session.firstAppAudioLogged.Swap(true) {
				session.log.Info("first audio frame received from app", "bytes", len(payload))
			}
			session.bytesAppToBackend.Add(int64(len(payload)))
			s.sendDeepgram(session, RealtimeDeepgramOutbound{MessageType: websocket.BinaryMessage, Payload: append([]byte(nil), payload...)})
		case websocket.TextMessage:
			event, err := ParseRealtimeVoiceControl(payload)
			if err != nil {
				session.log.Warn("invalid app control message", "error", err)
				s.sendApp(session, NewAppError("invalid_control_message", err.Error()))
				session.Cancel()
				return
			}
			s.handleAppControl(session, event)
			if event.Type == "stop" {
				session.Cancel()
				return
			}
		case websocket.CloseMessage:
			session.Cancel()
			return
		default:
			session.log.Warn("unsupported app websocket message type", "message_type", messageType)
			s.sendApp(session, NewAppError("unsupported_message_type", "Unsupported websocket message type"))
			session.Cancel()
			return
		}
	}
}

func (s *RealtimeVoiceService) handleAppControl(session *RealtimeVoiceSession, event RealtimeVoiceControlEvent) {
	switch event.Type {
	case "start":
		// The Deepgram settings flow is already active once the socket is ready.
	case "ping":
		s.sendApp(session, NewAppJSONEvent(RealtimeAppEvent{Type: AppEventPong}))
	case "interrupt":
		session.log.Info("user interruption received")
		s.sendApp(session, NewAppJSONEvent(RealtimeAppEvent{Type: AppEventInterrupted}))
	case "client_context":
		if event.BusinessID != "" && event.BusinessID != session.BusinessID {
			session.log.Warn("client_context business mismatch", "client_business_id", event.BusinessID)
			s.sendApp(session, NewAppError("business_id_mismatch", "client_context business_id does not match session"))
			session.Cancel()
			return
		}
		if event.ConversationID != "" {
			session.ConversationID = event.ConversationID
		}
	case "stop":
		s.sendApp(session, NewAppJSONEvent(RealtimeAppEvent{Type: "closed"}))
	}
}

func (s *RealtimeVoiceService) writeToAppLoop(session *RealtimeVoiceSession) {
	timeout := time.Duration(s.cfg.WriteTimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 5 * time.Second
	}
	for {
		select {
		case <-session.Ctx.Done():
			return
		case outbound := <-session.AppWrite:
			if outbound.Payload == nil && outbound.MessageType != websocket.PingMessage {
				continue
			}
			if err := session.AppConn.SetWriteDeadline(time.Now().Add(timeout)); err != nil {
				session.log.Warn("app websocket deadline failed", "error", err)
				session.Cancel()
				return
			}
			if err := session.AppConn.WriteMessage(outbound.MessageType, outbound.Payload); err != nil {
				session.log.Info("app websocket write stopped", "error", safeWebSocketError(err))
				session.Cancel()
				return
			}
			if outbound.MessageType == websocket.BinaryMessage {
				session.bytesBackendToApp.Add(int64(len(outbound.Payload)))
				if !session.firstAssistantAudioLogged.Swap(true) {
					session.log.Info("first assistant audio frame sent to app", "bytes", len(outbound.Payload))
				}
			}
		}
	}
}

func (s *RealtimeVoiceService) readFromDeepgramLoop(session *RealtimeVoiceSession, dg *DeepgramVoiceAgentClient) {
	for {
		messageType, payload, err := dg.ReadMessage()
		if err != nil {
			session.log.Info("Deepgram websocket read stopped", "error", safeWebSocketError(err))
			session.Cancel()
			return
		}

		switch messageType {
		case websocket.BinaryMessage:
			if !session.firstDeepgramAudioLogged.Swap(true) {
				session.log.Info("first audio frame received from Deepgram", "bytes", len(payload))
			}
			s.sendApp(session, RealtimeAppOutbound{MessageType: websocket.BinaryMessage, Payload: append([]byte(nil), payload...)})
		case websocket.TextMessage:
			s.handleDeepgramText(session, payload)
		case websocket.CloseMessage:
			session.Cancel()
			return
		}
	}
}

func (s *RealtimeVoiceService) handleDeepgramText(session *RealtimeVoiceSession, payload []byte) {
	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(payload, &envelope); err != nil {
		session.log.Warn("invalid Deepgram JSON", "error", err)
		s.sendApp(session, NewAppError("deepgram_invalid_json", "Deepgram sent an invalid JSON event"))
		return
	}

	switch envelope.Type {
	case "SettingsApplied":
		session.log.Info("settings applied")
	case "UserStartedSpeaking":
		session.log.Info("Deepgram user started speaking")
	case "Error":
		session.log.Error("Deepgram error", "payload_size", len(payload))
		s.metrics.errorCount.Add(1)
	case "Warning":
		session.log.Warn("Deepgram warning", "payload_size", len(payload))
	case "FunctionCallRequest":
		s.handleFunctionCallRequest(session, payload)
	}

	event, ok, err := MapDeepgramJSONEvent(payload)
	if err != nil {
		session.log.Warn("failed to map Deepgram event", "error", err)
		s.sendApp(session, NewAppError("deepgram_event_map_failed", "Could not map Deepgram event"))
		return
	}
	if ok {
		s.sendApp(session, NewAppJSONEvent(event))
	}
}

func (s *RealtimeVoiceService) handleFunctionCallRequest(session *RealtimeVoiceSession, payload []byte) {
	var req DeepgramFunctionCallRequest
	if err := json.Unmarshal(payload, &req); err != nil {
		return
	}
	for _, fn := range req.Functions {
		content := voiceMCPErrorContent("unsupported_function", "This voice function is not available.")
		if fn.Name == voiceMCPFunctionName {
			content = s.mcpBridge.HandleFunctionCall(session.Ctx, fn, session.BusinessID, session.AccessToken)
		} else {
			session.log.Warn("unsupported Deepgram function call requested", "function", fn.Name, "client_side", fn.ClientSide)
		}
		response := map[string]interface{}{
			"type":        "FunctionCallResponse",
			"id":          fn.ID,
			"name":        fn.Name,
			"content":     content,
			"client_side": false,
		}
		if strings.TrimSpace(fn.ThoughtSignature) != "" {
			response["thought_signature"] = fn.ThoughtSignature
		}
		s.sendDeepgram(session, NewDeepgramJSONMessage(response))
	}
}

func (s *RealtimeVoiceService) writeToDeepgramLoop(session *RealtimeVoiceSession, dg *DeepgramVoiceAgentClient, settings map[string]interface{}) {
	if err := dg.SendSettings(session.Ctx, settings); err != nil {
		session.log.Error("failed to send Deepgram settings", "error", err)
		s.sendApp(session, NewAppError("deepgram_settings_failed", "Could not configure realtime voice agent"))
		session.Cancel()
		return
	}

	for {
		select {
		case <-session.Ctx.Done():
			return
		case outbound := <-session.DeepgramWrite:
			var err error
			if outbound.MessageType == websocket.BinaryMessage {
				err = dg.SendBinary(outbound.Payload)
			} else {
				err = dg.SendRaw(outbound.MessageType, outbound.Payload)
			}
			if err != nil {
				session.log.Info("Deepgram websocket write stopped", "error", safeWebSocketError(err))
				session.Cancel()
				return
			}
		}
	}
}

func (s *RealtimeVoiceService) keepAliveLoop(session *RealtimeVoiceSession) {
	interval := time.Duration(s.cfg.PingIntervalSeconds) * time.Second
	if interval <= 0 {
		interval = 20 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-session.Ctx.Done():
			return
		case <-ticker.C:
			s.sendApp(session, RealtimeAppOutbound{MessageType: websocket.PingMessage, Payload: []byte("voice")})
			s.sendDeepgram(session, NewDeepgramJSONMessage(map[string]string{"type": "KeepAlive"}))
		}
	}
}

func (s *RealtimeVoiceService) sendApp(session *RealtimeVoiceSession, outbound RealtimeAppOutbound) bool {
	select {
	case <-session.Ctx.Done():
		return false
	case session.AppWrite <- outbound:
		return true
	}
}

func (s *RealtimeVoiceService) sendDeepgram(session *RealtimeVoiceSession, outbound RealtimeDeepgramOutbound) bool {
	select {
	case <-session.Ctx.Done():
		return false
	case session.DeepgramWrite <- outbound:
		return true
	}
}

func safeWebSocketError(err error) string {
	if err == nil {
		return ""
	}
	return safeCloseReason(err.Error())
}

func safeCloseReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if len(reason) > 160 {
		return reason[:160]
	}
	return reason
}
