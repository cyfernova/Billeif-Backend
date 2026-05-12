package mcp

import (
	"net/http"
	"testing"
)

func TestNewClientRejectsInsecureHTTP(t *testing.T) {
	if _, err := NewClient(Config{ServerURL: "http://mcp.example.test"}); err == nil {
		t.Fatal("expected MCP client to reject insecure http URL")
	}
}

func TestNewClientDisablesEnvironmentProxy(t *testing.T) {
	client, err := NewClient(Config{ServerURL: "https://mcp.example.test"})
	if err != nil {
		t.Fatalf("expected https MCP client to initialize: %v", err)
	}
	transport, ok := client.httpClient.Transport.(*http.Transport)
	if !ok {
		t.Fatalf("expected http transport, got %T", client.httpClient.Transport)
	}
	if transport.Proxy != nil {
		t.Fatal("MCP client must not inherit environment proxy settings")
	}
}
