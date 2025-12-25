-- Migration: 000002_customer_vendor_business_profile
-- Replaces Client and Company with Customer, Vendor, and BusinessProfile
-- Adds GST compliance, credit management, and balance tracking

-- Enable UUID extension (if not already enabled)
CREATE EXTENSION IF NOT EXISTS "uuid-ossp";

-- Business Profiles table (replaces and extends companies)
CREATE TABLE IF NOT EXISTS business_profiles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL,
    address TEXT,
    city VARCHAR(100),
    state VARCHAR(100),
    state_code VARCHAR(5),
    pincode VARCHAR(20),
    gstin VARCHAR(15) UNIQUE,
    pan VARCHAR(10),
    email VARCHAR(255),
    phone VARCHAR(20),
    website VARCHAR(255),
    logo_url VARCHAR(500),
    invoice_prefix VARCHAR(20) DEFAULT 'INV',
    invoice_starting_no INT DEFAULT 1,
    gst_return_frequency VARCHAR(50) DEFAULT 'monthly',
    bank_name VARCHAR(100),
    bank_account_no VARCHAR(50),
    bank_ifsc VARCHAR(20),
    bank_branch VARCHAR(100),
    upi_id VARCHAR(50),
    terms_and_conditions TEXT,
    invoice_notes TEXT,
    is_default BOOLEAN DEFAULT false,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE,

    CONSTRAINT valid_business_type CHECK (type IN ('sole_proprietorship', 'partnership', 'llp', 'pvt_ltd', 'public_ltd', 'other')),
    CONSTRAINT valid_gst_frequency CHECK (gst_return_frequency IN ('monthly', 'quarterly', 'annually'))
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_business_profiles_user_default ON business_profiles(user_id) WHERE is_default = true;
CREATE INDEX IF NOT EXISTS idx_business_profiles_user_id ON business_profiles(user_id);
CREATE INDEX IF NOT EXISTS idx_business_profiles_gstin ON business_profiles(gstin) WHERE gstin IS NOT NULL;

-- Customers table (replaces and extends clients)
CREATE TABLE IF NOT EXISTS customers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL DEFAULT 'retail',
    phone VARCHAR(20),
    email VARCHAR(255),
    address TEXT,
    city VARCHAR(100),
    state VARCHAR(100),
    pincode VARCHAR(20),
    gstin VARCHAR(15) UNIQUE,
    pan VARCHAR(10),
    credit_limit DECIMAL(15,2) DEFAULT 0,
    credit_period INT DEFAULT 0,
    balance DECIMAL(15,2) DEFAULT 0,
    is_active BOOLEAN DEFAULT true,
    created_by UUID,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE,

    CONSTRAINT fk_customers_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE,
    CONSTRAINT valid_customer_type CHECK (type IN ('retail', 'wholesale', 'b2b', 'government', 'other'))
);

CREATE INDEX IF NOT EXISTS idx_customers_business_id ON customers(business_id);
CREATE INDEX IF NOT EXISTS idx_customers_name ON customers(name);
CREATE INDEX IF NOT EXISTS idx_customers_type ON customers(type);
CREATE INDEX IF NOT EXISTS idx_customers_gstin ON customers(gstin) WHERE gstin IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_customers_email ON customers(email) WHERE email IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_customers_is_active ON customers(is_active) WHERE is_active = false;

-- Vendors table
CREATE TABLE IF NOT EXISTS vendors (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL DEFAULT 'other',
    phone VARCHAR(20),
    email VARCHAR(255),
    address TEXT,
    city VARCHAR(100),
    state VARCHAR(100),
    pincode VARCHAR(20),
    gstin VARCHAR(15) UNIQUE,
    pan VARCHAR(10),
    credit_limit DECIMAL(15,2) DEFAULT 0,
    credit_period INT DEFAULT 0,
    balance DECIMAL(15,2) DEFAULT 0,
    payment_terms VARCHAR(100),
    bank_account_no VARCHAR(50),
    bank_ifsc VARCHAR(20),
    bank_name VARCHAR(100),
    is_active BOOLEAN DEFAULT true,
    created_by UUID,
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    updated_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,
    deleted_at TIMESTAMP WITH TIME ZONE,

    CONSTRAINT fk_vendors_business FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE,
    CONSTRAINT valid_vendor_type CHECK (type IN ('manufacturer', 'distributor', 'wholesaler', 'retailer', 'service', 'other'))
);

CREATE INDEX IF NOT EXISTS idx_vendors_business_id ON vendors(business_id);
CREATE INDEX IF NOT EXISTS idx_vendors_name ON vendors(name);
CREATE INDEX IF NOT EXISTS idx_vendors_type ON vendors(type);
CREATE INDEX IF NOT EXISTS idx_vendors_gstin ON vendors(gstin) WHERE gstin IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_vendors_email ON vendors(email) WHERE email IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_vendors_is_active ON vendors(is_active) WHERE is_active = false;

-- Customer balance transaction history (for audit trail)
CREATE TABLE IF NOT EXISTS customer_balance_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    customer_id UUID NOT NULL REFERENCES customers(id) ON DELETE CASCADE,
    amount DECIMAL(15,2) NOT NULL,
    balance_before DECIMAL(15,2) NOT NULL,
    balance_after DECIMAL(15,2) NOT NULL,
    transaction_type VARCHAR(50) NOT NULL,
    description TEXT,
    reference_id UUID,
    reference_type VARCHAR(50),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT valid_transaction_type CHECK (transaction_type IN ('credit', 'debit', 'payment_received', 'invoice_created', 'adjustment'))
);

CREATE INDEX IF NOT EXISTS idx_customer_balance_transactions_customer_id ON customer_balance_transactions(customer_id);
CREATE INDEX IF NOT EXISTS idx_customer_balance_transactions_created_at ON customer_balance_transactions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_customer_balance_transactions_reference ON customer_balance_transactions(reference_type, reference_id) WHERE reference_id IS NOT NULL;

-- Vendor balance transaction history
CREATE TABLE IF NOT EXISTS vendor_balance_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    vendor_id UUID NOT NULL REFERENCES vendors(id) ON DELETE CASCADE,
    amount DECIMAL(15,2) NOT NULL,
    balance_before DECIMAL(15,2) NOT NULL,
    balance_after DECIMAL(15,2) NOT NULL,
    transaction_type VARCHAR(50) NOT NULL,
    description TEXT,
    reference_id UUID,
    reference_type VARCHAR(50),
    created_at TIMESTAMP WITH TIME ZONE DEFAULT CURRENT_TIMESTAMP,

    CONSTRAINT valid_vendor_transaction_type CHECK (transaction_type IN ('credit', 'debit', 'payment_made', 'purchase_created', 'adjustment'))
);

CREATE INDEX IF NOT EXISTS idx_vendor_balance_transactions_vendor_id ON vendor_balance_transactions(vendor_id);
CREATE INDEX IF NOT EXISTS idx_vendor_balance_transactions_created_at ON vendor_balance_transactions(created_at DESC);
CREATE INDEX IF NOT EXISTS idx_vendor_balance_transactions_reference ON vendor_balance_transactions(reference_type, reference_id) WHERE reference_id IS NOT NULL;

-- Triggers for updated_at
DROP TRIGGER IF EXISTS business_profiles_updated_at ON business_profiles;
CREATE TRIGGER business_profiles_updated_at BEFORE UPDATE ON business_profiles
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS customers_updated_at ON customers;
CREATE TRIGGER customers_updated_at BEFORE UPDATE ON customers
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

DROP TRIGGER IF EXISTS vendors_updated_at ON vendors;
CREATE TRIGGER vendors_updated_at BEFORE UPDATE ON vendors
    FOR EACH ROW EXECUTE FUNCTION update_updated_at();

-- Function to ensure only one default business profile per user
CREATE OR REPLACE FUNCTION ensure_single_default_business_profile()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.is_default = true THEN
        UPDATE business_profiles
        SET is_default = false
        WHERE user_id = NEW.user_id AND id != NEW.id AND is_default = true;
    END IF;
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

DROP TRIGGER IF EXISTS ensure_single_default_business_profile_trigger ON business_profiles;
CREATE TRIGGER ensure_single_default_business_profile_trigger
    BEFORE INSERT OR UPDATE OF is_default ON business_profiles
    FOR EACH ROW EXECUTE FUNCTION ensure_single_default_business_profile();
