package handlers

import (
	"os"
	"strings"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/logger"
)

func TestNewMCPHandlerFromConfigDisabledWithoutExplicitURL(t *testing.T) {
	handler := NewMCPHandlerFromConfig(&config.Config{}, logger.New())
	if handler != nil {
		t.Fatal("expected MCP handler to be disabled when MCP_SERVER_URL is not configured")
	}
}

func TestMCPHandlerDoesNotForwardRequestBearerToken(t *testing.T) {
	source, err := os.ReadFile("mcp_handler.go")
	if err != nil {
		t.Fatalf("read MCP handler source: %v", err)
	}
	if strings.Contains(string(source), `GetHeader("Authorization")`) {
		t.Fatal("MCP handler must not forward caller Authorization headers to the MCP server")
	}
}
