package sesfeedback

import (
	"errors"
	"fmt"
)

var ErrInvalidTransition = errors.New("invalid SES feedback transition")

func ResolveTransition(current string, eventType EventType) (string, bool, error) {
	switch current {
	case "sent":
		switch eventType {
		case EventTypeDelivery:
			return "delivered", true, nil
		case EventTypeBounce:
			return "bounced", true, nil
		case EventTypeComplaint:
			return "complained", true, nil
		}
	case "delivered":
		switch eventType {
		case EventTypeDelivery:
			return current, false, nil
		case EventTypeBounce:
			return "bounced", true, nil
		case EventTypeComplaint:
			return "complained", true, nil
		}
	case "bounced":
		switch eventType {
		case EventTypeDelivery, EventTypeBounce:
			return current, false, nil
		case EventTypeComplaint:
			return "complained", true, nil
		}
	case "complained":
		switch eventType {
		case EventTypeDelivery, EventTypeBounce, EventTypeComplaint:
			return current, false, nil
		}
	}
	return "", false, fmt.Errorf(
		"%w: status %q with event %q",
		ErrInvalidTransition,
		current,
		eventType,
	)
}
