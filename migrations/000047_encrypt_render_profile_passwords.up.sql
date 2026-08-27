ALTER TABLE render_profiles
    ADD COLUMN password_ciphertext TEXT;

COMMENT ON COLUMN render_profiles.password IS
    'Legacy plaintext compatibility only; startup backfill and compatibility reads migrate this value to password_ciphertext and clear it.';

COMMENT ON COLUMN render_profiles.password_ciphertext IS
    'Versioned AES-256-GCM ciphertext for the write-only render profile password.';
