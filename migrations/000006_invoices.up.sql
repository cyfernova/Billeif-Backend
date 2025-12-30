CREATE TABLE IF NOT EXISTS invoices (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL,
    customer_id UUID NOT NULL,
    invoice_no VARCHAR(50) NOT NULL,
    invoice_date TIMESTAMP NOT NULL,
    due_date TIMESTAMP,
    status VARCHAR(50) NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'sent', 'paid', 'overdue', 'void', 'canceled')),
    currency VARCHAR(3) NOT NULL DEFAULT 'USD',
    subtotal DECIMAL(15,2) DEFAULT 0,
    tax DECIMAL(15,2) DEFAULT 0,
    discount DECIMAL(15,2) DEFAULT 0,
    total DECIMAL(15,2) NOT NULL DEFAULT 0,
    paid_amount DECIMAL(15,2) DEFAULT 0,
    balance_due DECIMAL(15,2) DEFAULT 0,
    notes TEXT,
    pdf_url VARCHAR(500),
    pdf_filename VARCHAR(255),
    sent_at TIMESTAMP,
    paid_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP,
    FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE,
    FOREIGN KEY (customer_id) REFERENCES customers(id) ON DELETE CASCADE,
    CONSTRAINT uc_business_invoice UNIQUE (business_id, invoice_no)
);

CREATE INDEX idx_invoices_business_id ON invoices(business_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_invoices_customer_id ON invoices(customer_id) WHERE deleted_at IS NULL;
CREATE INDEX idx_invoices_status ON invoices(status) WHERE deleted_at IS NULL;
CREATE INDEX idx_invoices_invoice_date ON invoices(invoice_date) WHERE deleted_at IS NULL;
CREATE INDEX idx_invoices_deleted_at ON invoices(deleted_at);

CREATE TABLE IF NOT EXISTS invoice_items (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    invoice_id UUID NOT NULL,
    product_id UUID,
    description VARCHAR(500) NOT NULL,
    quantity DECIMAL(15,3) NOT NULL,
    unit_price DECIMAL(15,2) NOT NULL,
    discount DECIMAL(15,2) DEFAULT 0,
    tax_rate DECIMAL(5,2) DEFAULT 0,
    total DECIMAL(15,2) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    FOREIGN KEY (invoice_id) REFERENCES invoices(id) ON DELETE CASCADE,
    FOREIGN KEY (product_id) REFERENCES products(id) ON DELETE SET NULL
);

CREATE INDEX idx_invoice_items_invoice_id ON invoice_items(invoice_id);
CREATE INDEX idx_invoice_items_product_id ON invoice_items(product_id);
