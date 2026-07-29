package config

import (
	"context"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

const maxSSMBatchSize = 10

type SSMAPI interface {
	GetParameters(context.Context, *ssm.GetParametersInput, ...func(*ssm.Options)) (*ssm.GetParametersOutput, error)
}

type ssmCacheEntry struct {
	value     string
	expiresAt time.Time
}

type SSMResolver struct {
	client  SSMAPI
	allowed map[string]struct{}
	ttl     time.Duration
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]ssmCacheEntry
}

func NewSSMResolver(client SSMAPI, names []string, ttl time.Duration, now func() time.Time) (*SSMResolver, error) {
	if client == nil {
		return nil, &ConfigurationError{Resource: "SSM", Reason: "client is required"}
	}
	if ttl <= 0 {
		return nil, &ConfigurationError{Resource: "SSM cache", Reason: "TTL must be positive"}
	}
	if ttl > maxResolverTTL {
		ttl = maxResolverTTL
	}
	if now == nil {
		now = time.Now
	}
	allowed := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name != "" {
			allowed[name] = struct{}{}
		}
	}
	return &SSMResolver{
		client:  client,
		allowed: allowed,
		ttl:     ttl,
		now:     now,
		cache:   make(map[string]ssmCacheEntry),
	}, nil
}

func (r *SSMResolver) Get(ctx context.Context, names []string) (map[string]string, error) {
	normalized := make([]string, 0, len(names))
	seen := make(map[string]struct{}, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if _, ok := r.allowed[name]; !ok || name == "" {
			return nil, &ConfigurationError{Resource: "SSM parameter identifier", Reason: "identifier is not configured"}
		}
		if _, ok := seen[name]; !ok {
			seen[name] = struct{}{}
			normalized = append(normalized, name)
		}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	result := make(map[string]string, len(normalized))
	missing := make([]string, 0, len(normalized))
	for _, name := range normalized {
		if cached, ok := r.cache[name]; ok && now.Before(cached.expiresAt) {
			result[name] = cached.value
		} else {
			missing = append(missing, name)
		}
	}

	for start := 0; start < len(missing); start += maxSSMBatchSize {
		end := min(start+maxSSMBatchSize, len(missing))
		batch := missing[start:end]
		out, err := r.client.GetParameters(ctx, &ssm.GetParametersInput{
			Names:          batch,
			WithDecryption: aws.Bool(false),
		})
		if err != nil {
			return nil, &ResolutionError{Resource: "configured SSM parameters"}
		}
		if out == nil || len(out.InvalidParameters) != 0 {
			return nil, &ConfigurationError{Resource: "configured SSM parameters", Reason: "one or more parameters are missing"}
		}
		for _, parameter := range out.Parameters {
			if parameter.Name == nil || parameter.Value == nil {
				continue
			}
			r.cache[*parameter.Name] = ssmCacheEntry{value: *parameter.Value, expiresAt: now.Add(r.ttl)}
			result[*parameter.Name] = *parameter.Value
		}
	}

	for _, name := range normalized {
		if _, ok := result[name]; !ok {
			return nil, &ConfigurationError{Resource: "configured SSM parameters", Reason: "one or more parameters returned no value"}
		}
	}
	return result, nil
}
