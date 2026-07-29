package config

import (
	"context"
	"strings"
	"time"

	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/secretsmanager"
	"github.com/aws/aws-sdk-go-v2/service/ssm"
)

const runtimeResolverTTL = 5 * time.Minute

type RuntimeResolvers struct {
	Secrets SecretsManagerAPI
	SSM     SSMAPI
}

type SecretKind string

const (
	SecretDatabase             SecretKind = "database"
	SecretCredentialEncryption SecretKind = "credential-encryption"
	SecretRazorpay             SecretKind = "razorpay"
	SecretLLM                  SecretKind = "llm"
	SecretExa                  SecretKind = "exa"
	SecretGSTLookup            SecretKind = "gst-lookup"
	SecretGSTProvider          SecretKind = "gst-provider"
	SecretDeepgram             SecretKind = "deepgram"
	SecretDeepSeek             SecretKind = "deepseek"
)

var ApplicationSecretKinds = []SecretKind{
	SecretCredentialEncryption,
	SecretRazorpay,
	SecretLLM,
	SecretExa,
	SecretGSTLookup,
	SecretGSTProvider,
	SecretDeepgram,
	SecretDeepSeek,
}

func SecretKindsForEntrypoint(entrypoint string) []SecretKind {
	var kinds []SecretKind
	switch entrypoint {
	case "http", "a2a-stream", "server":
		kinds = []SecretKind{
			SecretCredentialEncryption, SecretRazorpay, SecretLLM,
			SecretExa, SecretGSTLookup, SecretGSTProvider,
		}
	case "sqs-invoice":
		kinds = []SecretKind{SecretCredentialEncryption}
	case "sqs-gst":
		kinds = []SecretKind{SecretCredentialEncryption, SecretGSTProvider}
	case "sqs-bargaining":
		kinds = []SecretKind{SecretCredentialEncryption, SecretLLM, SecretExa}
	case "voice-session":
		kinds = []SecretKind{SecretDeepgram, SecretDeepSeek}
	}
	return append([]SecretKind(nil), kinds...)
}

type RuntimeResolverOptions struct {
	Clients           RuntimeResolvers
	SecretIdentifiers []string
	ParameterNames    []string
	TTL               time.Duration
	Now               func() time.Time
}

// RuntimeResolver is a process-lifetime dependency. Its child resolvers own the
// TTL caches, so callers must retain this object rather than copying resolved
// values and discarding it after bootstrap.
type RuntimeResolver struct {
	secrets *SecretResolver
	ssm     *SSMResolver
}

func NewRuntimeResolver(opts RuntimeResolverOptions) (*RuntimeResolver, error) {
	ttl := opts.TTL
	if ttl <= 0 {
		ttl = runtimeResolverTTL
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	r := &RuntimeResolver{}
	if len(nonEmpty(opts.SecretIdentifiers)) != 0 {
		resolver, err := NewSecretResolver(opts.Clients.Secrets, opts.SecretIdentifiers, ttl, now)
		if err != nil {
			return nil, err
		}
		r.secrets = resolver
	}
	if len(nonEmpty(opts.ParameterNames)) != 0 {
		resolver, err := NewSSMResolver(opts.Clients.SSM, opts.ParameterNames, ttl, now)
		if err != nil {
			return nil, err
		}
		r.ssm = resolver
	}
	return r, nil
}

type DatabaseCredentials struct {
	Host     string
	Port     int
	User     string
	Password string
	Name     string
	SSLMode  string
}

// Database resolves credentials at connection creation/reconnection time.
func (r *RuntimeResolver) Database(ctx context.Context, cfg *Config) (DatabaseCredentials, error) {
	if cfg == nil {
		return DatabaseCredentials{}, &ConfigurationError{Resource: "application config", Reason: "config is required"}
	}
	result := DatabaseCredentials{
		Host: cfg.Database.Host, Port: cfg.Database.Port, User: cfg.Database.User,
		Password: cfg.Database.Password, Name: cfg.Database.Name, SSLMode: cfg.Database.SSLMode,
	}
	if strings.TrimSpace(result.Host) == "" && strings.TrimSpace(cfg.SSM.DatabaseHostParam) != "" {
		if r == nil || r.ssm == nil {
			return DatabaseCredentials{}, &ConfigurationError{Resource: "SSM resolver", Reason: "resolver is required"}
		}
		values, err := r.ssm.Get(ctx, []string{cfg.SSM.DatabaseHostParam})
		if err != nil {
			return DatabaseCredentials{}, err
		}
		result.Host = values[cfg.SSM.DatabaseHostParam]
	}
	if strings.TrimSpace(result.User) == "" || strings.TrimSpace(result.Password) == "" {
		if strings.TrimSpace(cfg.Secrets.Database) == "" {
			return DatabaseCredentials{}, &ConfigurationError{Resource: "database secret identifier", Reason: "identifier is required"}
		}
		if r == nil || r.secrets == nil {
			return DatabaseCredentials{}, &ConfigurationError{Resource: "Secrets Manager resolver", Reason: "resolver is required"}
		}
		var err error
		if strings.TrimSpace(result.User) == "" {
			result.User, err = r.secrets.JSONField(ctx, cfg.Secrets.Database, "username")
			if err != nil {
				return DatabaseCredentials{}, err
			}
		}
		if strings.TrimSpace(result.Password) == "" {
			result.Password, err = r.secrets.JSONField(ctx, cfg.Secrets.Database, "password")
			if err != nil {
				return DatabaseCredentials{}, err
			}
		}
	}
	return result, nil
}

// Resolve refreshes only the provider credentials explicitly required by an
// entrypoint. Database credentials are intentionally handled by Database.
func (r *RuntimeResolver) Resolve(ctx context.Context, cfg *Config, kinds []SecretKind) error {
	if cfg == nil {
		return &ConfigurationError{Resource: "application config", Reason: "config is required"}
	}
	for _, kind := range kinds {
		if !cfg.hasConfiguredBinding(kind) {
			if isProductionEnv(cfg.Environment) {
				return &ConfigurationError{Resource: string(kind) + " secret identifier", Reason: "identifier is required"}
			}
			continue
		}
		for _, binding := range cfg.secretBindings(kind) {
			if strings.TrimSpace(binding.identifier) == "" {
				return &ConfigurationError{Resource: string(kind) + " secret identifier", Reason: "identifier is required"}
			}
			if r == nil || r.secrets == nil {
				return &ConfigurationError{Resource: "Secrets Manager resolver", Reason: "resolver is required"}
			}
			value, err := r.secrets.JSONField(ctx, binding.identifier, binding.field)
			if err != nil {
				return err
			}
			*binding.target = value
		}
	}
	cfg.VoiceRealtime = cfg.VoiceRealtime.WithDefaults(cfg.Deepgram)
	return nil
}

// ResolveRuntime remains as a compatibility boundary for local/server callers.
// Lambda entrypoints retain RuntimeResolver directly through app.Runtime.
func ResolveRuntime(ctx context.Context, cfg *Config, clients RuntimeResolvers) error {
	if cfg == nil {
		return &ConfigurationError{Resource: "application config", Reason: "config is required"}
	}
	identifiers := cfg.configuredSecretIdentifiers()
	parameters := nonEmpty([]string{cfg.SSM.DatabaseHostParam})
	needsSecrets := len(identifiers) != 0 &&
		(strings.TrimSpace(cfg.Database.User) == "" || strings.TrimSpace(cfg.Database.Password) == "" || cfg.hasUnresolvedBindings(ApplicationSecretKinds))
	needsSSM := strings.TrimSpace(cfg.Database.Host) == "" && len(parameters) != 0
	if !needsSecrets {
		identifiers = nil
	}
	if !needsSSM {
		parameters = nil
	}
	if (needsSecrets && clients.Secrets == nil) || (needsSSM && clients.SSM == nil) {
		awsCfg, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(cfg.AWS.Region))
		if err != nil {
			return &ResolutionError{Resource: "AWS SDK configuration"}
		}
		if needsSecrets && clients.Secrets == nil {
			clients.Secrets = secretsmanager.NewFromConfig(awsCfg)
		}
		if needsSSM && clients.SSM == nil {
			clients.SSM = ssm.NewFromConfig(awsCfg)
		}
	}
	resolver, err := NewRuntimeResolver(RuntimeResolverOptions{
		Clients: clients, SecretIdentifiers: identifiers, ParameterNames: parameters,
	})
	if err != nil {
		return err
	}
	db, err := resolver.Database(ctx, cfg)
	if err != nil {
		return err
	}
	cfg.Database.Host, cfg.Database.User, cfg.Database.Password = db.Host, db.User, db.Password
	if needsSecrets {
		kinds := make([]SecretKind, 0, len(ApplicationSecretKinds))
		for _, kind := range ApplicationSecretKinds {
			if cfg.hasConfiguredBinding(kind) {
				kinds = append(kinds, kind)
			}
		}
		if err := resolver.Resolve(ctx, cfg, kinds); err != nil {
			return err
		}
	}
	return validate(cfg)
}

type secretBinding struct {
	identifier string
	field      string
	target     *string
}

func (c *Config) secretBindings(kind SecretKind) []secretBinding {
	switch kind {
	case SecretCredentialEncryption:
		return []secretBinding{{c.Secrets.CredentialEncryption, "encryption_key", &c.Credentials.EncryptionKey}}
	case SecretRazorpay:
		return []secretBinding{
			{c.Secrets.Razorpay, "key_id", &c.Razorpay.KeyID},
			{c.Secrets.Razorpay, "key_secret", &c.Razorpay.KeySecret},
			{c.Secrets.Razorpay, "webhook_secret", &c.Razorpay.WebhookSecret},
		}
	case SecretLLM:
		return []secretBinding{{c.Secrets.LLM, "api_key", &c.LLM.APIKey}}
	case SecretExa:
		return []secretBinding{{c.Secrets.Exa, "api_key", &c.LLM.ExaAPIKey}}
	case SecretGSTLookup:
		return []secretBinding{{c.Secrets.GSTLookup, "api_key", &c.GSTLookup.APIKey}}
	case SecretGSTProvider:
		return []secretBinding{
			{c.Secrets.GSTProvider, "client_id", &c.GST.ClientID},
			{c.Secrets.GSTProvider, "client_secret", &c.GST.ClientSecret},
			{c.Secrets.GSTProvider, "username", &c.GST.Username},
			{c.Secrets.GSTProvider, "password", &c.GST.Password},
			{c.Secrets.GSTProvider, "api_token", &c.GST.APIToken},
		}
	case SecretDeepgram:
		return []secretBinding{
			{c.Secrets.Deepgram, "api_key", &c.Deepgram.APIKey},
			{c.Secrets.Deepgram, "api_key", &c.VoiceRealtime.DeepgramAPIKey},
		}
	case SecretDeepSeek:
		return []secretBinding{{c.Secrets.DeepSeek, "api_key", &c.VoiceRealtime.DeepSeekAPIKey}}
	default:
		return nil
	}
}

func (c *Config) hasConfiguredBinding(kind SecretKind) bool {
	for _, binding := range c.secretBindings(kind) {
		if strings.TrimSpace(binding.identifier) != "" {
			return true
		}
	}
	return false
}

func (c *Config) hasUnresolvedBindings(kinds []SecretKind) bool {
	for _, kind := range kinds {
		for _, binding := range c.secretBindings(kind) {
			if strings.TrimSpace(binding.identifier) != "" && strings.TrimSpace(*binding.target) == "" {
				return true
			}
		}
	}
	return false
}

func (c *Config) configuredSecretIdentifiers() []string {
	return nonEmpty([]string{
		c.Secrets.Database,
		c.Secrets.CredentialEncryption,
		c.Secrets.Razorpay,
		c.Secrets.LLM,
		c.Secrets.Exa,
		c.Secrets.GSTLookup,
		c.Secrets.GSTProvider,
		c.Secrets.Deepgram,
		c.Secrets.DeepSeek,
	})
}

func (c *Config) SecretIdentifierValues() []string {
	return c.configuredSecretIdentifiers()
}

func nonEmpty(values []string) []string {
	seen := make(map[string]struct{}, len(values))
	result := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		result = append(result, value)
	}
	return result
}
