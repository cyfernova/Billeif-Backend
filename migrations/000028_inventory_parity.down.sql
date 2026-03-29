ALTER TABLE invoice_items
    DROP COLUMN IF EXISTS serial_ids,
    DROP COLUMN IF EXISTS batch_allocations,
    DROP COLUMN IF EXISTS variant_id,
    DROP COLUMN IF EXISTS warehouse_id;

ALTER TABLE document_lines
    DROP COLUMN IF EXISTS serial_ids,
    DROP COLUMN IF EXISTS batch_allocations,
    DROP COLUMN IF EXISTS variant_id;

DROP INDEX IF EXISTS idx_stock_moves_transaction_type;
DROP INDEX IF EXISTS idx_stock_moves_variant;

ALTER TABLE stock_moves
    DROP COLUMN IF EXISTS unit_cost,
    DROP COLUMN IF EXISTS transaction_type,
    DROP COLUMN IF EXISTS serial_number_id,
    DROP COLUMN IF EXISTS batch_id,
    DROP COLUMN IF EXISTS source_warehouse_id,
    DROP COLUMN IF EXISTS variant_id;

DROP TABLE IF EXISTS inventory_event_logs CASCADE;
DROP TABLE IF EXISTS assembly_recipe_components CASCADE;
DROP TABLE IF EXISTS assembly_recipes CASCADE;
DROP TABLE IF EXISTS inventory_snapshots CASCADE;
DROP TABLE IF EXISTS inventory_balances CASCADE;
DROP TABLE IF EXISTS product_serial_numbers CASCADE;
DROP TABLE IF EXISTS product_batches CASCADE;
DROP TABLE IF EXISTS product_warehouse_catalogs CASCADE;
DROP TABLE IF EXISTS warehouse_permissions CASCADE;
DROP TABLE IF EXISTS product_custom_values CASCADE;
DROP TABLE IF EXISTS product_custom_columns CASCADE;
DROP TABLE IF EXISTS product_variants CASCADE;
DROP TABLE IF EXISTS product_images CASCADE;
DROP TABLE IF EXISTS product_categories CASCADE;

DROP INDEX IF EXISTS idx_products_barcode;
DROP INDEX IF EXISTS idx_products_category;

ALTER TABLE products
    DROP COLUMN IF EXISTS extra_attributes,
    DROP COLUMN IF EXISTS low_stock_threshold,
    DROP COLUMN IF EXISTS barcode,
    DROP COLUMN IF EXISTS category_id;
