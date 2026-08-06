package tests

import (
	"strings"
	"testing"
)

func TestVoiceAgentCoreObservabilityIsFeatureGatedAndBounded(t *testing.T) {
	source := readRepositoryFile(t, "infrastructure", "terraform", "voice_agentcore_observability.tf")
	emitter := readRepositoryFile(t, "internal", "voice", "telemetry", "emitter.go")

	for _, required := range []string{
		`default     = false`,
		`var.provision_voice_infrastructure && var.enable_voice_observability`,
		`var.environment == "prod" ? 14 : 7`,
		`format("user:%s$%s"`,
		`actions   = ["cloudwatch:PutMetricData"]`,
		`variable = "cloudwatch:namespace"`,
		`values   = ["Billeif/NAT"]`,
		`metrics_collection_interval = 60`,
		`expression  = "RATE(counter)"`,
		`stat        = "Maximum"`,
		`local.nat_instance_enabled ? [`,
		`Service     = local.voice_signal_service_dimensions[each.key]`,
		`AgentCore Active Sessions (account-wide)`,
	} {
		if !strings.Contains(source, required) {
			t.Errorf("voice observability policy is missing %q", required)
		}
	}
	if !strings.Contains(emitter, `Dimensions: [][]string{{"Environment", "Service", "Outcome"}}`) {
		t.Error("voice EMF dimensions must remain exactly Environment, Service, and Outcome")
	}

	if got := strings.Count(source, `provider = aws.us_east_1`); got != 2 {
		t.Errorf("voice budget provider bindings = %d, want exactly 2 in us-east-1", got)
	}
	if got := strings.Count(source, `name   = "TagKeyValue"`); got != 2 {
		t.Errorf("voice TagKeyValue budget filters = %d, want exactly 2", got)
	}
	if got := strings.Count(source, `Service  = "AgentCore.Runtime"`); got < 2 {
		t.Errorf("AgentCore alarm Service dimensions = %d, want at least 2", got)
	}
	if strings.Contains(source, `local.voice_metric_namespace, "ActiveSessions"`) {
		t.Error("voice dashboard must not retain the custom ActiveSessions series")
	}
	if got := strings.Count(source, `[{ expression = "RATE(nat_`); got != 5 {
		t.Errorf("NAT dashboard RATE expressions = %d, want exactly 5 cumulative ENA counters", got)
	}
	if got := strings.Count(source, `visible = false`); got < 5 {
		t.Errorf("hidden NAT cumulative source series = %d, want at least 5", got)
	}
	for _, forbidden := range []string{
		`"CorrelationID"`,
		`"correlation_id"`,
		`"SessionID"`,
		`"UserID"`,
		`application_logs`,
	} {
		if strings.Contains(source, forbidden) {
			t.Errorf("voice Terraform contains forbidden high-cardinality or payload telemetry token %q", forbidden)
		}
	}
}

func TestVoiceAgentCoreRunbookPreservesPrivacyAndSafeRollback(t *testing.T) {
	runbook := readRepositoryFile(t, "docs", "operations", "voice-agentcore-runbook.md")

	for _, required := range []string{
		"Sarvam outage or throttling",
		"KVS allocation or ICE failure",
		"NAT failure or saturation",
		"Quota exhaustion",
		"Session leaks or durability failure",
		"ENVIRONMENT",
		"user:Workload$voice-agentcore",
		"seven days",
		"fourteen days",
		"one deterministic hash bucket out of 100",
		"The retired provider is not a rollback target",
		"per-second rate of cumulative maximum series",
		"`SessionLeaks` selects `voice-reconciler`",
		"`Service=AgentCore.Runtime` and the exact runtime",
		"is account-wide",
		"CloudWatch `RATE(...)` series returning to zero",
	} {
		if !strings.Contains(runbook, required) {
			t.Errorf("voice runbook is missing %q", required)
		}
	}
	if strings.Contains(runbook, "| `Billeif/Voice` | `ActiveSessions`") {
		t.Error("voice runbook must not advertise the retired custom ActiveSessions series")
	}
}
