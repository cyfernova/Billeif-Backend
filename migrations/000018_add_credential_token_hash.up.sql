ALTER TABLE credential_tokens
    ADD COLUMN IF NOT EXISTS token_hash VARCHAR(64);

UPDATE credential_tokens
SET token_hash = encode(digest(token, 'sha256'), 'hex')
WHERE token_hash IS NULL AND token IS NOT NULL;

ALTER TABLE credential_tokens
    ALTER COLUMN token_hash SET NOT NULL;

CREATE UNIQUE INDEX IF NOT EXISTS idx_credential_tokens_token_hash
    ON credential_tokens(token_hash);

ALTER TABLE credential_tokens
    DROP CONSTRAINT IF EXISTS credential_tokens_token_key;

ALTER TABLE credential_tokens
    ALTER COLUMN token DROP NOT NULL;
