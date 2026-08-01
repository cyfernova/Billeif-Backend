package sesfeedback

import "testing"

func TestResolveTransitionEnforcesNegativeFeedbackPrecedence(t *testing.T) {
	tests := []struct {
		current string
		event   EventType
		want    string
		changed bool
	}{
		{"sent", EventTypeDelivery, "delivered", true},
		{"sent", EventTypeBounce, "bounced", true},
		{"sent", EventTypeComplaint, "complained", true},
		{"delivered", EventTypeBounce, "bounced", true},
		{"delivered", EventTypeComplaint, "complained", true},
		{"bounced", EventTypeComplaint, "complained", true},
		{"delivered", EventTypeDelivery, "delivered", false},
		{"bounced", EventTypeDelivery, "bounced", false},
		{"complained", EventTypeDelivery, "complained", false},
		{"complained", EventTypeBounce, "complained", false},
		{"bounced", EventTypeBounce, "bounced", false},
		{"complained", EventTypeComplaint, "complained", false},
	}
	for _, test := range tests {
		t.Run(test.current+"_"+string(test.event), func(t *testing.T) {
			status, changed, err := ResolveTransition(test.current, test.event)
			if err != nil || status != test.want || changed != test.changed {
				t.Fatalf("transition = %q/%t/%v, want %q/%t", status, changed, err, test.want, test.changed)
			}
		})
	}
}

func TestResolveTransitionRejectsPreSendAndUnknownStates(t *testing.T) {
	for _, status := range []string{"waiting_for_render", "queued", "processing", "failed", "unknown"} {
		if _, _, err := ResolveTransition(status, EventTypeDelivery); err == nil {
			t.Fatalf("status %q accepted provider feedback", status)
		}
	}
}
