package ratelimit

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awssigner "github.com/aws/aws-sdk-go-v2/aws/signer/v4"
)

const (
	elastiCacheServiceName = "elasticache"
	emptyPayloadSHA256     = "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"
	iamTokenTTL            = 15 * time.Minute
)

type CredentialsProvider func(context.Context) (username string, password string, err error)

type IAMCredentialsOptions struct {
	Region      string
	UserID      string
	CacheName   string
	Credentials aws.CredentialsProvider
	Now         func() time.Time
}

func NewIAMCredentialsProvider(options IAMCredentialsOptions) (CredentialsProvider, error) {
	if strings.TrimSpace(options.Region) == "" {
		return nil, fmt.Errorf("AWS region is required for ElastiCache IAM authentication")
	}
	if strings.TrimSpace(options.UserID) == "" {
		return nil, fmt.Errorf("ElastiCache user ID is required for IAM authentication")
	}
	if strings.TrimSpace(options.CacheName) == "" {
		return nil, fmt.Errorf("ElastiCache cache name is required for IAM authentication")
	}
	if options.Credentials == nil {
		return nil, fmt.Errorf("AWS credentials provider is required for ElastiCache IAM authentication")
	}
	if options.Now == nil {
		options.Now = time.Now
	}
	region := strings.TrimSpace(options.Region)
	userID := strings.TrimSpace(options.UserID)
	cacheName := strings.ToLower(strings.TrimSpace(options.CacheName))
	signer := awssigner.NewSigner()

	return func(ctx context.Context) (string, string, error) {
		credentials, err := options.Credentials.Retrieve(ctx)
		if err != nil {
			return "", "", fmt.Errorf("retrieve AWS credentials for ElastiCache IAM authentication: %w", err)
		}

		query := url.Values{}
		query.Set("Action", "connect")
		query.Set("ResourceType", "ServerlessCache")
		query.Set("User", userID)
		query.Set("X-Amz-Expires", strconv.FormatInt(int64(iamTokenTTL/time.Second), 10))
		request, err := http.NewRequestWithContext(ctx, http.MethodGet, "http://"+cacheName+"/?"+query.Encode(), nil)
		if err != nil {
			return "", "", fmt.Errorf("build ElastiCache IAM authentication request: %w", err)
		}

		signedURI, _, err := signer.PresignHTTP(
			ctx,
			credentials,
			request,
			emptyPayloadSHA256,
			elastiCacheServiceName,
			region,
			options.Now(),
		)
		if err != nil {
			return "", "", fmt.Errorf("sign ElastiCache IAM authentication request: %w", err)
		}
		return userID, strings.TrimPrefix(signedURI, "http://"), nil
	}, nil
}
