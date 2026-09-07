package main

import (
	"encoding/json"
	"flag"
	"os"
	"time"

	"invoice-backend/internal/recovery"
)

func main() {
	var environment, backupID string
	var incident, recoveryPoint, restoreStarted, restoreReady string
	var rpoMinutes, rtoMinutes int
	var isolated, integrity, application, productionTarget bool
	flag.StringVar(&environment, "environment", "", "non-production drill environment")
	flag.StringVar(&backupID, "backup-id", "", "sanitized backup alias")
	flag.StringVar(&incident, "incident-at", "", "RFC3339 incident time")
	flag.StringVar(&recoveryPoint, "recovery-point-at", "", "RFC3339 recovery point")
	flag.StringVar(&restoreStarted, "restore-started-at", "", "RFC3339 restore start")
	flag.StringVar(&restoreReady, "restore-ready-at", "", "RFC3339 restore ready time")
	flag.IntVar(&rpoMinutes, "rpo-minutes", 15, "RPO objective")
	flag.IntVar(&rtoMinutes, "rto-minutes", 60, "RTO objective")
	flag.BoolVar(&isolated, "isolated-target", false, "confirm isolated target")
	flag.BoolVar(&integrity, "integrity-passed", false, "confirm database integrity checks")
	flag.BoolVar(&application, "application-checks-passed", false, "confirm application checks")
	flag.BoolVar(&productionTarget, "production-target", false, "must remain false")
	flag.Parse()
	parse := func(value string) time.Time { parsed, _ := time.Parse(time.RFC3339, value); return parsed }
	result := recovery.Classify(recovery.DrillInput{
		Environment: environment, BackupID: backupID, IncidentAt: parse(incident), RecoveryPointAt: parse(recoveryPoint),
		RestoreStartedAt: parse(restoreStarted), RestoreReadyAt: parse(restoreReady), RPOTarget: time.Duration(rpoMinutes) * time.Minute,
		RTOTarget: time.Duration(rtoMinutes) * time.Minute, IsolatedTarget: isolated, IntegrityPassed: integrity,
		ApplicationChecksPassed: application, ProductionTarget: productionTarget,
	})
	_ = json.NewEncoder(os.Stdout).Encode(result)
	if result.Status != "passed" {
		os.Exit(1)
	}
}
