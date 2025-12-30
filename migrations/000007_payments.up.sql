CREATE TABLE IF NOT EXISTS payments (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL,
    invoice_id UUID NOT NULL,
    amount DECIMAL(15,2) NOT NULL,
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    payment_date TIMESTAMP NOT NULL,
    payment_method VARCHAR(50) NOT NULL,
    reference VARCHAR(100),
    receipt_url VARCHAR(500),
    receipt_key VARCHAR(255),
    notes TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP,
    FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE,
    FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE
);

CREATE INDEX idx_payments_business_id ON payments(business_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_payments_invoice_id ON payments(invoice_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_payments_payment_date ON payments(payment_date) WHERE deleted_at IS NULL;
CREATE INDEX idx_payments_deleted_at ON payments(deleted_at);
