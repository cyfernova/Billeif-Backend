//go:build voice_live_load

package main

import (
	"flag"
	"io"

	"invoice-backend/internal/voice/loadtest"
)

type liveFlags struct {
	stageID                 *string
	environment             *string
	revision                *string
	imageDigest             *string
	apiEndpoint             *string
	agentCoreEndpoint       *string
	sttEndpoint             *string
	chatEndpoint            *string
	ttsEndpoint             *string
	confirmSpend            *bool
	confirmQuotas           *bool
	confirmStaging          *bool
	confirmDNSRevalidation  *bool
	confirmProviderApproval *bool
}

func registerLiveFlags(flags *flag.FlagSet, common commandOptions) liveCommand {
	live := liveFlags{
		stageID:                 flags.String("stage", "", "exact planned live stage ID"),
		environment:             flags.String("environment", "", "must be staging"),
		revision:                flags.String("revision", "", "immutable 40-character source revision"),
		imageDigest:             flags.String("image-digest", "", "immutable sha256 image digest"),
		apiEndpoint:             flags.String("api-endpoint", "", "explicit remote HTTPS API endpoint"),
		agentCoreEndpoint:       flags.String("agentcore-endpoint", "", "explicit remote HTTPS AgentCore endpoint"),
		sttEndpoint:             flags.String("stt-endpoint", "", "explicit remote WSS STT endpoint"),
		chatEndpoint:            flags.String("chat-endpoint", "", "explicit remote HTTPS chat endpoint"),
		ttsEndpoint:             flags.String("tts-endpoint", "", "explicit remote WSS TTS endpoint"),
		confirmSpend:            flags.Bool("confirm-spend", false, "confirm the run can incur charges"),
		confirmQuotas:           flags.Bool("confirm-quotas", false, "confirm provider and AWS quotas"),
		confirmStaging:          flags.Bool("confirm-staging", false, "confirm the target is isolated staging"),
		confirmDNSRevalidation:  flags.Bool("confirm-dns-revalidation", false, "confirm every connection revalidates DNS answers"),
		confirmProviderApproval: flags.Bool("confirm-provider-approval", false, "confirm provider approval for the requested stage"),
	}
	return func(_ io.Writer, getenv func(string) string) error {
		acknowledgement := ""
		if getenv != nil {
			acknowledgement = getenv(loadtest.LiveAcknowledgementEnvironment)
		}
		gate := loadtest.LiveGate{
			StageID:         *live.stageID,
			Sessions:        *common.sessions,
			Duration:        *common.duration,
			DryRun:          *common.dryRun,
			Environment:     *live.environment,
			Acknowledgement: acknowledgement,
			Revision:        *live.revision,
			ImageDigest:     *live.imageDigest,
			Endpoints: loadtest.LiveEndpoints{
				API: *live.apiEndpoint, AgentCore: *live.agentCoreEndpoint, STT: *live.sttEndpoint, Chat: *live.chatEndpoint, TTS: *live.ttsEndpoint,
			},
			ConfirmSpend:            *live.confirmSpend,
			ConfirmQuotas:           *live.confirmQuotas,
			ConfirmStaging:          *live.confirmStaging,
			ConfirmDNSRevalidation:  *live.confirmDNSRevalidation,
			ConfirmProviderApproval: *live.confirmProviderApproval,
		}
		if err := loadtest.ValidateLiveGate(gate); err != nil {
			return err
		}
		// Deliberately no transport is linked yet. A future implementation must
		// preserve this preflight and add a separately reviewed executor.
		return loadtest.ErrLiveExecutorUnavailable
	}
}
