DROP TABLE IF EXISTS party_addresses;
DROP TABLE IF EXISTS charge_definitions;
DROP TABLE IF EXISTS custom_field_values;
DROP TABLE IF EXISTS custom_field_definitions;
DROP TABLE IF EXISTS signed_document_artifacts;
DROP TABLE IF EXISTS signature_profiles;
DROP TABLE IF EXISTS activity_logs;
DROP TABLE IF EXISTS bulk_job_artifacts;
DROP TABLE IF EXISTS bulk_job_rows;
DROP TABLE IF EXISTS bulk_jobs;
DROP TABLE IF EXISTS party_group_members;
DROP TABLE IF EXISTS party_groups;
DROP TABLE IF EXISTS price_list_assignments;
DROP TABLE IF EXISTS price_list_items;
DROP TABLE IF EXISTS price_lists;
DROP TABLE IF EXISTS invoice_subscription_runs;
DROP TABLE IF EXISTS invoice_subscription_lines;
DROP TABLE IF EXISTS invoice_subscriptions;

ALTER TABLE stock_moves
    DROP COLUMN IF EXISTS actor_role,
    DROP COLUMN IF EXISTS actor_id,
    DROP COLUMN IF EXISTS unit_cost,
    DROP COLUMN IF EXISTS transaction_type,
    DROP COLUMN IF EXISTS serial_number_id,
    DROP COLUMN IF EXISTS batch_id,
    DROP COLUMN IF EXISTS project_id,
    DROP COLUMN IF EXISTS source_warehouse_id,
    DROP COLUMN IF EXISTS variant_id;

ALTER TABLE document_lines
    DROP COLUMN IF EXISTS report_tags,
    DROP COLUMN IF EXISTS serial_ids,
    DROP COLUMN IF EXISTS batch_allocations,
    DROP COLUMN IF EXISTS charge_linkage,
    DROP COLUMN IF EXISTS custom_fields,
    DROP COLUMN IF EXISTS mrp,
    DROP COLUMN IF EXISTS uqc_code,
    DROP COLUMN IF EXISTS variant_id;

ALTER TABLE documents
    DROP COLUMN IF EXISTS sign_metadata,
    DROP COLUMN IF EXISTS signed_by_profile_id,
    DROP COLUMN IF EXISTS signed_at,
    DROP COLUMN IF EXISTS report_tags,
    DROP COLUMN IF EXISTS tcs_total,
    DROP COLUMN IF EXISTS tds_total,
    DROP COLUMN IF EXISTS withholding_total,
    DROP COLUMN IF EXISTS origin_run_id,
    DROP COLUMN IF EXISTS origin_subscription_id,
    DROP COLUMN IF EXISTS price_list_id,
    DROP COLUMN IF EXISTS project_id,
    DROP COLUMN IF EXISTS bill_of_supply,
    DROP COLUMN IF EXISTS export_type,
    DROP COLUMN IF EXISTS supply_type,
    DROP COLUMN IF EXISTS party_state_code,
    DROP COLUMN IF EXISTS party_pan;

ALTER TABLE invoice_items
    DROP COLUMN IF EXISTS serial_ids,
    DROP COLUMN IF EXISTS batch_allocations,
    DROP COLUMN IF EXISTS charge_snapshot,
    DROP COLUMN IF EXISTS custom_fields,
    DROP COLUMN IF EXISTS cess_amount,
    DROP COLUMN IF EXISTS cess_rate,
    DROP COLUMN IF EXISTS mrp,
    DROP COLUMN IF EXISTS free_quantity,
    DROP COLUMN IF EXISTS warehouse_id,
    DROP COLUMN IF EXISTS variant_id;

ALTER TABLE invoices
    DROP COLUMN IF EXISTS sign_metadata,
    DROP COLUMN IF EXISTS signed_by_profile_id,
    DROP COLUMN IF EXISTS signed_at,
    DROP COLUMN IF EXISTS origin_run_id,
    DROP COLUMN IF EXISTS origin_subscription_id,
    DROP COLUMN IF EXISTS additional_charges,
    DROP COLUMN IF EXISTS custom_fields,
    DROP COLUMN IF EXISTS price_list_id;

ALTER TABLE team_members
    DROP COLUMN IF EXISTS permission_overrides;

ALTER TABLE vendors
    DROP COLUMN IF EXISTS preferences_json,
    DROP COLUMN IF EXISTS default_price_list_id;

ALTER TABLE customers
    DROP COLUMN IF EXISTS preferences_json,
    DROP COLUMN IF EXISTS default_price_list_id;

ALTER TABLE product_warehouse_catalogs
    DROP COLUMN IF EXISTS price_list_id;

ALTER TABLE product_variants
    DROP COLUMN IF EXISTS default_cess_rate,
    DROP COLUMN IF EXISTS mrp;

ALTER TABLE products
    DROP COLUMN IF EXISTS default_price_list_id,
    DROP COLUMN IF EXISTS default_cess_rate,
    DROP COLUMN IF EXISTS mrp;
