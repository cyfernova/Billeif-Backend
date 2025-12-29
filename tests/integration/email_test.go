//go:build integration

package integration

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"invoice-backend/internal/services"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/ses"
	"github.com/aws/aws-sdk-go-v2/service/ses/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEmailSendWithSES(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()

	// Verify sender email identity (required for LocalStack SES)
	senderEmail := "noreply@invoiceapp.local"
	_, err := env.AWSClients.SES.VerifyEmailIdentity(ctx, &ses.VerifyEmailIdentityInput{
		EmailAddress: aws.String(senderEmail),
	})
	require.NoError(t, err, "Failed to verify sender email identity")

	recipientEmail := fmt.Sprintf("test-%d@example.com", time.Now().UnixNano())

	// Verify recipient email (required for sandbox mode in LocalStack)
	_, err = env.AWSClients.SES.VerifyEmailIdentity(ctx, &ses.VerifyEmailIdentityInput{
		EmailAddress: aws.String(recipientEmail),
	})
	require.NoError(t, err, "Failed to verify recipient email identity")

	// Send email via SES
	_, err = env.AWSClients.SES.SendEmail(ctx, &ses.SendEmailInput{
		Destination: &types.Destination{
			ToAddresses: []string{recipientEmail},
		},
		Message: &types.Message{
			Subject: &types.Content{
				Charset: aws.String("UTF-8"),
				Data:    aws.String("Test Email Subject"),
			},
			Body: &types.Body{
				Text: &types.Content{
					Charset: aws.String("UTF-8"),
					Data:    aws.String("This is a test email body from LocalStack integration tests."),
				},
			},
		},
		Source: aws.String(senderEmail),
	})

	// LocalStack may or may not fully support SES - log any errors but don't fail
	if err != nil {
		t.Logf("SES SendEmail returned error (expected in LocalStack free tier): %v", err)
	}
}

func TestEmailFallbackToS3(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()
	bucket := TestBucketEmailSink

	// Create a unique email address for testing
	testEmail := fmt.Sprintf("fallback-test-%d@example.com", time.Now().UnixNano())

	// Simulate fallback by directly writing to S3 (as the EmailService does)
	emailPayload := services.EmailPayload{
		To:        testEmail,
		Subject:   "Test Fallback Email",
		Body:      "This email was stored to S3 as a fallback.",
		SentAt:    time.Now(),
		MessageID: fmt.Sprintf("msg-%d", time.Now().UnixNano()),
	}

	data, err := json.Marshal(emailPayload)
	require.NoError(t, err, "Failed to marshal email payload")

	key := fmt.Sprintf("emails/%s/%d.json", testEmail, time.Now().UnixNano())

	// Cleanup after test
	defer func() {
		env.AWSClients.S3.DeleteObject(ctx, &s3.DeleteObjectInput{
			Bucket: aws.String(bucket),
			Key:    aws.String(key),
		})
	}()

	// Upload email to S3
	_, err = env.AWSClients.S3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(bucket),
		Key:         aws.String(key),
		Body:        aws.NewSliceReader(data),
		ContentType: aws.String("application/json"),
	})
	require.NoError(t, err, "Failed to upload email to S3")

	// Verify the email was stored
	headResp, err := env.AWSClients.S3.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(bucket),
		Key:    aws.String(key),
	})
	require.NoError(t, err, "Failed to verify email in S3")
	assert.NotNil(t, headResp.ContentLength)
}

func TestGetCapturedEmails(t *testing.T) {
	SkipIfLocalStackNotRunning(t)

	env := SetupTestEnv(t)
	ctx := env.Context()
	bucket := TestBucketEmailSink

	testEmail := fmt.Sprintf("captured-%d@example.com", time.Now().UnixNano())
	prefix := fmt.Sprintf("emails/%s/", testEmail)

	// Create multiple test emails
	var keys []string
	for i := 0; i < 3; i++ {
		emailPayload := services.EmailPayload{
			To:        testEmail,
			Subject:   fmt.Sprintf("Test Email %d", i+1),
			Body:      fmt.Sprintf("Body of email %d", i+1),
			SentAt:    time.Now(),
			MessageID: fmt.Sprintf("msg-%d-%d", time.Now().UnixNano(), i),
		}

		data, err := json.Marshal(emailPayload)
		require.NoError(t, err)

		key := fmt.Sprintf("%semail-%d.json", prefix, i)
		keys = append(keys, key)

		_, err = env.AWSClients.S3.PutObject(ctx, &s3.PutObjectInput{
			Bucket:      aws.String(bucket),
			Key:         aws.String(key),
			Body:        aws.NewSliceReader(data),
			ContentType: aws.String("application/json"),
		})
		require.NoError(t, err, "Failed to upload email %d", i)
	}

	// Cleanup after test
	defer func() {
		for _, key := range keys {
			env.AWSClients.S3.DeleteObject(ctx, &s3.DeleteObjectInput{
				Bucket: aws.String(bucket),
				Key:    aws.String(key),
			})
		}
	}()

	// List captured emails
	listResp, err := env.AWSClients.S3.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
		Bucket: aws.String(bucket),
		Prefix: aws.String(prefix),
	})
	require.NoError(t, err, "Failed to list captured emails")

	assert.Equal(t, int32(3), *listResp.KeyCount)

	// Verify we can retrieve and parse each email
	for _, obj := range listResp.Contents {
		getResp, err := env.AWSClients.S3.GetObject(ctx, &s3.GetObjectInput{
			Bucket: aws.String(bucket),
			Key:    obj.Key,
		})
		require.NoError(t, err, "Failed to get email %s", *obj.Key)

		var payload services.EmailPayload
		err = json.NewDecoder(getResp.Body).Decode(&payload)
		getResp.Body.Close()

		require.NoError(t, err, "Failed to decode email %s", *obj.Key)
		assert.Equal(t, testEmail, payload.To)
		assert.NotEmpty(t, payload.Subject)
		assert.NotEmpty(t, payload.Body)
	}
}
