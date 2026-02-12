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
)

type Config struct {
	Cognito  *cognitoidentityprovider.Client
	DynamoDB *dynamodb.Client
	S3       *s3.Client
	SES      *ses.Client
	SQS      *sqs.Client
	SNS      *sns.Client
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
		Cognito:  cognitoidentityprovider.NewFromConfig(awsCfg),
		DynamoDB: dynamodb.NewFromConfig(awsCfg),
		S3:       s3.NewFromConfig(awsCfg),
		SES:      ses.NewFromConfig(awsCfg),
		SQS:      sqs.NewFromConfig(awsCfg),
		SNS:      sns.NewFromConfig(awsCfg),
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
			"",
		)))
	}

	if cfg.Endpoint != "" {
		loaders = append(loaders, config.WithEndpointResolverWithOptions(
			aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               cfg.Endpoint,
					SigningRegion:     cfg.Region,
					HostnameImmutable: true,
				}, nil
			}),
		))
	}

	return config.LoadDefaultConfig(ctx, loaders...)
}
