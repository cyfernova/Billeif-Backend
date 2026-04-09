package awsclients

import (
	"context"

	appconfig "invoice-backend/internal/config"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
	"github.com/aws/aws-sdk-go-v2/service/wafv2"
)

type Config struct {
	SDKConfig aws.Config

	Cognito  *cognitoidentityprovider.Client
	DynamoDB *dynamodb.Client
	S3       *s3.Client
	SES      *ses.Client
	SSM      *ssm.Client
	SQS      *sqs.Client
	SNS      *sns.Client
	WAF      *wafv2.Client
}

func New(ctx context.Context, cfg appconfig.AWSConfig, log *logger.Logger) (*Config, error) {
	if log == nil {
		log = logger.Global()
	}
	log = log.Named("aws_clients")
	log.Info("initializing AWS clients", "region", cfg.Region, "custom_endpoint", cfg.Endpoint != "")

	awsCfg, err := loadConfig(ctx, cfg)
	if err != nil {
		log.Error("failed to load AWS SDK config", "error", err, "region", cfg.Region)
		return nil, err
	}

	clients := &Config{
		SDKConfig: awsCfg,
		Cognito: cognitoidentityprovider.NewFromConfig(awsCfg, func(o *cognitoidentityprovider.Options) {
			if cfg.Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.Endpoint)
			}
		}),
		DynamoDB: dynamodb.NewFromConfig(awsCfg, func(o *dynamodb.Options) {
			if cfg.Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.Endpoint)
			}
		}),
		S3: s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			if cfg.Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.Endpoint)
			}
		}),
		SES: ses.NewFromConfig(awsCfg, func(o *ses.Options) {
			if cfg.Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.Endpoint)
			}
		}),
		SSM: ssm.NewFromConfig(awsCfg, func(o *ssm.Options) {
			if cfg.Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.Endpoint)
			}
		}),
		SQS: sqs.NewFromConfig(awsCfg, func(o *sqs.Options) {
			if cfg.Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.Endpoint)
			}
		}),
		SNS: sns.NewFromConfig(awsCfg, func(o *sns.Options) {
			if cfg.Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.Endpoint)
			}
		}),
		WAF: wafv2.NewFromConfig(awsCfg, func(o *wafv2.Options) {
			if cfg.Endpoint != "" {
				o.BaseEndpoint = aws.String(cfg.Endpoint)
			}
		}),
	}

	log.Info("AWS clients initialized")
	return clients, nil
}

func loadConfig(ctx context.Context, cfg appconfig.AWSConfig) (aws.Config, error) {
	loaders := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
	}

	if cfg.AccessKey != "" && cfg.SecretKey != "" {
		loaders = append(loaders, config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey,
			cfg.SecretKey,
			cfg.SessionToken,
		)))
	}

	return config.LoadDefaultConfig(ctx, loaders...)
}
