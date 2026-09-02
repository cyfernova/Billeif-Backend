CREATE TABLE accounting_period_policies (
    business_id UUID PRIMARY KEY REFERENCES business_profiles(id) ON DELETE CASCADE,
    lock_date DATE,
    reversal_policy VARCHAR(32) NOT NULL DEFAULT 'next_open_period'
        CHECK (reversal_policy IN ('next_open_period', 'blocked')),
    updated_by VARCHAR(255) NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

INSERT INTO accounting_period_policies (business_id, lock_date, reversal_policy, updated_by)
SELECT id, NULL, 'next_open_period', 'migration'
FROM business_profiles
ON CONFLICT (business_id) DO NOTHING;

CREATE OR REPLACE FUNCTION ensure_accounting_period_policy()
RETURNS TRIGGER AS $$
BEGIN
    INSERT INTO accounting_period_policies (business_id, lock_date, reversal_policy, updated_by)
    VALUES (NEW.id, NULL, 'next_open_period', 'system')
    ON CONFLICT (business_id) DO NOTHING;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trg_business_accounting_period_policy
AFTER INSERT ON business_profiles
FOR EACH ROW EXECUTE FUNCTION ensure_accounting_period_policy();

CREATE TABLE accounting_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    code VARCHAR(60) NOT NULL,
    name VARCHAR(120) NOT NULL,
    account_class VARCHAR(24) NOT NULL CHECK (account_class IN ('asset','liability','equity','revenue','expense','unclassified')),
    parent_code VARCHAR(60),
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (business_id, code),
    FOREIGN KEY (business_id, parent_code) REFERENCES accounting_accounts(business_id, code) ON DELETE RESTRICT
);
CREATE INDEX idx_accounting_accounts_hierarchy ON accounting_accounts (business_id, account_class, parent_code, code);
INSERT INTO accounting_accounts (business_id, code, name, account_class)
SELECT j.business_id, UPPER(jl.account_code), MAX(jl.account_name),
       CASE WHEN UPPER(jl.account_code) IN ('CASH','BANK','AR','INV','IN_GST','TDS_RECEIVABLE','GST_TDS_RECEIVABLE') OR UPPER(jl.account_code) LIKE 'ASSET%' THEN 'asset'
            WHEN UPPER(jl.account_code) IN ('AP','OUT_GST') OR UPPER(jl.account_code) LIKE 'LIAB%' THEN 'liability'
            WHEN UPPER(jl.account_code) IN ('REV','REVENUE','INTEREST_INCOME') OR UPPER(jl.account_code) LIKE 'REV%' THEN 'revenue'
            WHEN UPPER(jl.account_code) IN ('EQUITY','OPENING_EQUITY') OR UPPER(jl.account_code) LIKE 'EQUITY%' THEN 'equity'
            ELSE 'unclassified' END
FROM journals j JOIN journal_lines jl ON jl.journal_id = j.id
GROUP BY j.business_id, UPPER(jl.account_code)
ON CONFLICT (business_id, code) DO NOTHING;

CREATE TABLE accounting_lock_overrides (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    subject VARCHAR(255) NOT NULL,
    action VARCHAR(80) NOT NULL,
    resource VARCHAR(255) NOT NULL,
    command_identity VARCHAR(180) NOT NULL,
    posting_date DATE NOT NULL,
    reason VARCHAR(500) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    applied_at TIMESTAMPTZ,
    UNIQUE (business_id, command_identity)
);
CREATE INDEX idx_accounting_lock_overrides_scope
    ON accounting_lock_overrides (business_id, posting_date, applied_at);

CREATE TABLE accounting_audit_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    subject VARCHAR(255) NOT NULL,
    event_type VARCHAR(80) NOT NULL,
    resource_type VARCHAR(64) NOT NULL,
    resource_id VARCHAR(255) NOT NULL,
    outcome VARCHAR(32) NOT NULL,
    reason TEXT NOT NULL,
    occurred_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);
CREATE INDEX idx_accounting_audit_scope
    ON accounting_audit_events (business_id, occurred_at DESC);

ALTER TABLE invoices
    ADD COLUMN branch_id UUID REFERENCES branches(id) ON DELETE SET NULL;
UPDATE invoices i
SET branch_id = d.branch_id
FROM documents d
WHERE d.id = i.id
  AND d.business_id = i.business_id
  AND d.branch_id IS NOT NULL;
CREATE INDEX idx_invoices_branch_date
    ON invoices (business_id, branch_id, invoice_date DESC) WHERE deleted_at IS NULL;

ALTER TABLE journals
    ADD COLUMN branch_id UUID REFERENCES branches(id) ON DELETE SET NULL,
    ADD COLUMN lock_override_id UUID REFERENCES accounting_lock_overrides(id) ON DELETE RESTRICT;
UPDATE journals j
SET branch_id = d.branch_id
FROM documents d
WHERE j.source_type = 'document'
  AND j.source_id = d.id
  AND j.business_id = d.business_id
  AND d.branch_id IS NOT NULL;
UPDATE journals j
SET branch_id = d.branch_id
FROM payments p
JOIN documents d ON d.id = p.invoice_id AND d.business_id = p.business_id
WHERE j.source_type IN ('payment', 'payment_reversal')
  AND j.source_id = p.id
  AND j.business_id = p.business_id
  AND d.branch_id IS NOT NULL;
CREATE INDEX idx_journals_branch_date
    ON journals (business_id, branch_id, posting_date DESC) WHERE deleted_at IS NULL;

ALTER TABLE ledger_entries
    ADD COLUMN branch_id UUID REFERENCES branches(id) ON DELETE SET NULL;
UPDATE ledger_entries le
SET branch_id = j.branch_id
FROM journals j
WHERE le.transaction_id = j.id
  AND le.business_id = j.business_id
  AND j.branch_id IS NOT NULL;
CREATE INDEX idx_ledger_entries_branch_date
    ON ledger_entries (business_id, branch_id, entry_date DESC);

CREATE UNIQUE INDEX uq_branches_id_business ON branches (id, business_id);
ALTER TABLE invoices ADD CONSTRAINT fk_invoices_branch_business
    FOREIGN KEY (branch_id, business_id) REFERENCES branches(id, business_id) ON DELETE RESTRICT;
ALTER TABLE journals ADD CONSTRAINT fk_journals_branch_business
    FOREIGN KEY (branch_id, business_id) REFERENCES branches(id, business_id) ON DELETE RESTRICT;
ALTER TABLE ledger_entries ADD CONSTRAINT fk_ledger_entries_branch_business
    FOREIGN KEY (branch_id, business_id) REFERENCES branches(id, business_id) ON DELETE RESTRICT;

CREATE TABLE accounting_opening_balance_commands (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    idempotency_key VARCHAR(180) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    journal_id UUID NOT NULL REFERENCES journals(id) ON DELETE RESTRICT,
    subject VARCHAR(255) NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (business_id, idempotency_key)
);

CREATE TABLE accounting_inventory_opening_balances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    command_id UUID NOT NULL REFERENCES accounting_opening_balance_commands(id) ON DELETE RESTRICT,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE RESTRICT,
    warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE RESTRICT,
    quantity_micros BIGINT NOT NULL CHECK (quantity_micros >= 0),
    unit_cost_minor BIGINT NOT NULL CHECK (unit_cost_minor >= 0),
    currency CHAR(3) NOT NULL,
    as_of_date DATE NOT NULL,
    UNIQUE (command_id, product_id, warehouse_id)
);
CREATE INDEX idx_accounting_inventory_opening_scope
    ON accounting_inventory_opening_balances (business_id, warehouse_id, product_id);

CREATE TABLE bank_accounts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(120) NOT NULL,
    currency CHAR(3) NOT NULL,
    masked_account VARCHAR(64) NOT NULL,
    ledger_account VARCHAR(60) NOT NULL,
    opening_minor BIGINT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deactivated_at TIMESTAMPTZ,
    UNIQUE (business_id, name)
);

CREATE TABLE bank_statements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    bank_account_id UUID NOT NULL REFERENCES bank_accounts(id) ON DELETE RESTRICT,
    upload_id UUID NOT NULL REFERENCES security_pending_uploads(id) ON DELETE RESTRICT,
    idempotency_key VARCHAR(180) NOT NULL,
    request_hash CHAR(64) NOT NULL,
    status VARCHAR(24) NOT NULL CHECK (status IN ('pending', 'reconciled')),
    period_from DATE NOT NULL,
    period_to DATE NOT NULL,
    imported_by VARCHAR(255) NOT NULL,
    imported_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    reconciled_at TIMESTAMPTZ,
    reconciled_by VARCHAR(255) NOT NULL DEFAULT '',
    UNIQUE (business_id, idempotency_key),
	UNIQUE (business_id, upload_id),
    CHECK (period_to >= period_from)
);

CREATE TABLE bank_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    statement_id UUID NOT NULL REFERENCES bank_statements(id) ON DELETE CASCADE,
    external_id VARCHAR(180) NOT NULL,
    transaction_at DATE NOT NULL,
    amount_minor BIGINT NOT NULL CHECK (amount_minor <> 0),
    currency CHAR(3) NOT NULL,
    reference VARCHAR(180) NOT NULL DEFAULT '',
    description VARCHAR(500) NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    UNIQUE (statement_id, external_id)
);
CREATE INDEX idx_bank_transactions_scope
    ON bank_transactions (business_id, transaction_at DESC);

CREATE TABLE bank_matches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    bank_transaction_id UUID NOT NULL REFERENCES bank_transactions(id) ON DELETE RESTRICT,
    ledger_entry_id UUID NOT NULL REFERENCES ledger_entries(id) ON DELETE RESTRICT,
    match_type VARCHAR(24) NOT NULL CHECK (match_type IN ('manual', 'fee', 'interest')),
    matched_by VARCHAR(255) NOT NULL,
    reason VARCHAR(500) NOT NULL,
    matched_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    unmatched_at TIMESTAMPTZ,
    unmatched_by VARCHAR(255) NOT NULL DEFAULT ''
);
CREATE UNIQUE INDEX ux_active_bank_match
    ON bank_matches (bank_transaction_id) WHERE unmatched_at IS NULL;
CREATE UNIQUE INDEX ux_active_bank_ledger_match
    ON bank_matches (ledger_entry_id) WHERE unmatched_at IS NULL;
CREATE INDEX idx_bank_matches_scope
    ON bank_matches (business_id, matched_at DESC);

COMMENT ON COLUMN journal_lines.amount IS
    'Legacy decimal amount retained exactly; accounting reports round once to minor units per persisted line.';
