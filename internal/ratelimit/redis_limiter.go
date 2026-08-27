package ratelimit

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/redis/go-redis/v9"
)

const atomicDebitScript = `
local retry_after = 0

for index, key in ipairs(KEYS) do
  local offset = (index - 1) * 2
  local limit = tonumber(ARGV[offset + 1])
  local window = tonumber(ARGV[offset + 2])
  local current = tonumber(redis.call("GET", key) or "0")

  if current >= limit then
    local ttl = redis.call("PTTL", key)
    if ttl < 1 then
      ttl = window
    end
    if ttl > retry_after then
      retry_after = ttl
    end
  end
end

if retry_after > 0 then
  return {0, retry_after}
end

for index, key in ipairs(KEYS) do
  local offset = (index - 1) * 2
  local window = tonumber(ARGV[offset + 2])
  local current = redis.call("INCR", key)
  if current == 1 or redis.call("PTTL", key) < 0 then
    redis.call("PEXPIRE", key, window)
  end
end

return {1, 0}
`

type RedisOptions struct {
	Address             string
	Username            string
	Password            string
	ClusterMode         bool
	TLSEnabled          bool
	CredentialsProvider CredentialsProvider
	DecisionTimeout     time.Duration
}

type RedisLimiter struct {
	client          redis.UniversalClient
	script          *redis.Script
	decisionTimeout time.Duration
	closeOnce       sync.Once
	closeErr        error
}

func NewRedisLimiter(ctx context.Context, options RedisOptions) (*RedisLimiter, error) {
	address := strings.TrimSpace(options.Address)
	host, port, err := net.SplitHostPort(address)
	if err != nil || strings.TrimSpace(host) == "" || strings.TrimSpace(port) == "" {
		return nil, fmt.Errorf("invalid Redis address %q", address)
	}
	if options.DecisionTimeout <= 0 {
		return nil, fmt.Errorf("Redis decision timeout must be positive")
	}
	if strings.TrimSpace(options.Password) != "" && options.CredentialsProvider != nil {
		return nil, fmt.Errorf("static and dynamic Redis credentials cannot both be configured")
	}

	var tlsConfig *tls.Config
	if options.TLSEnabled {
		tlsConfig = &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: host,
		}
	}
	common := redisOptions{
		address:             address,
		username:            strings.TrimSpace(options.Username),
		password:            options.Password,
		credentialsProvider: options.CredentialsProvider,
		decisionTimeout:     options.DecisionTimeout,
		tlsConfig:           tlsConfig,
	}

	var client redis.UniversalClient
	if options.ClusterMode {
		client = redis.NewClusterClient(common.cluster())
	} else {
		client = redis.NewClient(common.standalone())
	}
	limiter := &RedisLimiter{
		client:          client,
		script:          redis.NewScript(atomicDebitScript),
		decisionTimeout: options.DecisionTimeout,
	}

	pingContext, cancel := context.WithTimeout(ctx, options.DecisionTimeout)
	defer cancel()
	if err := client.Ping(pingContext).Err(); err != nil {
		_ = client.Close()
		return nil, fmt.Errorf("connect to distributed rate-limit backend: %w", err)
	}
	return limiter, nil
}

func (limiter *RedisLimiter) Decide(ctx context.Context, buckets []Bucket) (Decision, error) {
	if limiter == nil || limiter.client == nil || limiter.script == nil {
		return Decision{}, fmt.Errorf("distributed rate-limit backend is unavailable")
	}
	if len(buckets) == 0 {
		return Decision{}, fmt.Errorf("at least one rate-limit bucket is required")
	}

	keys := make([]string, 0, len(buckets))
	arguments := make([]interface{}, 0, len(buckets)*2)
	seen := make(map[string]struct{}, len(buckets))
	for _, bucket := range buckets {
		key, err := bucketKey(bucket)
		if err != nil {
			return Decision{}, err
		}
		if _, exists := seen[key]; exists {
			return Decision{}, fmt.Errorf("duplicate rate-limit bucket %q", bucket.Namespace)
		}
		seen[key] = struct{}{}
		keys = append(keys, key)
		windowMilliseconds := bucket.Window.Milliseconds()
		if windowMilliseconds < 1 {
			windowMilliseconds = 1
		}
		arguments = append(arguments, bucket.Limit, windowMilliseconds)
	}

	decisionContext, cancel := context.WithTimeout(ctx, limiter.decisionTimeout)
	defer cancel()
	result, err := limiter.script.Run(decisionContext, limiter.client, keys, arguments...).Result()
	if err != nil {
		return Decision{}, fmt.Errorf("evaluate distributed rate limit: %w", err)
	}
	values, ok := result.([]interface{})
	if !ok || len(values) != 2 {
		return Decision{}, fmt.Errorf("invalid distributed rate-limit response")
	}
	allowed, err := redisInteger(values[0])
	if err != nil {
		return Decision{}, err
	}
	retryMilliseconds, err := redisInteger(values[1])
	if err != nil {
		return Decision{}, err
	}
	if allowed != 0 && allowed != 1 {
		return Decision{}, fmt.Errorf("invalid distributed rate-limit allowance %d", allowed)
	}
	if retryMilliseconds < 0 {
		return Decision{}, fmt.Errorf("invalid distributed rate-limit retry delay %d", retryMilliseconds)
	}
	return Decision{
		Allowed:    allowed == 1,
		RetryAfter: time.Duration(retryMilliseconds) * time.Millisecond,
	}, nil
}

func (limiter *RedisLimiter) Close() error {
	if limiter == nil || limiter.client == nil {
		return nil
	}
	limiter.closeOnce.Do(func() {
		limiter.closeErr = limiter.client.Close()
	})
	return limiter.closeErr
}

type redisOptions struct {
	address             string
	username            string
	password            string
	credentialsProvider CredentialsProvider
	decisionTimeout     time.Duration
	tlsConfig           *tls.Config
}

func (options redisOptions) standalone() *redis.Options {
	return &redis.Options{
		Addr:                       options.address,
		Username:                   options.username,
		Password:                   options.password,
		CredentialsProviderContext: options.credentialsProvider,
		TLSConfig:                  options.tlsConfig,
		MaxRetries:                 1,
		DialerRetries:              1,
		DialTimeout:                options.decisionTimeout,
		ReadTimeout:                options.decisionTimeout,
		WriteTimeout:               options.decisionTimeout,
		PoolTimeout:                options.decisionTimeout,
		ContextTimeoutEnabled:      true,
	}
}

func (options redisOptions) cluster() *redis.ClusterOptions {
	return &redis.ClusterOptions{
		Addrs:                      []string{options.address},
		Username:                   options.username,
		Password:                   options.password,
		CredentialsProviderContext: options.credentialsProvider,
		TLSConfig:                  options.tlsConfig,
		MaxRedirects:               3,
		MaxRetries:                 1,
		DialerRetries:              1,
		DialTimeout:                options.decisionTimeout,
		ReadTimeout:                options.decisionTimeout,
		WriteTimeout:               options.decisionTimeout,
		PoolTimeout:                options.decisionTimeout,
		ContextTimeoutEnabled:      true,
	}
}

func redisInteger(value interface{}) (int64, error) {
	switch value := value.(type) {
	case int64:
		return value, nil
	case string:
		parsed, err := strconv.ParseInt(value, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("invalid distributed rate-limit integer %q", value)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("invalid distributed rate-limit integer type %T", value)
	}
}
