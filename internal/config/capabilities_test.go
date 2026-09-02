package config

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCapabilityConfigurationSnapshotReportsPresenceWithoutSensitiveValues(t *testing.T) {
	cfg := &Config{
		Secrets: SecretIdentifiers{
			Razorpay:    "arn:aws:secretsmanager:ap-south-1:123:secret:razorpay-prod",
			GSTProvider: "arn:aws:secretsmanager:ap-south-1:123:secret:gst-prod",
			LLM:         "arn:aws:secretsmanager:ap-south-1:123:secret:llm-prod",
			Sarvam:      "arn:aws:secretsmanager:ap-south-1:123:secret:sarvam-prod",
		},
		Razorpay:     RazorpayConfig{BaseURL: "https://api.razorpay.com"},
		GST:          GSTConfig{Provider: "cleartax", BaseURL: "https://gst.example.test"},
		WhatsApp:     WhatsAppConfig{BaseURL: "https://graph.facebook.com/v20.0"},
		SES:          SESConfig{SenderEmail: "billing@example.test", ConfigurationSet: "prod-events"},
		S3:           S3Config{BucketDrive: "private-drive"},
		LLM:          LLMConfig{APIURL: "https://llm.example.test", Model: "model-internal"},
		AIGovernance: AIGovernanceConfig{ExecutionEnabled: true},
		Sarvam:       SarvamConfig{BaseURL: "https://api.sarvam.ai"},
		VoiceSession: VoiceSessionConfig{
			TableName: "voice-private-table", AgentRuntimeARN: "arn:aws:bedrock-agentcore:runtime/private",
			AdmissionEnabled: true,
		},
	}

	snapshot := CapabilityConfigurationSnapshot(cfg)
	require.True(t, snapshot.Razorpay)
	require.True(t, snapshot.GST)
	require.True(t, snapshot.WhatsApp)
	require.True(t, snapshot.Email)
	require.True(t, snapshot.S3Uploads)
	require.True(t, snapshot.AI)
	require.True(t, snapshot.Voice)

	raw, err := json.Marshal(snapshot)
	require.NoError(t, err)
	for _, sensitive := range []string{"razorpay-prod", "gst-prod", "llm-prod", "sarvam-prod", "private-drive", "private-table", "model-internal", "123"} {
		require.NotContains(t, strings.ToLower(string(raw)), strings.ToLower(sensitive))
	}
}

func TestCapabilityConfigurationSnapshotDoesNotConfuseDefaultsWithCredentials(t *testing.T) {
	snapshot := CapabilityConfigurationSnapshot(&Config{
		WhatsApp: WhatsAppConfig{BaseURL: "https://graph.facebook.com/v20.0"},
		Sarvam:   SarvamConfig{BaseURL: "https://api.sarvam.ai"},
	})

	require.True(t, snapshot.WhatsApp)
	require.False(t, snapshot.Razorpay)
	require.False(t, snapshot.GST)
	require.False(t, snapshot.Email)
	require.False(t, snapshot.S3Uploads)
	require.False(t, snapshot.AI)
	require.False(t, snapshot.Voice)
}

func TestCapabilityConfigurationSnapshotRejectsSimulatedGSTProvider(t *testing.T) {
	snapshot := CapabilityConfigurationSnapshot(&Config{
		Secrets: SecretIdentifiers{GSTProvider: "gst-secret"},
		GST:     GSTConfig{Provider: "simulated", BaseURL: "https://gst.example.test"},
	})

	require.False(t, snapshot.GST)
}
