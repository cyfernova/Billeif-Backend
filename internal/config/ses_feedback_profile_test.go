package config

import (
	"strings"
	"testing"

	"github.com/spf13/viper"
)

func TestSESFeedbackProfileRequiresOnlyDatabaseAndCorrelationConfiguration(t *testing.T) {
	cfg := completeProductionWorkerProfileConfig(ProfileSESFeedback)
	cfg.S3 = S3Config{}
	cfg.SQS = SQSConfig{}
	cfg.SES = SESConfig{
		SendingAccountID: "123456789012",
		ConfigurationSet: "Billeif-production-ses-config",
	}

	if err := ValidateForProfile(cfg, ProfileSESFeedback); err != nil {
		t.Fatalf("minimal SES feedback profile: %v", err)
	}

	for _, test := range []struct {
		name   string
		mutate func()
		want   string
	}{
		{
			name:   "account",
			mutate: func() { cfg.SES.SendingAccountID = "" },
			want:   "SES_SENDING_ACCOUNT_ID",
		},
		{
			name:   "configuration set",
			mutate: func() { cfg.SES.ConfigurationSet = "" },
			want:   "SES_CONFIGURATION_SET",
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			original := cfg.SES
			t.Cleanup(func() { cfg.SES = original })
			test.mutate()
			err := ValidateForProfile(cfg, ProfileSESFeedback)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("error = %v, want %s", err, test.want)
			}
		})
	}
}

func TestLoadSESFeedbackProfileBindsSendingAccountEnvironment(t *testing.T) {
	viper.Reset()
	t.Cleanup(viper.Reset)
	t.Setenv("ENVIRONMENT", "production")
	t.Setenv("AWS_REGION", "ap-south-1")
	t.Setenv("DATABASE_PORT", "5432")
	t.Setenv("DATABASE_NAME", "invoice")
	t.Setenv("DATABASE_SSL_MODE", "require")
	t.Setenv("DATABASE_HOST_SSM_PARAM", "/billeif/database/host")
	t.Setenv("DATABASE_SECRET_ARN", "database-secret")
	t.Setenv("SES_SENDING_ACCOUNT_ID", "123456789012")
	t.Setenv("SES_CONFIGURATION_SET", "Billeif-production-ses-config")

	cfg, err := LoadForProfile(ProfileSESFeedback)

	if err != nil {
		t.Fatalf("load SES feedback profile: %v", err)
	}
	if cfg.SES.SendingAccountID != "123456789012" {
		t.Fatalf("sending account id = %q", cfg.SES.SendingAccountID)
	}
}
