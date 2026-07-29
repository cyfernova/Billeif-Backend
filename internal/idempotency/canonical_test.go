package idempotency

import (
	"encoding/json"
	"errors"
	"math"
	"testing"
)

func TestCanonicalHashNormalizesObjectOrderAndJSONNumbers(t *testing.T) {
	first := map[string]interface{}{
		"nested": map[string]interface{}{"b": json.Number("1.00"), "a": "value"},
		"amount": json.Number("100"),
	}
	second := map[string]interface{}{
		"amount": json.Number("1e2"),
		"nested": map[string]interface{}{"a": "value", "b": json.Number("1")},
	}

	firstHash, err := CanonicalHash(first)
	if err != nil {
		t.Fatalf("hash first payload: %v", err)
	}
	secondHash, err := CanonicalHash(second)
	if err != nil {
		t.Fatalf("hash second payload: %v", err)
	}
	if firstHash != secondHash {
		t.Fatalf("equivalent payload hashes differ: %s != %s", firstHash, secondHash)
	}
}

func TestCanonicalHashPreservesArrayOrderAndNullVersusAbsence(t *testing.T) {
	fixtures := []struct {
		name  string
		left  interface{}
		right interface{}
	}{
		{
			name:  "array order",
			left:  map[string]interface{}{"items": []interface{}{"a", "b"}},
			right: map[string]interface{}{"items": []interface{}{"b", "a"}},
		},
		{
			name:  "null and absent",
			left:  map[string]interface{}{"optional": nil},
			right: map[string]interface{}{},
		},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			leftHash, err := CanonicalHash(fixture.left)
			if err != nil {
				t.Fatalf("hash left payload: %v", err)
			}
			rightHash, err := CanonicalHash(fixture.right)
			if err != nil {
				t.Fatalf("hash right payload: %v", err)
			}
			if leftHash == rightHash {
				t.Fatalf("meaningfully different payloads share hash %s", leftHash)
			}
		})
	}
}

func TestCanonicalHashHasStableSHA256Fixture(t *testing.T) {
	hash, err := CanonicalHash(map[string]interface{}{
		"b": []interface{}{true, nil, "x"},
		"a": json.Number("1.0"),
	})
	if err != nil {
		t.Fatalf("hash payload: %v", err)
	}

	const want = "eca8cfb31ab74533e1eb2f4c74d2d55dfe3c79ac704787e54be8647ea7777eb1"
	if hash != want {
		t.Fatalf("hash = %q, want %q", hash, want)
	}
}

func TestCanonicalHashRejectsUnsupportedAndNonFiniteValuesWithoutPayloadLeak(t *testing.T) {
	fixtures := []struct {
		name  string
		value interface{}
	}{
		{name: "function", value: func() {}},
		{name: "positive infinity", value: math.Inf(1)},
		{name: "not a number", value: math.NaN()},
		{name: "invalid json number", value: json.Number("private-token")},
	}

	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			_, err := CanonicalHash(fixture.value)
			if err == nil {
				t.Fatal("expected invalid payload error")
			}
			var invalid *InvalidPayloadError
			if !errors.As(err, &invalid) {
				t.Fatalf("error type = %T, want *InvalidPayloadError", err)
			}
			if err.Error() != "invalid canonical payload" {
				t.Fatalf("error leaked payload details: %q", err)
			}
			if errors.Unwrap(err) != nil {
				t.Fatal("invalid payload error exposes an underlying error that may contain request data")
			}
		})
	}
}
