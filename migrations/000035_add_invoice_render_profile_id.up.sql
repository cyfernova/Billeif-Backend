ALTER TABLE invoices
ADD COLUMN IF NOT EXISTS render_profile_id UUID;

DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1
        FROM pg_constraint
        WHERE conname = 'fk_invoices_render_profile'
    ) THEN
        ALTER TABLE invoices
        ADD CONSTRAINT fk_invoices_render_profile
        FOREIGN KEY (render_profile_id) REFERENCES render_profiles(id) ON DELETE SET NULL;
    END IF;
END $$;

CREATE INDEX IF NOT EXISTS idx_invoices_render_profile_id
ON invoices(render_profile_id)
WHERE deleted_at IS NULL;
