package services

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"strconv"
	"time"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	s3types "github.com/aws/aws-sdk-go-v2/service/s3/types"
	smithyhttp "github.com/aws/smithy-go/transport/http"
)

type S3Service struct {
	cfg    *config.Config
	client *s3.Client
	log    *logger.Logger
}

var ErrConditionalWriteContentMismatch = errors.New("existing object content does not match conditional upload")

type PresignedUpload struct {
	UploadURL       string            `json:"upload_url"`
	RequiredHeaders map[string]string `json:"required_headers"`
}

func NewS3Service(cfg *config.Config, aws *awsclients.Config, log *logger.Logger) *S3Service {
	return &S3Service{
		cfg:    cfg,
		client: aws.S3,
		log:    log,
	}
}

func (s *S3Service) GeneratePresignedUpload(
	ctx context.Context,
	bucket string,
	key string,
	contentType string,
	contentLength int64,
	expiresIn int64,
) (*PresignedUpload, error) {
	if contentLength <= 0 {
		return nil, fmt.Errorf("content length must be positive")
	}
	log := logger.FromContext(ctx).With("service", "s3", "operation", "presign_upload", "bucket", bucket, "key", key)
	start := time.Now()
	presignClient := s3.NewPresignClient(s.client)

	request, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(bucket),
		Key:           aws.String(key),
		ContentLength: aws.Int64(contentLength),
		ContentType:   aws.String(contentType),
	}, s3.WithPresignExpires(time.Duration(expiresIn)*time.Second))
	if err != nil {
		log.Error("failed to generate S3 upload URL", "error", err)
		return nil, fmt.Errorf("failed to generate presigned URL: %w", err)
	}
	requiredHeaders := map[string]string{
		"Content-Length": strconv.FormatInt(contentLength, 10),
		"Content-Type":   contentType,
	}
	for name, expected := range requiredHeaders {
		if signed := request.SignedHeader.Get(name); signed != expected {
			return nil, fmt.Errorf("presigned upload did not bind %s", name)
		}
	}

	log.Debug("generated S3 upload URL", "expires_in_seconds", expiresIn, "duration_ms", time.Since(start).Milliseconds())
	return &PresignedUpload{
		UploadURL:       request.URL,
		RequiredHeaders: requiredHeaders,
	}, nil
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

func (s *S3Service) PresignPendingUpload(ctx context.Context, spec PendingObjectSpec) (*PresignedUpload, error) {
	presignClient := s3.NewPresignClient(s.client)
	request, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(spec.Bucket), Key: aws.String(spec.Key), ContentType: aws.String(spec.ContentType),
		ContentLength: aws.Int64(spec.SizeBytes), ChecksumSHA256: aws.String(spec.ChecksumSHA256), Metadata: spec.Metadata,
	}, s3.WithPresignExpires(spec.ExpiresIn))
	if err != nil {
		return nil, fmt.Errorf("presign pending upload: %w", err)
	}
	required := map[string]string{
		"Content-Length": spec.RequiredLength(), "Content-Type": spec.ContentType, "x-amz-checksum-sha256": spec.ChecksumSHA256,
	}
	for key, value := range spec.Metadata {
		required["x-amz-meta-"+key] = value
	}
	for name, expected := range required {
		if signed := request.SignedHeader.Get(name); signed != expected {
			return nil, fmt.Errorf("presigned pending upload did not bind %s", name)
		}
	}
	return &PresignedUpload{UploadURL: request.URL, RequiredHeaders: required}, nil
}

func (s *S3Service) InspectPendingObject(ctx context.Context, bucket, key string) (PendingObjectMetadata, error) {
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{Bucket: aws.String(bucket), Key: aws.String(key), ChecksumMode: s3types.ChecksumModeEnabled})
	if err != nil {
		return PendingObjectMetadata{}, err
	}
	return PendingObjectMetadata{ContentType: aws.ToString(output.ContentType), SizeBytes: aws.ToInt64(output.ContentLength), ChecksumSHA256: aws.ToString(output.ChecksumSHA256), Metadata: output.Metadata}, nil
}

func (s *S3Service) PresignPendingDownload(ctx context.Context, bucket, key string, expires time.Duration) (string, error) {
	return s.GeneratePresignedDownloadURL(ctx, bucket, key, int64(expires/time.Second))
}

func (s *S3Service) DeletePendingObject(ctx context.Context, bucket, key string) error {
	return s.Delete(ctx, bucket, key)
}

func (s *S3Service) ReadPendingObject(ctx context.Context, bucket, key string) ([]byte, error) {
	return s.Download(ctx, bucket, key)
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
