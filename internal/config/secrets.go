package config

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
)

const maxResolverTTL = 15 * time.Minute

type SecretsManagerAPI interface {
	GetSecretValue(context.Context, *secretsmanager.GetSecretValueInput, ...func(*secretsmanager.Options)) (*secretsmanager.GetSecretValueOutput, error)
}

type secretCacheEntry struct {
	value     string
	expiresAt time.Time
}

type SecretResolver struct {
	client  SecretsManagerAPI
	allowed map[string]struct{}
	ttl     time.Duration
	now     func() time.Time

	mu    sync.Mutex
	cache map[string]secretCacheEntry
}

func NewSecretResolver(client SecretsManagerAPI, identifiers []string, ttl time.Duration, now func() time.Time) (*SecretResolver, error) {
	if client == nil {
		return nil, &ConfigurationError{Resource: "Secrets Manager", Reason: "client is required"}
	}
	if ttl <= 0 {
		return nil, &ConfigurationError{Resource: "Secrets Manager cache", Reason: "TTL must be positive"}
	}
	if ttl > maxResolverTTL {
		ttl = maxResolverTTL
	}
	if now == nil {
		now = time.Now
	}
	allowed := make(map[string]struct{}, len(identifiers))
	for _, identifier := range identifiers {
		identifier = strings.TrimSpace(identifier)
		if identifier != "" {
			allowed[identifier] = struct{}{}
		}
	}
	return &SecretResolver{
		client:  client,
		allowed: allowed,
		ttl:     ttl,
		now:     now,
		cache:   make(map[string]secretCacheEntry),
	}, nil
}

func (r *SecretResolver) String(ctx context.Context, identifier string) (string, error) {
	identifier = strings.TrimSpace(identifier)
	if _, ok := r.allowed[identifier]; !ok || identifier == "" {
		return "", &ConfigurationError{Resource: "secret identifier", Reason: "identifier is not configured"}
	}

	r.mu.Lock()
	defer r.mu.Unlock()
	now := r.now()
	if cached, ok := r.cache[identifier]; ok && now.Before(cached.expiresAt) {
		return cached.value, nil
	}

	out, err := r.client.GetSecretValue(ctx, &secretsmanager.GetSecretValueInput{SecretId: &identifier})
	if err != nil {
		return "", &ResolutionError{Resource: "configured secret"}
	}
	if out == nil || out.SecretString == nil {
		return "", &ConfigurationError{Resource: "configured secret", Reason: "SecretString is required"}
	}
	value := *out.SecretString
	r.cache[identifier] = secretCacheEntry{value: value, expiresAt: now.Add(r.ttl)}
	return value, nil
}

func (r *SecretResolver) JSONField(ctx context.Context, identifier, field string) (string, error) {
	field = strings.TrimSpace(field)
	if field == "" {
		return "", &ConfigurationError{Resource: "secret JSON field", Reason: "field name is required"}
	}
	value, err := r.String(ctx, identifier)
	if err != nil {
		return "", err
	}
	var document map[string]json.RawMessage
	if err := json.Unmarshal([]byte(value), &document); err != nil {
		return "", &ConfigurationError{Resource: "configured secret", Reason: "SecretString must be a JSON object"}
	}
	raw, ok := document[field]
	if !ok {
		return "", &ConfigurationError{Resource: "configured secret", Reason: "required JSON field is missing"}
	}
	var extracted string
	if err := json.Unmarshal(raw, &extracted); err != nil || strings.TrimSpace(extracted) == "" {
		return "", &ConfigurationError{Resource: "configured secret", Reason: "required JSON field must be a non-empty string"}
	}
	return extracted, nil
}
