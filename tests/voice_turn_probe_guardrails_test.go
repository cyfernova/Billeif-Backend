package tests

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

const voiceLiveProbeBuildTag = "voice_live_probe"

func TestVoiceNetworkSpikeRunbookKeepsLiveGateClosed(t *testing.T) {
	t.Parallel()

	body := readRepositoryFile(t, "docs", "operations", "voice-network-spike.md")
	top := body
	if index := strings.Index(body, "## What this spike must prove"); index >= 0 {
		top = body[:index]
	}
	for _, status := range []string{
		"| Live execution | NOT EXECUTED |",
		"| Compatibility | UNPROVEN |",
		"| Release gate | CLOSED |",
	} {
		if !strings.Contains(top, status) {
			t.Errorf("voice network spike must display top-level status %q", status)
		}
	}

	for _, required := range []string{
		"temporary operator-only probe deployments",
		"streaming or background",
		"30 uninterrupted",
		"source/destination checks disabled",
		"GetIceServerConfig",
		"I_ACKNOWLEDGE_LIVE_AGENTCORE_TURN_PROBE_MAY_INCUR_COSTS",
		"temporary Cognito app client",
		"read-only operator or observer role",
		"ConnectivityObservation",
		"RedactedICE",
		"NATHealthObservation",
		"EnduranceObservation",
		"CredentialLifecycleObservation",
		"BackendRegressionObservation",
		"ValidatedLiveEvidence",
		"privately validated live",
		"Stable redacted path identities",
		"Changing the configured path order must not change the identity",
		"one `ConnectivityObservation` per AWS control/signaling",
		"at least 30 uninterrupted",
		"credential-lifecycle artifact hash",
		"Required restart behavior",
		"backend-regression artifact hash",
		"conntrack",
		"VPC Flow Logs",
		"backend regression",
		"Clean up and regress",
		"chicken-and-egg dependency",
		"An invocation cannot select its subnet",
		"A normal synchronous invocation cannot prove 30-minute endurance",
		"customer-managed NAT instance",
		"https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/runtime-webrtc.html",
		"https://docs.aws.amazon.com/bedrock-agentcore/latest/devguide/agentcore-vpc.html",
		"https://docs.aws.amazon.com/kinesisvideostreams-webrtc-dg/latest/devguide/kvswebrtc-requirements.html",
		"https://docs.aws.amazon.com/kinesisvideostreams-webrtc-dg/latest/devguide/kvswebrtc-limits.html",
		"https://docs.aws.amazon.com/vpc/latest/userguide/work-with-nat-instances.html",
		"https://docs.aws.amazon.com/AWSEC2/latest/UserGuide/monitoring-network-performance-ena.html",
		"https://docs.aws.amazon.com/vpc/latest/userguide/flow-logs-basics.html",
	} {
		if !strings.Contains(body, required) {
			t.Errorf("voice network spike runbook is missing guardrail %q", required)
		}
	}

	evidenceIndex := strings.Index(body, "## Evidence record")
	if evidenceIndex < 0 {
		t.Fatal("voice network spike runbook must include an evidence record")
	}
	emptyEvidenceRows := 0
	for _, line := range strings.Split(body[evidenceIndex:], "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(line, "|")
		if len(cells) < 4 {
			continue
		}
		field := strings.TrimSpace(cells[1])
		value := strings.TrimSpace(cells[2])
		if field == "Field" || strings.Trim(field, "- ") == "" {
			continue
		}
		if value != "" {
			t.Errorf("live evidence field %q must remain empty before execution, got %q", field, value)
		}
		emptyEvidenceRows++
	}
	if emptyEvidenceRows < 45 {
		t.Fatalf("voice network spike should retain a complete empty evidence record, found %d fields", emptyEvidenceRows)
	}
}

func TestVoiceTurnHarnessModelsCompletePrivatelyValidatedEvidence(t *testing.T) {
	t.Parallel()

	path := filepath.Join("..", "internal", "voice", "webrtc", "turn.go")
	source := readRepositoryFile(t, "internal", "voice", "webrtc", "turn.go")
	for _, contract := range []string{
		"ConnectivityObservation",
		"RedactedICE",
		"NATHealthObservation",
		"EnduranceObservation",
		"CredentialLifecycleObservation",
		"BackendRegressionObservation",
		"ValidatedLiveEvidence",
	} {
		if !strings.Contains(source, "type "+contract+" struct") {
			t.Errorf("TURN proof harness is missing evidence contract %s", contract)
		}
	}
	for _, forgeableGate := range []string{
		"func (result ProbeResult) LiveEvidenceSatisfied",
		"func LiveReleaseGateSatisfied(results ...ProbeResult)",
	} {
		if strings.Contains(source, forgeableGate) {
			t.Errorf("TURN proof harness must not release-gate caller-constructible ProbeResult values: %q", forgeableGate)
		}
	}

	parsed, err := parser.ParseFile(token.NewFileSet(), path, source, 0)
	if err != nil {
		t.Fatalf("parse TURN proof harness: %v", err)
	}
	validatedType := findStructType(parsed, "ValidatedLiveEvidence")
	if validatedType == nil {
		t.Fatal("ValidatedLiveEvidence must be a struct with private validated state")
	}
	privateFields := 0
	for _, field := range validatedType.Fields.List {
		if len(field.Names) == 0 {
			t.Fatal("ValidatedLiveEvidence must not expose embedded construction state")
		}
		for _, name := range field.Names {
			if ast.IsExported(name.Name) {
				t.Errorf("ValidatedLiveEvidence field %s must remain private", name.Name)
			}
			privateFields++
		}
	}
	if privateFields == 0 {
		t.Fatal("ValidatedLiveEvidence must carry private validation state")
	}
}

func TestVoiceProbePackageTestsAreFakeOnlyAndRunInSafeCI(t *testing.T) {
	t.Parallel()

	makefile := readRepositoryFile(t, "Makefile")
	recipe := makeTargetRecipe(t, makefile, "test-agentcore")
	for _, command := range []string{
		"go test -race -count=1 ./internal/voice/protocol ./internal/voice/runtime ./internal/voice/webrtc ./cmd/agentcore/voice-runtime",
		"go test -race -count=1 -tags=voice_live_probe ./internal/voice/webrtc",
	} {
		if !strings.Contains(recipe, command) {
			t.Errorf("test-agentcore must execute fake-only probe check %q", command)
		}
	}
	if count := strings.Count(makefile, voiceLiveProbeBuildTag); count != 1 {
		t.Fatalf("Makefile should mention %s only in the tagged package test, found %d mentions", voiceLiveProbeBuildTag, count)
	}

	targetPattern := regexp.MustCompile(`(?m)^([A-Za-z0-9_.-]+):[^=]*$`)
	for _, match := range targetPattern.FindAllStringSubmatch(makefile, -1) {
		name := strings.ToLower(match[1])
		if strings.Contains(name, "voice") && (strings.Contains(name, "live") || strings.Contains(name, "probe")) {
			t.Fatalf("Makefile must not expose a convenient live voice probe target %q", match[1])
		}
	}

	workflow := readRepositoryFile(t, ".github", "workflows", "deploy.yml")
	if !strings.Contains(workflow, "run: make test-agentcore") {
		t.Fatal("safe CI verification must run the AgentCore package test target")
	}

	testSource := readRepositoryFile(t, "internal", "voice", "webrtc", "turn_test.go")
	for _, fakeOnly := range []string{
		"successfulFakeDependencies",
		"poisonDependencies",
	} {
		if !strings.Contains(testSource, fakeOnly) {
			t.Errorf("tagged probe package tests are missing fake-only invariant %q", fakeOnly)
		}
	}
	for _, forbidden := range []string{
		"net.Dial(",
		"http.Get(",
		"http.Post(",
		"exec.Command(",
		"LoadDefaultConfig(",
		"GetSecretValue(",
	} {
		if strings.Contains(testSource, forbidden) {
			t.Errorf("probe package tests must use injected fakes, found live-capable call %q", forbidden)
		}
	}
}

func TestProductionBuildAndDeployCannotCompileLiveVoiceProbe(t *testing.T) {
	t.Parallel()

	makefile := readRepositoryFile(t, "Makefile")
	if strings.Contains(makeTargetRecipe(t, makefile, "build-agentcore"), voiceLiveProbeBuildTag) {
		t.Fatalf("production AgentCore build must not use %s", voiceLiveProbeBuildTag)
	}

	workflow := readRepositoryFile(t, ".github", "workflows", "deploy.yml")
	if strings.Contains(workflow, voiceLiveProbeBuildTag) {
		t.Fatalf("deployment workflow must not use %s", voiceLiveProbeBuildTag)
	}

	repositoryRoot := filepath.Join("..")
	err := filepath.WalkDir(filepath.Join(repositoryRoot, "deploy"), func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.EqualFold(entry.Name(), "Dockerfile") {
			return nil
		}
		contents, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		if strings.Contains(string(contents), voiceLiveProbeBuildTag) {
			t.Errorf("production Dockerfile %s must not use %s", filepath.ToSlash(path), voiceLiveProbeBuildTag)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("inspect production Dockerfiles: %v", err)
	}
}

func readRepositoryFile(t *testing.T, elements ...string) string {
	t.Helper()
	path := filepath.Join(append([]string{".."}, elements...)...)
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", filepath.ToSlash(path), err)
	}
	return string(body)
}

func findStructType(file *ast.File, name string) *ast.StructType {
	for _, declaration := range file.Decls {
		general, ok := declaration.(*ast.GenDecl)
		if !ok || general.Tok != token.TYPE {
			continue
		}
		for _, specification := range general.Specs {
			typeSpec, ok := specification.(*ast.TypeSpec)
			if !ok || typeSpec.Name.Name != name {
				continue
			}
			value, _ := typeSpec.Type.(*ast.StructType)
			return value
		}
	}
	return nil
}

func makeTargetRecipe(t *testing.T, makefile, target string) string {
	t.Helper()
	lines := strings.Split(makefile, "\n")
	header := target + ":"
	start := -1
	for index, line := range lines {
		if strings.HasPrefix(line, header) {
			start = index + 1
			break
		}
	}
	if start < 0 {
		t.Fatalf("Makefile is missing target %q", target)
	}

	var recipe []string
	for _, line := range lines[start:] {
		if strings.HasPrefix(line, "\t") {
			recipe = append(recipe, strings.TrimSpace(line))
			continue
		}
		if strings.TrimSpace(line) == "" {
			if len(recipe) == 0 {
				continue
			}
			break
		}
		break
	}
	if len(recipe) == 0 {
		t.Fatalf("Makefile target %q has no recipe", target)
	}
	return strings.Join(recipe, "\n")
}
