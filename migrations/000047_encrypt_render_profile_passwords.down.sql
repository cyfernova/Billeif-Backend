DO $$
BEGIN
    IF EXISTS (
        SELECT 1
        FROM render_profiles
        WHERE password_ciphertext IS NOT NULL
          AND password_ciphertext <> ''
    ) THEN
        RAISE EXCEPTION
            'cannot roll back render profile password encryption while ciphertext values remain';
    END IF;
END
$$;

COMMENT ON COLUMN render_profiles.password IS NULL;

ALTER TABLE render_profiles
    DROP COLUMN password_ciphertext;
