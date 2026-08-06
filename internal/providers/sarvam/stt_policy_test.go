package sarvam

import (
	"net/http"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestSTTClientRequiresAnExplicitDialer(t *testing.T) {
	client, err := NewClient(Config{
		APIKey:  "offline-policy-key",
		BaseURL: "https://sarvam.invalid",
	}, &http.Client{})
	if err != nil {
		t.Fatalf("NewClient() error = %v", err)
	}

	if got, err := NewSTTClient(client, nil); got != nil || err != ErrSTTDialerRequired {
		t.Fatalf("NewSTTClient(nil dialer) = (%v, %v), want (nil, ErrSTTDialerRequired)", got, err)
	}
	var typedNil *websocket.Dialer
	if got, err := NewSTTClient(client, typedNil); got != nil || err != ErrSTTDialerRequired {
		t.Fatalf("NewSTTClient(typed nil dialer) = (%v, %v), want (nil, ErrSTTDialerRequired)", got, err)
	}
}

// This test owns the offline boundary for the fake-provider suite. Keep the
// forbidden spellings split here so this policy file cannot trip its own scan.
func TestSTTProviderTestsStayOffline(t *testing.T) {
	_, currentFile, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller() did not return this test file")
	}
	testSourcePath := filepath.Join(filepath.Dir(currentFile), "stt_test.go")
	testSource, err := os.ReadFile(testSourcePath)
	if err != nil {
		t.Fatalf("read %s: %v", testSourcePath, err)
	}

	for _, forbidden := range []string{
		"api." + "sarvam.ai",
		"Default" + "BaseURL",
		"websocket." + "DefaultDialer",
		"SARVAM" + "_API_KEY",
		"." + "env",
	} {
		if strings.Contains(string(testSource), forbidden) {
			t.Errorf("%s contains forbidden live-provider token %q", filepath.Base(testSourcePath), forbidden)
		}
	}
}
