package services

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

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
