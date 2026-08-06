package loadtest

import (
	"context"
	"errors"
	"net/netip"
	"reflect"
	"testing"
	"time"
)

func TestPlannedLiveStagesRemainUnexecutedWithBlankEvidenceAndCost(t *testing.T) {
	want := []struct {
		id              string
		provider        ProviderMode
		sessions        int
		durationMinutes int
	}{
		{id: "aws-fake-25", provider: ProviderFakeSarvam, sessions: 25},
		{id: "aws-fake-50", provider: ProviderFakeSarvam, sessions: 50},
		{id: "aws-fake-100", provider: ProviderFakeSarvam, sessions: 100},
		{id: "aws-fake-120", provider: ProviderFakeSarvam, sessions: 120, durationMinutes: 30},
		{id: "sarvam-real-10", provider: ProviderRealSarvam, sessions: 10},
		{id: "sarvam-real-25", provider: ProviderRealSarvam, sessions: 25},
		{id: "sarvam-real-100", provider: ProviderRealSarvam, sessions: 100},
	}

	stages := PlannedLiveStages()
	if len(stages) != len(want) {
		t.Fatalf("planned stages = %d, want %d", len(stages), len(want))
	}
	for index, expected := range want {
		stage := stages[index]
		if stage.ID != expected.id || stage.Provider != expected.provider || stage.Sessions != expected.sessions || stage.DurationMinutes != expected.durationMinutes {
			t.Errorf("stage[%d] = %#v, want id=%q provider=%q sessions=%d duration=%d", index, stage, expected.id, expected.provider, expected.sessions, expected.durationMinutes)
		}
		if stage.ExecutionStatus != StatusNotExecuted || stage.PassCriteriaStatus != StatusNotExecuted {
			t.Errorf("stage[%d] execution/pass status = %q/%q, want NOT EXECUTED", index, stage.ExecutionStatus, stage.PassCriteriaStatus)
		}
		if stage.Metrics != (LiveMetrics{}) || stage.Cost != (CostFields{}) {
			t.Errorf("stage[%d] contains fabricated live evidence or cost: metrics=%#v cost=%#v", index, stage.Metrics, stage.Cost)
		}
	}

	stages[0].ID = "mutated"
	if got := PlannedLiveStages()[0].ID; got != "aws-fake-25" {
		t.Fatalf("PlannedLiveStages returned shared mutable state: %q", got)
	}
}

func TestRunDryDeterministicallyModelsBidirectionalAudioBargeInAndICERestart(t *testing.T) {
	config := DryRunConfig{Sessions: 120, VirtualDuration: 30 * time.Minute, DryRun: true}
	first, err := RunDry(context.Background(), config)
	if err != nil {
		t.Fatalf("RunDry(first) error = %v", err)
	}
	second, err := RunDry(context.Background(), config)
	if err != nil {
		t.Fatalf("RunDry(second) error = %v", err)
	}
	if !reflect.DeepEqual(first, second) {
		t.Fatal("identical dry-run inputs produced different reports")
	}

	wantMetrics := LocalMetrics{
		SessionsAttempted:        120,
		SessionsAdmitted:         120,
		SessionsCompleted:        120,
		VirtualConnectedMinutes:  3600,
		InboundAudioFrames:       960,
		OutboundAudioFrames:      1200,
		PlayedAudioFrames:        720,
		BargeIns:                 120,
		StaleFramesDropped:       480,
		StaleFramesPlayed:        0,
		ICERestarts:              120,
		FinalTranscripts:         120,
		ChatRequests:             120,
		ChatBeforeFinal:          0,
		TTSStreams:               240,
		PlaybackAcknowledgments:  360,
		CapacityHeldBeforeRepair: 120,
		CapacityRepairs:          120,
		CapacityLeaksAfterRepair: 0,
	}
	if first.SchemaVersion != 1 || first.Mode != ModeDeterministicFake || !first.DryRun || first.LocalStatus != StatusVerifiedLocal {
		t.Fatalf("dry report envelope = %#v", first)
	}
	if first.Metrics != wantMetrics {
		t.Fatalf("local metrics = %#v, want %#v", first.Metrics, wantMetrics)
	}
	if first.ProductionPassCriteriaStatus != StatusNotExecuted || first.Cost != (CostFields{}) {
		t.Fatalf("dry run fabricated production evidence: status=%q cost=%#v", first.ProductionPassCriteriaStatus, first.Cost)
	}

	wantInvariants := map[string]string{
		"audio.bidirectional":                OutcomeVerified,
		"barge_in.stale_generation_fenced":   OutcomeVerified,
		"ice.restart":                        OutcomeVerified,
		"llm.after_final_transcript":         OutcomeVerified,
		"playback.current_generation_ack":    OutcomeVerified,
		"capacity.abandoned_client_repaired": OutcomeVerified,
	}
	assertChecks(t, first.Invariants, wantInvariants)
}

func TestRunDryRejectsAnythingThatCouldBeMistakenForLiveExecution(t *testing.T) {
	tests := []struct {
		name   string
		config DryRunConfig
	}{
		{name: "dry run false", config: DryRunConfig{Sessions: 1, VirtualDuration: time.Minute}},
		{name: "zero sessions", config: DryRunConfig{DryRun: true, VirtualDuration: time.Minute}},
		{name: "too many sessions", config: DryRunConfig{DryRun: true, Sessions: 121, VirtualDuration: time.Minute}},
		{name: "zero duration", config: DryRunConfig{DryRun: true, Sessions: 1}},
		{name: "duration above ceiling", config: DryRunConfig{DryRun: true, Sessions: 1, VirtualDuration: 30*time.Minute + time.Second}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := RunDry(context.Background(), test.config); !errors.Is(err, ErrInvalidDryRun) {
				t.Fatalf("RunDry() error = %v, want ErrInvalidDryRun", err)
			}
		})
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := RunDry(cancelled, DryRunConfig{DryRun: true, Sessions: 1, VirtualDuration: time.Minute}); !errors.Is(err, context.Canceled) {
		t.Fatalf("RunDry(cancelled) error = %v, want context.Canceled", err)
	}
}

func TestVirtualVoiceSessionRejectsStaleGenerationAfterBargeIn(t *testing.T) {
	session := virtualVoiceSession{activeGeneration: 1}
	staleGeneration := session.startTTS(4)
	session.interrupt()
	session.playGeneration(staleGeneration, 1)

	if session.metrics.StaleFramesPlayed != 0 || session.metrics.PlayedAudioFrames != 0 {
		t.Fatalf("stale generation reached playback: metrics=%#v", session.metrics)
	}
	if session.metrics.StaleFramesDropped != 4 || session.activeGeneration != 2 {
		t.Fatalf("generation fence did not purge old audio: metrics=%#v generation=%d", session.metrics, session.activeGeneration)
	}
}

func TestVirtualVoiceSessionRepairsAbandonedCapacityOnlyAfterExpiryReconciliation(t *testing.T) {
	session := virtualVoiceSession{}
	session.acquireCapacity()
	session.abandonCapacity()

	if session.capacityHeld != 1 || session.metrics.CapacityHeldBeforeRepair != 1 || session.metrics.CapacityRepairs != 0 {
		t.Fatalf("pre-repair capacity state = held %d metrics %#v", session.capacityHeld, session.metrics)
	}
	session.reconcileExpiredCapacity()
	if session.capacityHeld != 0 || session.metrics.CapacityRepairs != 1 || session.metrics.CapacityLeaksAfterRepair != 0 {
		t.Fatalf("post-repair capacity state = held %d metrics %#v", session.capacityHeld, session.metrics)
	}
}

func TestCapacityInvariantRequiresObservedHoldAndMatchingRepair(t *testing.T) {
	tests := []struct {
		name    string
		metrics LocalMetrics
		outcome string
	}{
		{name: "zero value is not evidence", metrics: LocalMetrics{}, outcome: "FAILED"},
		{name: "held but unrepaired", metrics: LocalMetrics{CapacityHeldBeforeRepair: 1, CapacityLeaksAfterRepair: 1}, outcome: "FAILED"},
		{name: "repair count mismatch", metrics: LocalMetrics{CapacityHeldBeforeRepair: 2, CapacityRepairs: 1}, outcome: "FAILED"},
		{name: "explicitly repaired", metrics: LocalMetrics{CapacityHeldBeforeRepair: 1, CapacityRepairs: 1}, outcome: OutcomeVerified},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			for _, check := range localInvariants(test.metrics) {
				if check.ID == "capacity.abandoned_client_repaired" {
					if check.Outcome != test.outcome || check.Evidence != StatusModeledLocal {
						t.Fatalf("capacity invariant = %#v, want outcome %q evidence %q", check, test.outcome, StatusModeledLocal)
					}
					return
				}
			}
			t.Fatal("capacity invariant missing")
		})
	}
}

func TestFailureMatrixExercisesEveryPlannedRecovery(t *testing.T) {
	want := map[string]string{
		"agentcore.session_stop":             OutcomeRecovered,
		"cognito.token_expired":              OutcomeRecovered,
		"datachannel.drop_media_alive":       OutcomeRecovered,
		"dynamodb.throttle":                  OutcomeRecovered,
		"mobile.client_death_without_delete": OutcomeCapacityRepaired,
		"mobile.network_switch":              OutcomeRecovered,
		"nat.stop_recover":                   OutcomeRecovered,
		"sarvam.chat_429":                    OutcomeRecovered,
		"sarvam.chat_503":                    OutcomeRecovered,
		"sarvam.chat_timeout":                OutcomeRecovered,
		"sarvam.stt_close":                   OutcomeRecovered,
		"sarvam.tts_close":                   OutcomeRecovered,
	}
	checks := RunFailureMatrix()
	assertChecks(t, checks, want)

	wantTransitions := map[string]string{
		"agentcore.session_stop":             "ACTIVE -> TERMINAL -> REPLACEMENT REQUIRED -> RECOVERED",
		"cognito.token_expired":              "ACTIVE -> MEDIA PAUSED -> REATTACH REQUIRED -> RECOVERED",
		"datachannel.drop_media_alive":       "ACTIVE -> CONTROL LOST -> PEER REBUILT -> RECOVERED",
		"dynamodb.throttle":                  "ACTIVE -> THROTTLED -> LEASE RECONCILED -> RECOVERED",
		"mobile.client_death_without_delete": "ACTIVE -> ABANDONED -> LEASE EXPIRED -> CAPACITY REPAIRED",
		"mobile.network_switch":              "ACTIVE -> PATH CHANGED -> ICE RESTARTED -> RECOVERED",
		"nat.stop_recover":                   "ACTIVE -> NAT UNHEALTHY -> RELAY ICE RESTARTED -> RECOVERED",
		"sarvam.chat_429":                    "ACTIVE -> CHAT RATE LIMITED -> BOUNDED RETRY -> RECOVERED",
		"sarvam.chat_503":                    "ACTIVE -> CHAT UNAVAILABLE -> BOUNDED RETRY -> RECOVERED",
		"sarvam.chat_timeout":                "ACTIVE -> CHAT TIMED OUT -> BOUNDED RETRY -> RECOVERED",
		"sarvam.stt_close":                   "ACTIVE -> STT CLOSED -> NEXT TURN STREAM -> RECOVERED",
		"sarvam.tts_close":                   "ACTIVE -> TTS CLOSED -> GENERATION PURGED -> RECOVERED",
	}
	for _, check := range checks {
		if check.Detail != wantTransitions[check.ID] {
			t.Errorf("failure %q transition trace = %q, want %q", check.ID, check.Detail, wantTransitions[check.ID])
		}
	}
}

func TestSecurityMatrixBlocksEveryPlannedAttackWithoutSecretsInEvidence(t *testing.T) {
	want := map[string]string{
		"attach.cross_tenant":          OutcomeBlocked,
		"cognito.wrong_client":         OutcomeBlocked,
		"logs.authorization_redaction": OutcomeBlocked,
		"logs.provider_key_redaction":  OutcomeBlocked,
		"payload.datachannel_oversize": OutcomeBlocked,
		"payload.signaling_oversize":   OutcomeBlocked,
		"runtime_id.reuse":             OutcomeBlocked,
		"ssrf.dns_rebinding":           OutcomeBlocked,
		"ssrf.localhost":               OutcomeBlocked,
		"ssrf.metadata_ipv4":           OutcomeBlocked,
		"ssrf.metadata_ipv6":           OutcomeBlocked,
		"ssrf.private_ipv4":            OutcomeBlocked,
		"tool.unallowlisted_url":       OutcomeBlocked,
	}
	checks := RunSecurityMatrix()
	assertChecks(t, checks, want)
	for _, check := range checks {
		for _, secret := range []string{"Bearer security-canary", "sarvam-security-canary", "AKIAIOSFODNN7EXAMPLE"} {
			if contains(check.Detail, secret) {
				t.Fatalf("security evidence for %q leaked %q: %q", check.ID, secret, check.Detail)
			}
		}
	}
}

func TestRedactLogFieldsRedactsOpaqueCredentialValuesByFieldName(t *testing.T) {
	fields := map[string]string{
		"provider_key":        "opaque-provider-value",
		"aws_access_key":      "opaque-aws-value",
		"subscription-key":    "opaque-subscription-value",
		"ordinary_safe_field": "kept",
	}
	redacted := RedactLogFields(fields)
	for _, key := range []string{"provider_key", "aws_access_key", "subscription-key"} {
		if redacted[key] != "[REDACTED]" {
			t.Errorf("credential field %q = %q, want redacted", key, redacted[key])
		}
	}
	if redacted["ordinary_safe_field"] != "kept" {
		t.Fatalf("safe field = %q, want kept", redacted["ordinary_safe_field"])
	}
	if fields["provider_key"] != "opaque-provider-value" {
		t.Fatal("RedactLogFields mutated its input")
	}
}

func TestResolutionValidationRejectsPrivateAnswersOnEveryConnection(t *testing.T) {
	if err := ValidateResolvedAddresses([]netip.Addr{netip.MustParseAddr("8.8.8.8")}); err != nil {
		t.Fatalf("public resolution error = %v", err)
	}
	if err := ValidateResolvedAddresses([]netip.Addr{netip.MustParseAddr("127.0.0.1")}); !errors.Is(err, ErrUnsafeEndpoint) {
		t.Fatalf("rebound resolution error = %v, want ErrUnsafeEndpoint", err)
	}
}

func TestValidateLiveGateRequiresEveryIndependentSafetyGate(t *testing.T) {
	valid := validLiveGate()
	if err := ValidateLiveGate(valid); err != nil {
		t.Fatalf("ValidateLiveGate(valid) error = %v", err)
	}

	tests := []struct {
		name   string
		mutate func(*LiveGate)
	}{
		{name: "dry run not explicitly false", mutate: func(gate *LiveGate) { gate.DryRun = true }},
		{name: "wrong acknowledgement", mutate: func(gate *LiveGate) { gate.Acknowledgement = "yes" }},
		{name: "production environment", mutate: func(gate *LiveGate) { gate.Environment = "production" }},
		{name: "unknown stage", mutate: func(gate *LiveGate) { gate.StageID = "custom" }},
		{name: "stage session mismatch", mutate: func(gate *LiveGate) { gate.Sessions = 119 }},
		{name: "session ceiling", mutate: func(gate *LiveGate) { gate.Sessions = 121 }},
		{name: "duration ceiling", mutate: func(gate *LiveGate) { gate.Duration = 30*time.Minute + time.Second }},
		{name: "mutable revision", mutate: func(gate *LiveGate) { gate.Revision = "main" }},
		{name: "mutable image", mutate: func(gate *LiveGate) { gate.ImageDigest = "latest" }},
		{name: "loopback API", mutate: func(gate *LiveGate) { gate.Endpoints.API = "https://127.0.0.1" }},
		{name: "non TLS AgentCore", mutate: func(gate *LiveGate) { gate.Endpoints.AgentCore = "http://agentcore.example.com" }},
		{name: "non WSS STT", mutate: func(gate *LiveGate) { gate.Endpoints.STT = "ws://stt.example.com/stream" }},
		{name: "localhost chat", mutate: func(gate *LiveGate) { gate.Endpoints.Chat = "https://localhost/chat" }},
		{name: "private TTS", mutate: func(gate *LiveGate) { gate.Endpoints.TTS = "wss://10.0.0.1/tts" }},
		{name: "spend confirmation", mutate: func(gate *LiveGate) { gate.ConfirmSpend = false }},
		{name: "quota confirmation", mutate: func(gate *LiveGate) { gate.ConfirmQuotas = false }},
		{name: "staging confirmation", mutate: func(gate *LiveGate) { gate.ConfirmStaging = false }},
		{name: "DNS confirmation", mutate: func(gate *LiveGate) { gate.ConfirmDNSRevalidation = false }},
		{name: "provider confirmation", mutate: func(gate *LiveGate) { gate.ConfirmProviderApproval = false }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate := valid
			test.mutate(&candidate)
			if err := ValidateLiveGate(candidate); !errors.Is(err, ErrLiveGateClosed) {
				t.Fatalf("ValidateLiveGate() error = %v, want ErrLiveGateClosed", err)
			}
		})
	}
}

func validLiveGate() LiveGate {
	return LiveGate{
		StageID:         "aws-fake-120",
		Sessions:        120,
		Duration:        30 * time.Minute,
		DryRun:          false,
		Environment:     "staging",
		Acknowledgement: LiveAcknowledgement,
		Revision:        "0123456789abcdef0123456789abcdef01234567",
		ImageDigest:     "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		Endpoints: LiveEndpoints{
			API:       "https://api.example.com/staging",
			AgentCore: "https://agentcore.example.com/invocations",
			STT:       "wss://stt.example.com/stream",
			Chat:      "https://chat.example.com/v1/chat",
			TTS:       "wss://tts.example.com/stream",
		},
		ConfirmSpend:            true,
		ConfirmQuotas:           true,
		ConfirmStaging:          true,
		ConfirmDNSRevalidation:  true,
		ConfirmProviderApproval: true,
	}
}

func assertChecks(t *testing.T, checks []Check, want map[string]string) {
	t.Helper()
	if len(checks) != len(want) {
		t.Fatalf("checks = %d, want %d: %#v", len(checks), len(want), checks)
	}
	seen := make(map[string]struct{}, len(checks))
	for _, check := range checks {
		if _, duplicate := seen[check.ID]; duplicate {
			t.Errorf("duplicate check ID %q", check.ID)
		}
		seen[check.ID] = struct{}{}
		if check.Evidence != StatusModeledLocal || check.Outcome != want[check.ID] {
			t.Errorf("check %q = outcome %q evidence %q, want %q/%q", check.ID, check.Outcome, check.Evidence, want[check.ID], StatusModeledLocal)
		}
	}
}

func contains(value, fragment string) bool {
	for index := 0; index+len(fragment) <= len(value); index++ {
		if value[index:index+len(fragment)] == fragment {
			return true
		}
	}
	return false
}
