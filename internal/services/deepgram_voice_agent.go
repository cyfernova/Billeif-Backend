package services

import (
	"context"
	"fmt"
	"net/http"
	"strings"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"

	"github.com/gorilla/websocket"
)

type DeepgramVoiceAgentClient struct {
	url          string
	apiKey       string
	writeTimeout time.Duration
	dialer       *websocket.Dialer
	conn         *websocket.Conn
	log          *logger.Logger
}

func NewDeepgramVoiceAgentClient(cfg config.VoiceRealtimeConfig, log *logger.Logger) *DeepgramVoiceAgentClient {
	writeTimeout := time.Duration(cfg.WriteTimeoutSeconds) * time.Second
	if writeTimeout <= 0 {
		writeTimeout = 5 * time.Second
	}
	return &DeepgramVoiceAgentClient{
		url:          cfg.DeepgramVoiceAgentURL,
		apiKey:       cfg.DeepgramAPIKey,
		writeTimeout: writeTimeout,
		dialer:       websocket.DefaultDialer,
		log:          log.Named("deepgram_voice_agent"),
	}
}

func (c *DeepgramVoiceAgentClient) Connect(ctx context.Context) error {
	if strings.TrimSpace(c.url) == "" {
		return fmt.Errorf("Deepgram Voice Agent URL is required")
	}
	if strings.TrimSpace(c.apiKey) == "" {
		return fmt.Errorf("Deepgram API key is required")
	}

	headers := http.Header{}
	headers.Set("Authorization", "Token "+strings.TrimSpace(c.apiKey))

	conn, resp, err := c.dialer.DialContext(ctx, c.url, headers)
	if err != nil {
		status := ""
		if resp != nil {
			status = resp.Status
		}
		return fmt.Errorf("connect Deepgram Voice Agent websocket: %w %s", err, status)
	}
	c.conn = conn
	return nil
}

func (c *DeepgramVoiceAgentClient) Conn() *websocket.Conn {
	return c.conn
}

func (c *DeepgramVoiceAgentClient) SendSettings(ctx context.Context, settings map[string]interface{}) error {
	return c.SendJSON(ctx, settings)
}

func (c *DeepgramVoiceAgentClient) SendJSON(ctx context.Context, payload interface{}) error {
	if c.conn == nil {
		return fmt.Errorf("Deepgram websocket is not connected")
	}
	deadline := time.Now().Add(c.writeTimeout)
	if deadlineFromCtx, ok := ctx.Deadline(); ok && deadlineFromCtx.Before(deadline) {
		deadline = deadlineFromCtx
	}
	if err := c.conn.SetWriteDeadline(deadline); err != nil {
		return err
	}
	return c.conn.WriteJSON(payload)
}

func (c *DeepgramVoiceAgentClient) SendBinary(audio []byte) error {
	if c.conn == nil {
		return fmt.Errorf("Deepgram websocket is not connected")
	}
	if err := c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
		return err
	}
	return c.conn.WriteMessage(websocket.BinaryMessage, audio)
}

func (c *DeepgramVoiceAgentClient) SendRaw(messageType int, payload []byte) error {
	if c.conn == nil {
		return fmt.Errorf("Deepgram websocket is not connected")
	}
	if err := c.conn.SetWriteDeadline(time.Now().Add(c.writeTimeout)); err != nil {
		return err
	}
	return c.conn.WriteMessage(messageType, payload)
}

func (c *DeepgramVoiceAgentClient) ReadMessage() (int, []byte, error) {
	if c.conn == nil {
		return 0, nil, fmt.Errorf("Deepgram websocket is not connected")
	}
	return c.conn.ReadMessage()
}

func (c *DeepgramVoiceAgentClient) Close() error {
	if c.conn == nil {
		return nil
	}
	err := c.conn.Close()
	c.conn = nil
	return err
}
