package services

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"invoice-backend/pkg/logger"

	"github.com/gorilla/websocket"
	"github.com/stretchr/testify/require"
)

type fakeVoiceDeepgram struct {
	messageType int
	payload     []byte
	err         error
	block       bool
	sentRaw     [][]byte
}

func (f *fakeVoiceDeepgram) Connect(context.Context) error                              { return nil }
func (f *fakeVoiceDeepgram) SendSettings(context.Context, map[string]interface{}) error { return nil }
func (f *fakeVoiceDeepgram) SendBinary([]byte) error                                    { return nil }
func (f *fakeVoiceDeepgram) SendRaw(_ int, payload []byte) error {
	f.sentRaw = append(f.sentRaw, append([]byte(nil), payload...))
	return nil
}
func (f *fakeVoiceDeepgram) Close() error { return nil }
func (f *fakeVoiceDeepgram) ReadMessage() (int, []byte, error) {
	if f.block {
		select {}
	}
	return f.messageType, f.payload, f.err
}

func TestWaitForLambdaDeepgramWelcome(t *testing.T) {
	err := waitForLambdaDeepgramWelcome(context.Background(), &fakeVoiceDeepgram{
		messageType: websocket.TextMessage,
		payload:     []byte(`{"type":"Welcome"}`),
	}, time.Second)
	if err != nil {
		t.Fatalf("expected welcome to succeed: %v", err)
	}
}

func TestWaitForLambdaDeepgramWelcomeRejectsErrorEvent(t *testing.T) {
	err := waitForLambdaDeepgramWelcome(context.Background(), &fakeVoiceDeepgram{
		messageType: websocket.TextMessage,
		payload:     []byte(`{"type":"Error","message":"bad config"}`),
	}, time.Second)
	if err == nil {
		t.Fatal("expected Deepgram error event to fail")
	}
}

func TestWaitForLambdaDeepgramWelcomeTimesOut(t *testing.T) {
	err := waitForLambdaDeepgramWelcome(context.Background(), &fakeVoiceDeepgram{block: true}, time.Millisecond)
	if err == nil {
		t.Fatal("expected timeout")
	}
}

func TestWaitForLambdaDeepgramWelcomeReturnsReadError(t *testing.T) {
	readErr := errors.New("read failed")
	err := waitForLambdaDeepgramWelcome(context.Background(), &fakeVoiceDeepgram{err: readErr}, time.Second)
	if !errors.Is(err, readErr) {
		t.Fatalf("expected read error, got %v", err)
	}
}

func TestVoiceLambdaSessionRunnerRespondsToMCPFunctionCall(t *testing.T) {
	caller := &fakeVoiceMCPCaller{}
	bridge := NewVoiceMCPBridge(caller, logger.New())
	runner := &VoiceLambdaSessionRunner{mcpBridge: bridge}
	deepgram := &fakeVoiceDeepgram{}
	session := &VoiceLambdaSession{
		SessionID:  "session-1",
		BusinessID: "biz-123",
	}

	payload := []byte(`{"type":"FunctionCallRequest","functions":[{"id":"fn-1","name":"run_billeif_mcp_tool","arguments":{"tool":"get_customers","args":{"query":{"limit":3}}},"client_side":false}]}`)
	err := runner.respondFunctionCall(context.Background(), session, deepgram, payload, "access-token")
	require.NoError(t, err)
	require.Equal(t, 1, caller.calls)
	require.Len(t, deepgram.sentRaw, 1)

	var response struct {
		Type       string `json:"type"`
		ID         string `json:"id"`
		Name       string `json:"name"`
		Content    string `json:"content"`
		ClientSide bool   `json:"client_side"`
	}
	require.NoError(t, json.Unmarshal(deepgram.sentRaw[0], &response))
	require.Equal(t, "FunctionCallResponse", response.Type)
	require.Equal(t, "fn-1", response.ID)
	require.Equal(t, voiceMCPFunctionName, response.Name)
	require.False(t, response.ClientSide)
	require.Contains(t, response.Content, `"ok":true`)
	require.Contains(t, string(caller.args), `"business_id":"biz-123"`)
}
