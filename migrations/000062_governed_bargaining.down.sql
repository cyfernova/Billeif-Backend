UPDATE ai_governance_controls SET execution_enabled = FALSE,
    reason_code = 'disabled_by_default', version = version + 1, updated_at = NOW()
WHERE scope_kind = 'global' AND reason_code = 'governed_bargaining_rollout' AND version = 2;
DROP TABLE ai_bargaining_proposals;
