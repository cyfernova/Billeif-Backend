+ CREATE TABLE IF NOT EXISTS ledger_entries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL,
    invoice_id UUID,
    payment_id UUID,
    transaction_id VARCHAR(100) NOT NULL,
    entry_date TIMESTAMP NOT NULL,
    entry_type VARCHAR(50) NOT NULL CHECK (entry_type IN ('debit', 'credit')),
    category VARCHAR(50),
    description VARCHAR(500) NOT NULL,
    amount DECIMAL(15,2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    balance DECIMAL(15,2),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE,
    FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE SET NULL,
    FOREIGN KEY (payment_id) REFERENCES payments(id) ON DELETE SET NULL
);

CREATE INDEX idx_ledger_entries_business_id ON ledger_entries(business_id);
CREATE INDEX idx_ledger_entries_invoice_id ON ledger_entries(invoice_id);
CREATE INDEX idx_ledger_entries_payment_id ON ledger_entries(payment_id);
CREATE INDEX idx_ledger_entries_entry_date ON ledger_entries(entry_date);
CREATE INDEX idx_ledger_entries_transaction_id ON ledger_entries(transaction_id);
