ALTER TABLE bargaining_negotiations ADD COLUMN session_id VARCHAR(100);

CREATE INDEX idx_bargaining_negotiations_session_id ON bargaining_negotiations(session_id);
