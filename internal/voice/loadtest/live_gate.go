package loadtest

import (
	"errors"
	"regexp"
	"time"
)

const (
	LiveAcknowledgementEnvironment = "BILLEIF_VOICE_LIVE_LOAD_ACK"
	LiveAcknowledgement            = "I_ACKNOWLEDGE_LIVE_VOICE_LOAD_MAY_INCUR_COSTS"
)

var (
	ErrLiveGateClosed          = errors.New("live voice load gate is closed")
	ErrLiveExecutorUnavailable = errors.New("live voice load executor is intentionally unavailable")
	fullRevisionPattern        = regexp.MustCompile(`^[0-9a-f]{40}$`)
	imageDigestPattern         = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
)

// LiveEndpoints must all be explicit remote TLS endpoints.
type LiveEndpoints struct {
	API       string
	AgentCore string
	STT       string
	Chat      string
	TTS       string
}

// LiveGate is a preflight contract only. This package has no live executor.
type LiveGate struct {
	StageID         string
	Sessions        int
	Duration        time.Duration
	DryRun          bool
	Environment     string
	Acknowledgement string
	Revision        string
	ImageDigest     string
	Endpoints       LiveEndpoints

	ConfirmSpend            bool
	ConfirmQuotas           bool
	ConfirmStaging          bool
	ConfirmDNSRevalidation  bool
	ConfirmProviderApproval bool
}

// ValidateLiveGate requires every safety condition independently. It never
// resolves DNS or opens a connection.
func ValidateLiveGate(gate LiveGate) error {
	stage, ok := plannedStage(gate.StageID)
	if !ok || gate.DryRun || gate.Environment != "staging" || gate.Acknowledgement != LiveAcknowledgement ||
		gate.Sessions <= 0 || gate.Sessions > maxDryRunSessions || gate.Sessions != stage.Sessions ||
		gate.Duration <= 0 || gate.Duration > maxDryRunDuration ||
		(stage.ID == "aws-fake-120" && gate.Duration != 30*time.Minute) ||
		!fullRevisionPattern.MatchString(gate.Revision) || !imageDigestPattern.MatchString(gate.ImageDigest) ||
		!gate.ConfirmSpend || !gate.ConfirmQuotas || !gate.ConfirmStaging || !gate.ConfirmDNSRevalidation || !gate.ConfirmProviderApproval {
		return ErrLiveGateClosed
	}
	if validateEndpoint(gate.Endpoints.API, map[string]struct{}{"https": {}}, "") != nil ||
		validateEndpoint(gate.Endpoints.AgentCore, map[string]struct{}{"https": {}}, "") != nil ||
		validateEndpoint(gate.Endpoints.STT, map[string]struct{}{"wss": {}}, "") != nil ||
		validateEndpoint(gate.Endpoints.Chat, map[string]struct{}{"https": {}}, "") != nil ||
		validateEndpoint(gate.Endpoints.TTS, map[string]struct{}{"wss": {}}, "") != nil {
		return ErrLiveGateClosed
	}
	return nil
}

func plannedStage(id string) (LiveStage, bool) {
	for _, stage := range PlannedLiveStages() {
		if stage.ID == id {
			return stage, true
		}
	}
	return LiveStage{}, false
}
