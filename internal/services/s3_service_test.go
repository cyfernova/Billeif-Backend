package services

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestS3ServiceUploadIfAbsentUsesConditionalCreate(t *testing.T) {
	status := http.StatusOK
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("If-None-Match"); got != "*" {
			t.Errorf("If-None-Match = %q, want *", got)
		}
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
		t.Fatalf("412 deterministic duplicate: %v", err)
	}
	status = http.StatusConflict
	if err := service.UploadIfAbsent(context.Background(), "bucket", "final.pdf", []byte("pdf"), "application/pdf"); err == nil {
		t.Fatal("409 conditional write conflict was accepted")
	}
}
