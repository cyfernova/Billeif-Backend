// Package loadtest provides a deterministic, provider-free voice validation
// model. It contains no network or cloud client and cannot execute a live run.
package loadtest

import "time"

const (
	SchemaVersion = 1

	ModeDeterministicFake = "deterministic_fake"

	StatusVerifiedLocal = "VERIFIED LOCAL"
	StatusModeledLocal  = "MODELED LOCAL"
	StatusNotExecuted   = "NOT EXECUTED"

	OutcomeVerified         = "VERIFIED"
	OutcomeRecovered        = "RECOVERED"
	OutcomeCapacityRepaired = "CAPACITY REPAIRED"
	OutcomeBlocked          = "BLOCKED"
)

// ProviderMode identifies the provider dependency planned for a live stage.
type ProviderMode string

const (
	ProviderFakeSarvam ProviderMode = "fake_sarvam"
	ProviderRealSarvam ProviderMode = "real_sarvam"
)

// CostFields deliberately uses strings so unmeasured values remain blank
// rather than becoming misleading numeric zeroes.
type CostFields struct {
	AgentCoreUSD          string `json:"agentcore_usd"`
	KVSTURNUSD            string `json:"kvs_turn_usd"`
	NATUSD                string `json:"nat_usd"`
	DynamoDBUSD           string `json:"dynamodb_usd"`
	APIGatewayLambdaUSD   string `json:"api_gateway_lambda_usd"`
	SarvamUSD             string `json:"sarvam_usd"`
	TotalUSD              string `json:"total_usd"`
	USDPerConnectedMinute string `json:"usd_per_connected_minute"`
}

// LiveMetrics stores only collected live evidence. Every field is blank until
// a separately authorized live execution produces an evidence artifact.
type LiveMetrics struct {
	SessionCreationSuccessPercent string `json:"session_creation_success_percent"`
	TURNConnectionSuccessPercent  string `json:"turn_connection_success_percent"`
	EndOfSpeechFirstAudioP95MS    string `json:"end_of_speech_first_audio_p95_ms"`
	InterruptToSilenceP95MS       string `json:"interrupt_to_silence_p95_ms"`
	AgentCoreResidentMemoryP95MiB string `json:"agentcore_resident_memory_p95_mib"`
	AgentCoreCPUP95Percent        string `json:"agentcore_cpu_p95_percent"`
	NATConntrackPeak              string `json:"nat_conntrack_peak"`
	KVSAllocationsPeak            string `json:"kvs_allocations_peak"`
	DynamoDBReadCapacityUnits     string `json:"dynamodb_read_capacity_units"`
	DynamoDBWriteCapacityUnits    string `json:"dynamodb_write_capacity_units"`
	APIGatewayRequests            string `json:"api_gateway_requests"`
	LambdaInvocations             string `json:"lambda_invocations"`
}

// LiveStage is one planned billable validation stage. It is schema, not proof.
type LiveStage struct {
	ID                 string       `json:"id"`
	Provider           ProviderMode `json:"provider"`
	Sessions           int          `json:"sessions"`
	DurationMinutes    int          `json:"duration_minutes"`
	ExecutionStatus    string       `json:"execution_status"`
	PassCriteriaStatus string       `json:"pass_criteria_status"`
	Metrics            LiveMetrics  `json:"metrics"`
	Cost               CostFields   `json:"cost"`
}

// LocalMetrics are virtual counters. They are never production measurements.
type LocalMetrics struct {
	SessionsAttempted        int `json:"sessions_attempted"`
	SessionsAdmitted         int `json:"sessions_admitted"`
	SessionsCompleted        int `json:"sessions_completed"`
	VirtualConnectedMinutes  int `json:"virtual_connected_minutes"`
	InboundAudioFrames       int `json:"inbound_audio_frames"`
	OutboundAudioFrames      int `json:"outbound_audio_frames"`
	PlayedAudioFrames        int `json:"played_audio_frames"`
	BargeIns                 int `json:"barge_ins"`
	StaleFramesDropped       int `json:"stale_frames_dropped"`
	StaleFramesPlayed        int `json:"stale_frames_played"`
	ICERestarts              int `json:"ice_restarts"`
	FinalTranscripts         int `json:"final_transcripts"`
	ChatRequests             int `json:"chat_requests"`
	ChatBeforeFinal          int `json:"chat_before_final"`
	TTSStreams               int `json:"tts_streams"`
	PlaybackAcknowledgments  int `json:"playback_acknowledgments"`
	CapacityHeldBeforeRepair int `json:"capacity_held_before_repair"`
	CapacityRepairs          int `json:"capacity_repairs"`
	CapacityLeaksAfterRepair int `json:"capacity_leaks_after_repair"`
}

// Check is a sanitized local assertion or injected-scenario result.
type Check struct {
	ID       string `json:"id"`
	Outcome  string `json:"outcome"`
	Evidence string `json:"evidence"`
	Detail   string `json:"detail"`
}

// Report separates local model verification from unexecuted live evidence.
type Report struct {
	SchemaVersion                int          `json:"schema_version"`
	Mode                         string       `json:"mode"`
	DryRun                       bool         `json:"dry_run"`
	LocalStatus                  string       `json:"local_status"`
	Metrics                      LocalMetrics `json:"local_metrics"`
	Invariants                   []Check      `json:"invariants"`
	Failures                     []Check      `json:"failure_injection"`
	Security                     []Check      `json:"security"`
	LiveStages                   []LiveStage  `json:"live_stages"`
	ProductionPassCriteriaStatus string       `json:"production_pass_criteria_status"`
	Cost                         CostFields   `json:"cost"`
}

// DryRunConfig bounds the virtual model. VirtualDuration never causes sleep.
type DryRunConfig struct {
	Sessions        int
	VirtualDuration time.Duration
	DryRun          bool
}

// DefaultDryRunConfig is safe for ordinary CI and local execution.
func DefaultDryRunConfig() DryRunConfig {
	return DryRunConfig{Sessions: 120, VirtualDuration: 30 * time.Minute, DryRun: true}
}

// PlannedLiveStages returns detached, empty evidence records for every stage
// from Task 16. Zero duration means the live duration remains operator-defined.
func PlannedLiveStages() []LiveStage {
	definitions := []struct {
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
	stages := make([]LiveStage, 0, len(definitions))
	for _, definition := range definitions {
		stages = append(stages, LiveStage{
			ID:                 definition.id,
			Provider:           definition.provider,
			Sessions:           definition.sessions,
			DurationMinutes:    definition.durationMinutes,
			ExecutionStatus:    StatusNotExecuted,
			PassCriteriaStatus: StatusNotExecuted,
		})
	}
	return stages
}
