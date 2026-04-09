package services

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

type lookupIPAddrFunc func(ctx context.Context, host string) ([]net.IPAddr, error)
type dialContextFunc func(ctx context.Context, network, address string) (net.Conn, error)

func defaultLookupIPAddr(ctx context.Context, host string) ([]net.IPAddr, error) {
	return net.DefaultResolver.LookupIPAddr(ctx, host)
}

func newWebhookDeliveryHTTPClient(timeout time.Duration) *http.Client {
	dialer := &net.Dialer{
		Timeout:   10 * time.Second,
		KeepAlive: 30 * time.Second,
	}
	transport := newWebhookDeliveryTransport(defaultLookupIPAddr, dialer.DialContext)
	return &http.Client{
		Timeout:   timeout,
		Transport: transport,
		// Do not follow redirects for webhook delivery to prevent host pivoting.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}
}

func newWebhookDeliveryTransport(lookup lookupIPAddrFunc, dial dialContextFunc) *http.Transport {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		return dialWebhookAddress(ctx, network, address, lookup, dial)
	}
	return transport
}

func dialWebhookAddress(ctx context.Context, network, address string, lookup lookupIPAddrFunc, dial dialContextFunc) (net.Conn, error) {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("invalid webhook destination: %w", err)
	}

	ips, err := resolveAllowedWebhookIPs(ctx, host, lookup)
	if err != nil {
		return nil, err
	}

	var lastErr error
	for _, ip := range ips {
		target := net.JoinHostPort(ip.String(), port)
		conn, dialErr := dial(ctx, network, target)
		if dialErr == nil {
			return conn, nil
		}
		lastErr = dialErr
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, fmt.Errorf("no reachable webhook destination")
}

func resolveAllowedWebhookIPs(ctx context.Context, host string, lookup lookupIPAddrFunc) ([]net.IP, error) {
	normalizedHost := normalizeWebhookHost(host)
	if err := validateWebhookHost(normalizedHost); err != nil {
		return nil, err
	}

	if ip := net.ParseIP(normalizedHost); ip != nil {
		if isDeniedIP(ip) {
			return nil, fmt.Errorf("private or local IP addresses are not allowed")
		}
		return []net.IP{ip}, nil
	}

	lookedUp, err := lookup(ctx, normalizedHost)
	if err != nil {
		return nil, fmt.Errorf("failed to resolve webhook host")
	}

	ips := make([]net.IP, 0, len(lookedUp))
	for _, candidate := range lookedUp {
		if isDeniedIP(candidate.IP) {
			return nil, fmt.Errorf("private or local IP addresses are not allowed")
		}
		ips = append(ips, candidate.IP)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("no resolved IP addresses for webhook host")
	}
	return ips, nil
}

func normalizeWebhookHost(host string) string {
	trimmed := strings.ToLower(strings.TrimSpace(host))
	return strings.TrimSuffix(trimmed, ".")
}

func validateWebhookHost(host string) error {
	if host == "" {
		return fmt.Errorf("webhook host is required")
	}
	if host == "localhost" || strings.HasSuffix(host, ".localhost") || strings.HasSuffix(host, ".internal") {
		return fmt.Errorf("local or internal hosts are not allowed")
	}
	return nil
}

func isDeniedIP(ip net.IP) bool {
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
