-- Create agent registry table for agent discovery
CREATE TABLE IF NOT EXISTS agent_registry (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID NOT NULL UNIQUE REFERENCES agents(id) ON DELETE CASCADE,

    -- Agent identification
    agent_name VARCHAR(255) NOT NULL,
    agent_description TEXT,
    agent_type VARCHAR(50) NOT NULL, -- shopping, merchant, credential_provider, payment_processor

    -- Agent Card (AP2 specification)
    agent_card JSONB NOT NULL, -- Complete agent card per AP2 spec

    -- Discovery metadata
    domain VARCHAR(255),
    well_known_uri VARCHAR(500) UNIQUE,
    a2a_endpoint VARCHAR(500),

    -- Searchable fields (denormalized for faster queries)
    capabilities TEXT[] DEFAULT '{}',
    tags TEXT[] DEFAULT '{}',
    jurisdictions TEXT[] DEFAULT '{}',
    currencies TEXT[] DEFAULT '{}',
    supported_languages TEXT[] DEFAULT '{}',

    -- Pricing information
    pricing_model JSONB DEFAULT '{}', -- { "commission": 2.5, "monthly_fee": 0, "currency": "INR" }

    -- Registration metadata
    is_verified BOOLEAN DEFAULT FALSE,
    is_active BOOLEAN DEFAULT TRUE,
    is_public BOOLEAN DEFAULT FALSE,

    -- Health checking
    last_health_check TIMESTAMP,
    health_check_status VARCHAR(20), -- 'healthy', 'degraded', 'unhealthy'
    health_check_message TEXT,

    -- Performance metrics (denormalized)
    average_response_time_ms FLOAT DEFAULT 0,
    success_rate FLOAT DEFAULT 100.0,
    total_requests INT DEFAULT 0,
    failed_requests INT DEFAULT 0,

    -- Ratings and reviews
    average_rating FLOAT DEFAULT 0,
    total_reviews INT DEFAULT 0,

    -- Timestamps
    verified_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    deleted_at TIMESTAMP
);

-- Indexes for agent discovery
CREATE INDEX idx_agent_registry_active ON agent_registry(is_active, deleted_at);
CREATE INDEX idx_agent_registry_verified ON agent_registry(is_verified, deleted_at);
CREATE INDEX idx_agent_registry_public ON agent_registry(is_public, is_active, deleted_at);
CREATE INDEX idx_agent_registry_type ON agent_registry(agent_type, is_active, deleted_at);
CREATE INDEX idx_agent_registry_domain ON agent_registry(domain, deleted_at);

-- GIN indexes for array searches (capabilities, tags, jurisdictions, currencies, languages)
CREATE INDEX idx_agent_registry_capabilities ON agent_registry USING GIN(capabilities)
WHERE is_active AND deleted_at IS NULL;

CREATE INDEX idx_agent_registry_tags ON agent_registry USING GIN(tags)
WHERE is_active AND deleted_at IS NULL;

CREATE INDEX idx_agent_registry_jurisdictions ON agent_registry USING GIN(jurisdictions)
WHERE is_active AND deleted_at IS NULL;

CREATE INDEX idx_agent_registry_currencies ON agent_registry USING GIN(currencies)
WHERE is_active AND deleted_at IS NULL;

CREATE INDEX idx_agent_registry_languages ON agent_registry USING GIN(supported_languages)
WHERE is_active AND deleted_at IS NULL;

-- GIN index for JSONB agent_card (for searching within card)
CREATE INDEX idx_agent_registry_card_search ON agent_registry USING GIN(agent_card)
WHERE is_active AND deleted_at IS NULL;

-- Index for health checks and monitoring
CREATE INDEX idx_agent_registry_health ON agent_registry(health_check_status, is_active, deleted_at);

-- Index for ratings/reviews
CREATE INDEX idx_agent_registry_ratings ON agent_registry(average_rating DESC)
WHERE is_public AND is_active AND deleted_at IS NULL;

-- Index for searching by name
CREATE INDEX idx_agent_registry_name_search ON agent_registry USING BTREE(agent_name)
WHERE is_active AND deleted_at IS NULL;

-- Create agent discovery audit log table
CREATE TABLE IF NOT EXISTS agent_discovery_audit (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_registry_id UUID NOT NULL REFERENCES agent_registry(id) ON DELETE CASCADE,

    -- Audit information
    action VARCHAR(50) NOT NULL, -- 'registered', 'updated', 'verified', 'deactivated', 'health_check'
    performed_by VARCHAR(50), -- 'system', 'admin', 'agent'
    reason TEXT,

    -- Previous and current state
    previous_state JSONB,
    current_state JSONB,

    -- Timestamps
    created_at TIMESTAMP DEFAULT NOW()
);

-- Indexes for audit log
CREATE INDEX idx_agent_discovery_audit_registry ON agent_discovery_audit(agent_registry_id);
CREATE INDEX idx_agent_discovery_audit_action ON agent_discovery_audit(action);
CREATE INDEX idx_agent_discovery_audit_date ON agent_discovery_audit(created_at DESC);

-- Create agent discovery stats table (for analytics)
CREATE TABLE IF NOT EXISTS agent_discovery_stats (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_registry_id UUID NOT NULL UNIQUE REFERENCES agent_registry(id) ON DELETE CASCADE,

    -- Discovery metrics
    total_views INT DEFAULT 0,
    total_searches_found INT DEFAULT 0,
    total_inquiries INT DEFAULT 0,
    total_integrations INT DEFAULT 0,

    -- Recent activity
    last_viewed_at TIMESTAMP,
    last_search_found_at TIMESTAMP,
    last_inquiry_at TIMESTAMP,
    last_integration_at TIMESTAMP,

    -- Weekly stats
    views_this_week INT DEFAULT 0,
    inquiries_this_week INT DEFAULT 0,
    integrations_this_week INT DEFAULT 0,

    -- Timestamps
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

-- Index for stats
CREATE INDEX idx_agent_discovery_stats_views ON agent_discovery_stats(total_views DESC);
CREATE INDEX idx_agent_discovery_stats_integrations ON agent_discovery_stats(total_integrations DESC);

-- Create function to update updated_at timestamp
CREATE OR REPLACE FUNCTION update_agent_registry_updated_at()
RETURNS TRIGGER AS $$
BEGIN
    NEW.updated_at = NOW();
    RETURN NEW;
END;
$$ LANGUAGE plpgsql;

-- Create trigger to automatically update updated_at
CREATE TRIGGER trigger_agent_registry_updated_at
BEFORE UPDATE ON agent_registry
FOR EACH ROW
EXECUTE FUNCTION update_agent_registry_updated_at();

-- Create view for active agents registry
CREATE OR REPLACE VIEW active_agents_registry AS
SELECT
    ar.*,
    ds.total_views,
    ds.total_searches_found,
    ds.total_inquiries,
    ds.total_integrations
FROM agent_registry ar
LEFT JOIN agent_discovery_stats ds ON ar.id = ds.agent_registry_id
WHERE ar.is_active AND ar.deleted_at IS NULL
ORDER BY ar.average_rating DESC, ar.total_reviews DESC, ar.created_at DESC;

-- Create view for public agents registry
CREATE OR REPLACE VIEW public_agents_registry AS
SELECT
    ar.*,
    ds.total_views,
    ds.total_searches_found,
    ds.total_inquiries,
    ds.total_integrations
FROM agent_registry ar
LEFT JOIN agent_discovery_stats ds ON ar.id = ds.agent_registry_id
WHERE ar.is_public AND ar.is_active AND ar.is_verified AND ar.deleted_at IS NULL
ORDER BY ar.average_rating DESC, ar.total_reviews DESC, ar.created_at DESC;
