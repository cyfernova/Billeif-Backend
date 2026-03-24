DROP TRIGGER IF EXISTS trigger_procurement_candidates_updated_at ON procurement_candidates;
DROP TRIGGER IF EXISTS trigger_procurement_runs_updated_at ON procurement_runs;
DROP FUNCTION IF EXISTS update_procurement_updated_at();

DROP TABLE IF EXISTS procurement_candidates;
DROP TABLE IF EXISTS procurement_runs;

ALTER TABLE marketplace_products
DROP COLUMN IF EXISTS reserved_inventory_count;
