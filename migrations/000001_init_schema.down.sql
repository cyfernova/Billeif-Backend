-- Rollback migration for invoice_backend

-- Drop triggers
DROP TRIGGER IF EXISTS users_updated_at ON users;
DROP TRIGGER IF EXISTS companies_updated_at ON companies;
DROP TRIGGER IF EXISTS clients_updated_at ON clients;
DROP TRIGGER IF EXISTS invoices_updated_at ON invoices;
DROP TRIGGER IF EXISTS invoice_items_updated_at ON invoice_items;

-- Drop function
DROP FUNCTION IF EXISTS update_updated_at();

-- Drop tables
DROP TABLE IF EXISTS invoice_items;
DROP TABLE IF EXISTS invoices;
DROP TABLE IF EXISTS clients;
DROP TABLE IF EXISTS companies;
DROP TABLE IF EXISTS users;

-- Drop extension
DROP EXTENSION IF NOT EXISTS "uuid-ossp";
