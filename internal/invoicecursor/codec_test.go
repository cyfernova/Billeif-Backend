package invoicecursor

import (
	"encoding/base64"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestCodecRoundTripUsesTenantBoundUTCPosition(t *testing.T) {
	codec, err := NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	businessID, invoiceID := uuid.NewString(), uuid.NewString()
	createdAt := time.Date(2026, time.July, 30, 9, 10, 11, 123456789, time.FixedZone("IST", 5*60*60+30*60))

	token, err := codec.Encode(businessID, Position{CreatedAt: createdAt, ID: invoiceID})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	position, err := codec.Decode(token, businessID)
	if err != nil {
		t.Fatalf("Decode() error = %v", err)
	}
	if position.ID != invoiceID || !position.CreatedAt.Equal(createdAt) || position.CreatedAt.Location() != time.UTC {
		t.Fatalf("decoded position = %#v, want invoice and UTC timestamp", position)
	}
	if strings.Contains(token, "=") || !strings.HasPrefix(token, "v1.") {
		t.Fatalf("token = %q, want v1 raw URL-safe encoding", token)
	}
}

func TestCodecRejectsShortSigningKey(t *testing.T) {
	if _, err := NewCodec([]byte("too-short")); err == nil {
		t.Fatal("NewCodec() accepted a key shorter than 32 bytes")
	}
}

func TestCodecRejectsTamperedMalformedAndCrossTenantTokens(t *testing.T) {
	codec, err := NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	businessID := uuid.NewString()
	token, err := codec.Encode(businessID, Position{
		CreatedAt: time.Date(2026, time.July, 30, 1, 2, 3, 4, time.UTC),
		ID:        uuid.NewString(),
	})
	if err != nil {
		t.Fatalf("Encode() error = %v", err)
	}
	parts := strings.Split(token, ".")
	tamperedPayload := append([]byte(nil), parts[1]...)
	if tamperedPayload[len(tamperedPayload)-1] == 'A' {
		tamperedPayload[len(tamperedPayload)-1] = 'B'
	} else {
		tamperedPayload[len(tamperedPayload)-1] = 'A'
	}
	tamperedSignature := append([]byte(nil), parts[2]...)
	if tamperedSignature[len(tamperedSignature)-1] == 'A' {
		tamperedSignature[len(tamperedSignature)-1] = 'B'
	} else {
		tamperedSignature[len(tamperedSignature)-1] = 'A'
	}

	tests := []struct {
		name  string
		token string
		scope string
	}{
		{name: "tampered payload", token: "v1." + string(tamperedPayload) + "." + parts[2], scope: businessID},
		{name: "tampered signature", token: "v1." + parts[1] + "." + string(tamperedSignature), scope: businessID},
		{name: "wrong version", token: "v2." + parts[1] + "." + parts[2], scope: businessID},
		{name: "padded base64", token: "v1." + parts[1] + "=." + parts[2], scope: businessID},
		{name: "empty segment", token: "v1.." + parts[2], scope: businessID},
		{name: "extra segment", token: token + ".extra", scope: businessID},
		{name: "too long", token: strings.Repeat("x", 1025), scope: businessID},
		{name: "cross tenant", token: token, scope: uuid.NewString()},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := codec.Decode(test.token, test.scope)
			if !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("Decode() error = %v, want ErrInvalidCursor", err)
			}
		})
	}
}

func TestCodecRejectsUnknownTrailingAndInvalidPayloadFields(t *testing.T) {
	codec, err := NewCodec([]byte("0123456789abcdef0123456789abcdef"))
	if err != nil {
		t.Fatalf("NewCodec() error = %v", err)
	}
	businessID := uuid.NewString()
	tests := []struct {
		name    string
		payload string
	}{
		{name: "unknown field", payload: `{"business_id":"` + businessID + `","created_at":"2026-07-30T01:02:03Z","id":"` + uuid.NewString() + `","extra":true}`},
		{name: "trailing JSON", payload: `{"business_id":"` + businessID + `","created_at":"2026-07-30T01:02:03Z","id":"` + uuid.NewString() + `"} {}`},
		{name: "invalid business UUID", payload: `{"business_id":"not-a-uuid","created_at":"2026-07-30T01:02:03Z","id":"` + uuid.NewString() + `"}`},
		{name: "invalid invoice UUID", payload: `{"business_id":"` + businessID + `","created_at":"2026-07-30T01:02:03Z","id":"not-a-uuid"}`},
		{name: "invalid timestamp", payload: `{"business_id":"` + businessID + `","created_at":"yesterday","id":"` + uuid.NewString() + `"}`},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			payload := base64.RawURLEncoding.EncodeToString([]byte(test.payload))
			token := "v1." + payload + "." + codec.sign("v1."+payload)
			if _, err := codec.Decode(token, businessID); !errors.Is(err, ErrInvalidCursor) {
				t.Fatalf("Decode() error = %v, want ErrInvalidCursor", err)
			}
		})
	}
}
