package idempotency

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"math/big"
	"regexp"
	"sort"
	"strings"
)

var jsonNumberPattern = regexp.MustCompile(`^-?(0|[1-9][0-9]*)(\.[0-9]+)?([eE][+-]?[0-9]+)?$`)

type InvalidPayloadError struct{}

func (e *InvalidPayloadError) Error() string {
	return "invalid canonical payload"
}

func CanonicalHash(value interface{}) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", invalidPayload(err)
	}

	decoder := json.NewDecoder(bytes.NewReader(encoded))
	decoder.UseNumber()
	var normalized interface{}
	if err := decoder.Decode(&normalized); err != nil {
		return "", invalidPayload(err)
	}
	if err := requireJSONEOF(decoder); err != nil {
		return "", invalidPayload(err)
	}

	var canonical bytes.Buffer
	if err := writeCanonicalJSON(&canonical, normalized); err != nil {
		return "", invalidPayload(err)
	}
	digest := sha256.Sum256(canonical.Bytes())
	return hex.EncodeToString(digest[:]), nil
}

func writeCanonicalJSON(dst *bytes.Buffer, value interface{}) error {
	switch typed := value.(type) {
	case nil:
		dst.WriteString("null")
	case bool:
		if typed {
			dst.WriteString("true")
		} else {
			dst.WriteString("false")
		}
	case string:
		encoded, err := json.Marshal(typed)
		if err != nil {
			return err
		}
		dst.Write(encoded)
	case json.Number:
		normalized, err := normalizeJSONNumber(string(typed))
		if err != nil {
			return err
		}
		dst.WriteString(normalized)
	case []interface{}:
		dst.WriteByte('[')
		for index, item := range typed {
			if index > 0 {
				dst.WriteByte(',')
			}
			if err := writeCanonicalJSON(dst, item); err != nil {
				return err
			}
		}
		dst.WriteByte(']')
	case map[string]interface{}:
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		dst.WriteByte('{')
		for index, key := range keys {
			if index > 0 {
				dst.WriteByte(',')
			}
			encodedKey, err := json.Marshal(key)
			if err != nil {
				return err
			}
			dst.Write(encodedKey)
			dst.WriteByte(':')
			if err := writeCanonicalJSON(dst, typed[key]); err != nil {
				return err
			}
		}
		dst.WriteByte('}')
	default:
		return errors.New("unsupported canonical JSON value")
	}
	return nil
}

func normalizeJSONNumber(raw string) (string, error) {
	if !jsonNumberPattern.MatchString(raw) {
		return "", errors.New("invalid JSON number")
	}

	negative := strings.HasPrefix(raw, "-")
	unsigned := strings.TrimPrefix(raw, "-")
	exponentText := "0"
	if exponentIndex := strings.IndexAny(unsigned, "eE"); exponentIndex >= 0 {
		exponentText = unsigned[exponentIndex+1:]
		unsigned = unsigned[:exponentIndex]
	}

	integerPart := unsigned
	fractionPart := ""
	if decimalIndex := strings.IndexByte(unsigned, '.'); decimalIndex >= 0 {
		integerPart = unsigned[:decimalIndex]
		fractionPart = unsigned[decimalIndex+1:]
	}

	digits := strings.TrimLeft(integerPart+fractionPart, "0")
	if digits == "" {
		return "0", nil
	}
	trailingZeros := len(digits) - len(strings.TrimRight(digits, "0"))
	digits = strings.TrimRight(digits, "0")

	exponent := new(big.Int)
	if _, ok := exponent.SetString(exponentText, 10); !ok {
		return "", errors.New("invalid JSON exponent")
	}
	exponent.Sub(exponent, big.NewInt(int64(len(fractionPart))))
	exponent.Add(exponent, big.NewInt(int64(trailingZeros)))
	exponent.Add(exponent, big.NewInt(int64(len(digits)-1)))

	var normalized strings.Builder
	if negative {
		normalized.WriteByte('-')
	}
	normalized.WriteByte(digits[0])
	if len(digits) > 1 {
		normalized.WriteByte('.')
		normalized.WriteString(digits[1:])
	}
	if exponent.Sign() != 0 {
		normalized.WriteByte('e')
		normalized.WriteString(exponent.String())
	}
	return normalized.String(), nil
}

func requireJSONEOF(decoder *json.Decoder) error {
	var trailing interface{}
	err := decoder.Decode(&trailing)
	if errors.Is(err, io.EOF) {
		return nil
	}
	if err == nil {
		return errors.New("multiple JSON values")
	}
	return err
}

func invalidPayload(_ error) error {
	return &InvalidPayloadError{}
}
