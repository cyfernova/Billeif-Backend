package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

type fakeVoiceDeepgram struct {
	messageType int
	payload     []byte
	err         error
	block       bool
}

func (f *fakeVoiceDeepgram) Connect(context.Context) error                              { return nil }
func (f *fakeVoiceDeepgram) SendSettings(context.Context, map[string]interface{}) error { return nil }
func (f *fakeVoiceDeepgram) SendBinary([]byte) error                                    { return nil }
func (f *fakeVoiceDeepgram) SendRaw(int, []byte) error                                  { return nil }
func (f *fakeVoiceDeepgram) Close() error                                               { return nil }
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
