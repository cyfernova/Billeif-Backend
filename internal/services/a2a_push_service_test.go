package services

import (
	"context"
	"testing"
)

func TestValidateWebhookURL(t *testing.T) {
	tests := []struct {
		name    string
		rawURL  string
		wantErr bool
	}{
		{name: "reject http scheme", rawURL: "http://1.1.1.1/webhook", wantErr: true},
		{name: "reject localhost", rawURL: "https://localhost/webhook", wantErr: true},
		{name: "reject loopback", rawURL: "https://127.0.0.1/webhook", wantErr: true},
		{name: "reject link local", rawURL: "https://169.254.169.254/webhook", wantErr: true},
		{name: "reject private ipv4", rawURL: "https://10.0.0.1/webhook", wantErr: true},
		{name: "reject internal hostname", rawURL: "https://api.service.internal/webhook", wantErr: true},
		{name: "allow public https ip", rawURL: "https://1.1.1.1/webhook", wantErr: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateWebhookURL(context.Background(), tt.rawURL)
			if tt.wantErr && err == nil {
				t.Fatalf("expected error for %s", tt.rawURL)
			}
			if !tt.wantErr && err != nil {
				t.Fatalf("unexpected error for %s: %v", tt.rawURL, err)
			}
		})
	}
}
