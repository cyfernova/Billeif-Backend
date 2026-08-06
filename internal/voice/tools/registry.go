package tools

import (
	"context"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"invoice-backend/internal/providers/sarvam"

	"github.com/google/uuid"
)

var (
	ErrAuthorizationSourceRequired = errors.New("voice tool authorization source is required")
	ErrInvalidSessionScope         = errors.New("voice tool session scope is invalid")
	ErrSessionBindingClosed        = errors.New("voice tool session binding is closed")
	ErrInvalidConfiguration        = errors.New("voice tool registry configuration is invalid")
	ErrRegistryClosed              = errors.New("voice tool registry is closed")
	ErrUnknownTool                 = errors.New("voice tool is not available")
	ErrInvalidToolArguments        = errors.New("voice tool arguments are invalid")
	ErrBranchScopeUnsupported      = errors.New("voice tools cannot verify branch-scoped data")
	ErrAuthorizationUnavailable    = errors.New("voice tool authorization is unavailable")
	ErrToolUnavailable             = errors.New("voice tool service is unavailable")
	ErrToolNotFound                = errors.New("voice tool resource was not found")
	ErrInvalidToolResponse         = errors.New("voice tool service returned an invalid response")
	ErrToolResponseTooLarge        = errors.New("voice tool service response exceeds the allowed size")
)

// AuthorizationRefreshRequiredError tells the turn orchestrator to pause tool
// execution while the signaling layer refreshes the user's credential.
type AuthorizationRefreshRequiredError struct{}

func (*AuthorizationRefreshRequiredError) Error() string {
	return "voice tool authorization refresh is required"
}

func (*AuthorizationRefreshRequiredError) AuthorizationRefreshRequired() bool { return true }

func (*AuthorizationRefreshRequiredError) Is(target error) bool {
	_, ok := target.(*AuthorizationRefreshRequiredError)
	return ok
}

var ErrAuthorizationRefreshRequired error = &AuthorizationRefreshRequiredError{}

// AuthorizationSource supplies the latest validated user Authorization value.
// Implementations are owned by the signaling/session layer, not by Registry.
type AuthorizationSource interface {
	Authorization(context.Context) (string, error)
}

// Resolver and Dialer are paired transport dependencies. When injected, the
// registry never falls back to the process resolver or network dialer.
type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}

type Dialer interface {
	DialContext(context.Context, string, string) (net.Conn, error)
}

type Config struct {
	Origin              string
	Timeout             time.Duration
	Resolver            Resolver
	Dialer              Dialer
	RootCAs             *x509.CertPool
	EnableCustomerTools bool
}

// SessionBinding binds a registry to one already-validated business/branch
// scope without exposing the Authorization source to the turn orchestrator.
type SessionBinding struct {
	mu sync.RWMutex

	authorization AuthorizationSource
	businessID    string
	branchID      string
	closed        bool
	closeOnce     sync.Once
}

func NewSessionBinding(source AuthorizationSource, businessID, branchID string) (*SessionBinding, error) {
	if nilInterface(source) {
		return nil, ErrAuthorizationSourceRequired
	}
	if !canonicalUUID(businessID) || (branchID != "" && !canonicalUUID(branchID)) {
		return nil, ErrInvalidSessionScope
	}
	return &SessionBinding{authorization: source, businessID: businessID, branchID: branchID}, nil
}

func (*SessionBinding) String() string { return "SessionBinding{redacted}" }

func (binding *SessionBinding) GoString() string { return binding.String() }

func (*SessionBinding) MarshalJSON() ([]byte, error) { return []byte("{}"), nil }

func (binding *SessionBinding) Close() error {
	if binding == nil {
		return nil
	}
	binding.closeOnce.Do(func() {
		binding.mu.Lock()
		binding.closed = true
		binding.authorization = nil
		binding.businessID = ""
		binding.branchID = ""
		binding.mu.Unlock()
	})
	return nil
}

func (binding *SessionBinding) snapshot() (AuthorizationSource, string, string, error) {
	if binding == nil {
		return nil, "", "", ErrSessionBindingClosed
	}
	binding.mu.RLock()
	defer binding.mu.RUnlock()
	if binding.closed || nilInterface(binding.authorization) {
		return nil, "", "", ErrSessionBindingClosed
	}
	return binding.authorization, binding.businessID, binding.branchID, nil
}

type Registry struct {
	binding             *SessionBinding
	client              *http.Client
	transport           *http.Transport
	origin              string
	timeout             time.Duration
	enableCustomerTools bool
	rootContext         context.Context
	cancel              context.CancelFunc
	closed              atomic.Bool
	close               sync.Once
}

func NewRegistry(config Config, binding *SessionBinding) (*Registry, error) {
	if binding == nil {
		return nil, ErrSessionBindingClosed
	}
	if _, _, _, err := binding.snapshot(); err != nil {
		return nil, err
	}
	client, transport, origin, timeout, err := newHTTPClient(config)
	if err != nil {
		return nil, err
	}
	rootContext, cancel := context.WithCancel(context.Background())
	return &Registry{
		binding:             binding,
		client:              client,
		transport:           transport,
		origin:              origin,
		timeout:             timeout,
		enableCustomerTools: config.EnableCustomerTools,
		rootContext:         rootContext,
		cancel:              cancel,
	}, nil
}

func (registry *Registry) Definitions() []sarvam.ChatToolDefinition {
	if registry == nil {
		return nil
	}
	definitions := make([]sarvam.ChatToolDefinition, 0, len(registryDefinitions))
	for _, definition := range registryDefinitions {
		if !registry.enableCustomerTools && isCustomerTool(definition.Name) {
			continue
		}
		definition.Parameters = append(json.RawMessage(nil), definition.Parameters...)
		definitions = append(definitions, definition)
	}
	return definitions
}

func (registry *Registry) Close() error {
	if registry == nil {
		return nil
	}
	registry.close.Do(func() {
		registry.closed.Store(true)
		registry.cancel()
		registry.transport.CloseIdleConnections()
		_ = registry.binding.Close()
	})
	return nil
}

func isCustomerTool(name string) bool {
	return name == "list_customers" || name == "get_customer"
}

var registryDefinitions = [...]sarvam.ChatToolDefinition{
	{
		Name:        "list_invoices",
		Description: "List invoices for the current business using bounded cursor pagination.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"limit":{"type":"integer","minimum":1,"maximum":20,"default":10},"cursor":{"type":"string","minLength":1,"maxLength":1024,"pattern":"^v1\\.[A-Za-z0-9_-]+\\.[A-Za-z0-9_-]+$"}},"additionalProperties":false}`),
	},
	{
		Name:        "get_invoice",
		Description: "Get one invoice in the current business by its canonical UUID.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","format":"uuid"}},"required":["id"],"additionalProperties":false}`),
	},
	{
		Name:        "list_customers",
		Description: "List customers for the current business using bounded page pagination.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"page":{"type":"integer","minimum":1,"maximum":1000,"default":1},"limit":{"type":"integer","minimum":1,"maximum":20,"default":10}},"additionalProperties":false}`),
	},
	{
		Name:        "get_customer",
		Description: "Get one customer in the current business by its canonical UUID.",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"id":{"type":"string","format":"uuid"}},"required":["id"],"additionalProperties":false}`),
	},
}

func canonicalUUID(value string) bool {
	parsed, err := uuid.Parse(value)
	return err == nil && parsed.String() == value
}

func nilInterface(value any) bool {
	if value == nil {
		return true
	}
	reflected := reflect.ValueOf(value)
	switch reflected.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return reflected.IsNil()
	default:
		return false
	}
}

var _ fmt.Stringer = (*SessionBinding)(nil)
