package config

import (
	"context"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

type RuntimeResolvers struct {
	Secrets SecretsManagerAPI
	SSM     SSMAPI
}

func ResolveRuntime(ctx context.Context, cfg *Config, clients RuntimeResolvers) error {
	if cfg == nil {
		return &ConfigurationError{Resource: "application config", Reason: "config is required"}
	}
	needsDBSecret := strings.TrimSpace(cfg.Database.User) == "" || strings.TrimSpace(cfg.Database.Password) == ""
	needsDBHost := strings.TrimSpace(cfg.Database.Host) == "" && strings.TrimSpace(cfg.SSM.DatabaseHostParam) != ""
	secretBindings := cfg.secretBindings()
	needsSecrets := needsDBSecret
	for _, binding := range secretBindings {
		if strings.TrimSpace(*binding.target) == "" && strings.TrimSpace(binding.identifier) != "" {
			needsSecrets = true
			break
		}
	}
	if !needsSecrets && !needsDBHost {
		return validate(cfg)
	}

	if clients.Secrets == nil && needsSecrets || clients.SSM == nil && needsDBHost {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWS.Region))
		if err != nil {
			return &ResolutionError{Resource: "AWS SDK configuration"}
		}
		if clients.Secrets == nil {
			clients.Secrets = secretsmanager.NewFromConfig(awsCfg)
		}
		if clients.SSM == nil {
			clients.SSM = ssm.NewFromConfig(awsCfg)
		}
	}

	if needsDBHost {
		resolver, err := NewSSMResolver(clients.SSM, []string{cfg.SSM.DatabaseHostParam}, 5*time.Minute, time.Now)
		if err != nil {
			return err
		}
		values, err := resolver.Get(ctx, []string{cfg.SSM.DatabaseHostParam})
		if err != nil {
			return err
		}
		cfg.Database.Host = values[cfg.SSM.DatabaseHostParam]
	}

	identifiers := cfg.configuredSecretIdentifiers()
	if len(identifiers) == 0 && needsSecrets {
		return &ConfigurationError{Resource: "secret identifiers", Reason: "required identifier is missing"}
	}
	if len(identifiers) != 0 {
		resolver, err := NewSecretResolver(clients.Secrets, identifiers, 5*time.Minute, time.Now)
		if err != nil {
			return err
		}
		if needsDBSecret {
			if strings.TrimSpace(cfg.Secrets.Database) == "" {
				return &ConfigurationError{Resource: "database secret identifier", Reason: "identifier is required"}
			}
			if strings.TrimSpace(cfg.Database.User) == "" {
				cfg.Database.User, err = resolver.JSONField(ctx, cfg.Secrets.Database, "username")
				if err != nil {
					return err
				}
			}
			if strings.TrimSpace(cfg.Database.Password) == "" {
				cfg.Database.Password, err = resolver.JSONField(ctx, cfg.Secrets.Database, "password")
				if err != nil {
					return err
				}
			}
		}
		for _, binding := range secretBindings {
			if strings.TrimSpace(*binding.target) != "" || strings.TrimSpace(binding.identifier) == "" {
				continue
			}
			value, err := resolver.JSONField(ctx, binding.identifier, binding.field)
			if err != nil {
				return err
			}
			*binding.target = value
		}
	}
	cfg.VoiceRealtime = cfg.VoiceRealtime.WithDefaults(cfg.Deepgram)
	return validate(cfg)
}

type secretBinding struct {
	identifier string
	field      string
	target     *string
}

func (c *Config) secretBindings() []secretBinding {
	return []secretBinding{
		{c.Secrets.CredentialEncryption, "encryption_key", &c.Credentials.EncryptionKey},
		{c.Secrets.Razorpay, "key_id", &c.Razorpay.KeyID},
		{c.Secrets.Razorpay, "key_secret", &c.Razorpay.KeySecret},
		{c.Secrets.Razorpay, "webhook_secret", &c.Razorpay.WebhookSecret},
		{c.Secrets.LLM, "api_key", &c.LLM.APIKey},
		{c.Secrets.Exa, "api_key", &c.LLM.ExaAPIKey},
		{c.Secrets.GSTLookup, "api_key", &c.GSTLookup.APIKey},
		{c.Secrets.Deepgram, "api_key", &c.Deepgram.APIKey},
		{c.Secrets.Deepgram, "api_key", &c.VoiceRealtime.DeepgramAPIKey},
		{c.Secrets.DeepSeek, "api_key", &c.VoiceRealtime.DeepSeekAPIKey},
	}
}

func (c *Config) configuredSecretIdentifiers() []string {
	values := []string{
		c.Secrets.Database,
		c.Secrets.CredentialEncryption,
		c.Secrets.Razorpay,
		c.Secrets.LLM,
		c.Secrets.Exa,
		c.Secrets.GSTLookup,
		c.Secrets.Deepgram,
		c.Secrets.DeepSeek,
	}
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
