package awsclients

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/cognitoidentityprovider"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/sns"
	"github.com/aws/aws-sdk-go-v2/service/sqs"
	appconfig "invoice-backend/internal/config"
)

type Config struct {
	Cognito  *cognitoidentityprovider.Client
	DynamoDB *dynamodb.Client
	S3       *s3.Client
	SES      *ses.Client
	SQS      *sqs.Client
	SNS      *sns.Client
}

func New(ctx context.Context, cfg appconfig.AWSConfig) (*Config, error) {
	awsCfg, err := loadConfig(ctx, cfg)
	if err != nil {
		return nil, err
	}

	clients := &Config{
		Cognito:  cognitoidentityprovider.NewFromConfig(awsCfg),
		DynamoDB: dynamodb.NewFromConfig(awsCfg),
		S3: s3.NewFromConfig(awsCfg, func(o *s3.Options) {
			o.UsePathStyle = true
		}),
		SES: ses.NewFromConfig(awsCfg),
		SQS: sqs.NewFromConfig(awsCfg),
		SNS: sns.NewFromConfig(awsCfg),
	}

	return clients, nil
}

func loadConfig(ctx context.Context, cfg appconfig.AWSConfig) (aws.Config, error) {
	loaders := []func(*config.LoadOptions) error{
		config.WithRegion(cfg.Region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(
			cfg.AccessKey,
			cfg.SecretKey,
			"",
		)),
	}

	if cfg.LocalStack {
		loaders = append(loaders, config.WithEndpointResolverWithOptions(
			aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               cfg.Endpoint,
					SigningRegion:     cfg.Region,
					Source:            aws.EndpointSourceCustom,
					HostnameImmutable: true,
				}, nil
			}),
		))
	}

	return config.LoadDefaultConfig(ctx, loaders...)
}
