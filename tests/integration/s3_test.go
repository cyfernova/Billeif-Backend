//go:build integration

package integration

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestS3Upload(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	client := CreateTestS3Client(t)
	ctx := context.Background()
	bucket := TestBucketLogos
	key := fmt.Sprintf("test-upload-%d.txt", time.Now().UnixNano())
	content := []byte("Hello, LocalStack!")

	// Cleanup after test
	defer func() {
		client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
	}()

	// Upload object
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("text/plain"),
	})
	require.NoError(t, err, "Failed to upload object")

	// Verify object exists
	headResp, err := client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	require.NoError(t, err, "Failed to get object metadata")
	assert.Equal(t, int64(len(content)), *headResp.ContentLength)
}

func TestS3Download(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	client := CreateTestS3Client(t)
	ctx := context.Background()
	bucket := TestBucketLogos
	key := fmt.Sprintf("test-download-%d.txt", time.Now().UnixNano())
	content := []byte("Content to download from LocalStack")

	// Cleanup after test
	defer func() {
		client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
	}()

	// Upload object first
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("text/plain"),
	})
	require.NoError(t, err, "Failed to upload object")

	// Download object
	getResp, err := client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	require.NoError(t, err, "Failed to download object")
	defer getResp.Body.Close()

	// Verify content
	downloadedContent, err := io.ReadAll(getResp.Body)
	require.NoError(t, err, "Failed to read object body")
	assert.Equal(t, content, downloadedContent)
}

func TestS3Delete(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	client := CreateTestS3Client(t)
	ctx := context.Background()
	bucket := TestBucketLogos
	key := fmt.Sprintf("test-delete-%d.txt", time.Now().UnixNano())
	content := []byte("Content to delete")

	// Upload object
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        bytes.NewReader(content),
		ContentType: aws.String("text/plain"),
	})
	require.NoError(t, err, "Failed to upload object")

	// Delete object
	_, err = client.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	require.NoError(t, err, "Failed to delete object")

	// Verify object no longer exists
	_, err = client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	assert.Error(t, err, "Object should not exist after deletion")
}

func TestS3ListObjects(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	client := CreateTestS3Client(t)
	ctx := context.Background()
	bucket := TestBucketLogos
	prefix := fmt.Sprintf("test-list-%d/", time.Now().UnixNano())

	// Upload multiple objects
	keys := []string{
		prefix + "file1.txt",
		prefix + "file2.txt",
		prefix + "file3.txt",
	}

	// Cleanup after test
	defer func() {
		for _, key := range keys {
			client.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(key),
			})
		}
	}()

	for _, key := range keys {
		_, err := client.PutObject(ctx, &s3.PutObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
			Body:   bytes.NewReader([]byte("content")),
		})
		require.NoError(t, err, "Failed to upload object %s", key)
	}

	// List objects with prefix
	listResp, err := client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	require.NoError(t, err, "Failed to list objects")

	assert.Equal(t, int32(3), *listResp.KeyCount)

	// Verify all keys are present
	foundKeys := make(map[string]bool)
	for _, obj := range listResp.Contents {
		foundKeys[*obj.Key] = true
	}

	for _, key := range keys {
		assert.True(t, foundKeys[key], "Key %s should be in list", key)
	}
}

func TestS3PresignedUploadURL(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	client := CreateTestS3Client(t)
	ctx := context.Background()
	bucket := TestBucketLogos
	key := fmt.Sprintf("test-presigned-%d.txt", time.Now().UnixNano())

	// Cleanup after test
	defer func() {
		client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
	}()

	// Generate presigned URL for PUT
	presignClient := s3.NewPresignClient(client)
	presignResp, err := presignClient.PresignPutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		ContentType: aws.String("text/plain"),
	}, s3.WithPresignExpires(time.Hour))
	require.NoError(t, err, "Failed to generate presigned URL")

	// Verify URL is valid
	assert.NotEmpty(t, presignResp.URL)
	assert.True(t, strings.Contains(presignResp.URL, bucket))
	assert.True(t, strings.Contains(presignResp.URL, key))
}

func TestS3PresignedDownloadURL(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	client := CreateTestS3Client(t)
	ctx := context.Background()
	bucket := TestBucketLogos
	key := fmt.Sprintf("test-presigned-download-%d.txt", time.Now().UnixNano())
	content := []byte("Content for presigned download")

	// Cleanup after test
	defer func() {
		client.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
	}()

	// Upload object first
	_, err := client.PutObject(ctx, &s3.PutObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
		Body:   bytes.NewReader(content),
	})
	require.NoError(t, err, "Failed to upload object")

	// Generate presigned URL for GET
	presignClient := s3.NewPresignClient(client)
	presignResp, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(time.Hour))
	require.NoError(t, err, "Failed to generate presigned download URL")

	// Verify URL is valid
	assert.NotEmpty(t, presignResp.URL)
	assert.True(t, strings.Contains(presignResp.URL, bucket))
	assert.True(t, strings.Contains(presignResp.URL, key))
}
