package recovery

import (
	"testing"
	"time"
)

func TestClassifyRequiresIsolatedNonProductionIntegrityAndMeetsObjectives(t *testing.T) {
	incident := time.Date(2026, 9, 2, 10, 0, 0, 0, time.UTC)
	result := Classify(DrillInput{
		Environment: "staging", BackupID: "backup-alias-1", IncidentAt: incident,
		RecoveryPointAt: incident.Add(-10 * time.Minute), RestoreStartedAt: incident.Add(time.Minute), RestoreReadyAt: incident.Add(20 * time.Minute),
		RPOTarget: 15 * time.Minute, RTOTarget: 30 * time.Minute, IsolatedTarget: true, IntegrityPassed: true, ApplicationChecksPassed: true,
	})
	if result.Status != "passed" || result.RPOMinutes != 10 || result.RTOMinutes != 20 {
		t.Fatalf("result=%+v", result)
	}
	if got := Classify(DrillInput{Environment: "production", ProductionTarget: true}); got.Status != "blocked" {
		t.Fatalf("production result=%+v", got)
	}
	for _, environment := range []string{"Production", "PROD", "qa", "unknown"} {
		input := DrillInput{Environment: environment, BackupID: "backup-alias-1", IncidentAt: incident, RecoveryPointAt: incident.Add(-time.Minute), RestoreStartedAt: incident, RestoreReadyAt: incident.Add(time.Minute), RPOTarget: 15 * time.Minute, RTOTarget: 30 * time.Minute, IsolatedTarget: true, IntegrityPassed: true, ApplicationChecksPassed: true}
		if got := Classify(input); got.Status != "blocked" {
			t.Fatalf("environment %q result=%+v", environment, got)
		}
	}
}
