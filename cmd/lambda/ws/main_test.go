package main

import (
	"testing"

	"github.com/aws/aws-lambda-go/events"
)

func TestDecodeWebSocketBodyJSON(t *testing.T) {
	var payload struct {
		Action string `json:"action"`
	}
	err := decodeWebSocketBody(events.APIGatewayWebsocketProxyRequest{
		Body: `{"action":"voice.start"}`,
	}, &payload)
	if err != nil {
		t.Fatalf("expected JSON body to decode: %v", err)
	}
	if payload.Action != "voice.start" {
		t.Fatalf("unexpected action: %s", payload.Action)
	}
}

func TestDecodeWebSocketBodyRejectsInvalidBase64(t *testing.T) {
	var payload struct{}
	err := decodeWebSocketBody(events.APIGatewayWebsocketProxyRequest{
		Body:            "not-base64",
		IsBase64Encoded: true,
	}, &payload)
	if err == nil {
		t.Fatal("expected invalid base64 to fail")
	}
}

func TestFirstNonEmptyTrimsValues(t *testing.T) {
	if got := firstNonEmpty("", "  business-1  "); got != "business-1" {
		t.Fatalf("unexpected value: %q", got)
	}
}

func TestExtractAuthTokenAcceptsAuthorizationQuery(t *testing.T) {
	token := "Bearer query-token"
	got := extractAuthToken(events.APIGatewayWebsocketProxyRequest{
		QueryStringParameters: map[string]string{
			"authorization": token,
		},
	})
	if got != token {
		t.Fatalf("unexpected token: %q", got)
	}
}
