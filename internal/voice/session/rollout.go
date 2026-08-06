package session

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/binary"
	"fmt"
	"strconv"
	"strings"
)

const rolloutHashDomain = "billeif-voice-rollout-v1"

func rolloutAllows(cfg Config, scope Scope) bool {
	if !cfg.AdmissionEnabled {
		return false
	}

	switch cfg.RolloutStage {
	case "internal":
		actual := fmt.Sprintf("sha256:%x", sha256.Sum256([]byte(scope.UserID)))
		for _, allowed := range cfg.RolloutInternalSubjectHashes {
			if subtle.ConstantTimeCompare([]byte(actual), []byte(strings.TrimSpace(allowed))) == 1 {
				return true
			}
		}
		return false
	case "5", "25", "50", "100":
		percentage, err := strconv.Atoi(cfg.RolloutStage)
		if err != nil {
			return false
		}
		payload := rolloutHashDomain + "\x00" + scope.UserID + "\x00" + scope.BusinessID
		digest := sha256.Sum256([]byte(payload))
		bucket := binary.BigEndian.Uint64(digest[:8]) % 100
		return bucket < uint64(percentage)
	default:
		return false
	}
}
