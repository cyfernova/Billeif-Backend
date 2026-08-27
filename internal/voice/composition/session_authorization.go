package composition

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"errors"
	"io"
	"mime"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	voicesession "invoice-backend/internal/voice/session"
	"invoice-backend/internal/voice/webrtc"

	"github.com/google/uuid"
)

const (
	defaultSessionAuthorizationTimeout = 3 * time.Second
	maximumSessionAuthorizationTimeout = 5 * time.Second
	maxCurrentSessionResponseBytes     = 8 << 10
	maxCurrentSessionHeaderBytes       = 16 << 10
)

// HTTPSessionAuthorizerConfig describes the authenticated Billeif API
// boundary used to recheck current session access. Transport is injectable for
// deterministic tests; production leaves it nil and receives a proxy-free,
// TLS-pinned transport owned by the authorizer.
type HTTPSessionAuthorizerConfig struct {
	Origin    string
	Timeout   time.Duration
	Transport http.RoundTripper
}

// HTTPSessionAuthorizer asks the existing authenticated voice-session
// endpoint to derive the caller's current branch access. It retains no bearer
// value or response body between calls.
type HTTPSessionAuthorizer struct {
	client         *http.Client
	ownedTransport *http.Transport
	origin         string
	timeout        time.Duration
	rootContext    context.Context
	cancel         context.CancelFunc
	closed         atomic.Bool
	closeOnce      sync.Once
}

func NewHTTPSessionAuthorizer(config HTTPSessionAuthorizerConfig) (*HTTPSessionAuthorizer, error) {
	origin, host, err := validateSessionAuthorizationOrigin(config.Origin)
	if err != nil {
		return nil, ErrInvalidFactoryConfig
	}
	timeout := config.Timeout
	if timeout == 0 {
		timeout = defaultSessionAuthorizationTimeout
	}
	if timeout < 0 || timeout > maximumSessionAuthorizationTimeout {
		return nil, ErrInvalidFactoryConfig
	}

	transport := config.Transport
	if nilInterface(transport) {
		if transport != nil {
			return nil, ErrInvalidFactoryConfig
		}
		networkDialer := &net.Dialer{Timeout: timeout, KeepAlive: 30 * time.Second}
		productionTransport := &http.Transport{
			Proxy:                  nil,
			DialContext:            networkDialer.DialContext,
			ForceAttemptHTTP2:      true,
			DisableCompression:     true,
			MaxIdleConns:           4,
			MaxIdleConnsPerHost:    4,
			MaxConnsPerHost:        8,
			IdleConnTimeout:        30 * time.Second,
			TLSHandshakeTimeout:    timeout,
			ResponseHeaderTimeout:  timeout,
			ExpectContinueTimeout:  time.Second,
			MaxResponseHeaderBytes: maxCurrentSessionHeaderBytes,
			TLSClientConfig: &tls.Config{
				MinVersion: tls.VersionTLS12,
				ServerName: host,
			},
		}
		transport = productionTransport
		rootContext, cancel := context.WithCancel(context.Background())
		return &HTTPSessionAuthorizer{
			client: &http.Client{
				Transport: transport,
				Timeout:   timeout,
				CheckRedirect: func(*http.Request, []*http.Request) error {
					return http.ErrUseLastResponse
				},
			},
			ownedTransport: productionTransport,
			origin:         origin,
			timeout:        timeout,
			rootContext:    rootContext,
			cancel:         cancel,
		}, nil
	}

	rootContext, cancel := context.WithCancel(context.Background())
	return &HTTPSessionAuthorizer{
		client: &http.Client{
			Transport: transport,
			Timeout:   timeout,
			CheckRedirect: func(*http.Request, []*http.Request) error {
				return http.ErrUseLastResponse
			},
		},
		origin: origin, timeout: timeout, rootContext: rootContext, cancel: cancel,
	}, nil
}

func (authorizer *HTTPSessionAuthorizer) Authorize(
	ctx context.Context,
	authorization string,
	sessionID string,
) (webrtc.CurrentSessionAuthorization, error) {
	if ctx == nil {
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnavailable
	}
	if err := ctx.Err(); err != nil {
		return webrtc.CurrentSessionAuthorization{}, err
	}
	if authorizer == nil || authorizer.closed.Load() || authorizer.client == nil || authorizer.rootContext == nil {
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnavailable
	}
	if !validForwardedAuthorization(authorization) || !validCurrentSessionID(sessionID) {
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnauthorized
	}

	callContext, cancel := context.WithTimeout(ctx, authorizer.timeout)
	stopCloseCancellation := context.AfterFunc(authorizer.rootContext, cancel)
	defer func() {
		stopCloseCancellation()
		cancel()
	}()
	select {
	case <-authorizer.rootContext.Done():
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnavailable
	default:
	}

	request, err := http.NewRequestWithContext(
		callContext,
		http.MethodGet,
		authorizer.origin+"/api/v1/voice/sessions/"+url.PathEscape(sessionID),
		nil,
	)
	if err != nil {
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnavailable
	}
	request.Header.Set("Accept", "application/json")
	request.Header.Set("Accept-Encoding", "identity")
	request.Header.Set("Authorization", authorization)
	defer request.Header.Del("Authorization")
	response, err := authorizer.client.Do(request)
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return webrtc.CurrentSessionAuthorization{}, contextErr
		}
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnavailable
	}
	defer response.Body.Close()

	switch response.StatusCode {
	case http.StatusOK:
	case http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnauthorized
	default:
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnavailable
	}
	mediaType, _, err := mime.ParseMediaType(response.Header.Get("Content-Type"))
	if err != nil || mediaType != "application/json" || response.Header.Get("Content-Encoding") != "" {
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnavailable
	}
	body, err := io.ReadAll(io.LimitReader(response.Body, maxCurrentSessionResponseBytes+1))
	if err != nil {
		if contextErr := ctx.Err(); contextErr != nil {
			return webrtc.CurrentSessionAuthorization{}, contextErr
		}
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnavailable
	}
	if len(body) == 0 || len(body) > maxCurrentSessionResponseBytes || !utf8.Valid(body) ||
		rejectDuplicateSessionAuthorizationFields(body) != nil || validateSessionAuthorizationFields(body) != nil {
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnavailable
	}

	decoder := json.NewDecoder(bytes.NewReader(body))
	var value voicesession.SessionResponse
	if err := decoder.Decode(&value); err != nil {
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnavailable
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnavailable
	}
	if value.SessionID != sessionID || !validCurrentRuntimeSessionID(value.RuntimeSessionID) || !validCurrentBranchID(value.BranchID) {
		return webrtc.CurrentSessionAuthorization{}, webrtc.ErrCurrentSessionUnavailable
	}
	return webrtc.CurrentSessionAuthorization{BranchID: value.BranchID}, nil
}

func (authorizer *HTTPSessionAuthorizer) Close() error {
	if authorizer == nil {
		return nil
	}
	authorizer.closeOnce.Do(func() {
		authorizer.closed.Store(true)
		if authorizer.cancel != nil {
			authorizer.cancel()
		}
		if authorizer.ownedTransport != nil {
			authorizer.ownedTransport.CloseIdleConnections()
		}
	})
	return nil
}

func (*HTTPSessionAuthorizer) String() string   { return "voice current session authorizer{redacted}" }
func (*HTTPSessionAuthorizer) GoString() string { return "voice current session authorizer{redacted}" }
func (*HTTPSessionAuthorizer) MarshalJSON() ([]byte, error) {
	return json.Marshal(struct{}{})
}

func validateSessionAuthorizationOrigin(raw string) (origin, host string, resultErr error) {
	if raw == "" || strings.TrimSpace(raw) != raw || !utf8.ValidString(raw) {
		return "", "", ErrInvalidFactoryConfig
	}
	parsed, err := url.Parse(raw)
	if err != nil || parsed.Scheme != "https" || parsed.Opaque != "" || parsed.User != nil || parsed.Host == "" ||
		parsed.RawQuery != "" || parsed.Fragment != "" || parsed.RawPath != "" {
		return "", "", ErrInvalidFactoryConfig
	}
	host = strings.ToLower(parsed.Hostname())
	if host == "" || strings.TrimSuffix(host, ".") != host || strings.ContainsAny(host, "\x00\r\n\t /\\") {
		return "", "", ErrInvalidFactoryConfig
	}
	if parsed.Path != "" && parsed.Path != "/" {
		for _, segment := range strings.Split(strings.Trim(strings.TrimSpace(parsed.Path), "/"), "/") {
			if segment == "" || segment == "." || segment == ".." {
				return "", "", ErrInvalidFactoryConfig
			}
		}
	}
	parsed.Scheme = "https"
	parsed.Host = strings.ToLower(parsed.Host)
	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	return strings.TrimSuffix(parsed.String(), "/"), host, nil
}

func validCurrentSessionID(value string) bool {
	if len(value) <= len("voice_") || len(value) > 96 || !strings.HasPrefix(value, "voice_") || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e || character == '/' || character == '\\' || character == '?' || character == '#' {
			return false
		}
	}
	return true
}

func validCurrentRuntimeSessionID(value string) bool {
	if len(value) < 33 || len(value) > 256 || !strings.HasPrefix(value, "voice-session-") || strings.TrimSpace(value) != value {
		return false
	}
	for _, character := range value {
		if character < 0x21 || character > 0x7e {
			return false
		}
	}
	return true
}

func validCurrentBranchID(value string) bool {
	if value == "" {
		return true
	}
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func validateSessionAuthorizationFields(body []byte) error {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(body, &fields); err != nil || fields == nil {
		return webrtc.ErrCurrentSessionUnavailable
	}
	for key := range fields {
		switch strings.ToLower(key) {
		case "session_id", "runtime_session_id", "branch_id":
			if key != strings.ToLower(key) {
				return webrtc.ErrCurrentSessionUnavailable
			}
		}
	}
	if _, present := fields["session_id"]; !present {
		return webrtc.ErrCurrentSessionUnavailable
	}
	if _, present := fields["runtime_session_id"]; !present {
		return webrtc.ErrCurrentSessionUnavailable
	}
	if rawBranch, present := fields["branch_id"]; present {
		var branchID string
		if bytes.Equal(bytes.TrimSpace(rawBranch), []byte("null")) || json.Unmarshal(rawBranch, &branchID) != nil {
			return webrtc.ErrCurrentSessionUnavailable
		}
	}
	return nil
}

func rejectDuplicateSessionAuthorizationFields(body []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(body))
	decoder.UseNumber()
	if err := consumeUniqueSessionAuthorizationJSON(decoder, 0); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return webrtc.ErrCurrentSessionUnavailable
	}
	return nil
}

func consumeUniqueSessionAuthorizationJSON(decoder *json.Decoder, depth int) error {
	if depth > 16 {
		return webrtc.ErrCurrentSessionUnavailable
	}
	token, err := decoder.Token()
	if err != nil {
		return webrtc.ErrCurrentSessionUnavailable
	}
	delimiter, isDelimiter := token.(json.Delim)
	if !isDelimiter {
		return nil
	}
	switch delimiter {
	case '{':
		seen := make(map[string]struct{})
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return webrtc.ErrCurrentSessionUnavailable
			}
			key, ok := keyToken.(string)
			if !ok {
				return webrtc.ErrCurrentSessionUnavailable
			}
			if _, duplicate := seen[key]; duplicate {
				return webrtc.ErrCurrentSessionUnavailable
			}
			seen[key] = struct{}{}
			if err := consumeUniqueSessionAuthorizationJSON(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return webrtc.ErrCurrentSessionUnavailable
		}
		return nil
	case '[':
		for decoder.More() {
			if err := consumeUniqueSessionAuthorizationJSON(decoder, depth+1); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return webrtc.ErrCurrentSessionUnavailable
		}
		return nil
	default:
		return webrtc.ErrCurrentSessionUnavailable
	}
}

var _ webrtc.CurrentSessionAuthorizer = (*HTTPSessionAuthorizer)(nil)
var _ io.Closer = (*HTTPSessionAuthorizer)(nil)
