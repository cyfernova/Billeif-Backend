package ap2

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

var (
	ErrCanonicalizeJSON = errors.New("JSON canonicalization failed")
)

// CanonicalizeJSON implements RFC 8785 JSON Canonicalization Scheme (JCS).
// This ensures deterministic byte-for-byte identical JSON representation for signature verification.
//
// RFC 8785 rules:
// 1. Whitespace is removed
// 2. Object keys are lexicographically sorted
// 3. Unicode characters are escaped minimally
// 4. Numbers are serialized without unnecessary characters
func CanonicalizeJSON(data interface{}) ([]byte, error) {
	// First marshal to JSON
	jsonBytes, err := json.Marshal(data)
	if err != nil {
		return nil, fmt.Errorf("%w: marshal failed: %v", ErrCanonicalizeJSON, err)
	}

	// Parse the JSON to work with it
	var obj interface{}
	decoder := json.NewDecoder(strings.NewReader(string(jsonBytes)))
	decoder.UseNumber() // Use Number to preserve numeric precision
	if err := decoder.Decode(&obj); err != nil {
		return nil, fmt.Errorf("%w: decode failed: %v", ErrCanonicalizeJSON, err)
	}

	// Canonicalize the object
	canonical := canonicalizeValue(obj)

	// Marshal to JSON with minimal output
	result, err := json.Marshal(canonical)
	if err != nil {
		return nil, fmt.Errorf("%w: remarshal failed: %v", ErrCanonicalizeJSON, err)
	}

	return result, nil
}

// canonicalizeValue recursively canonicalizes a JSON value according to RFC 8785
func canonicalizeValue(value interface{}) interface{} {
	switch v := value.(type) {
	case map[string]interface{}:
		// Sort object keys lexicographically
		return canonicalizeObject(v)
	case []interface{}:
		// Recursively canonicalize array elements
		return canonicalizeArray(v)
	case json.Number:
		// Handle numbers carefully to preserve precision
		return canonicalizeNumber(v)
	case string:
		// Strings don't need special canonicalization
		return v
	case bool, float64, nil:
		return v
	default:
		return v
	}
}

// canonicalizeObject sorts object keys lexicographically and canonicalizes values
func canonicalizeObject(obj map[string]interface{}) map[string]interface{} {
	// Create sorted keys
	keys := make([]string, 0, len(obj))
	for k := range obj {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	// Create new object with sorted keys
	result := make(map[string]interface{})
	for _, k := range keys {
		result[k] = canonicalizeValue(obj[k])
	}

	return result
}

// canonicalizeArray recursively canonicalizes array elements
func canonicalizeArray(arr []interface{}) []interface{} {
	result := make([]interface{}, len(arr))
	for i, v := range arr {
		result[i] = canonicalizeValue(v)
	}
	return result
}

// canonicalizeNumber handles RFC 8785 number canonicalization
func canonicalizeNumber(n json.Number) interface{} {
	// Try to parse as float64 to normalize
	f, err := n.Float64()
	if err != nil {
		// If it fails, try integer parsing
		if strings.Contains(n.String(), ".") || strings.Contains(n.String(), "e") || strings.Contains(n.String(), "E") {
			// It looks like a float but failed to parse, return as-is
			return n.String()
		}
		// Try as integer
		_, err := n.Int64()
		if err != nil {
			return n.String()
		}
		return n.String()
	}

	// RFC 8785: Use minimal representation
	// Check if it's an integer
	if f == float64(int64(f)) && f >= -9007199254740992 && f <= 9007199254740992 {
		// Return as integer
		return int64(f)
	}

	// Return as float64 (JSON marshaler will handle minimal representation)
	return f
}

// MandateCanonicalData prepares mandate data for signing in RFC 8785 format
func MandateCanonicalData(mandateData map[string]interface{}) ([]byte, error) {
	// Remove signature field if present (signatures are never included in canonicalization)
	dataCopy := make(map[string]interface{})
	for k, v := range mandateData {
		if k != "signature" {
			dataCopy[k] = v
		}
	}

	return CanonicalizeJSON(dataCopy)
}

// IntentMandateCanonicalData creates canonical representation of intent mandate
func IntentMandateCanonicalData(userID, agentID, intent string, constraints interface{}, expiresAt int64) ([]byte, error) {
	data := map[string]interface{}{
		"user_id":                 userID,
		"agent_id":                agentID,
		"constraints":             constraints,
		"natural_language_intent": intent,
		"expires_at":              expiresAt,
	}
	return CanonicalizeJSON(data)
}

// CartMandateCanonicalData creates canonical representation of cart mandate
func CartMandateCanonicalData(userID, agentID string, items []interface{}, totalAmount float64, expiresAt int64) ([]byte, error) {
	data := map[string]interface{}{
		"user_id":      userID,
		"agent_id":     agentID,
		"items":        items,
		"total_amount": totalAmount,
		"expires_at":   expiresAt,
	}
	return CanonicalizeJSON(data)
}

// PaymentMandateCanonicalData creates canonical representation of payment mandate
func PaymentMandateCanonicalData(userID, cartMandateID string, amount float64) ([]byte, error) {
	data := map[string]interface{}{
		"user_id":         userID,
		"cart_mandate_id": cartMandateID,
		"amount":          amount,
	}
	return CanonicalizeJSON(data)
}

// EscapeString applies RFC 8785 string escaping rules
// This is called by json.Marshal, so we don't need to implement it separately
// but it's here for reference and potential custom encoding
func escapeString(s string) string {
	result := strings.Builder{}
	for _, r := range s {
		switch r {
		case '"':
			result.WriteString(`\"`)
		case '\\':
			result.WriteString(`\\`)
		case '\b':
			result.WriteString(`\b`)
		case '\f':
			result.WriteString(`\f`)
		case '\n':
			result.WriteString(`\n`)
		case '\r':
			result.WriteString(`\r`)
		case '\t':
			result.WriteString(`\t`)
		default:
			if r < 0x20 || (r >= 0x7F && r <= 0x9F) {
				// Control characters must be escaped
				fmt.Fprintf(&result, `\u%04x`, r)
			} else if !utf8.ValidRune(r) {
				// Invalid Unicode
				result.WriteString(`\ufffd`)
			} else {
				result.WriteRune(r)
			}
		}
	}
	return result.String()
}
