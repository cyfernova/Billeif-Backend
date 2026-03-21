package a2a

import (
	"context"
	"errors"
	"net/http"
)

var (
	ErrInvalidTaskState     = errors.New("invalid task state")
	ErrTaskStateTerminal    = errors.New("cannot transition from terminal task state")
	ErrTaskNotFound         = errors.New("task not found")
	ErrUnauthorized         = errors.New("unauthorized")
	ErrUnsupportedVersion   = errors.New("unsupported A2A version")
	ErrInvalidMessage       = errors.New("invalid message")
	ErrInvalidPageToken     = errors.New("invalid page token")
	ErrPushConfigNotFound   = errors.New("push notification config not found")
	ErrPushConfigInvalid    = errors.New("invalid push notification configuration")
	ErrUnsupportedOperation = errors.New("unsupported operation")
	ErrMissingAuthorization = errors.New("missing authorization header")
	ErrMissingVersionHeader = errors.New("missing A2A-Version header")
)

const (
	ProblemTypeInvalidRequest       = "https://a2a-protocol.org/errors/invalid-request"
	ProblemTypeUnauthorized         = "https://a2a-protocol.org/errors/unauthorized"
	ProblemTypeTaskNotFound         = "https://a2a-protocol.org/errors/task-not-found"
	ProblemTypeVersionNotSupported  = "https://a2a-protocol.org/errors/version-not-supported"
	ProblemTypeUnsupportedOperation = "https://a2a-protocol.org/errors/unsupported-operation"
	ProblemTypeInternal             = "https://a2a-protocol.org/errors/internal-error"
)

type Problem struct {
	Type              string   `json:"type"`
	Title             string   `json:"title"`
	Status            int      `json:"status"`
	Detail            string   `json:"detail,omitempty"`
	SupportedVersions []string `json:"supportedVersions,omitempty"`
}

func NewProblem(status int, problemType, title, detail string) Problem {
	return Problem{
		Type:   problemType,
		Title:  title,
		Status: status,
		Detail: detail,
	}
}

func VersionNotSupportedProblem(requested string) Problem {
	return Problem{
		Type:              ProblemTypeVersionNotSupported,
		Title:             "Protocol Version Not Supported",
		Status:            http.StatusBadRequest,
		Detail:            "The requested A2A protocol version " + requested + " is not supported by this agent",
		SupportedVersions: []string{SupportedVersion},
	}
}

type ctxKey string

const authorizationHeaderContextKey ctxKey = "a2a.authorization_header"

func WithAuthorizationHeader(ctx context.Context, header string) context.Context {
	return context.WithValue(ctx, authorizationHeaderContextKey, header)
}

func AuthorizationHeaderFromContext(ctx context.Context) (string, bool) {
	header, ok := ctx.Value(authorizationHeaderContextKey).(string)
	return header, ok && header != ""
}
