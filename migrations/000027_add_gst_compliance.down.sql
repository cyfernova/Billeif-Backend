DROP TABLE IF EXISTS gstr2b_match_results;
DROP TABLE IF EXISTS gstr2b_import_lines;
DROP TABLE IF EXISTS gstr2b_imports;
DROP TABLE IF EXISTS gst_report_runs;
DROP TABLE IF EXISTS payment_withholdings;
DROP TABLE IF EXISTS document_withholdings;
DROP TABLE IF EXISTS tax_section_master;

ALTER TABLE document_lines
    DROP COLUMN IF EXISTS report_tags,
    DROP COLUMN IF EXISTS uqc_code;

ALTER TABLE documents
    DROP COLUMN IF EXISTS report_tags,
    DROP COLUMN IF EXISTS tcs_total,
    DROP COLUMN IF EXISTS tds_total,
    DROP COLUMN IF EXISTS withholding_total,
    DROP COLUMN IF EXISTS bill_of_supply,
    DROP COLUMN IF EXISTS export_type,
    DROP COLUMN IF EXISTS supply_type,
    DROP COLUMN IF EXISTS party_state_code,
    DROP COLUMN IF EXISTS party_pan,
    DROP COLUMN IF EXISTS party_gstin;

ALTER TABLE payments
    DROP COLUMN IF EXISTS withholding_data,
    DROP COLUMN IF EXISTS payment_type;

ALTER TABLE invoices
    DROP COLUMN IF EXISTS tax_profile;

ALTER TABLE products
    DROP COLUMN IF EXISTS gst_metadata,
    DROP COLUMN IF EXISTS uqc_code;

ALTER TABLE vendors
    DROP COLUMN IF EXISTS withholding_defaults_json,
    DROP COLUMN IF EXISTS shipping_address_json,
    DROP COLUMN IF EXISTS billing_address_json,
    DROP COLUMN IF EXISTS state_code,
    DROP COLUMN IF EXISTS company_name,
    DROP COLUMN IF EXISTS pan,
    DROP COLUMN IF EXISTS gstin;

ALTER TABLE customers
    DROP COLUMN IF EXISTS withholding_defaults_json,
    DROP COLUMN IF EXISTS shipping_address_json,
    DROP COLUMN IF EXISTS billing_address_json,
    DROP COLUMN IF EXISTS state_code,
    DROP COLUMN IF EXISTS company_name,
    DROP COLUMN IF EXISTS pan,
    DROP COLUMN IF EXISTS gstin;

ALTER TABLE business_profiles
    DROP COLUMN IF EXISTS tax_preferences_json,
    DROP COLUMN IF EXISTS gst_tds_enabled,
    DROP COLUMN IF EXISTS gst_registered,
    DROP COLUMN IF EXISTS gst_filing_frequency;
