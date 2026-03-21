DROP INDEX IF EXISTS idx_users_phone_number_unique_active;
DROP INDEX IF EXISTS idx_users_email_unique_active;

ALTER TABLE users
    DROP COLUMN IF EXISTS phone_number;

UPDATE users
SET email = CONCAT('legacy-', id::text, '@example.invalid')
WHERE email IS NULL OR email = '';

ALTER TABLE users
    ALTER COLUMN email SET NOT NULL;

ALTER TABLE users
    ADD CONSTRAINT users_email_key UNIQUE (email);
