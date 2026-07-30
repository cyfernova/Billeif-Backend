package invoicecursor

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/google/uuid"
)

const (
	cursorVersion   = "v1"
	maxCursorLength = 1024
	minimumKeyBytes = 32
)

var ErrInvalidCursor = errors.New("invalid cursor")

type Position struct {
	CreatedAt time.Time
	ID        string
}

type Codec struct {
	key []byte
}

type payload struct {
	BusinessID string `json:"business_id"`
	CreatedAt  string `json:"created_at"`
	ID         string `json:"id"`
}

func NewCodec(key []byte) (*Codec, error) {
	if len(key) < minimumKeyBytes {
		return nil, fmt.Errorf("invoice cursor signing key must be at least %d bytes", minimumKeyBytes)
	}
	return &Codec{key: append([]byte(nil), key...)}, nil
}

func (c *Codec) Encode(businessID string, position Position) (string, error) {
	if c == nil || uuid.Validate(businessID) != nil || uuid.Validate(position.ID) != nil || position.CreatedAt.IsZero() {
		return "", ErrInvalidCursor
	}
	document, err := json.Marshal(payload{
		BusinessID: businessID,
		CreatedAt:  position.CreatedAt.UTC().Format(time.RFC3339Nano),
		ID:         position.ID,
	})
	if err != nil {
		return "", fmt.Errorf("encode invoice cursor: %w", err)
	}
	encodedPayload := base64.RawURLEncoding.EncodeToString(document)
	signingInput := cursorVersion + "." + encodedPayload
	token := signingInput + "." + c.sign(signingInput)
	if len(token) > maxCursorLength {
		return "", ErrInvalidCursor
	}
	return token, nil
}

func (c *Codec) Decode(token, expectedBusinessID string) (Position, error) {
	if c == nil || len(token) == 0 || len(token) > maxCursorLength || uuid.Validate(expectedBusinessID) != nil {
		return Position{}, ErrInvalidCursor
	}
	segments := strings.Split(token, ".")
	if len(segments) != 3 || segments[0] != cursorVersion ||
		segments[1] == "" || segments[2] == "" {
		return Position{}, ErrInvalidCursor
	}
	document, err := base64.RawURLEncoding.Strict().DecodeString(segments[1])
	if err != nil {
		return Position{}, ErrInvalidCursor
	}
	signature, err := base64.RawURLEncoding.Strict().DecodeString(segments[2])
	if err != nil || len(signature) != sha256.Size {
		return Position{}, ErrInvalidCursor
	}
	signingInput := cursorVersion + "." + segments[1]
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write([]byte(signingInput))
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return Position{}, ErrInvalidCursor
	}

	decoder := json.NewDecoder(bytes.NewReader(document))
	decoder.DisallowUnknownFields()
	var decoded payload
	if err := decoder.Decode(&decoded); err != nil {
		return Position{}, ErrInvalidCursor
	}
	if err := rejectTrailingJSON(decoder); err != nil {
		return Position{}, ErrInvalidCursor
	}
	if uuid.Validate(decoded.BusinessID) != nil || uuid.Validate(decoded.ID) != nil ||
		decoded.BusinessID != expectedBusinessID {
		return Position{}, ErrInvalidCursor
	}
	createdAt, err := time.Parse(time.RFC3339Nano, decoded.CreatedAt)
	if err != nil {
		return Position{}, ErrInvalidCursor
	}
	return Position{CreatedAt: createdAt.UTC(), ID: decoded.ID}, nil
}

func (c *Codec) sign(signingInput string) string {
	mac := hmac.New(sha256.New, c.key)
	_, _ = mac.Write([]byte(signingInput))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}

func rejectTrailingJSON(decoder *json.Decoder) error {
	var trailing interface{}
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return ErrInvalidCursor
		}
		return err
	}
	return nil
}
