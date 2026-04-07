package app

import "testing"

func TestShouldExposeAdminLocalEmails(t *testing.T) {
	tests := []struct {
		name        string
		environment string
		want        bool
	}{
		{name: "dev exposes", environment: "dev", want: true},
		{name: "staging exposes", environment: "staging", want: true},
		{name: "prod hides", environment: "prod", want: false},
		{name: "production hides", environment: "production", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldExposeAdminLocalEmails(tt.environment)
			if got != tt.want {
				t.Fatalf("expected %v, got %v for env %q", tt.want, got, tt.environment)
			}
		})
	}
}
