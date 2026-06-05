CREATE TABLE IF NOT EXISTS llm_chat_conversations (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    business_id UUID NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    title VARCHAR(160) NOT NULL,
    last_message TEXT NOT NULL DEFAULT '',
    message_count INT NOT NULL DEFAULT 0,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    deleted_at TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_llm_chat_conversations_scope
    ON llm_chat_conversations (business_id, user_id, updated_at DESC)
    WHERE deleted_at IS NULL;

CREATE TABLE IF NOT EXISTS llm_chat_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    conversation_id UUID NOT NULL REFERENCES llm_chat_conversations(id) ON DELETE CASCADE,
    business_id UUID NOT NULL,
    user_id VARCHAR(255) NOT NULL,
    role VARCHAR(20) NOT NULL CHECK (role IN ('user', 'assistant', 'system')),
    content TEXT NOT NULL,
    web_search JSONB,
    created_at TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE INDEX IF NOT EXISTS idx_llm_chat_messages_conversation
    ON llm_chat_messages (conversation_id, created_at);

CREATE INDEX IF NOT EXISTS idx_llm_chat_messages_scope
    ON llm_chat_messages (business_id, user_id, created_at DESC);
