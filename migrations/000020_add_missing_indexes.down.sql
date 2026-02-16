-- Migration: add_missing_indexes (rollback)
-- Created at: 20260216213837
-- Description: Rollback for add_missing_indexes

DROP INDEX IF EXISTS idx_webhooks_is_active;
DROP INDEX IF EXISTS idx_products_is_active;
