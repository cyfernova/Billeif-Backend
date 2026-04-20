ALTER TABLE bargaining_negotiations ADD COLUMN IF NOT EXISTS session_id VARCHAR(100);
CREATE INDEX IF NOT EXISTS idx_bargaining_negotiations_session_id ON bargaining_negotiations(session_id);