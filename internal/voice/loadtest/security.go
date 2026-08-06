package loadtest

import (
	"encoding/json"
	"errors"
	"net/netip"
	"net/url"
	"strings"

	"invoice-backend/internal/voice/protocol"
)

var (
	ErrAttachRejected  = errors.New("offline attach policy rejected the attempt")
	ErrUnsafeEndpoint  = errors.New("offline endpoint policy rejected the target")
	ErrPayloadTooLarge = errors.New("offline payload policy rejected the payload")
)

const MaxSignalingPayloadBytes = 16 << 10

var forbiddenPrefixes = [...]netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("::/128"),
	netip.MustParsePrefix("::1/128"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("100::/64"),
	netip.MustParsePrefix("2001::/23"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("fc00::/7"),
	netip.MustParsePrefix("fe80::/10"),
	netip.MustParsePrefix("ff00::/8"),
}

type securityBinding struct {
	logicalSessionID string
	runtimeSessionID string
	userID           string
	businessID       string
	clientID         string
	terminal         bool
}

type attachAttempt struct {
	logicalSessionID string
	runtimeSessionID string
	userID           string
	businessID       string
	clientID         string
}

func validateAttach(binding securityBinding, attempt attachAttempt) error {
	if binding.terminal || binding.logicalSessionID != attempt.logicalSessionID || binding.runtimeSessionID != attempt.runtimeSessionID ||
		binding.userID != attempt.userID || binding.businessID != attempt.businessID || binding.clientID != attempt.clientID {
		return ErrAttachRejected
	}
	return nil
}

// ValidateResolvedAddresses must be applied to every fresh connection so a
// previously public DNS name cannot rebind to a non-public target.
func ValidateResolvedAddresses(addresses []netip.Addr) error {
	if len(addresses) == 0 {
		return ErrUnsafeEndpoint
	}
	seen := make(map[netip.Addr]struct{}, len(addresses))
	for _, address := range addresses {
		address = address.Unmap()
		if !address.IsValid() || address.Zone() != "" || !address.IsGlobalUnicast() || forbiddenAddress(address) {
			return ErrUnsafeEndpoint
		}
		seen[address] = struct{}{}
	}
	if len(seen) == 0 {
		return ErrUnsafeEndpoint
	}
	return nil
}

func forbiddenAddress(address netip.Addr) bool {
	for _, prefix := range forbiddenPrefixes {
		if prefix.Contains(address) {
			return true
		}
	}
	return false
}

func validateEndpoint(raw string, allowedSchemes map[string]struct{}, allowedHost string) error {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return ErrUnsafeEndpoint
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return ErrUnsafeEndpoint
	}
	if _, ok := allowedSchemes[strings.ToLower(parsed.Scheme)]; !ok {
		return ErrUnsafeEndpoint
	}
	host := strings.ToLower(parsed.Hostname())
	if host == "" || strings.TrimSuffix(host, ".") != host || strings.ContainsAny(host, "\x00\r\n\t /\\") {
		return ErrUnsafeEndpoint
	}
	if allowedHost != "" && host != strings.ToLower(allowedHost) {
		return ErrUnsafeEndpoint
	}
	if address, parseErr := netip.ParseAddr(host); parseErr == nil {
		return ValidateResolvedAddresses([]netip.Addr{address})
	}
	if !validPublicDNSName(host) {
		return ErrUnsafeEndpoint
	}
	return nil
}

func validPublicDNSName(host string) bool {
	if len(host) > 253 || !strings.Contains(host, ".") || host == "localhost" ||
		strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".home") || strings.HasSuffix(host, ".lan") {
		return false
	}
	for _, label := range strings.Split(host, ".") {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for _, character := range label {
			if (character >= 'a' && character <= 'z') || (character >= '0' && character <= '9') || character == '-' {
				continue
			}
			return false
		}
	}
	return true
}

func validateToolEndpoint(raw, allowedHost string, addresses []netip.Addr) error {
	if err := validateEndpoint(raw, map[string]struct{}{"https": {}}, allowedHost); err != nil {
		return err
	}
	return ValidateResolvedAddresses(addresses)
}

func validateSignalingPayload(size int) error {
	if size <= 0 || size > MaxSignalingPayloadBytes {
		return ErrPayloadTooLarge
	}
	return nil
}

// RedactLogFields returns a detached, bounded map suitable for local evidence.
func RedactLogFields(fields map[string]string) map[string]string {
	redacted := make(map[string]string, len(fields))
	for key, value := range fields {
		normalized := strings.NewReplacer("_", "", "-", "", ".", "", " ", "").Replace(strings.ToLower(key))
		if strings.Contains(normalized, "authorization") || strings.Contains(normalized, "token") ||
			strings.Contains(normalized, "apikey") || strings.Contains(normalized, "providerkey") ||
			strings.Contains(normalized, "accesskey") || strings.Contains(normalized, "subscriptionkey") ||
			strings.Contains(normalized, "secret") || strings.Contains(normalized, "credential") ||
			containsSensitiveMarker(value) {
			redacted[key] = "[REDACTED]"
			continue
		}
		if len(value) > 256 {
			value = value[:256]
		}
		redacted[key] = value
	}
	return redacted
}

func containsSensitiveMarker(value string) bool {
	lower := strings.ToLower(value)
	return strings.Contains(lower, "bearer ") || strings.Contains(lower, "sarvam-") || strings.Contains(value, "AKIA")
}

// RunSecurityMatrix executes deterministic attack inputs through the local
// policy model and the real DataChannel decoder.
func RunSecurityMatrix() []Check {
	binding := securityBinding{
		logicalSessionID: "voice_01KLOCAL", runtimeSessionID: "voice-session-01KLOCAL000000000000000", userID: "user-a", businessID: "business-a", clientID: "mobile-a",
	}
	validAttempt := attachAttempt{
		logicalSessionID: binding.logicalSessionID, runtimeSessionID: binding.runtimeSessionID, userID: binding.userID, businessID: binding.businessID, clientID: binding.clientID,
	}

	results := make([]Check, 0, 13)
	crossTenant := validAttempt
	crossTenant.userID = "user-b"
	results = append(results, blockedCheck("attach.cross_tenant", validateAttach(binding, crossTenant)))
	terminal := binding
	terminal.terminal = true
	results = append(results, blockedCheck("runtime_id.reuse", validateAttach(terminal, validAttempt)))
	wrongClient := validAttempt
	wrongClient.clientID = "mobile-wrong"
	results = append(results, blockedCheck("cognito.wrong_client", validateAttach(binding, wrongClient)))

	public := []netip.Addr{netip.MustParseAddr("8.8.8.8")}
	results = append(results, blockedCheck("tool.unallowlisted_url", validateToolEndpoint("https://evil.example.com/tool", "api.example.com", public)))
	results = append(results, blockedCheck("ssrf.metadata_ipv4", validateToolEndpoint("https://169.254.169.254/latest/meta-data", "", public)))
	results = append(results, blockedCheck("ssrf.metadata_ipv6", validateToolEndpoint("https://[fd00:ec2::254]/latest/meta-data", "", public)))
	results = append(results, blockedCheck("ssrf.localhost", validateToolEndpoint("https://localhost/tool", "", public)))
	results = append(results, blockedCheck("ssrf.private_ipv4", validateToolEndpoint("https://10.0.0.1/tool", "", public)))
	rebindErr := ValidateResolvedAddresses(public)
	if rebindErr == nil {
		rebindErr = ValidateResolvedAddresses([]netip.Addr{netip.MustParseAddr("127.0.0.1")})
	}
	results = append(results, blockedCheck("ssrf.dns_rebinding", rebindErr))

	dataChannel := make([]byte, protocol.MaxControlMessageBytes+1)
	_, dataChannelErr := protocol.DecodeControlMessage(dataChannel, protocol.ClientToRuntime, nil)
	results = append(results, blockedCheck("payload.datachannel_oversize", dataChannelErr))
	results = append(results, blockedCheck("payload.signaling_oversize", validateSignalingPayload(MaxSignalingPayloadBytes+1)))

	redacted := RedactLogFields(map[string]string{
		"authorization":  "Bearer security-canary",
		"provider_key":   "sarvam-security-canary",
		"aws_access_key": "AKIAIOSFODNN7EXAMPLE",
	})
	encoded, _ := json.Marshal(redacted)
	authorizationLeaked := strings.Contains(string(encoded), "Bearer security-canary")
	results = append(results, blockedCheck("logs.authorization_redaction", redactionRejection(authorizationLeaked)))
	providerKeyLeaked := strings.Contains(string(encoded), "sarvam-security-canary") || strings.Contains(string(encoded), "AKIAIOSFODNN7EXAMPLE")
	results = append(results, blockedCheck("logs.provider_key_redaction", redactionRejection(providerKeyLeaked)))
	return results
}

func redactionRejection(leaked bool) error {
	if leaked {
		return nil
	}
	return ErrAttachRejected
}

func blockedCheck(id string, rejection error) Check {
	outcome := OutcomeBlocked
	if rejection == nil {
		outcome = "ALLOWED"
	}
	return localCheck(id, outcome)
}
