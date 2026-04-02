DROP TABLE IF EXISTS notification_deliveries;
DROP TABLE IF EXISTS notification_templates;
DROP TABLE IF EXISTS whatsapp_configs;
DROP TABLE IF EXISTS drive_assets;
DROP TABLE IF EXISTS storefront_coupon_redemptions;
DROP TABLE IF EXISTS store_order_events;
DROP TABLE IF EXISTS store_order_lines;
DROP TABLE IF EXISTS store_orders;
DROP TABLE IF EXISTS fx_rates;
DROP TABLE IF EXISTS storefront_coupons;
DROP TABLE IF EXISTS storefront_products;
DROP TABLE IF EXISTS storefront_categories;
DROP TABLE IF EXISTS storefront_domains;
DROP TABLE IF EXISTS storefronts;
DROP TABLE IF EXISTS feature_entitlements;

ALTER TABLE documents
    DROP COLUMN IF EXISTS fx_metadata,
    DROP COLUMN IF EXISTS fx_rate_timestamp,
    DROP COLUMN IF EXISTS fx_quote_currency,
    DROP COLUMN IF EXISTS fx_base_currency,
    DROP COLUMN IF EXISTS fx_provider,
    DROP COLUMN IF EXISTS branch_id;

ALTER TABLE warehouses
    DROP COLUMN IF EXISTS branch_id;

DROP TABLE IF EXISTS branches;

ALTER TABLE team_members
    DROP COLUMN IF EXISTS is_owner,
    DROP COLUMN IF EXISTS invited_by,
    DROP COLUMN IF EXISTS branch_scope_json,
    DROP COLUMN IF EXISTS status,
    DROP COLUMN IF EXISTS role_id;

DROP TABLE IF EXISTS role_permissions;
DROP TABLE IF EXISTS roles;

ALTER TABLE subscriptions
    DROP COLUMN IF EXISTS catalog_version,
    DROP COLUMN IF EXISTS plan_code;
