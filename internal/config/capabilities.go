package config

import "strings"

// CapabilityConfiguration is a secret-safe presence snapshot. It intentionally
// carries no identifiers or values and must not be treated as provider health.
type CapabilityConfiguration struct {
	Razorpay  bool `json:"razorpay"`
	GST       bool `json:"gst"`
	WhatsApp  bool `json:"whatsapp"`
	Email     bool `json:"email"`
	S3Uploads bool `json:"s3_uploads"`
	AI        bool `json:"ai"`
	Voice     bool `json:"voice"`
}

func CapabilityConfigurationSnapshot(cfg *Config) CapabilityConfiguration {
	if cfg == nil {
		return CapabilityConfiguration{}
	}
	hasRazorpayCredentials := anyConfigured(cfg.Secrets.Razorpay) ||
		allConfigured(cfg.Razorpay.KeyID, cfg.Razorpay.KeySecret)
	hasGSTCredentials := anyConfigured(cfg.Secrets.GSTProvider) || anyConfigured(
		cfg.GST.APIToken,
		cfg.GST.ClientID,
		cfg.GST.ClientSecret,
		cfg.GST.Username,
		cfg.GST.Password,
	)
	hasAICredentials := anyConfigured(cfg.Secrets.LLM, cfg.LLM.APIKey)
	hasSarvamCredentials := anyConfigured(cfg.Secrets.Sarvam, cfg.Sarvam.APIKey)
	gstProvider := strings.ToLower(strings.TrimSpace(cfg.GST.Provider))

	return CapabilityConfiguration{
		Razorpay:  hasRazorpayCredentials,
		GST:       hasGSTCredentials && gstProvider != "" && gstProvider != "simulated" && anyConfigured(cfg.GST.BaseURL),
		WhatsApp:  anyConfigured(cfg.WhatsApp.BaseURL),
		Email:     allConfigured(cfg.SES.SenderEmail, cfg.SES.ConfigurationSet),
		S3Uploads: anyConfigured(cfg.S3.BucketDrive, cfg.S3.BucketInvoices, cfg.S3.BucketLogos, cfg.S3.BucketProducts),
		AI:        cfg.AIGovernance.ExecutionEnabled && hasAICredentials && anyConfigured(cfg.LLM.APIURL),
		Voice:     cfg.VoiceSession.Enabled() && cfg.VoiceSession.AdmissionEnabled && hasSarvamCredentials,
	}
}

func anyConfigured(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func allConfigured(values ...string) bool {
	for _, value := range values {
		if strings.TrimSpace(value) == "" {
			return false
		}
	}
	return len(values) > 0
}
