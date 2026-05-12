package a2a

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type lookupIPAddrFunc func(ctx context.Context, host string) ([]net.IPAddr, error)
type dialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

func newSafeA2AHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}

	return &http.Client{
		Timeout:   timeout,
		Transport: newSafeA2ATransport(defaultLookupIPAddr, dialer.DialContext),
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func newSafeA2ATransport(lookup lookupIPAddrFunc, dial dialContextFunc) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialSafeA2AAddress(ctx, network, address, lookup, dial)
	}
	return transport
}

func defaultLookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

func dialSafeA2AAddress(ctx context.Context, network, address string, lookup lookupIPAddrFunc, dial dialContextFunc) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid A2A destination: %w", err)
	}

	ips, err := resolveAllowedA2AIPs(ctx, host, lookup)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, ip := range ips {
		conn, dialErr := dial(ctx, network, net.JoinHostPort(ip.String(), port))
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}
	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no reachable A2A destination")
}

func resolveAllowedA2AIPs(ctx context.Context, host string, lookup lookupIPAddrFunc) ([]net.IP, error) {
	normalizedHost := normalizeA2AHost(host)
	if err := validateA2AHost(normalizedHost); err != nil {
		return nil, err
	}

	if ip := net.ParseIP(normalizedHost); ip != nil {
		if isDeniedA2AIP(ip) {
			return nil, fmt.Errorf("private or local IP addresses are not allowed")
		}
		return []net.IP{ip}, nil
	}

	lookedUp, err := lookup(ctx, normalizedHost)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve A2A host")
	}

	ips := make([]net.IP, 0, len(lookedUp))
	for _, candidate := range lookedUp {
		if isDeniedA2AIP(candidate.IP) {
			return nil, fmt.Errorf("private or local IP addresses are not allowed")
		}
		ips = append(ips, candidate.IP)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no resolved IP addresses for A2A host")
	}
	return ips, nil
}

func safeA2AMessageURL(receiverEndpoint string) (string, error) {
	endpoint := normalizeA2ABaseURL(receiverEndpoint) + "/message:send"
	if err := validateA2AURL(endpoint); err != nil {
		return "", err
	}
	return endpoint, nil
}

func validateA2AURL(rawURL string) error {
	trimmed := strings.TrimSpace(rawURL)
	if trimmed == "" {
		return fmt.Errorf("A2A endpoint is required")
	}

	parsed, err := url.Parse(trimmed)
	if err != nil {
		return fmt.Errorf("invalid A2A endpoint: %w", err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("A2A endpoint must use http or https")
	}
	if parsed.Host == "" || parsed.Hostname() == "" {
		return fmt.Errorf("A2A endpoint host is required")
	}
	if parsed.User != nil {
		return fmt.Errorf("A2A endpoint must not include userinfo")
	}

	host := normalizeA2AHost(parsed.Hostname())
	if err := validateA2AHost(host); err != nil {
		return err
	}
	if ip := net.ParseIP(host); ip != nil && isDeniedA2AIP(ip) {
		return fmt.Errorf("private or local IP addresses are not allowed")
	}
	return nil
}

func normalizeA2AHost(host string) string {
	trimmed := strings.ToLower(strings.TrimSpace(host))
	return strings.TrimSuffix(trimmed, ".")
}

func validateA2AHost(host string) error {
	if host == "" {
		return fmt.Errorf("A2A host is required")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".internal") {
		return fmt.Errorf("local or internal hosts are not allowed")
	}
	return nil
}

func isDeniedA2AIP(ip net.IP) bool {
	if ip == nil {
		return true
	}
	return ip.IsLoopback() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() ||
		ip.IsMulticast()
}
