# Backup and restore drill

Production restore is an operator-controlled destructive workflow and is never
started by repository automation. The targets are RPO 15 minutes and RTO 60
minutes. Confirm deployed backup policy and retention with read-only inventory,
restore only to a newly isolated non-production target, run database integrity
and application verification, then classify the evidence locally.

Required evidence: sanitized backup alias, incident time, recovery-point time,
restore start/ready times, isolated-target proof, database integrity result and
application verification result. Never record account IDs, ARNs, endpoints,
credentials or database contents.

```bash
go run ./cmd/recovery-drill \
  --environment staging --backup-id backup-alias-1 \
  --incident-at 2026-09-02T10:00:00Z \
  --recovery-point-at 2026-09-02T09:50:00Z \
  --restore-started-at 2026-09-02T10:01:00Z \
  --restore-ready-at 2026-09-02T10:20:00Z \
  --isolated-target --integrity-passed --application-checks-passed
```

Any production target, missing isolation proof, missing integrity evidence, or
missed objective returns non-zero. Provider cleanup and target removal remain
separate approved operator actions and are never embedded in the drill.
