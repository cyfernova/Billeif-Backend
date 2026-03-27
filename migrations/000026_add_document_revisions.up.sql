CREATE TABLE IF NOT EXISTS document_revisions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    document_id UUID NOT NULL REFERENCES documents(id) ON DELETE CASCADE,
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    action VARCHAR(60) NOT NULL,
    snapshot JSONB NOT NULL DEFAULT '{}'::jsonb,
    metadata JSONB NOT NULL DEFAULT '{}'::jsonb,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_document_revisions_document_id ON document_revisions(document_id);
CREATE INDEX IF NOT EXISTS idx_document_revisions_business_id ON document_revisions(business_id);
CREATE INDEX IF NOT EXISTS idx_document_revisions_action ON document_revisions(action);
CREATE INDEX IF NOT EXISTS idx_document_revisions_created_at ON document_revisions(created_at DESC);

CREATE TRIGGER update_document_revisions_updated_at
    BEFORE UPDATE ON document_revisions
    FOR EACH ROW
    EXECUTE FUNCTION update_updated_at_column();
