ALTER TABLE bargaining_negotiations
    ADD COLUMN business_id UUID;

CREATE FUNCTION populate_bargaining_business_scope()
RETURNS TRIGGER AS $$
BEGIN
    IF NEW.business_id IS NULL THEN
        SELECT business_id INTO NEW.business_id
        FROM agents
        WHERE id = NEW.buyer_agent_id;
    END IF;

    IF NEW.business_id IS NULL THEN
        RAISE EXCEPTION 'cannot create bargaining negotiation: buyer business_id is missing';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_populate_bargaining_business_scope
BEFORE INSERT ON bargaining_negotiations
FOR EACH ROW
EXECUTE FUNCTION populate_bargaining_business_scope();

UPDATE bargaining_negotiations AS negotiation
SET business_id = buyer.business_id
FROM agents AS buyer
WHERE buyer.id = negotiation.buyer_agent_id
  AND negotiation.business_id IS NULL;

DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM bargaining_negotiations
        WHERE business_id IS NULL
    ) THEN
        RAISE EXCEPTION 'cannot scope bargaining negotiations: buyer business_id is missing';
    END IF;

    IF EXISTS (
        SELECT session_id
        FROM bargaining_negotiations
        WHERE session_id IS NOT NULL
        GROUP BY session_id
        HAVING COUNT(*) > 1
    ) THEN
        RAISE EXCEPTION 'cannot scope bargaining negotiations: duplicate session_id exists';
    END IF;
END
$$;

ALTER TABLE bargaining_negotiations
    ADD CONSTRAINT bargaining_negotiations_business_id_not_null
    CHECK (business_id IS NOT NULL) NOT VALID;

ALTER TABLE bargaining_negotiations
    VALIDATE CONSTRAINT bargaining_negotiations_business_id_not_null;

ALTER TABLE bargaining_negotiations
    ALTER COLUMN business_id SET NOT NULL;

ALTER TABLE bargaining_negotiations
    DROP CONSTRAINT bargaining_negotiations_business_id_not_null;

ALTER TABLE bargaining_negotiations
    ADD CONSTRAINT bargaining_negotiations_business_id_fkey
    FOREIGN KEY (business_id) REFERENCES business_profiles(id) ON DELETE CASCADE
    NOT VALID;

ALTER TABLE bargaining_negotiations
    VALIDATE CONSTRAINT bargaining_negotiations_business_id_fkey;

CREATE UNIQUE INDEX idx_bargaining_negotiations_session_unique
    ON bargaining_negotiations (session_id)
    WHERE session_id IS NOT NULL;

CREATE INDEX idx_bargaining_negotiations_owner_session
    ON bargaining_negotiations (business_id, user_id, session_id)
    WHERE deleted_at IS NULL;

CREATE INDEX idx_bargaining_negotiations_owner_id
    ON bargaining_negotiations (business_id, user_id, id)
    WHERE deleted_at IS NULL;

CREATE FUNCTION prevent_bargaining_scope_mutation()
RETURNS TRIGGER AS $$
BEGIN
    IF OLD.user_id IS DISTINCT FROM NEW.user_id
        OR OLD.business_id IS DISTINCT FROM NEW.business_id THEN
        RAISE EXCEPTION 'bargaining negotiation ownership scope is immutable';
    END IF;

    IF OLD.session_id IS NOT NULL
        AND OLD.session_id IS DISTINCT FROM NEW.session_id THEN
        RAISE EXCEPTION 'bargaining negotiation session scope is immutable';
    END IF;

    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

CREATE TRIGGER trigger_bargaining_scope_immutable
BEFORE UPDATE OF user_id, business_id, session_id ON bargaining_negotiations
FOR EACH ROW
EXECUTE FUNCTION prevent_bargaining_scope_mutation();

COMMENT ON COLUMN bargaining_negotiations.business_id IS
    'Immutable effective business scope captured when the negotiation is created';
