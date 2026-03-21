ALTER TABLE users
    ADD COLUMN IF NOT EXISTS phone_number VARCHAR(20);

ALTER TABLE users
    ALTER COLUMN email DROP NOT NULL;

ALTER TABLE users
    DROP CONSTRAINT IF EXISTS users_email_key;

DROP INDEX IF EXISTS idx_users_email_unique_active;
CREATE UNIQUE INDEX idx_users_email_unique_active
    ON users(email)
    WHERE deleted_at IS NULL AND email IS NOT NULL AND email <> '';

DROP INDEX IF EXISTS idx_users_phone_number_unique_active;
CREATE UNIQUE INDEX idx_users_phone_number_unique_active
    ON users(phone_number)
    WHERE deleted_at IS NULL AND phone_number IS NOT NULL AND phone_number <> '';
