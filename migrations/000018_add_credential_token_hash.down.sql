ALTER TABLE credential_tokens
    ALTER COLUMN token SET NOT NULL;

ALTER TABLE credential_tokens
    ADD CONSTRAINT credential_tokens_token_key UNIQUE (token);

DROP INDEX IF EXISTS idx_credential_tokens_token_hash;

ALTER TABLE credential_tokens
    DROP COLUMN IF EXISTS token_hash;
