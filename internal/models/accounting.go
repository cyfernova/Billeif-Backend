package models

import "time"

const (
	ReversalPolicyNextOpenPeriod = "next_open_period"
	ReversalPolicyBlocked        = "blocked"

	BankStatementPending    = "pending"
	BankStatementReconciled = "reconciled"
)

type AccountingPeriodPolicy struct {
	BusinessID     string     `gorm:"primaryKey;type:uuid" json:"business_id"`
	LockDate       *time.Time `gorm:"type:date" json:"lock_date,omitempty"`
	ReversalPolicy string     `gorm:"not null;size:32;default:'next_open_period'" json:"reversal_policy"`
	UpdatedBy      string     `gorm:"not null;size:255" json:"updated_by"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
}

type AccountingAccount struct {
	ID           string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID   string    `gorm:"not null;index;uniqueIndex:uq_accounting_account_code,priority:1" json:"business_id"`
	Code         string    `gorm:"not null;size:60;uniqueIndex:uq_accounting_account_code,priority:2" json:"code"`
	Name         string    `gorm:"not null;size:120" json:"name"`
	AccountClass string    `gorm:"not null;size:24;index" json:"account_class"`
	ParentCode   *string   `gorm:"size:60" json:"parent_code,omitempty"`
	CreatedAt    time.Time `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime" json:"updated_at"`
}

func (AccountingAccount) TableName() string { return "accounting_accounts" }

func (AccountingPeriodPolicy) TableName() string { return "accounting_period_policies" }

type AccountingLockOverride struct {
	ID              string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID      string     `gorm:"not null;index" json:"business_id"`
	Subject         string     `gorm:"not null;size:255" json:"subject"`
	Action          string     `gorm:"not null;size:80" json:"action"`
	Resource        string     `gorm:"not null;size:255" json:"resource"`
	CommandIdentity string     `gorm:"not null;size:180" json:"command_identity"`
	PostingDate     time.Time  `gorm:"not null;type:date" json:"posting_date"`
	Reason          string     `gorm:"not null;size:500" json:"reason"`
	CreatedAt       time.Time  `gorm:"autoCreateTime" json:"created_at"`
	AppliedAt       *time.Time `json:"applied_at,omitempty"`
}

func (AccountingLockOverride) TableName() string { return "accounting_lock_overrides" }

type AccountingAuditEvent struct {
	ID           string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID   string    `gorm:"not null;index" json:"business_id"`
	Subject      string    `gorm:"not null;size:255" json:"subject"`
	EventType    string    `gorm:"not null;size:80" json:"event_type"`
	ResourceType string    `gorm:"not null;size:64" json:"resource_type"`
	ResourceID   string    `gorm:"not null;size:255" json:"resource_id"`
	Outcome      string    `gorm:"not null;size:32" json:"outcome"`
	Reason       string    `gorm:"not null;type:text" json:"reason"`
	OccurredAt   time.Time `gorm:"autoCreateTime" json:"occurred_at"`
}

func (AccountingAuditEvent) TableName() string { return "accounting_audit_events" }

type OpeningBalanceCommand struct {
	ID             string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID     string    `gorm:"not null;uniqueIndex:ux_opening_balance_command" json:"business_id"`
	IdempotencyKey string    `gorm:"not null;size:180;uniqueIndex:ux_opening_balance_command" json:"idempotency_key"`
	RequestHash    string    `gorm:"not null;size:64" json:"request_hash"`
	JournalID      string    `gorm:"not null;type:uuid" json:"journal_id"`
	Subject        string    `gorm:"not null;size:255" json:"subject"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (OpeningBalanceCommand) TableName() string { return "accounting_opening_balance_commands" }

type InventoryOpeningBalance struct {
	ID             string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID     string    `gorm:"not null;index" json:"business_id"`
	CommandID      string    `gorm:"not null;index" json:"command_id"`
	ProductID      string    `gorm:"not null;index" json:"product_id"`
	WarehouseID    string    `gorm:"not null;index" json:"warehouse_id"`
	QuantityMicros int64     `gorm:"not null" json:"quantity_micros"`
	UnitCostMinor  int64     `gorm:"not null" json:"unit_cost_minor"`
	Currency       string    `gorm:"not null;size:3" json:"currency"`
	AsOfDate       time.Time `gorm:"not null;type:date" json:"as_of_date"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (InventoryOpeningBalance) TableName() string { return "accounting_inventory_opening_balances" }

type BankAccount struct {
	ID            string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID    string     `gorm:"not null;index;uniqueIndex:ux_bank_account_business_name" json:"business_id"`
	Name          string     `gorm:"not null;size:120;uniqueIndex:ux_bank_account_business_name" json:"name"`
	Currency      string     `gorm:"not null;size:3" json:"currency"`
	MaskedAccount string     `gorm:"not null;size:64" json:"masked_account"`
	LedgerAccount string     `gorm:"not null;size:60" json:"ledger_account"`
	OpeningMinor  int64      `gorm:"not null;default:0" json:"opening_minor"`
	IsActive      bool       `gorm:"not null;default:true" json:"is_active"`
	CreatedAt     time.Time  `gorm:"autoCreateTime" json:"created_at"`
	UpdatedAt     time.Time  `gorm:"autoUpdateTime" json:"updated_at"`
	DeactivatedAt *time.Time `json:"deactivated_at,omitempty"`
}

func (BankAccount) TableName() string { return "bank_accounts" }

type BankStatement struct {
	ID             string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID     string     `gorm:"not null;index;uniqueIndex:ux_bank_statement_command;uniqueIndex:ux_bank_statement_upload" json:"business_id"`
	BankAccountID  string     `gorm:"not null;index" json:"bank_account_id"`
	UploadID       string     `gorm:"not null;index;uniqueIndex:ux_bank_statement_upload" json:"upload_id"`
	IdempotencyKey string     `gorm:"not null;size:180;uniqueIndex:ux_bank_statement_command" json:"idempotency_key"`
	RequestHash    string     `gorm:"not null;size:64" json:"request_hash"`
	Status         string     `gorm:"not null;size:24" json:"status"`
	PeriodFrom     time.Time  `gorm:"not null;type:date" json:"period_from"`
	PeriodTo       time.Time  `gorm:"not null;type:date" json:"period_to"`
	ImportedBy     string     `gorm:"not null;size:255" json:"imported_by"`
	ImportedAt     time.Time  `gorm:"autoCreateTime" json:"imported_at"`
	ReconciledAt   *time.Time `json:"reconciled_at,omitempty"`
	ReconciledBy   string     `gorm:"size:255" json:"reconciled_by,omitempty"`
}

func (BankStatement) TableName() string { return "bank_statements" }

type BankTransaction struct {
	ID            string    `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID    string    `gorm:"not null;index" json:"business_id"`
	StatementID   string    `gorm:"not null;index;uniqueIndex:ux_bank_transaction_external" json:"statement_id"`
	ExternalID    string    `gorm:"not null;size:180;uniqueIndex:ux_bank_transaction_external" json:"external_id"`
	TransactionAt time.Time `gorm:"not null;type:date;index" json:"transaction_at"`
	AmountMinor   int64     `gorm:"not null" json:"amount_minor"`
	Currency      string    `gorm:"not null;size:3" json:"currency"`
	Reference     string    `gorm:"size:180" json:"reference"`
	Description   string    `gorm:"size:500" json:"description"`
	CreatedAt     time.Time `gorm:"autoCreateTime" json:"created_at"`
}

func (BankTransaction) TableName() string { return "bank_transactions" }

type BankMatch struct {
	ID                string     `gorm:"primaryKey;type:uuid;default:gen_random_uuid()" json:"id"`
	BusinessID        string     `gorm:"not null;index" json:"business_id"`
	BankTransactionID string     `gorm:"not null;index;uniqueIndex:ux_active_bank_match" json:"bank_transaction_id"`
	LedgerEntryID     string     `gorm:"not null;index" json:"ledger_entry_id"`
	MatchType         string     `gorm:"not null;size:24" json:"match_type"`
	MatchedBy         string     `gorm:"not null;size:255" json:"matched_by"`
	Reason            string     `gorm:"not null;size:500" json:"reason"`
	MatchedAt         time.Time  `gorm:"autoCreateTime" json:"matched_at"`
	UnmatchedAt       *time.Time `json:"unmatched_at,omitempty"`
	UnmatchedBy       string     `gorm:"size:255" json:"unmatched_by,omitempty"`
}

func (BankMatch) TableName() string { return "bank_matches" }
