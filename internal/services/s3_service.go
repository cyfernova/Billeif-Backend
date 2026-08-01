package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type S3Service struct {
	cfg    *config.Config
	client *s3.Client
	log    *logger.Logger
}

var ErrConditionalWriteContentMismatch = errors.New("existing object content does not match conditional upload")

func NewS3Service(cfg *config.Config, aws *awsclients.Config, log *logger.Logger) *S3Service {
	return &S3Service{
		cfg:    cfg,
		client: aws.S3,
		log:    log,
	}
}

func (s *S3Service) GeneratePresignedUploadURL(ctx context.Context, bucket, key, contentType string, expiresIn int64) (string, error) {
	log := logger.FromContext(ctx).With("service", "s3", "operation", "presign_upload", "bucket", bucket, "key", key)
	start := time.Now()
	presignClient := s3.NewPresignClient(s.client)

	request, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		ContentType: aws.String(contentType),
	}, s3.WithPresignExpires(time.Duration(expiresIn)*time.Second))

	if err != nil {
		log.Error("failed to generate S3 upload URL", "error", err)
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	log.Debug("generated S3 upload URL", "expires_in_seconds", expiresIn, "duration_ms", time.Since(start).Milliseconds())
	return request.URL, nil
}

func (s *S3Service) GeneratePresignedDownloadURL(ctx context.Context, bucket, key string, expiresIn int64) (string, error) {
	log := logger.FromContext(ctx).With("service", "s3", "operation", "presign_download", "bucket", bucket, "key", key)
	start := time.Now()
	presignClient := s3.NewPresignClient(s.client)

	request, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(time.Duration(expiresIn)*time.Second))

	if err != nil {
		log.Error("failed to generate S3 download URL", "error", err)
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	log.Debug("generated S3 download URL", "expires_in_seconds", expiresIn, "duration_ms", time.Since(start).Milliseconds())
	return request.URL, nil
}

func (s *S3Service) Upload(ctx context.Context, bucket, key string, data []byte, contentType string) error {
	log := logger.FromContext(ctx).With("service", "s3", "operation", "upload", "bucket", bucket, "key", key)
	start := time.Now()
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		log.Error("S3 upload failed", "error", err, "size_bytes", len(data), "duration_ms", time.Since(start).Milliseconds())
		return err
	}
	log.Info("S3 upload completed", "size_bytes", len(data), "duration_ms", time.Since(start).Milliseconds())
	return err
}

// UploadIfAbsent creates an object without overwriting an existing key.
// A precondition failure means another writer already created the object.
func (s *S3Service) UploadIfAbsent(ctx context.Context, bucket, key string, data []byte, contentType string) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
		IfNoneMatch: aws.String("*"),
	})
	if err == nil {
		return nil
	}
	var responseError *smithyhttp.ResponseError
	if errors.As(err, &responseError) && responseError.HTTPStatusCode() == 412 {
		result, getErr := s.client.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
		if getErr != nil {
			return fmt.Errorf("verify existing conditional object: %w", getErr)
		}
		defer result.Body.Close()
		existing, readErr := io.ReadAll(result.Body)
		if readErr != nil {
			return fmt.Errorf("read existing conditional object: %w", readErr)
		}
		candidateSum := sha256.Sum256(data)
		existingSum := sha256.Sum256(existing)
		if candidateSum != existingSum {
			return ErrConditionalWriteContentMismatch
		}
		return nil
	}
	return err
}

func (s *S3Service) Download(ctx context.Context, bucket, key string) ([]byte, error) {
	log := logger.FromContext(ctx).With("service", "s3", "operation", "download", "bucket", bucket, "key", key)
	start := time.Now()
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		log.Error("S3 download failed", "error", err)
		return nil, err
	}
	defer result.Body.Close()

	data, err := io.ReadAll(result.Body)
	if err != nil {
		log.Error("failed reading S3 object body", "error", err, "duration_ms", time.Since(start).Milliseconds())
		return nil, err
	}

	log.Debug("S3 download completed", "size_bytes", len(data), "duration_ms", time.Since(start).Milliseconds())
	return data, nil
}

func (s *S3Service) Delete(ctx context.Context, bucket, key string) error {
	log := logger.FromContext(ctx).With("service", "s3", "operation", "delete", "bucket", bucket, "key", key)
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		log.Error("S3 delete failed", "error", err)
		return err
	}
	log.Info("S3 object deleted")
	return err
}

func (s *S3Service) ListObjects(ctx context.Context, bucket, prefix string) ([]string, error) {
	log := logger.FromContext(ctx).With("service", "s3", "operation", "list_objects", "bucket", bucket, "prefix", prefix)
	start := time.Now()
	result, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	if err != nil {
		log.Error("S3 list objects failed", "error", err)
		return nil, err
	}

	var keys []string
	for _, obj := range result.Contents {
		keys = append(keys, *obj.Key)
	}
	log.Debug("S3 list objects completed", "count", len(keys), "duration_ms", time.Since(start).Milliseconds())
	return keys, nil
}

func (s *S3Service) GetObjectURL(bucket, key string) string {
	return fmt.Sprintf("https://%s.s3.%s.amazonaws.com/%s", bucket, s.cfg.AWS.Region, key)
}
