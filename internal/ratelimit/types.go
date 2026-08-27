package ratelimit

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
	"time"
)

var namespacePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9/_-]{0,127}$`)

type Bucket struct {
	Namespace string
	Identity  string
	Limit     int64
	Window    time.Duration
}

type Decision struct {
	Allowed    bool
	RetryAfter time.Duration
}

type Limiter interface {
	Decide(context.Context, []Bucket) (Decision, error)
}

type DisabledLimiter struct{}

func (DisabledLimiter) Decide(context.Context, []Bucket) (Decision, error) {
	return Decision{Allowed: true}, nil
}

func (DisabledLimiter) Close() error {
	return nil
}

func bucketKey(bucket Bucket) (string, error) {
	namespace := strings.TrimSpace(bucket.Namespace)
	identity := strings.TrimSpace(bucket.Identity)
	if !namespacePattern.MatchString(namespace) {
		return "", fmt.Errorf("invalid rate-limit namespace %q", namespace)
	}
	if identity == "" {
		return "", fmt.Errorf("rate-limit identity is required")
	}
	if bucket.Limit <= 0 {
		return "", fmt.Errorf("rate-limit quota must be positive")
	}
	if bucket.Window <= 0 {
		return "", fmt.Errorf("rate-limit window must be positive")
	}

	digest := sha256.Sum256([]byte(identity))
	return "{billeif-rate-limit}:" + strings.ReplaceAll(namespace, "/", ":") + ":" + hex.EncodeToString(digest[:]), nil
}
