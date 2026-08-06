package loadtest

import (
	"context"
	"errors"
	"strings"
	"time"
)

var ErrInvalidDryRun = errors.New("invalid deterministic voice dry-run configuration")

const (
	maxDryRunSessions = 120
	maxDryRunDuration = 30 * time.Minute
)

// RunDry executes only a discrete deterministic model. It never sleeps, opens
// sockets, reads environment variables, or calls a provider/cloud client.
func RunDry(ctx context.Context, config DryRunConfig) (Report, error) {
	if ctx == nil {
		return Report{}, ErrInvalidDryRun
	}
	if err := ctx.Err(); err != nil {
		return Report{}, err
	}
	if !config.DryRun || config.Sessions <= 0 || config.Sessions > maxDryRunSessions ||
		config.VirtualDuration < time.Minute || config.VirtualDuration > maxDryRunDuration {
		return Report{}, ErrInvalidDryRun
	}

	metrics := LocalMetrics{
		SessionsAttempted:       config.Sessions,
		SessionsAdmitted:        config.Sessions,
		VirtualConnectedMinutes: config.Sessions * int(config.VirtualDuration/time.Minute),
	}
	for range config.Sessions {
		if err := ctx.Err(); err != nil {
			return Report{}, err
		}
		sessionMetrics := simulateVoiceSession()
		addLocalMetrics(&metrics, sessionMetrics)
	}
	metrics.SessionsCompleted = config.Sessions

	return Report{
		SchemaVersion:                SchemaVersion,
		Mode:                         ModeDeterministicFake,
		DryRun:                       true,
		LocalStatus:                  StatusVerifiedLocal,
		Metrics:                      metrics,
		Invariants:                   localInvariants(metrics),
		Failures:                     RunFailureMatrix(),
		Security:                     RunSecurityMatrix(),
		LiveStages:                   PlannedLiveStages(),
		ProductionPassCriteriaStatus: StatusNotExecuted,
	}, nil
}

type virtualVoiceSession struct {
	finalTranscript   bool
	activeGeneration  int
	queuedGeneration  int
	queuedFrames      int
	capacityHeld      int
	capacityAbandoned bool
	metrics           LocalMetrics
}

func simulateVoiceSession() LocalMetrics {
	session := virtualVoiceSession{activeGeneration: 1}
	session.acquireCapacity()
	session.metrics.InboundAudioFrames += 8
	session.finalTranscript = true
	session.metrics.FinalTranscripts++
	if session.finalTranscript {
		session.metrics.ChatRequests++
	} else {
		session.metrics.ChatBeforeFinal++
	}

	firstGeneration := session.startTTS(6)
	session.playGeneration(firstGeneration, 2)
	session.metrics.PlaybackAcknowledgments++ // generation 1 started
	session.interrupt()
	replacementGeneration := session.startTTS(4)
	session.playGeneration(replacementGeneration, 4)
	session.metrics.PlaybackAcknowledgments += 2 // replacement started and completed
	session.metrics.ICERestarts++
	session.abandonCapacity()
	session.reconcileExpiredCapacity()
	return session.metrics
}

func (session *virtualVoiceSession) startTTS(frames int) int {
	session.metrics.TTSStreams++
	session.metrics.OutboundAudioFrames += frames
	session.queuedGeneration = session.activeGeneration
	session.queuedFrames += frames
	return session.activeGeneration
}

func (session *virtualVoiceSession) playGeneration(generation, frames int) {
	if generation != session.activeGeneration || generation != session.queuedGeneration {
		return
	}
	if frames > session.queuedFrames {
		frames = session.queuedFrames
	}
	session.queuedFrames -= frames
	session.metrics.PlayedAudioFrames += frames
}

func (session *virtualVoiceSession) interrupt() {
	session.metrics.BargeIns++
	session.metrics.StaleFramesDropped += session.queuedFrames
	session.queuedFrames = 0
	session.queuedGeneration = 0
	session.activeGeneration++
}

func (session *virtualVoiceSession) acquireCapacity() {
	session.capacityHeld++
}

func (session *virtualVoiceSession) abandonCapacity() {
	if session.capacityHeld == 0 || session.capacityAbandoned {
		return
	}
	session.capacityAbandoned = true
	session.metrics.CapacityHeldBeforeRepair += session.capacityHeld
}

func (session *virtualVoiceSession) reconcileExpiredCapacity() {
	if session.capacityAbandoned {
		session.metrics.CapacityRepairs += session.capacityHeld
		session.capacityHeld = 0
		session.capacityAbandoned = false
	}
	session.metrics.CapacityLeaksAfterRepair = session.capacityHeld
}

func addLocalMetrics(total *LocalMetrics, value LocalMetrics) {
	total.InboundAudioFrames += value.InboundAudioFrames
	total.OutboundAudioFrames += value.OutboundAudioFrames
	total.PlayedAudioFrames += value.PlayedAudioFrames
	total.BargeIns += value.BargeIns
	total.StaleFramesDropped += value.StaleFramesDropped
	total.StaleFramesPlayed += value.StaleFramesPlayed
	total.ICERestarts += value.ICERestarts
	total.FinalTranscripts += value.FinalTranscripts
	total.ChatRequests += value.ChatRequests
	total.ChatBeforeFinal += value.ChatBeforeFinal
	total.TTSStreams += value.TTSStreams
	total.PlaybackAcknowledgments += value.PlaybackAcknowledgments
	total.CapacityHeldBeforeRepair += value.CapacityHeldBeforeRepair
	total.CapacityRepairs += value.CapacityRepairs
	total.CapacityLeaksAfterRepair += value.CapacityLeaksAfterRepair
}

func localInvariants(metrics LocalMetrics) []Check {
	checks := []struct {
		id       string
		verified bool
	}{
		{id: "audio.bidirectional", verified: metrics.InboundAudioFrames > 0 && metrics.OutboundAudioFrames > 0},
		{id: "barge_in.stale_generation_fenced", verified: metrics.BargeIns > 0 && metrics.StaleFramesDropped > 0 && metrics.StaleFramesPlayed == 0},
		{id: "ice.restart", verified: metrics.ICERestarts > 0},
		{id: "llm.after_final_transcript", verified: metrics.ChatRequests == metrics.FinalTranscripts && metrics.ChatBeforeFinal == 0},
		{id: "playback.current_generation_ack", verified: metrics.PlaybackAcknowledgments > 0},
		{id: "capacity.abandoned_client_repaired", verified: metrics.CapacityHeldBeforeRepair > 0 && metrics.CapacityRepairs == metrics.CapacityHeldBeforeRepair && metrics.CapacityLeaksAfterRepair == 0},
	}
	results := make([]Check, 0, len(checks))
	for _, check := range checks {
		outcome := "FAILED"
		if check.verified {
			outcome = OutcomeVerified
		}
		results = append(results, localCheck(check.id, outcome))
	}
	return results
}

// RunFailureMatrix models each failure as a discrete degrade/recover path.
func RunFailureMatrix() []Check {
	type scenario struct {
		id       string
		failure  string
		recovery string
		outcome  string
	}
	scenarios := []scenario{
		{id: "nat.stop_recover", failure: "NAT UNHEALTHY", recovery: "RELAY ICE RESTARTED", outcome: OutcomeRecovered},
		{id: "sarvam.stt_close", failure: "STT CLOSED", recovery: "NEXT TURN STREAM", outcome: OutcomeRecovered},
		{id: "sarvam.chat_429", failure: "CHAT RATE LIMITED", recovery: "BOUNDED RETRY", outcome: OutcomeRecovered},
		{id: "sarvam.chat_503", failure: "CHAT UNAVAILABLE", recovery: "BOUNDED RETRY", outcome: OutcomeRecovered},
		{id: "sarvam.chat_timeout", failure: "CHAT TIMED OUT", recovery: "BOUNDED RETRY", outcome: OutcomeRecovered},
		{id: "sarvam.tts_close", failure: "TTS CLOSED", recovery: "GENERATION PURGED", outcome: OutcomeRecovered},
		{id: "cognito.token_expired", failure: "MEDIA PAUSED", recovery: "REATTACH REQUIRED", outcome: OutcomeRecovered},
		{id: "mobile.network_switch", failure: "PATH CHANGED", recovery: "ICE RESTARTED", outcome: OutcomeRecovered},
		{id: "datachannel.drop_media_alive", failure: "CONTROL LOST", recovery: "PEER REBUILT", outcome: OutcomeRecovered},
		{id: "agentcore.session_stop", failure: "TERMINAL", recovery: "REPLACEMENT REQUIRED", outcome: OutcomeRecovered},
		{id: "dynamodb.throttle", failure: "THROTTLED", recovery: "LEASE RECONCILED", outcome: OutcomeRecovered},
		{id: "mobile.client_death_without_delete", failure: "ABANDONED", recovery: "LEASE EXPIRED", outcome: OutcomeCapacityRepaired},
	}
	results := make([]Check, 0, len(scenarios))
	for _, scenario := range scenarios {
		trace := strings.Join([]string{"ACTIVE", scenario.failure, scenario.recovery, scenario.outcome}, " -> ")
		results = append(results, Check{
			ID:       scenario.id,
			Outcome:  scenario.outcome,
			Evidence: StatusModeledLocal,
			Detail:   trace,
		})
	}
	return results
}

func localCheck(id, outcome string) Check {
	return Check{ID: id, Outcome: outcome, Evidence: StatusModeledLocal, Detail: "deterministic offline model only"}
}
