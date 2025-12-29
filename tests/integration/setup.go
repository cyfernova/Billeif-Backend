//go:build integration

package integration

import (
	"context"
	"os"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// TestEnv holds the test environment configuration
type TestEnv struct {
	AWSClients *awsclients.Config
	Config     *config.Config
	Logger     *logger.Logger
	ctx        context.Context
}

// LocalStackEndpoint is the default LocalStack endpoint
const LocalStackEndpoint = "http://localhost:4566"

// TestBuckets for testing - matching Terraform bucket names
const (
	TestBucketLogos     = "business-logos"
	TestBucketInvoices  = "invoices-pdf"
	TestBucketProducts  = "product-images"
	TestBucketEmailSink = "email-sink"
)

// SetupTestEnv creates a test environment connected to LocalStack
func SetupTestEnv(t *testing.T) *TestEnv {
	t.Helper()

	ctx := context.Background()
	log := logger.New()

	cfg := &config.Config{
		Environment: "test",
		AWS: config.AWSConfig{
			Region:     "us-east-1",
			LocalStack: true,
			Endpoint:   getEnvOrDefault("AWS_ENDPOINT", LocalStackEndpoint),
			AccessKey:  "test",
			SecretKey:  "test",
		},
		Cognito: config.CognitoConfig{
			UserPoolID: getEnvOrDefault("COGNITO_USER_POOL_ID", "us-east-1_testpool"),
			ClientID:   getEnvOrDefault("COGNITO_CLIENT_ID", "testclient"),
			Region:     "us-east-1",
		},
		S3: config.S3Config{
			BucketLogos:     TestBucketLogos,
			BucketInvoices:  TestBucketInvoices,
			BucketProducts:  TestBucketProducts,
			BucketEmailSink: TestBucketEmailSink,
		},
	}

	awsClients, err := awsclients.New(ctx, cfg.AWS)
	if err != nil {
		t.Fatalf("Failed to create AWS clients: %v", err)
	}

	return &TestEnv{
		AWSClients: awsClients,
		Config:     cfg,
		Logger:     log,
		ctx:        ctx,
	}
}

// Context returns the test context
func (e *TestEnv) Context() context.Context {
	return e.ctx
}

// getEnvOrDefault returns environment variable value or default
func getEnvOrDefault(key, defaultVal string) string {
	if val := os.Getenv(key); val != "" {
		return val
	}
	return defaultVal
}

// CreateTestS3Client creates a standalone S3 client for testing
func CreateTestS3Client(t *testing.T) *s3.Client {
	t.Helper()

	endpoint := getEnvOrDefault("AWS_ENDPOINT", LocalStackEndpoint)

	cfg, err := awsconfig.LoadDefaultConfig(context.Background(),
		awsconfig.WithRegion("us-east-1"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
		awsconfig.WithEndpointResolverWithOptions(
			aws.EndpointResolverWithOptionsFunc(func(service, region string, options ...interface{}) (aws.Endpoint, error) {
				return aws.Endpoint{
					URL:               endpoint,
					SigningRegion:     "us-east-1",
					HostnameImmutable: true,
				}, nil
			}),
		),
	)
	if err != nil {
		t.Fatalf("Failed to load AWS config: %v", err)
	}

	return s3.NewFromConfig(cfg, func(o *s3.Options) {
		o.UsePathStyle = true
	})
}

// CleanupBucket removes all objects from a bucket
func CleanupBucket(t *testing.T, client *s3.Client, bucket string) {
	t.Helper()
	ctx := context.Background()

	// List all objects
	listOutput, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
	})
	if err != nil {
		t.Logf("Warning: Could not list objects in bucket %s: %v", bucket, err)
		return
	}

	// Delete each object
	for _, obj := range listOutput.Contents {
		_, err := client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    obj.Key,
		})
		if err != nil {
			t.Logf("Warning: Could not delete object %s: %v", *obj.Key, err)
		}
	}
}

// SkipIfLocalStackNotRunning skips test if LocalStack is not available
func SkipIfLocalStackNotRunning(t *testing.T) {
	t.Helper()

	client := CreateTestS3Client(t)
	ctx := context.Background()

	_, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
	if err != nil {
		t.Skipf("LocalStack not running, skipping integration test: %v", err)
	}
}
