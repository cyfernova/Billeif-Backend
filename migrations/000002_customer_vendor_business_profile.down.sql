-- Rollback Migration: 000002_customer_vendor_business_profile

-- Drop triggers
DROP TRIGGER IF EXISTS business_profiles_updated_at ON business_profiles;
DROP TRIGGER IF EXISTS customers_updated_at ON customers;
DROP TRIGGER IF EXISTS vendors_updated_at ON vendors;
DROP TRIGGER IF EXISTS ensure_single_default_business_profile_trigger ON business_profiles;

-- Drop function
DROP FUNCTION IF EXISTS ensure_single_default_business_profile();

-- Drop tables in correct order (respecting foreign key dependencies)
DROP TABLE IF EXISTS vendor_balance_transactions CASCADE;
DROP TABLE IF EXISTS customer_balance_transactions CASCADE;
DROP TABLE IF EXISTS vendors CASCADE;
DROP TABLE IF EXISTS customers CASCADE;
DROP TABLE IF EXISTS business_profiles CASCADE;
