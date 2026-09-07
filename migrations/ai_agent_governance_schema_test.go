package migrationbundle

import (
	"strings"
	"testing"
)

func TestAIAgentGovernanceSchemaIsDurableAndFailClosed(t *testing.T) {
	up := migrationSQL(t, "000061_ai_agent_governance.up.sql")
	requireSQLFragments(t, up,
		"CREATE TABLE ai_governance_controls",
		"CREATE TABLE ai_tool_approvals",
		"CREATE TABLE ai_agent_runs",
		"CREATE TABLE ai_budget_periods",
		"CREATE TABLE ai_tool_executions",
		"CREATE TABLE ai_spend_reservations",
		"CREATE TABLE ai_provider_circuit_breakers",
		"execution_enabled BOOLEAN NOT NULL DEFAULT FALSE",
		"INSERT INTO ai_governance_controls",
		"'read-only', 'internal draft', 'reversible write', 'external communication', 'financial commitment', 'tax or compliance', 'credential or security', 'irreversible or legally significant'",
		"arguments_hash CHAR(64)",
		"jsonb_typeof(sanitized_arguments) = 'object'",
		"UNIQUE (run_id, idempotency_key)",
		"reserved_micros + settled_micros <= limit_micros",
		"status IN ('reserved', 'reconciliation_required')",
		"state IN ('closed', 'open', 'half_open')",
		"ON DELETE RESTRICT",
	)
	for _, forbidden := range []string{"api_key ", "access_token ", "raw_prompt ", "raw_arguments ", "raw_result ", "response_body ", "raw_error ", "authorization "} {
		if strings.Contains(strings.ToLower(up), forbidden) {
			t.Fatalf("governance schema contains forbidden sensitive column %q", forbidden)
		}
	}
}

func TestAIAgentGovernanceRollbackDropsDependentsFirst(t *testing.T) {
	down := migrationSQL(t, "000061_ai_agent_governance.down.sql")
	want := []string{
		"DROP TABLE IF EXISTS ai_spend_reservations",
		"DROP TABLE IF EXISTS ai_tool_executions",
		"DROP TABLE IF EXISTS ai_budget_periods",
		"DROP TABLE IF EXISTS ai_agent_runs",
		"DROP TABLE IF EXISTS ai_tool_approvals",
		"DROP TABLE IF EXISTS ai_provider_circuit_breakers",
		"DROP TABLE IF EXISTS ai_governance_controls",
		"DROP INDEX IF EXISTS uq_agents_id_business",
	}
	previous := -1
	for _, fragment := range want {
		position := strings.Index(down, fragment)
		if position <= previous {
			t.Fatalf("rollback fragment %q is missing or out of order", fragment)
		}
		previous = position
	}
}
