package recovery

import (
	"strings"
	"time"
)

type DrillInput struct {
	Environment                                                                string
	BackupID                                                                   string
	IncidentAt, RecoveryPointAt, RestoreStartedAt, RestoreReadyAt              time.Time
	RPOTarget, RTOTarget                                                       time.Duration
	IsolatedTarget, IntegrityPassed, ApplicationChecksPassed, ProductionTarget bool
}

type DrillResult struct {
	Status            string `json:"status"`
	ReasonCode        string `json:"reason_code"`
	RPOMinutes        int64  `json:"rpo_minutes"`
	RTOMinutes        int64  `json:"rto_minutes"`
	RPOWithinTarget   bool   `json:"rpo_within_target"`
	RTOWithinTarget   bool   `json:"rto_within_target"`
	IntegrityVerified bool   `json:"integrity_verified"`
}

func Classify(input DrillInput) DrillResult {
	result := DrillResult{Status: "blocked", ReasonCode: "invalid_drill"}
	environment := strings.ToLower(strings.TrimSpace(input.Environment))
	allowedEnvironment := environment == "staging" || environment == "development" || environment == "test" || environment == "local"
	if !allowedEnvironment || input.ProductionTarget || !input.IsolatedTarget ||
		input.BackupID == "" || input.IncidentAt.IsZero() || input.RecoveryPointAt.IsZero() || input.RestoreStartedAt.IsZero() || input.RestoreReadyAt.IsZero() ||
		input.RecoveryPointAt.After(input.IncidentAt) || input.RestoreStartedAt.Before(input.IncidentAt) || input.RestoreReadyAt.Before(input.RestoreStartedAt) ||
		input.RPOTarget <= 0 || input.RTOTarget <= 0 {
		return result
	}
	rpo := input.IncidentAt.Sub(input.RecoveryPointAt)
	rto := input.RestoreReadyAt.Sub(input.IncidentAt)
	result.RPOMinutes, result.RTOMinutes = int64(rpo/time.Minute), int64(rto/time.Minute)
	result.RPOWithinTarget, result.RTOWithinTarget = rpo <= input.RPOTarget, rto <= input.RTOTarget
	result.IntegrityVerified = input.IntegrityPassed && input.ApplicationChecksPassed
	if !result.IntegrityVerified {
		result.ReasonCode = "integrity_unverified"
		return result
	}
	if !result.RPOWithinTarget || !result.RTOWithinTarget {
		result.Status, result.ReasonCode = "failed", "recovery_objective_missed"
		return result
	}
	result.Status, result.ReasonCode = "passed", "objectives_met"
	return result
}
