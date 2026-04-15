DROP INDEX IF EXISTS idx_invoices_render_profile_id;

ALTER TABLE invoices
DROP CONSTRAINT IF EXISTS fk_invoices_render_profile;

ALTER TABLE invoices
DROP COLUMN IF EXISTS render_profile_id;
