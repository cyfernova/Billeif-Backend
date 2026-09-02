package verify

import (
	"errors"
	"testing"
)

func TestSelectEnvironmentRequiresExplicitTarget(t *testing.T) {
	cases := []struct {
		name            string
		raw             string
		allowProduction bool
		want            string
	}{
		{name: "staging", raw: "staging", want: "staging"},
		{name: "staging mixed case and spaces", raw: "  Staging  ", want: "staging"},
		{name: "dev", raw: "dev", want: "dev"},
		{name: "local", raw: "local", want: "local"},
		{name: "qa", raw: "QA", want: "qa"},
		{name: "preview", raw: "preview", want: "preview"},
		{name: "production with override", raw: "production", allowProduction: true, want: "production"},
		{name: "prod alias with override", raw: "prod", allowProduction: true, want: "production"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			env, err := SelectEnvironment(tc.raw, tc.allowProduction)
			if err != nil {
				t.Fatalf("SelectEnvironment(%q) error = %v, want nil", tc.raw, err)
			}
			if string(env) != tc.want {
				t.Fatalf("SelectEnvironment(%q) = %q, want %q", tc.raw, env, tc.want)
			}
		})
	}
}

func TestSelectEnvironmentRejectsMissingAndUnknown(t *testing.T) {
	cases := []struct {
		name string
		raw  string
	}{
		{name: "empty", raw: ""},
		{name: "whitespace", raw: "   "},
		{name: "unknown", raw: "planet-earth"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := SelectEnvironment(tc.raw, false); err == nil {
				t.Fatalf("SelectEnvironment(%q) error = nil, want refusal", tc.raw)
			}
		})
	}
}

func TestSelectEnvironmentRefusesProductionByDefault(t *testing.T) {
	for _, raw := range []string{"production", "prod", "PROD", "Production"} {
		_, err := SelectEnvironment(raw, false)
		var refusal *ProductionRefusalError
		if !errors.As(err, &refusal) {
			t.Fatalf("SelectEnvironment(%q) error = %v, want ProductionRefusalError", raw, err)
		}
		if refusal.Environment != "production" {
			t.Fatalf("refusal environment = %q, want production", refusal.Environment)
		}
		if refusal.Reason != ReasonProductionRequiresOverride {
			t.Fatalf("refusal reason = %q, want %q", refusal.Reason, ReasonProductionRequiresOverride)
		}
	}
}

func TestSelectEnvironmentOverrideIsDeliberateAndExplicit(t *testing.T) {
	env, err := SelectEnvironment("prod", true)
	if err != nil {
		t.Fatalf("SelectEnvironment with override error = %v, want nil", err)
	}
	if env != EnvironmentProduction {
		t.Fatalf("environment = %q, want %q", env, EnvironmentProduction)
	}
}

func TestEnvironmentIsProduction(t *testing.T) {
	if !EnvironmentProduction.IsProduction() {
		t.Fatal("production environment must report IsProduction()=true")
	}
	for _, raw := range []string{"staging", "dev", "qa", "local"} {
		env, err := SelectEnvironment(raw, false)
		if err != nil {
			t.Fatalf("SelectEnvironment(%q) error = %v", raw, err)
		}
		if env.IsProduction() {
			t.Fatalf("environment %q must not report IsProduction()=true", env)
		}
	}
}
