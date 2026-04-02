ALTER TABLE subscriptions
    ADD COLUMN IF NOT EXISTS plan_code VARCHAR(50),
    ADD COLUMN IF NOT EXISTS catalog_version VARCHAR(50);

UPDATE subscriptions
SET
    plan_code = CASE LOWER(COALESCE(plan, 'free'))
        WHEN 'starter' THEN 'pro'
        WHEN 'professional' THEN 'rise'
        WHEN 'enterprise' THEN 'biz'
        ELSE 'free'
    END,
    catalog_version = COALESCE(NULLIF(catalog_version, ''), 'swipe-v1')
WHERE plan_code IS NULL OR catalog_version IS NULL;

CREATE TABLE IF NOT EXISTS roles (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(120) NOT NULL,
    key VARCHAR(120) NOT NULL,
    description TEXT,
    is_system BOOLEAN NOT NULL DEFAULT FALSE,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_roles_business_key
    ON roles (business_id, key)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS role_permissions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    role_id UUID NOT NULL REFERENCES roles(id) ON DELETE CASCADE,
    permission_key VARCHAR(150) NOT NULL,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_role_permissions_role_permission
    ON role_permissions (role_id, permission_key)
    WHERE deleted_at IS NULL;

ALTER TABLE team_members
    ADD COLUMN IF NOT EXISTS role_id UUID REFERENCES roles(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS status VARCHAR(30) NOT NULL DEFAULT 'active',
    ADD COLUMN IF NOT EXISTS branch_scope_json JSONB NOT NULL DEFAULT '[]',
    ADD COLUMN IF NOT EXISTS invited_by UUID,
    ADD COLUMN IF NOT EXISTS is_owner BOOLEAN NOT NULL DEFAULT FALSE;

INSERT INTO roles (business_id, name, key, description, is_system, metadata)
SELECT bp.id, 'Admin', 'admin', 'System administrator', TRUE, '{}'
FROM business_profiles bp
LEFT JOIN roles r
    ON r.business_id = bp.id
   AND r.key = 'admin'
   AND r.deleted_at IS NULL
WHERE r.id IS NULL;

INSERT INTO roles (business_id, name, key, description, is_system, metadata)
SELECT bp.id, 'Accountant', 'accountant', 'Accounting operations', TRUE, '{}'
FROM business_profiles bp
LEFT JOIN roles r
    ON r.business_id = bp.id
   AND r.key = 'accountant'
   AND r.deleted_at IS NULL
WHERE r.id IS NULL;

INSERT INTO roles (business_id, name, key, description, is_system, metadata)
SELECT bp.id, 'Viewer', 'viewer', 'Read-only access', TRUE, '{}'
FROM business_profiles bp
LEFT JOIN roles r
    ON r.business_id = bp.id
   AND r.key = 'viewer'
   AND r.deleted_at IS NULL
WHERE r.id IS NULL;

INSERT INTO role_permissions (role_id, permission_key)
SELECT r.id, seeds.permission_key
FROM roles r
JOIN (
    VALUES
        ('admin', '*'),
        ('accountant', 'documents.manage'),
        ('accountant', 'documents.export'),
        ('accountant', 'storefront.manage'),
        ('accountant', 'storefront.view'),
        ('accountant', 'orders.manage'),
        ('accountant', 'orders.view'),
        ('accountant', 'teams.view'),
        ('accountant', 'branches.view'),
        ('accountant', 'drive.manage'),
        ('accountant', 'drive.view'),
        ('accountant', 'notifications.manage'),
        ('accountant', 'subscriptions.view'),
        ('viewer', 'storefront.view'),
        ('viewer', 'orders.view'),
        ('viewer', 'teams.view'),
        ('viewer', 'branches.view'),
        ('viewer', 'drive.view'),
        ('viewer', 'subscriptions.view')
) AS seeds(role_key, permission_key)
    ON seeds.role_key = r.key
LEFT JOIN role_permissions rp
    ON rp.role_id = r.id
   AND rp.permission_key = seeds.permission_key
   AND rp.deleted_at IS NULL
WHERE rp.id IS NULL;

UPDATE team_members tm
SET
    role_id = r.id,
    status = COALESCE(NULLIF(tm.status, ''), 'active'),
    branch_scope_json = COALESCE(tm.branch_scope_json, '[]'::jsonb)
FROM roles r
WHERE r.business_id = tm.business_id
  AND LOWER(r.key) = LOWER(tm.role)
  AND tm.deleted_at IS NULL
  AND tm.role_id IS NULL;

CREATE TABLE IF NOT EXISTS branches (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(160) NOT NULL,
    code VARCHAR(60) NOT NULL,
    email VARCHAR(255),
    phone VARCHAR(50),
    address VARCHAR(500),
    city VARCHAR(100),
    state VARCHAR(100),
    country VARCHAR(100),
    postal_code VARCHAR(20),
    is_default BOOLEAN NOT NULL DEFAULT FALSE,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_branches_business_code
    ON branches (business_id, code)
    WHERE deleted_at IS NULL;

INSERT INTO branches (business_id, name, code, email, phone, address, city, state, country, postal_code, is_default, metadata)
SELECT
    bp.id,
    bp.name || ' Main Branch',
    'MAIN',
    bp.email,
    bp.phone,
    bp.address,
    bp.city,
    bp.state,
    bp.country,
    bp.postal_code,
    TRUE,
    '{}'
FROM business_profiles bp
LEFT JOIN branches b
    ON b.business_id = bp.id
   AND b.is_default = TRUE
   AND b.deleted_at IS NULL
WHERE b.id IS NULL;

ALTER TABLE warehouses
    ADD COLUMN IF NOT EXISTS branch_id UUID REFERENCES branches(id) ON DELETE SET NULL;

UPDATE warehouses w
SET branch_id = b.id
FROM branches b
WHERE b.business_id = w.business_id
  AND b.is_default = TRUE
  AND b.deleted_at IS NULL
  AND w.branch_id IS NULL
  AND w.deleted_at IS NULL;

ALTER TABLE documents
    ADD COLUMN IF NOT EXISTS branch_id UUID REFERENCES branches(id) ON DELETE SET NULL,
    ADD COLUMN IF NOT EXISTS fx_provider VARCHAR(80),
    ADD COLUMN IF NOT EXISTS fx_base_currency VARCHAR(3),
    ADD COLUMN IF NOT EXISTS fx_quote_currency VARCHAR(3),
    ADD COLUMN IF NOT EXISTS fx_rate_timestamp TIMESTAMP,
    ADD COLUMN IF NOT EXISTS fx_metadata JSONB NOT NULL DEFAULT '{}';

UPDATE documents d
SET branch_id = b.id
FROM branches b
WHERE b.business_id = d.business_id
  AND b.is_default = TRUE
  AND b.deleted_at IS NULL
  AND d.branch_id IS NULL
  AND d.deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS feature_entitlements (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    feature_key VARCHAR(120) NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    limit_value BIGINT,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_feature_entitlements_business_feature
    ON feature_entitlements (business_id, feature_key)
    WHERE deleted_at IS NULL;

WITH plan_source AS (
    SELECT
        bp.id AS business_id,
        COALESCE(s.plan_code,
            CASE LOWER(COALESCE(s.plan, 'free'))
                WHEN 'starter' THEN 'pro'
                WHEN 'professional' THEN 'rise'
                WHEN 'enterprise' THEN 'biz'
                ELSE 'free'
            END
        ) AS plan_code,
        COALESCE(s.max_users, 1) AS max_users,
        COALESCE(s.max_storage_mb, 100) AS max_storage_mb
    FROM business_profiles bp
    LEFT JOIN subscriptions s
        ON s.business_id = bp.id
       AND s.deleted_at IS NULL
)
INSERT INTO feature_entitlements (business_id, feature_key, enabled, limit_value, metadata)
SELECT seeded.business_id, seeded.feature_key, seeded.enabled, seeded.limit_value, '{}'::jsonb
FROM (
    SELECT business_id, 'online_store' AS feature_key, plan_code IN ('pro', 'rise', 'biz') AS enabled, NULL::BIGINT AS limit_value FROM plan_source
    UNION ALL SELECT business_id, 'multi_currency', plan_code IN ('rise', 'biz'), NULL::BIGINT FROM plan_source
    UNION ALL SELECT business_id, 'export_documents', plan_code IN ('pro', 'rise', 'biz'), NULL::BIGINT FROM plan_source
    UNION ALL SELECT business_id, 'sez_documents', plan_code IN ('rise', 'biz'), NULL::BIGINT FROM plan_source
    UNION ALL SELECT business_id, 'deemed_export_documents', plan_code IN ('rise', 'biz'), NULL::BIGINT FROM plan_source
    UNION ALL SELECT business_id, 'multi_user', plan_code <> 'free', max_users FROM plan_source
    UNION ALL SELECT business_id, 'custom_roles', plan_code IN ('rise', 'biz'), NULL::BIGINT FROM plan_source
    UNION ALL SELECT business_id, 'multi_business', plan_code = 'biz', NULL::BIGINT FROM plan_source
    UNION ALL SELECT business_id, 'branches', plan_code IN ('rise', 'biz'), CASE WHEN plan_code = 'biz' THEN 50 ELSE 10 END::BIGINT FROM plan_source
    UNION ALL SELECT business_id, 'priority_support', plan_code = 'biz', NULL::BIGINT FROM plan_source
    UNION ALL SELECT business_id, 'drive_storage_mb', TRUE, CASE WHEN plan_code = 'biz' THEN GREATEST(max_storage_mb, 10240) WHEN plan_code = 'rise' THEN GREATEST(max_storage_mb, 2048) WHEN plan_code = 'pro' THEN GREATEST(max_storage_mb, 512) ELSE GREATEST(max_storage_mb, 100) END::BIGINT FROM plan_source
    UNION ALL SELECT business_id, 'whatsapp_notifications', plan_code IN ('rise', 'biz'), NULL::BIGINT FROM plan_source
) seeded
LEFT JOIN feature_entitlements fe
    ON fe.business_id = seeded.business_id
   AND fe.feature_key = seeded.feature_key
   AND fe.deleted_at IS NULL
WHERE fe.id IS NULL;

CREATE TABLE IF NOT EXISTS storefronts (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(160) NOT NULL,
    slug VARCHAR(160) NOT NULL,
    status VARCHAR(30) NOT NULL DEFAULT 'draft',
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    allow_cod BOOLEAN NOT NULL DEFAULT TRUE,
    allow_online_payment BOOLEAN NOT NULL DEFAULT FALSE,
    auto_invoice_on_paid BOOLEAN NOT NULL DEFAULT TRUE,
    minimum_order_value DECIMAL(15,2) NOT NULL DEFAULT 0,
    settings JSONB NOT NULL DEFAULT '{}',
    blocked_users JSONB NOT NULL DEFAULT '[]',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_storefronts_business_slug
    ON storefronts (business_id, slug)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS storefront_domains (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    storefront_id UUID NOT NULL REFERENCES storefronts(id) ON DELETE CASCADE,
    domain VARCHAR(255) NOT NULL,
    is_primary BOOLEAN NOT NULL DEFAULT FALSE,
    verified_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_storefront_domains_domain
    ON storefront_domains (domain)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS storefront_categories (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    storefront_id UUID NOT NULL REFERENCES storefronts(id) ON DELETE CASCADE,
    name VARCHAR(160) NOT NULL,
    slug VARCHAR(160) NOT NULL,
    description TEXT,
    image_url VARCHAR(500),
    sort_order INT NOT NULL DEFAULT 0,
    seo JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_storefront_categories_slug
    ON storefront_categories (storefront_id, slug)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS storefront_products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    storefront_id UUID NOT NULL REFERENCES storefronts(id) ON DELETE CASCADE,
    product_id UUID NOT NULL REFERENCES products(id) ON DELETE CASCADE,
    category_id UUID REFERENCES storefront_categories(id) ON DELETE SET NULL,
    is_published BOOLEAN NOT NULL DEFAULT FALSE,
    display_price DECIMAL(15,2) NOT NULL DEFAULT 0,
    compare_at_price DECIMAL(15,2) NOT NULL DEFAULT 0,
    sort_order INT NOT NULL DEFAULT 0,
    badge VARCHAR(80),
    seo JSONB NOT NULL DEFAULT '{}',
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_storefront_products_unique
    ON storefront_products (storefront_id, product_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS storefront_coupons (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    storefront_id UUID NOT NULL REFERENCES storefronts(id) ON DELETE CASCADE,
    code VARCHAR(80) NOT NULL,
    discount_type VARCHAR(30) NOT NULL,
    discount_value DECIMAL(15,2) NOT NULL DEFAULT 0,
    minimum_order_value DECIMAL(15,2) NOT NULL DEFAULT 0,
    max_discount_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    usage_limit BIGINT NOT NULL DEFAULT 0,
    usage_limit_per_customer BIGINT NOT NULL DEFAULT 0,
    starts_at TIMESTAMP,
    ends_at TIMESTAMP,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_storefront_coupons_code
    ON storefront_coupons (storefront_id, code)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS fx_rates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    provider VARCHAR(80) NOT NULL,
    base_currency VARCHAR(3) NOT NULL,
    quote_currency VARCHAR(3) NOT NULL,
    rate DECIMAL(18,6) NOT NULL DEFAULT 1,
    fetched_at TIMESTAMP NOT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_fx_rates_pair_fetched
    ON fx_rates (base_currency, quote_currency, fetched_at DESC, deleted_at);

CREATE TABLE IF NOT EXISTS store_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    storefront_id UUID NOT NULL REFERENCES storefronts(id) ON DELETE CASCADE,
    branch_id UUID REFERENCES branches(id) ON DELETE SET NULL,
    customer_id UUID REFERENCES customers(id) ON DELETE SET NULL,
    coupon_id UUID REFERENCES storefront_coupons(id) ON DELETE SET NULL,
    sales_order_id UUID REFERENCES documents(id) ON DELETE SET NULL,
    sales_invoice_id UUID REFERENCES documents(id) ON DELETE SET NULL,
    public_token VARCHAR(120) NOT NULL,
    order_number VARCHAR(80) NOT NULL,
    status VARCHAR(40) NOT NULL DEFAULT 'pending',
    payment_status VARCHAR(40) NOT NULL DEFAULT 'pending',
    payment_method VARCHAR(40),
    currency VARCHAR(3) NOT NULL DEFAULT 'INR',
    exchange_rate DECIMAL(18,6) NOT NULL DEFAULT 1,
    fx_provider VARCHAR(80),
    fx_base_currency VARCHAR(3),
    fx_quote_currency VARCHAR(3),
    fx_rate_timestamp TIMESTAMP,
    subtotal DECIMAL(15,2) NOT NULL DEFAULT 0,
    discount_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    tax_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    shipping_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    total DECIMAL(15,2) NOT NULL DEFAULT 0,
    snapshot JSONB NOT NULL DEFAULT '{}',
    billing_address JSONB NOT NULL DEFAULT '{}',
    shipping_address JSONB NOT NULL DEFAULT '{}',
    notes TEXT,
    idempotency_key VARCHAR(255),
    external_order_id VARCHAR(160),
    external_payment_id VARCHAR(160),
    gateway_order_id VARCHAR(160),
    gateway_payment_id VARCHAR(160),
    webhook_reference VARCHAR(160),
    ordered_at TIMESTAMP NOT NULL DEFAULT NOW(),
    paid_at TIMESTAMP,
    cancelled_at TIMESTAMP,
    cancellation_reason TEXT,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_store_orders_public_token
    ON store_orders (public_token)
    WHERE deleted_at IS NULL;
CREATE UNIQUE INDEX IF NOT EXISTS idx_store_orders_idempotency
    ON store_orders (storefront_id, idempotency_key)
    WHERE deleted_at IS NULL AND idempotency_key IS NOT NULL;
CREATE INDEX IF NOT EXISTS idx_store_orders_storefront_status
    ON store_orders (storefront_id, status, created_at DESC, deleted_at);

CREATE TABLE IF NOT EXISTS store_order_lines (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_order_id UUID NOT NULL REFERENCES store_orders(id) ON DELETE CASCADE,
    product_id UUID REFERENCES products(id) ON DELETE SET NULL,
    variant_id UUID REFERENCES product_variants(id) ON DELETE SET NULL,
    warehouse_id UUID REFERENCES warehouses(id) ON DELETE SET NULL,
    title VARCHAR(255) NOT NULL,
    sku VARCHAR(100),
    quantity DECIMAL(15,3) NOT NULL DEFAULT 0,
    unit_price DECIMAL(15,2) NOT NULL DEFAULT 0,
    discount_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    tax_rate DECIMAL(7,3) NOT NULL DEFAULT 0,
    tax_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    line_total DECIMAL(15,2) NOT NULL DEFAULT 0,
    snapshot JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE TABLE IF NOT EXISTS store_order_events (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    store_order_id UUID NOT NULL REFERENCES store_orders(id) ON DELETE CASCADE,
    event_type VARCHAR(80) NOT NULL,
    status VARCHAR(40),
    payload JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_store_order_events_order_created
    ON store_order_events (store_order_id, created_at DESC, deleted_at);

CREATE TABLE IF NOT EXISTS storefront_coupon_redemptions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    storefront_coupon_id UUID NOT NULL REFERENCES storefront_coupons(id) ON DELETE CASCADE,
    store_order_id UUID REFERENCES store_orders(id) ON DELETE SET NULL,
    customer_id UUID REFERENCES customers(id) ON DELETE SET NULL,
    customer_email VARCHAR(255),
    discount_amount DECIMAL(15,2) NOT NULL DEFAULT 0,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_storefront_coupon_redemptions_coupon_customer
    ON storefront_coupon_redemptions (storefront_coupon_id, customer_id, customer_email, deleted_at);

CREATE TABLE IF NOT EXISTS drive_assets (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    uploaded_by UUID,
    name VARCHAR(255) NOT NULL,
    bucket VARCHAR(255) NOT NULL,
    object_key VARCHAR(500) NOT NULL,
    content_type VARCHAR(120),
    size_bytes BIGINT NOT NULL DEFAULT 0,
    category VARCHAR(80),
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_drive_assets_business_created
    ON drive_assets (business_id, created_at DESC, deleted_at);

INSERT INTO drive_assets (business_id, name, bucket, object_key, content_type, size_bytes, category, metadata)
SELECT bp.id, bp.name || ' Logo', '', bp.logo_key, 'image/*', 0, 'logo', '{"backfilled":true}'::jsonb
FROM business_profiles bp
WHERE COALESCE(bp.logo_key, '') <> ''
  AND NOT EXISTS (
      SELECT 1 FROM drive_assets da
      WHERE da.business_id = bp.id
        AND da.object_key = bp.logo_key
        AND da.deleted_at IS NULL
  );

INSERT INTO drive_assets (business_id, name, bucket, object_key, content_type, size_bytes, category, metadata)
SELECT p.business_id, p.name, '', p.image_key, 'image/*', 0, 'product_image', '{"backfilled":true}'::jsonb
FROM products p
WHERE COALESCE(p.image_key, '') <> ''
  AND NOT EXISTS (
      SELECT 1 FROM drive_assets da
      WHERE da.business_id = p.business_id
        AND da.object_key = p.image_key
        AND da.deleted_at IS NULL
  );

CREATE TABLE IF NOT EXISTS whatsapp_configs (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    phone_number_id VARCHAR(120),
    access_token TEXT,
    webhook_secret TEXT,
    verify_token VARCHAR(255),
    default_recipient VARCHAR(50),
    enabled BOOLEAN NOT NULL DEFAULT FALSE,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_whatsapp_configs_business
    ON whatsapp_configs (business_id)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS notification_templates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    channel VARCHAR(40) NOT NULL,
    event_key VARCHAR(120) NOT NULL,
    name VARCHAR(160) NOT NULL,
    language_code VARCHAR(20) NOT NULL DEFAULT 'en',
    body TEXT NOT NULL,
    enabled BOOLEAN NOT NULL DEFAULT TRUE,
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE UNIQUE INDEX IF NOT EXISTS idx_notification_templates_business_event_channel
    ON notification_templates (business_id, channel, event_key)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS notification_deliveries (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    store_order_id UUID REFERENCES store_orders(id) ON DELETE SET NULL,
    channel VARCHAR(40) NOT NULL,
    event_key VARCHAR(120) NOT NULL,
    recipient VARCHAR(120),
    status VARCHAR(40) NOT NULL DEFAULT 'queued',
    request_payload JSONB NOT NULL DEFAULT '{}',
    response_payload JSONB NOT NULL DEFAULT '{}',
    external_message_id VARCHAR(160),
    attempt_count INT NOT NULL DEFAULT 0,
    last_attempt_at TIMESTAMP,
    delivered_at TIMESTAMP,
    created_at TIMESTAMP NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMP NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_notification_deliveries_business_created
    ON notification_deliveries (business_id, created_at DESC, deleted_at);
