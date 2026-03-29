ALTER TABLE products
    ADD COLUMN IF NOT EXISTS category_id UUID,
    ADD COLUMN IF NOT EXISTS barcode VARCHAR(128),
    ADD COLUMN IF NOT EXISTS low_stock_threshold BIGINT NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS extra_attributes JSONB NOT NULL DEFAULT '{}';

CREATE INDEX IF NOT EXISTS idx_products_category ON products (category_id);
CREATE INDEX IF NOT EXISTS idx_products_barcode ON products (barcode) WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS product_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    parent_id UUID REFERENCES product_categories(id) ON DELETE SET NULL,
    name VARCHAR(120) NOT NULL,
    slug VARCHAR(140) NOT NULL,
    sort_order INT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_product_categories_slug
    ON product_categories (business_id, slug)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS product_images (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    variant_id UUID,
    url VARCHAR(500) NOT NULL,
    key VARCHAR(255),
    alt_text VARCHAR(255),
    position INT NOT NULL DEFAULT 0,
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_product_images_product ON product_images (product_id, position, deleted_at);
CREATE INDEX IF NOT EXISTS idx_product_images_variant ON product_images (variant_id, position, deleted_at);

CREATE TABLE IF NOT EXISTS product_variants (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    name VARCHAR(150) NOT NULL,
    sku VARCHAR(100) NOT NULL,
    barcode VARCHAR(128),
    attributes JSONB NOT NULL DEFAULT '{}',
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    track_batches BOOLEAN NOT NULL DEFAULT FALSE,
    track_serials BOOLEAN NOT NULL DEFAULT FALSE,
    price DECIMAL(15,2) NOT NULL DEFAULT 0,
    cost_price DECIMAL(15,2) NOT NULL DEFAULT 0,
    stock_level DECIMAL(15,3) NOT NULL DEFAULT 0,
    reserved_level DECIMAL(15,3) NOT NULL DEFAULT 0,
    low_stock_threshold DECIMAL(15,3) NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_product_variants_default
    ON product_variants (product_id, is_default)
    WHERE deleted_at IS NULL AND is_default = TRUE;
CREATE INDEX IF NOT EXISTS idx_product_variants_product ON product_variants (product_id, deleted_at);
CREATE INDEX IF NOT EXISTS idx_product_variants_sku ON product_variants (business_id, sku, deleted_at);
CREATE INDEX IF NOT EXISTS idx_product_variants_barcode ON product_variants (business_id, barcode, deleted_at);

CREATE TABLE IF NOT EXISTS product_custom_columns (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(120) NOT NULL,
    slug VARCHAR(140) NOT NULL,
    data_type VARCHAR(40) NOT NULL,
    options JSONB NOT NULL DEFAULT '[]',
    is_required BOOLEAN NOT NULL DEFAULT FALSE,
    sort_order INT NOT NULL DEFAULT 0,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_product_custom_columns_slug
    ON product_custom_columns (business_id, slug)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS product_custom_values (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    column_id UUID NOT NULL REFERENCES product_custom_columns(id) ON DELETE CASCADE,
    value JSONB NOT NULL DEFAULT 'null',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_product_custom_values_product_column
    ON product_custom_values (product_id, column_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS warehouse_permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    can_view_catalog BOOLEAN NOT NULL DEFAULT TRUE,
    can_manage_catalog BOOLEAN NOT NULL DEFAULT FALSE,
    can_move_stock BOOLEAN NOT NULL DEFAULT FALSE,
    can_view_reports BOOLEAN NOT NULL DEFAULT FALSE,
    can_manage_warehouse BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_warehouse_permissions_unique
    ON warehouse_permissions (warehouse_id, user_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS product_warehouse_catalogs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    is_visible BOOLEAN NOT NULL DEFAULT TRUE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    price_override DECIMAL(15,2),
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_product_warehouse_catalogs_unique
    ON product_warehouse_catalogs (product_id, warehouse_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS product_batches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    variant_id UUID NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    batch_number VARCHAR(120) NOT NULL,
    manufactured_at TIMESTAMP,
    expires_at TIMESTAMP,
    extra_fields JSONB NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_product_batches_unique
    ON product_batches (business_id, variant_id, batch_number)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS product_serial_numbers (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    variant_id UUID NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    batch_id UUID REFERENCES product_batches(id) ON DELETE SET NULL,
    warehouse_id UUID REFERENCES warehouses(id) ON DELETE SET NULL,
    serial_number VARCHAR(160) NOT NULL,
    imei VARCHAR(160),
    status VARCHAR(40) NOT NULL DEFAULT 'available',
    sold_document_id UUID REFERENCES documents(id) ON DELETE SET NULL,
    sold_document_line_id UUID REFERENCES document_lines(id) ON DELETE SET NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_product_serial_numbers_serial
    ON product_serial_numbers (business_id, serial_number)
    WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_product_serial_numbers_imei
    ON product_serial_numbers (business_id, imei)
    WHERE deleted_at IS NULL AND imei IS NOT NULL;

CREATE TABLE IF NOT EXISTS inventory_balances (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    variant_id UUID NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    batch_id UUID REFERENCES product_batches(id) ON DELETE SET NULL,
    batch_key VARCHAR(64) NOT NULL DEFAULT '',
    on_hand DECIMAL(15,3) NOT NULL DEFAULT 0,
    reserved DECIMAL(15,3) NOT NULL DEFAULT 0,
    stock_value DECIMAL(15,2) NOT NULL DEFAULT 0,
    last_recorded_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_inventory_balances_unique
    ON inventory_balances (business_id, product_id, variant_id, warehouse_id, batch_key)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS inventory_snapshots (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    snapshot_date TIMESTAMP NOT NULL,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    variant_id UUID NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    warehouse_id UUID NOT NULL REFERENCES warehouses(id) ON DELETE CASCADE,
    on_hand DECIMAL(15,3) NOT NULL DEFAULT 0,
    reserved DECIMAL(15,3) NOT NULL DEFAULT 0,
    stock_value DECIMAL(15,2) NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_inventory_snapshots_scope
    ON inventory_snapshots (business_id, snapshot_date, warehouse_id, product_id, variant_id);

CREATE TABLE IF NOT EXISTS assembly_recipes (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(150) NOT NULL,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    variant_id UUID NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    output_quantity DECIMAL(15,3) NOT NULL DEFAULT 1,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS assembly_recipe_components (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    recipe_id UUID NOT NULL REFERENCES assembly_recipes(id) ON DELETE CASCADE,
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    component_product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    component_variant_id UUID NOT NULL REFERENCES product_variants(id) ON DELETE CASCADE,
    quantity DECIMAL(15,3) NOT NULL DEFAULT 1,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_assembly_recipe_components_recipe
    ON assembly_recipe_components (recipe_id, deleted_at);

CREATE TABLE IF NOT EXISTS inventory_event_logs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    event_type VARCHAR(80) NOT NULL,
    entity_type VARCHAR(80),
    entity_id VARCHAR(80),
    payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_inventory_event_logs_business_event
    ON inventory_event_logs (business_id, event_type, created_at DESC);

ALTER TABLE stock_moves
    ADD COLUMN IF NOT EXISTS variant_id UUID REFERENCES product_variants(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS source_warehouse_id UUID REFERENCES warehouses(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS batch_id UUID REFERENCES product_batches(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS serial_number_id UUID REFERENCES product_serial_numbers(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS transaction_type VARCHAR(40) NOT NULL DEFAULT 'adjustment',
    ADD COLUMN IF NOT EXISTS unit_cost DECIMAL(15,2) NOT NULL DEFAULT 0;

CREATE INDEX IF NOT EXISTS idx_stock_moves_variant ON stock_moves (variant_id, recorded_at DESC);
CREATE INDEX IF NOT EXISTS idx_stock_moves_transaction_type ON stock_moves (transaction_type, recorded_at DESC);

ALTER TABLE document_lines
    ADD COLUMN IF NOT EXISTS variant_id UUID REFERENCES product_variants(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS batch_allocations JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS serial_ids JSONB NOT NULL DEFAULT '[]';

ALTER TABLE invoice_items
    ADD COLUMN IF NOT EXISTS warehouse_id UUID REFERENCES warehouses(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS variant_id UUID REFERENCES product_variants(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS batch_allocations JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS serial_ids JSONB NOT NULL DEFAULT '[]';

INSERT INTO warehouses (id, business_id, name, code, is_default, created_at, updated_at)
SELECT gen_random_uuid(), b.id, 'Main Warehouse', 'MAIN', TRUE, NOW(), NOW()
FROM business_profiles b
WHERE NOT EXISTS (
    SELECT 1 FROM warehouses w
    WHERE w.business_id = b.id AND w.deleted_at IS NULL
);

UPDATE products
SET low_stock_threshold = COALESCE(NULLIF(low_stock_threshold, 0), min_stock),
    barcode = COALESCE(NULLIF(barcode, ''), NULLIF(sku, ''))
WHERE deleted_at IS NULL;

INSERT INTO product_variants (
    id, business_id, product_id, name, sku, barcode, attributes, is_default,
    track_batches, track_serials, price, cost_price, stock_level, reserved_level,
    low_stock_threshold, is_active, created_at, updated_at
)
SELECT
    gen_random_uuid(),
    p.business_id,
    p.id,
    'Default',
    p.sku,
    COALESCE(NULLIF(p.barcode, ''), NULLIF(p.sku, '')),
    '{}'::jsonb,
    TRUE,
    FALSE,
    FALSE,
    p.price,
    p.cost_price,
    p.stock_level,
    0,
    COALESCE(NULLIF(p.low_stock_threshold, 0), p.min_stock),
    p.is_active,
    COALESCE(p.created_at, NOW()),
    COALESCE(p.updated_at, NOW())
FROM products p
WHERE p.deleted_at IS NULL
AND NOT EXISTS (
    SELECT 1 FROM product_variants v
    WHERE v.product_id = p.id AND v.deleted_at IS NULL
);

INSERT INTO inventory_balances (
    id, business_id, product_id, variant_id, warehouse_id, batch_key,
    on_hand, reserved, stock_value, last_recorded_at, created_at, updated_at
)
SELECT
    gen_random_uuid(),
    p.business_id,
    p.id,
    v.id,
    w.id,
    '',
    p.stock_level,
    0,
    p.stock_level * COALESCE(NULLIF(v.cost_price, 0), p.cost_price, 0),
    NOW(),
    NOW(),
    NOW()
FROM products p
JOIN product_variants v
  ON v.product_id = p.id AND v.is_default = TRUE AND v.deleted_at IS NULL
JOIN warehouses w
  ON w.business_id = p.business_id AND w.is_default = TRUE AND w.deleted_at IS NULL
WHERE p.deleted_at IS NULL
  AND p.stock_level <> 0
  AND NOT EXISTS (
      SELECT 1 FROM inventory_balances ib
      WHERE ib.product_id = p.id
        AND ib.variant_id = v.id
        AND ib.warehouse_id = w.id
        AND ib.batch_key = ''
        AND ib.deleted_at IS NULL
  );

INSERT INTO stock_moves (
    id, business_id, product_id, variant_id, warehouse_id, direction, quantity,
    transaction_type, unit_cost, reason, metadata, recorded_at, created_at, updated_at
)
SELECT
    gen_random_uuid(),
    p.business_id,
    p.id,
    v.id,
    w.id,
    'in',
    p.stock_level,
    'opening_balance',
    COALESCE(NULLIF(v.cost_price, 0), p.cost_price, 0),
    'legacy opening balance backfill',
    jsonb_build_object('source', 'migration_000028')::jsonb,
    NOW(),
    NOW(),
    NOW()
FROM products p
JOIN product_variants v
  ON v.product_id = p.id AND v.is_default = TRUE AND v.deleted_at IS NULL
JOIN warehouses w
  ON w.business_id = p.business_id AND w.is_default = TRUE AND w.deleted_at IS NULL
WHERE p.deleted_at IS NULL
  AND p.stock_level > 0;
