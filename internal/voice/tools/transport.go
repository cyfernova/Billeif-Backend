package tools

import (
	"context"
	"crypto/tls"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	defaultToolTimeout = 3 * time.Second
	maximumToolTimeout = 5 * time.Second
)

var forbiddenAddressPrefixes = [...]netip.Prefix{
	// IPv4 non-public and special-purpose address space.
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("10.0.0.0/8"),
	netip.MustParsePrefix("100.64.0.0/10"),
	netip.MustParsePrefix("127.0.0.0/8"),
	netip.MustParsePrefix("169.254.0.0/16"),
	netip.MustParsePrefix("172.16.0.0/12"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("192.0.2.0/24"),
	netip.MustParsePrefix("192.88.99.0/24"),
	netip.MustParsePrefix("192.168.0.0/16"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("198.51.100.0/24"),
	netip.MustParsePrefix("203.0.113.0/24"),
	netip.MustParsePrefix("224.0.0.0/4"),
	netip.MustParsePrefix("240.0.0.0/4"),

	// IPv6 unspecified, loopback, translation, documentation, private,
	// link-local, and multicast address space.
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

type validatingDialer struct {
	host     string
	port     string
	resolver Resolver
	dialer   Dialer
}

func newHTTPClient(config Config) (*http.Client, *http.Transport, string, time.Duration, error) {
	origin, host, port, err := validateOrigin(config.Origin)
	if err != nil {
		return nil, nil, "", 0, ErrInvalidConfiguration
	}

	timeout := config.Timeout
	if timeout == 0 {
		timeout = defaultToolTimeout
	}
	if timeout < 0 || timeout > maximumToolTimeout {
		return nil, nil, "", 0, ErrInvalidConfiguration
	}

	resolverMissing := nilInterface(config.Resolver)
	dialerMissing := nilInterface(config.Dialer)
	if resolverMissing != dialerMissing {
		return nil, nil, "", 0, ErrInvalidConfiguration
	}
	var resolver Resolver = config.Resolver
	var dialer Dialer = config.Dialer
	if resolverMissing {
		resolver = net.DefaultResolver
		dialer = &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
	}

	var roots = config.RootCAs
	if roots != nil {
		roots = roots.Clone()
	}
	dialContext := (&validatingDialer{
		host: host, port: port, resolver: resolver, dialer: dialer,
	}).DialContext
	transport := &http.Transport{
		Proxy:                 nil,
		DialContext:           dialContext,
		ForceAttemptHTTP2:     true,
		DisableCompression:    true,
		MaxIdleConns:          4,
		MaxIdleConnsPerHost:   4,
		MaxConnsPerHost:       8,
		IdleConnTimeout:       30 * time.Second,
		TLSHandshakeTimeout:   timeout,
		ResponseHeaderTimeout: timeout,
		ExpectContinueTimeout: time.Second,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			ServerName: host,
			RootCAs:    roots,
		},
	}
	client := &http.Client{
		Transport: transport,
		Timeout:   timeout,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
	return client, transport, origin, timeout, nil
}

func validateOrigin(raw string) (origin, host, port string, err error) {
	if raw == "" || strings.TrimSpace(raw) != raw {
		return "", "", "", ErrInvalidConfiguration
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil ||
		parsed.Host == "" || parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" ||
		!validOriginBasePath(parsed.Path) {
		return "", "", "", ErrInvalidConfiguration
	}
	host = strings.ToLower(parsed.Hostname())
	if host == "" || strings.TrimSuffix(host, ".") != host || strings.ContainsAny(host, "\x00\r\n\t /\\") {
		return "", "", "", ErrInvalidConfiguration
	}
	if literal, parseErr := netip.ParseAddr(host); parseErr == nil {
		if literal.Zone() != "" || !publicAddress(literal) {
			return "", "", "", ErrInvalidConfiguration
		}
	} else if !validDNSHost(host) {
		return "", "", "", ErrInvalidConfiguration
	}
	port = parsed.Port()
	if port == "" {
		if strings.HasSuffix(parsed.Host, ":") {
			return "", "", "", ErrInvalidConfiguration
		}
		port = "443"
	}
	portNumber, parsePortErr := strconv.Atoi(port)
	if parsePortErr != nil || portNumber < 1 || portNumber > 65535 {
		return "", "", "", ErrInvalidConfiguration
	}
	parsed.Scheme = "https"
	parsed.Host = strings.ToLower(parsed.Host)
	if parsed.Path == "/" {
		parsed.Path = ""
	} else {
		parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	}
	origin = strings.TrimSuffix(parsed.String(), "/")
	return origin, host, port, nil
}

func validDNSHost(host string) bool {
	if len(host) > 253 || !strings.Contains(host, ".") || host == "localhost" ||
		strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".local") ||
		strings.HasSuffix(host, ".internal") || strings.HasSuffix(host, ".home") ||
		strings.HasSuffix(host, ".lan") {
		return false
	}
	labels := strings.Split(host, ".")
	allNumeric := true
	for _, label := range labels {
		if len(label) == 0 || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return false
		}
		for index := 0; index < len(label); index++ {
			character := label[index]
			if character >= '0' && character <= '9' {
				continue
			}
			allNumeric = false
			if (character >= 'a' && character <= 'z') || character == '-' {
				continue
			}
			return false
		}
	}
	return !allNumeric
}

func validOriginBasePath(value string) bool {
	if value == "" || value == "/" {
		return true
	}
	if value[0] != '/' || strings.Contains(value, "//") {
		return false
	}
	trimmed := strings.TrimSuffix(value, "/")
	for _, segment := range strings.Split(strings.TrimPrefix(trimmed, "/"), "/") {
		if segment == "" || segment == "." || segment == ".." {
			return false
		}
		for index := 0; index < len(segment); index++ {
			character := segment[index]
			if (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z') ||
				(character >= '0' && character <= '9') || strings.ContainsRune("-._~$", rune(character)) {
				continue
			}
			return false
		}
	}
	return true
}

func (dialer *validatingDialer) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil || !strings.EqualFold(host, dialer.host) || port != dialer.port {
		return nil, ErrToolUnavailable
	}
	answers, err := dialer.resolver.LookupIPAddr(ctx, dialer.host)
	if err != nil || len(answers) == 0 {
		return nil, ErrToolUnavailable
	}

	validated := make([]netip.Addr, 0, len(answers))
	seen := make(map[netip.Addr]struct{}, len(answers))
	for _, answer := range answers {
		if answer.Zone != "" {
			return nil, ErrToolUnavailable
		}
		resolved, ok := netip.AddrFromSlice(answer.IP)
		if !ok {
			return nil, ErrToolUnavailable
		}
		resolved = resolved.Unmap()
		if !publicAddress(resolved) {
			return nil, ErrToolUnavailable
		}
		if _, duplicate := seen[resolved]; duplicate {
			continue
		}
		seen[resolved] = struct{}{}
		validated = append(validated, resolved)
	}
	if len(validated) == 0 {
		return nil, ErrToolUnavailable
	}

	for _, resolved := range validated {
		connection, dialErr := dialer.dialer.DialContext(ctx, network, net.JoinHostPort(resolved.String(), port))
		if dialErr == nil {
			return connection, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}
	return nil, ErrToolUnavailable
}

func publicAddress(address netip.Addr) bool {
	if !address.IsValid() {
		return false
	}
	address = address.Unmap()
	if !address.IsGlobalUnicast() || address.IsPrivate() || address.IsLoopback() ||
		address.IsLinkLocalUnicast() || address.IsLinkLocalMulticast() || address.IsMulticast() ||
		address.IsUnspecified() {
		return false
	}
	for _, prefix := range forbiddenAddressPrefixes {
		if prefix.Contains(address) {
			return false
		}
	}
	return true
}

var _ interface {
	DialContext(context.Context, string, string) (net.Conn, error)
} = (*validatingDialer)(nil)
