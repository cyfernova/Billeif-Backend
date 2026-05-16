package config

import (
	"context"
	"fmt"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

func resolveSSMParameters(cfg *Config) error {
	if cfg == nil {
		return fmt.Errorf("config is nil")
	}

	paramTargets := map[string]*string{
		cfg.SSM.DatabaseHostParam:            &cfg.Database.Host,
		cfg.SSM.DatabaseUserParam:            &cfg.Database.User,
		cfg.SSM.DatabasePasswordParam:        &cfg.Database.Password,
		cfg.SSM.CredentialEncryptionKeyParam: &cfg.Credentials.EncryptionKey,
		cfg.SSM.RazorpayKeyIDParam:           &cfg.Razorpay.KeyID,
		cfg.SSM.RazorpayKeySecretParam:       &cfg.Razorpay.KeySecret,
		cfg.SSM.RazorpayWebhookSecretParam:   &cfg.Razorpay.WebhookSecret,
	}

	hasParams := false
	for name, target := range paramTargets {
		if name != "" && (target == nil || strings.TrimSpace(*target) == "") {
			hasParams = true
			break
		}
	}
	if !hasParams {
		return nil
	}

	loaders := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.AWS.Region),
	}
	if cfg.AWS.AccessKey != "" && cfg.AWS.SecretKey != "" {
		loaders = append(loaders, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AWS.AccessKey,
			cfg.AWS.SecretKey,
			cfg.AWS.SessionToken,
		)))
	}

	awsCfg, err := config.LoadDefaultConfig(context.Background(), loaders...)
	if err != nil {
		return fmt.Errorf("load aws config for ssm: %w", err)
	}

	client := ssm.NewFromConfig(awsCfg)
	for paramName, target := range paramTargets {
		if paramName == "" {
			continue
		}
		if target != nil && strings.TrimSpace(*target) != "" {
			continue
		}

		out, err := client.GetParameter(context.Background(), &ssm.GetParameterInput{
			Name:           &paramName,
			WithDecryption: aws.Bool(true),
		})
		if err != nil {
			return fmt.Errorf("get SSM parameter %s: %w", paramName, err)
		}
		if out.Parameter != nil && out.Parameter.Value != nil {
			*target = *out.Parameter.Value
		}
	}

	return nil
}
