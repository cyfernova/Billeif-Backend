package verify

import (
	"fmt"
	"reflect"
	"regexp"
	"strings"
)

// redactedPlaceholder is the value substituted for every redacted field. The
// distinct suffixes below keep diagnostics useful while never exposing the
// underlying secret material.
const redactedPlaceholder = "[REDACTED]"

const (
	redactedBearerToken    = "[REDACTED_BEARER_TOKEN]"
	redactedJWT            = "[REDACTED_JWT]"
	redactedAWSAccessKeyID = "[REDACTED_AWS_ACCESS_KEY_ID]"
	redactedRazorpayKeyID  = "[REDACTED_RAZORPAY_KEY_ID]"
	redactedCredential     = "[REDACTED_CREDENTIAL]"
	redactedARN            = "[REDACTED_ARN]"
	redactedAccountID      = "[REDACTED_ACCOUNT_ID]"
	redactedURL            = "[REDACTED_URL]"
	truncationMarker       = "[truncated]"
)

const (
	defaultMaxDepth    = 12
	defaultMaxEntries  = 256
	defaultMaxStrBytes = 512
)

// sensitiveKeyFragments are matched case-insensitively against map keys. Any
// match redacts the whole value. The list covers credentials, authorization
// material, cookies, tokens, provider secrets and session material.
var sensitiveKeyFragments = []string{
	"password", "passwd", "secret", "token", "authorization", "cookie",
	"credential", "api_key", "apikey", "access_key", "private_key",
	"session_key", "signature", "otp", "webhook_secret",
	"client_secret", "bearer", "dsn", "access_token", "id_token",
	"key_secret", "key_id", "accesskey",
}

var (
	bearerPattern       = regexp.MustCompile(`(?i)bearer\s+[a-z0-9._~+/=-]{8,}`)
	jwtPattern          = regexp.MustCompile(`eyJ[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}\.[A-Za-z0-9_-]{8,}`)
	awsAccessKeyPattern = regexp.MustCompile(`\b(?:AKIA|ASIA)[0-9A-Z]{16}\b`)
	razorpayKeyPattern  = regexp.MustCompile(`\brzp_(?:test|live)_[A-Za-z0-9]{6,}\b`)
	arnPattern          = regexp.MustCompile(`\barn:[a-z0-9-]*:[^\s"']+`)
	accountIDPattern    = regexp.MustCompile(`\b[0-9]{12}\b`)
	urlPattern          = regexp.MustCompile(`https?://[^\s"']+`)
	hexCredential       = regexp.MustCompile(`\b[0-9a-fA-F]{32,}\b`)
	// querySecretPattern redacts values of secret-bearing query parameters.
	querySecretPattern = regexp.MustCompile(`(?i)((?:token|secret|password|passwd|sig|signature|api_?key|access_?key|code|otp)=)([^&\s]+)`)
	// dsnCredentialPattern redacts user:password@ inside connection strings.
	dsnCredentialPattern = regexp.MustCompile(`([a-z][a-z0-9+.-]*://[^:/\s@]+:)([^@\s/]+)@`)
)

// RedactString sanitizes a single string value. It removes bearer tokens,
// JWTs, cloud access key ids, provider key ids, ARNs, credential-shaped hex
// strings, secret query parameters and DSN passwords, and bounds length.
func RedactString(s string) string {
	s = bearerPattern.ReplaceAllString(s, redactedBearerToken)
	s = jwtPattern.ReplaceAllString(s, redactedJWT)
	s = awsAccessKeyPattern.ReplaceAllString(s, redactedAWSAccessKeyID)
	s = razorpayKeyPattern.ReplaceAllString(s, redactedRazorpayKeyID)
	s = arnPattern.ReplaceAllString(s, redactedARN)
	s = accountIDPattern.ReplaceAllString(s, redactedAccountID)
	s = urlPattern.ReplaceAllString(s, redactedURL)
	s = hexCredential.ReplaceAllString(s, redactedCredential)
	s = dsnCredentialPattern.ReplaceAllString(s, "$1[REDACTED]@")
	s = querySecretPattern.ReplaceAllString(s, "$1[REDACTED]")
	s = redactBasicAuthURL(s)
	if len(s) > defaultMaxStrBytes {
		s = s[:defaultMaxStrBytes] + truncationMarker
	}
	return s
}

func redactBasicAuthURL(s string) string {
	parts := strings.Split(s, "://")
	if len(parts) < 2 {
		return s
	}
	scheme := parts[0]
	rest := strings.Join(parts[1:], "://")
	at := strings.LastIndex(rest, "@")
	if at < 0 {
		return s
	}
	authority := rest[:at]
	if !strings.Contains(authority, ":") {
		return s
	}
	colon := strings.Index(authority, ":")
	if colon < 0 {
		return s
	}
	return scheme + "://" + authority[:colon] + ":[REDACTED]@" + rest[at+1:]
}

// RedactValue recursively sanitizes any JSON-like value. Maps with sensitive
// keys are redacted wholesale; string values pass through RedactString. The
// walk is depth-bounded, entry-bounded, cycle-safe, and never mutates its
// input. Values of unknown types degrade to a Go type name so arbitrary
// Stringer output can never leak.
func RedactValue(value any) any {
	return redactValue(value, defaultMaxDepth, make(map[uintptr]struct{}))
}

func redactValue(value any, depth int, seen map[uintptr]struct{}) any {
	if depth <= 0 {
		return "[REDACTED_DEPTH_LIMIT]"
	}
	switch typed := value.(type) {
	case nil:
		return nil
	case string:
		return RedactString(typed)
	case bool, int, int8, int16, int32, int64,
		uint, uint8, uint16, uint32, uint64,
		float32, float64:
		return typed
	case map[string]any:
		if key, cyclic := cycleKey(typed, seen); cyclic {
			return map[string]any{"cyclic": true, "ref": key}
		}
		out := make(map[string]any, len(typed))
		for key, item := range typed {
			if isSensitiveKey(key) {
				out[key] = redactedPlaceholder
				continue
			}
			out[key] = redactValue(item, depth-1, seen)
		}
		return out
	case []any:
		if key, cyclic := cycleKey(typed, seen); cyclic {
			return []any{map[string]any{"cyclic": true, "ref": key}}
		}
		if len(typed) > defaultMaxEntries {
			bounded := make([]any, 0, defaultMaxEntries+1)
			for _, item := range typed[:defaultMaxEntries] {
				bounded = append(bounded, redactValue(item, depth-1, seen))
			}
			return append(bounded, fmt.Sprintf("[... %d more entries omitted]", len(typed)-defaultMaxEntries))
		}
		out := make([]any, len(typed))
		for i, item := range typed {
			out[i] = redactValue(item, depth-1, seen)
		}
		return out
	case error:
		return RedactString(typed.Error())
	case fmt.Stringer:
		// Stringers may embed secrets; treat their output as untrusted text.
		return RedactString(typed.String())
	default:
		return fmt.Sprintf("%T", typed)
	}
}

// cycleKey builds a stable reference for pointer-backed containers so cycles
// can be detected without mutation of the input.
func cycleKey(value any, seen map[uintptr]struct{}) (string, bool) {
	var ptr uintptr
	switch typed := value.(type) {
	case map[string]any:
		if len(typed) == 0 {
			return "", false
		}
		ptr = reflect.ValueOf(typed).Pointer()
	case []any:
		if len(typed) == 0 {
			return "", false
		}
		ptr = reflect.ValueOf(typed).Pointer()
	default:
		return "", false
	}
	if _, cyclic := seen[ptr]; cyclic {
		return fmt.Sprintf("%#x", ptr), true
	}
	seen[ptr] = struct{}{}
	return "", false
}

func isSensitiveKey(key string) bool {
	normalized := strings.ToLower(key)
	normalized = strings.ReplaceAll(normalized, "-", "_")
	for _, fragment := range sensitiveKeyFragments {
		if strings.Contains(normalized, fragment) {
			return true
		}
	}
	return false
}

// SanitizeEvidence redacts and sorts evidence so the report is deterministic
// and free of secrets regardless of what a probe attached.
func SanitizeEvidence(evidence []Evidence) []Evidence {
	out := make([]Evidence, 0, len(evidence))
	for _, item := range evidence {
		out = append(out, Evidence{
			Key:   RedactString(item.Key),
			Value: RedactValue(item.Value),
		})
	}
	sortEvidence(out)
	return out
}

func sortEvidence(evidence []Evidence) {
	for i := 1; i < len(evidence); i++ {
		for j := i; j > 0 && evidence[j].Key < evidence[j-1].Key; j-- {
			evidence[j], evidence[j-1] = evidence[j-1], evidence[j]
		}
	}
}
