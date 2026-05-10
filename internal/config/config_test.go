package config

import "testing"

func TestSetDefaultsDevelopmentAllowedOriginsIncludesExpoWeb(t *testing.T) {
	cfg := &Config{Environment: "dev"}

	setDefaults(cfg)

	for _, origin := range []string{
		"http://localhost:3000",
		"http://localhost:8081",
		"http://127.0.0.1:8081",
		"http://localhost:19006",
	} {
		if !containsString(cfg.AllowedOrigins, origin) {
			t.Fatalf("expected default development allowed origins to include %q; got %#v", origin, cfg.AllowedOrigins)
		}
	}
}

func TestParseAllowedOriginsTrimsCommaSeparatedValues(t *testing.T) {
	got := parseAllowedOrigins(" http://localhost:3000, http://localhost:8081 ,,http://127.0.0.1:8081 ")
	want := []string{"http://localhost:3000", "http://localhost:8081", "http://127.0.0.1:8081"}

	if len(got) != len(want) {
		t.Fatalf("expected %d origins, got %d: %#v", len(want), len(got), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("expected origin %d to be %q, got %q", i, want[i], got[i])
		}
	}
}

func containsString(values []string, needle string) bool {
	for _, value := range values {
		if value == needle {
			return true
		}
	}
	return false
}
