package services

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"invoice-backend/internal/config"
	"invoice-backend/pkg/awsclients"
	"invoice-backend/pkg/logger"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestS3ServiceGeneratePresignedUploadBindsKeyContentTypeAndLength(t *testing.T) {
	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String("https://storage.example.com")
		options.UsePathStyle = true
	})
	service := &S3Service{client: client}

	result, err := service.GeneratePresignedUpload(
		context.Background(),
		"private-uploads",
		"businesses/business-123/products/product-456/image",
		"image/png",
		4096,
		900,
	)
	if err != nil {
		t.Fatalf("generate presigned upload: %v", err)
	}

	parsed, err := url.Parse(result.UploadURL)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}
	if got, want := parsed.EscapedPath(), "/private-uploads/businesses/business-123/products/product-456/image"; got != want {
		t.Fatalf("object path = %q, want %q", got, want)
	}
	signedHeaders := strings.Split(parsed.Query().Get("X-Amz-SignedHeaders"), ";")
	for _, required := range []string{"content-length", "content-type", "host"} {
		if !containsSignedHeader(signedHeaders, required) {
			t.Fatalf("signed headers %v do not include %q", signedHeaders, required)
		}
	}
	if got := result.RequiredHeaders["Content-Type"]; got != "image/png" {
		t.Fatalf("required Content-Type = %q, want image/png", got)
	}
	if got := result.RequiredHeaders["Content-Length"]; got != "4096" {
		t.Fatalf("required Content-Length = %q, want 4096", got)
	}
}

func TestS3ServiceGeneratePresignedUploadRejectsNonPositiveLength(t *testing.T) {
	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	})
	service := &S3Service{client: client}

	for name, sizeBytes := range map[string]int64{"zero": 0, "negative": -1} {
		t.Run(name, func(t *testing.T) {
			result, err := service.GeneratePresignedUpload(
				context.Background(), "bucket", "key", "image/png", sizeBytes, 900,
			)
			if err == nil || result != nil {
				t.Fatalf("GeneratePresignedUpload(size=%d) = %#v, %v; want rejection", sizeBytes, result, err)
			}
		})
	}
}

func TestS3ServicePresignPendingUploadBindsChecksumAndTenantMetadata(t *testing.T) {
	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String("https://storage.example.com")
		options.UsePathStyle = true
	})
	service := &S3Service{client: client}
	result, err := service.PresignPendingUpload(context.Background(), PendingObjectSpec{
		Bucket: "quarantine", Key: "pending/business/upload/import", ContentType: "text/csv", SizeBytes: 12,
		ChecksumSHA256: "AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=",
		Metadata:       map[string]string{"business-id": "business", "uploader-id": "user", "upload-id": "upload"},
	})
	if err != nil {
		t.Fatalf("presign pending upload: %v", err)
	}
	parsed, err := url.Parse(result.UploadURL)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}
	signedHeaders := strings.Split(parsed.Query().Get("X-Amz-SignedHeaders"), ";")
	for _, required := range []string{"content-length", "content-type", "host", "x-amz-checksum-sha256", "x-amz-meta-business-id", "x-amz-meta-uploader-id", "x-amz-meta-upload-id"} {
		if !containsSignedHeader(signedHeaders, required) {
			t.Fatalf("signed headers %v do not include %q", signedHeaders, required)
		}
	}
}

func containsSignedHeader(values []string, target string) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}

func TestS3ServiceUploadIfAbsentUsesConditionalCreate(t *testing.T) {
	status := http.StatusOK
	existing := []byte("pdf")
	getCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			getCalls++
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(existing)
			return
		}
		if got := r.Header.Get("If-None-Match"); got != "*" {
			t.Errorf("If-None-Match = %q, want *", got)
		}
		_, _ = io.Copy(io.Discard, r.Body)
		w.WriteHeader(status)
	}))
	defer server.Close()

	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(server.URL)
		options.UsePathStyle = true
	})
	service := &S3Service{client: client}

	if err := service.UploadIfAbsent(context.Background(), "bucket", "final.pdf", []byte("pdf"), "application/pdf"); err != nil {
		t.Fatalf("conditional create: %v", err)
	}
	status = http.StatusPreconditionFailed
	if err := service.UploadIfAbsent(context.Background(), "bucket", "final.pdf", []byte("pdf"), "application/pdf"); err != nil {
		t.Fatalf("412 same-byte deterministic duplicate: %v", err)
	}
	if getCalls != 1 {
		t.Errorf("412 same-byte verification GET calls = %d, want 1", getCalls)
	}
	existing = []byte("different profile bytes")
	if err := service.UploadIfAbsent(context.Background(), "bucket", "final.pdf", []byte("pdf"), "application/pdf"); err == nil {
		t.Fatal("412 different-byte deterministic conflict was accepted")
	}
	status = http.StatusConflict
	if err := service.UploadIfAbsent(context.Background(), "bucket", "final.pdf", []byte("pdf"), "application/pdf"); err == nil {
		t.Fatal("409 conditional write conflict was accepted")
	}
}

func TestAssetPresignersUseConfiguredDeploymentBuckets(t *testing.T) {
	t.Parallel()

	const (
		region        = "ap-south-1"
		logoBucket    = "billeif-test-123456789012-business-logos"
		productBucket = "billeif-test-123456789012-product-images"
	)
	appConfig := &config.Config{
		AWS: config.AWSConfig{Region: region},
		S3: config.S3Config{
			BucketLogos:    logoBucket,
			BucketProducts: productBucket,
		},
	}
	client := s3.NewFromConfig(aws.Config{
		Region:      region,
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	})
	log := logger.New()
	storage := NewS3Service(appConfig, &awsclients.Config{S3: client}, log)

	tests := []struct {
		name       string
		wantBucket string
		wantPath   string
		presign    func(context.Context) (*PresignedUpload, error)
	}{
		{
			name:       "business logo",
			wantBucket: logoBucket,
			wantPath:   "/logos/business-id/logo",
			presign: func(ctx context.Context) (*PresignedUpload, error) {
				return NewBusinessService(nil, storage, log).GetLogoUploadURL(ctx, "business-id", "image/png", 1024)
			},
		},
		{
			name:       "product image",
			wantBucket: productBucket,
			wantPath:   "/products/business-id/product-id/image",
			presign: func(ctx context.Context) (*PresignedUpload, error) {
				return NewProductService(nil, nil, storage, nil, log).getImageUploadURL(ctx, "business-id", "product-id", "image/png", 1024)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			upload, err := test.presign(context.Background())
			if err != nil {
				t.Fatalf("presign configured bucket: %v", err)
			}
			parsed, err := url.Parse(upload.UploadURL)
			if err != nil {
				t.Fatalf("parse presigned URL: %v", err)
			}
			if !strings.HasPrefix(parsed.Host, test.wantBucket+".") {
				t.Fatalf("presigned host = %q, want configured bucket %q", parsed.Host, test.wantBucket)
			}
			if parsed.Path != test.wantPath {
				t.Fatalf("presigned path = %q, want tenant-scoped path %q", parsed.Path, test.wantPath)
			}
		})
	}
}
