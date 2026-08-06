package tests

import (
	"context"
	"errors"
	"go/build"
	"go/parser"
	"go/token"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"invoice-backend/internal/voice/loadtest"
	"invoice-backend/internal/voice/protocol"
	"invoice-backend/internal/voice/tools"
)

func TestVoiceLoadReportSchemaCannotClaimUnexecutedProductionEvidence(t *testing.T) {
	report, err := loadtest.RunDry(context.Background(), loadtest.DefaultDryRunConfig())
	if err != nil {
		t.Fatalf("RunDry() error = %v", err)
	}
	if report.LocalStatus != loadtest.StatusVerifiedLocal || report.ProductionPassCriteriaStatus != loadtest.StatusNotExecuted {
		t.Fatalf("local/live statuses = %q/%q", report.LocalStatus, report.ProductionPassCriteriaStatus)
	}
	if report.Cost != (loadtest.CostFields{}) {
		t.Fatalf("offline report contains cost values: %#v", report.Cost)
	}
	for _, stage := range report.LiveStages {
		if stage.ExecutionStatus != loadtest.StatusNotExecuted || stage.PassCriteriaStatus != loadtest.StatusNotExecuted ||
			stage.Metrics != (loadtest.LiveMetrics{}) || stage.Cost != (loadtest.CostFields{}) {
			t.Fatalf("stage %q contains fabricated evidence: %#v", stage.ID, stage)
		}
	}
}

func TestVoiceLoadDefaultBuildHasNoNetworkProviderOrAWSImports(t *testing.T) {
	repositoryRoot := ".."
	contexts := []string{
		filepath.Join(repositoryRoot, "internal", "voice", "loadtest"),
		filepath.Join(repositoryRoot, "cmd", "loadtest", "voice"),
	}
	for _, directory := range contexts {
		entries, err := filepath.Glob(filepath.Join(directory, "*.go"))
		if err != nil {
			t.Fatalf("glob %s: %v", directory, err)
		}
		for _, path := range entries {
			name := filepath.Base(path)
			if strings.HasSuffix(name, "_test.go") {
				continue
			}
			matches, err := build.Default.MatchFile(directory, name)
			if err != nil {
				t.Fatalf("match default build file %s: %v", path, err)
			}
			if !matches {
				continue
			}
			parsed, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
			if err != nil {
				t.Fatalf("parse imports %s: %v", path, err)
			}
			for _, imported := range parsed.Imports {
				value, err := strconv.Unquote(imported.Path.Value)
				if err != nil {
					t.Fatalf("unquote import %s: %v", imported.Path.Value, err)
				}
				for _, forbidden := range []string{"net/http", "os/exec", "github.com/aws/", "internal/providers/sarvam"} {
					if value == forbidden || strings.HasPrefix(value, forbidden) {
						t.Errorf("ordinary load-test file %s imports live-capable package %q", path, value)
					}
				}
			}
		}
	}

	liveDirectory := filepath.Join(repositoryRoot, "cmd", "loadtest", "voice")
	if matches, err := build.Default.MatchFile(liveDirectory, "live_enabled.go"); err != nil || matches {
		t.Fatalf("default build includes live_enabled.go: matches=%v error=%v", matches, err)
	}
	tagged := build.Default
	tagged.BuildTags = []string{"voice_live_load"}
	if matches, err := tagged.MatchFile(liveDirectory, "live_enabled.go"); err != nil || !matches {
		t.Fatalf("voice_live_load build excludes live_enabled.go: matches=%v error=%v", matches, err)
	}
}

func TestVoiceLoadSecurityRegressionsExerciseRealOfflineBoundaries(t *testing.T) {
	binding, err := tools.NewSessionBinding(staticVoiceLoadAuthorization("Bearer offline-security-canary"), "11111111-1111-4111-8111-111111111111", "")
	if err != nil {
		t.Fatalf("NewSessionBinding() error = %v", err)
	}
	defer binding.Close()
	for _, origin := range []string{
		"https://169.254.169.254",
		"https://[fd00:ec2::254]",
		"https://localhost",
		"https://127.0.0.1",
		"https://10.0.0.1",
	} {
		if _, err := tools.NewRegistry(tools.Config{Origin: origin}, binding); !errors.Is(err, tools.ErrInvalidConfiguration) {
			t.Errorf("NewRegistry(%q) error = %v, want ErrInvalidConfiguration", origin, err)
		}
	}

	registry, err := tools.NewRegistry(tools.Config{
		Origin: "https://api.example.com", Resolver: poisonVoiceLoadResolver{}, Dialer: poisonVoiceLoadDialer{},
	}, binding)
	if err != nil {
		t.Fatalf("NewRegistry(public fake origin) error = %v", err)
	}
	defer registry.Close()
	if _, err := registry.Execute(context.Background(), "fetch_arbitrary_url", []byte(`{"url":"https://example.com"}`)); !errors.Is(err, tools.ErrUnknownTool) {
		t.Fatalf("unallowlisted tool error = %v, want ErrUnknownTool", err)
	}

	oversized := make([]byte, protocol.MaxControlMessageBytes+1)
	if _, err := protocol.DecodeControlMessage(oversized, protocol.ClientToRuntime, nil); !errors.Is(err, protocol.ErrControlMessageTooLarge) {
		t.Fatalf("oversized DataChannel error = %v, want ErrControlMessageTooLarge", err)
	}

	checks := loadtest.RunSecurityMatrix()
	if len(checks) != 13 {
		t.Fatalf("security matrix checks = %d, want 13", len(checks))
	}
	for _, check := range checks {
		if check.Outcome != loadtest.OutcomeBlocked || check.Evidence != loadtest.StatusModeledLocal {
			t.Errorf("security check %q = %#v", check.ID, check)
		}
	}
}

type staticVoiceLoadAuthorization string

func (value staticVoiceLoadAuthorization) Authorization(context.Context) (string, error) {
	return string(value), nil
}

type poisonVoiceLoadResolver struct{}

func (poisonVoiceLoadResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	panic("offline unallowlisted-tool test attempted DNS")
}

type poisonVoiceLoadDialer struct{}

func (poisonVoiceLoadDialer) DialContext(context.Context, string, string) (net.Conn, error) {
	panic("offline unallowlisted-tool test attempted network")
}
