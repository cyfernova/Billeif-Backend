package nlp

import (
	"context"
	"testing"
)

type intentClientResponse string

func (response intentClientResponse) Call(context.Context, string) (string, error) {
	return string(response), nil
}

func TestConfiguredIntentParserRejectsUnusableSearch(t *testing.T) {
	for _, response := range []string{`{}`, `null`, `{"keywords":[" "]}`, `{"keywords":["chairs"],"quantity":-2}`, `{"keywords":["chairs"],"price_range":{"min":5000,"max":1000}}`, `not json`} {
		t.Run(response, func(t *testing.T) {
			result, err := NewIntentParserWithClient(intentClientResponse(response)).ParseIntent(context.Background(), "Find chairs")
			if err != nil {
				t.Fatal(err)
			}
			if result.Parsed || result.Error == "" {
				t.Fatalf("unusable extraction accepted: %#v", result)
			}
		})
	}
}

func TestRuleParserQuantityAndUrgency(t *testing.T) {
	for _, tc := range []struct {
		input    string
		quantity int
		urgency  string
	}{
		{"Find 2 office chairs", 2, "medium"},
		{"Please buy 12 chairs ASAP", 12, "urgent"},
		{"Find a printer under 15000 rupees", 1, "medium"},
		{"Find iPhone 15", 1, "medium"},
		{"Need 3 desks tomorrow", 3, "high"},
		{"Find QuickBooks software", 1, "medium"},
		{"Find a chair TODAY", 1, "urgent"},
	} {
		t.Run(tc.input, func(t *testing.T) {
			result, err := NewRuleBasedIntentParser().ParseIntent(context.Background(), tc.input)
			if err != nil {
				t.Fatal(err)
			}
			if result.Intent.Quantity != tc.quantity || result.Intent.Urgency != tc.urgency {
				t.Fatalf("got quantity=%d urgency=%s; want quantity=%d urgency=%s", result.Intent.Quantity, result.Intent.Urgency, tc.quantity, tc.urgency)
			}
		})
	}
}
