package services

import (
	"context"
	"net/url"
	"strings"
	"testing"

	"invoice-backend/internal/config"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestAuthServiceProfilePicturePresignBindsUserKeyTypeAndSize(t *testing.T) {
	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	}, func(options *s3.Options) {
		options.BaseEndpoint = aws.String("https://storage.example.com")
		options.UsePathStyle = true
	})
	service := &AuthService{
		cfg: &config.Config{S3: config.S3Config{BucketLogos: "private-logos"}},
		s3:  &S3Service{client: client},
	}

	result, err := service.GetProfilePictureUploadURL(context.Background(), "user-123", "image/webp", 4096)
	if err != nil {
		t.Fatalf("profile picture presign: %v", err)
	}
	parsed, err := url.Parse(result.UploadURL)
	if err != nil {
		t.Fatalf("parse presigned URL: %v", err)
	}
	if !strings.HasPrefix(parsed.EscapedPath(), "/private-logos/profile-pictures/user-123/") || !strings.HasSuffix(parsed.EscapedPath(), ".webp") {
		t.Fatalf("presigned object path %q is not scoped to the authorized user and MIME extension", parsed.EscapedPath())
	}
	if got := result.RequiredHeaders["Content-Type"]; got != "image/webp" {
		t.Fatalf("required Content-Type = %q, want image/webp", got)
	}
	if got := result.RequiredHeaders["Content-Length"]; got != "4096" {
		t.Fatalf("required Content-Length = %q, want 4096", got)
	}
}

func TestAuthServiceProfilePicturePresignRejectsInvalidRequest(t *testing.T) {
	client := s3.NewFromConfig(aws.Config{
		Region:      "us-east-1",
		Credentials: credentials.NewStaticCredentialsProvider("test", "test", ""),
	})
	service := &AuthService{s3: &S3Service{client: client}}
	tests := map[string]struct {
		contentType string
		sizeBytes   int64
	}{
		"missing or zero":     {contentType: "image/png", sizeBytes: 0},
		"above 5 MiB":         {contentType: "image/png", sizeBytes: 5*1024*1024 + 1},
		"unsupported content": {contentType: "text/html", sizeBytes: 4096},
	}
	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			result, err := service.GetProfilePictureUploadURL(context.Background(), "user-123", test.contentType, test.sizeBytes)
			if err == nil || result != nil {
				t.Fatalf("GetProfilePictureUploadURL(type=%q, size=%d) = %#v, %v; want rejection", test.contentType, test.sizeBytes, result, err)
			}
		})
	}
}
