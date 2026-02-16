-- Migration: add_missing_indexes
-- Created at: 20260216213837
-- Description: Add missing indexes for frequently filtered flags

CREATE INDEX IF NOT EXISTS idx_products_is_active
ON products(is_active)
WHERE deleted_at IS NULL;

CREATE INDEX IF NOT EXISTS idx_webhooks_is_active
ON webhooks(is_active)
WHERE deleted_at IS NULL;
