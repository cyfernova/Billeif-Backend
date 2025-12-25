package awsclients

import (
	"context"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

// S3Client wraps the AWS S3 client
type S3Client struct {
	Client        *s3.Client
	PresignClient *s3.PresignClient
}

// NewS3Client creates a new S3 client
func NewS3Client(ctx context.Context, region, accessKey, secretKey, endpoint string) (*S3Client, error) {
	cfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKey, secretKey, "")),
	)
	if err != nil {
		return nil, err
	}

	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" && endpoint != "http://localhost:4566" && endpoint != "http://localstack:4566" {
			o.BaseEndpoint = aws.String(endpoint)
		}
		// For LocalStack, disable path-style addressing
		o.UsePathStyle = true
	})

	return &S3Client{
		Client:        client,
		PresignClient: s3.NewPresignClient(client),
	}, nil
}

// HealthCheck checks if S3 is accessible
func (s *S3Client) HealthCheck(ctx context.Context) error {
	if s.Client == nil {
		return ErrClientNotInitialized
	}

	// List buckets to check connectivity
	_, err := s.Client.ListBuckets(ctx, &s3.ListBucketsInput{})
	return err
}

// PutObject puts an object in S3
func (s *S3Client) PutObject(ctx context.Context, input *s3.PutObjectInput) (*s3.PutObjectOutput, error) {
	return s.Client.PutObject(ctx, input)
}

// GetObject gets an object from S3
func (s *S3Client) GetObject(ctx context.Context, input *s3.GetObjectInput) (*s3.GetObjectOutput, error) {
	return s.Client.GetObject(ctx, input)
}

// DeleteObject deletes an object from S3
func (s *S3Client) DeleteObject(ctx context.Context, input *s3.DeleteObjectInput) (*s3.DeleteObjectOutput, error) {
	return s.Client.DeleteObject(ctx, input)
}

// HeadObject heads an object in S3
func (s *S3Client) HeadObject(ctx context.Context, input *s3.HeadObjectInput) (*s3.HeadObjectOutput, error) {
	return s.Client.HeadObject(ctx, input)
}

// ListObjectsV2 lists objects in a bucket
func (s *S3Client) ListObjectsV2(ctx context.Context, input *s3.ListObjectsV2Input) (*s3.ListObjectsV2Output, error) {
	return s.Client.ListObjectsV2(ctx, input)
}

// PresignGetObject generates a presigned URL for getting an object
func (s *S3Client) PresignGetObject(ctx context.Context, input *s3.GetObjectInput) (string, error) {
	req, err := s.PresignClient.PresignGetObject(ctx, input)
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// PresignPutObject generates a presigned URL for putting an object
func (s *S3Client) PresignPutObject(ctx context.Context, input *s3.PutObjectInput) (string, error) {
	req, err := s.PresignClient.PresignPutObject(ctx, input)
	if err != nil {
		return "", err
	}
	return req.URL, nil
}

// CreateBucket creates a bucket
func (s *S3Client) CreateBucket(ctx context.Context, input *s3.CreateBucketInput) (*s3.CreateBucketOutput, error) {
	return s.Client.CreateBucket(ctx, input)
}

// DeleteBucket deletes a bucket
func (s *S3Client) DeleteBucket(ctx context.Context, input *s3.DeleteBucketInput) (*s3.DeleteBucketOutput, error) {
	return s.Client.DeleteBucket(ctx, input)
}

// HeadBucket checks if a bucket exists
func (s *S3Client) HeadBucket(ctx context.Context, bucket string) (*s3.HeadBucketOutput, error) {
	input := &s3.HeadBucketInput{
		Bucket: aws.String(bucket),
	}
	return s.Client.HeadBucket(ctx, input)
}
