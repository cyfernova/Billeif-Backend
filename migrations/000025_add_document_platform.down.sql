DROP TRIGGER IF EXISTS trigger_shipping_labels_updated_at ON shipping_labels;
DROP TRIGGER IF EXISTS trigger_shipments_updated_at ON shipments;
DROP TRIGGER IF EXISTS trigger_journal_lines_updated_at ON journal_lines;
DROP TRIGGER IF EXISTS trigger_journals_updated_at ON journals;
DROP TRIGGER IF EXISTS trigger_inventory_reservations_updated_at ON inventory_reservations;
DROP TRIGGER IF EXISTS trigger_stock_moves_updated_at ON stock_moves;
DROP TRIGGER IF EXISTS trigger_warehouses_updated_at ON warehouses;
DROP TRIGGER IF EXISTS trigger_document_render_jobs_updated_at ON document_render_jobs;
DROP TRIGGER IF EXISTS trigger_render_profiles_updated_at ON render_profiles;
DROP TRIGGER IF EXISTS trigger_document_lines_updated_at ON document_lines;
DROP TRIGGER IF EXISTS trigger_documents_updated_at ON documents;
DROP FUNCTION IF EXISTS update_document_platform_updated_at;

DROP TABLE IF EXISTS shipping_labels CASCADE;
DROP TABLE IF EXISTS shipments CASCADE;
DROP TABLE IF EXISTS journal_lines CASCADE;
DROP TABLE IF EXISTS journals CASCADE;
DROP TABLE IF EXISTS inventory_reservations CASCADE;
DROP TABLE IF EXISTS stock_moves CASCADE;
DROP TABLE IF EXISTS warehouses CASCADE;
DROP TABLE IF EXISTS document_render_jobs CASCADE;
DROP TABLE IF EXISTS render_profiles CASCADE;
DROP TABLE IF EXISTS document_links CASCADE;
DROP TABLE IF EXISTS document_lines CASCADE;
DROP TABLE IF EXISTS documents CASCADE;

ALTER TABLE products
    DROP COLUMN IF EXISTS is_service,
    DROP COLUMN IF EXISTS hsn_sac_code,
    DROP COLUMN IF EXISTS valuation_method,
    DROP COLUMN IF EXISTS cost_price;

ALTER TABLE business_profiles
    DROP COLUMN IF EXISTS numbering_rules,
    DROP COLUMN IF EXISTS sez_enabled,
    DROP COLUMN IF EXISTS export_lut_enabled,
    DROP COLUMN IF EXISTS default_gst_treatment,
    DROP COLUMN IF EXISTS composition_enabled,
    DROP COLUMN IF EXISTS business_state_code,
    DROP COLUMN IF EXISTS gstin;
