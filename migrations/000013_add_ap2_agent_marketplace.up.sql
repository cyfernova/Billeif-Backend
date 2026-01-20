-- Migration: Add AP2 Agent Marketplace Tables
-- Created at: 2025-01-20
-- Description: Add tables for AP2 protocol implementation including agents, mandates, credentials, and marketplace

-- Agent Management
CREATE TABLE agents (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    owner_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    business_id UUID NOT NULL REFERENCES business_profiles(id) ON DELETE CASCADE,
    name VARCHAR(255) NOT NULL,
    type VARCHAR(50) NOT NULL CHECK (type IN ('shopping', 'merchant', 'credential_provider', 'payment_processor')),
    description TEXT,
    capabilities JSONB NOT NULL DEFAULT '[]',
    config JSONB NOT NULL DEFAULT '{}',
    a2a_endpoint VARCHAR(500),
    is_public BOOLEAN DEFAULT FALSE,
    is_active BOOLEAN DEFAULT TRUE,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW(),
    deleted_at TIMESTAMP,
    CONSTRAINT unique_agent_name UNIQUE(owner_id, name)
);

CREATE TABLE agent_capabilities (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    capability_type VARCHAR(100) NOT NULL,
    description TEXT,
    config JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP DEFAULT NOW(),
    UNIQUE(agent_id, capability_type)
);

CREATE TABLE agent_transactions (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    mandate_id UUID NOT NULL,
    transaction_type VARCHAR(50) NOT NULL CHECK (transaction_type IN ('intent', 'cart', 'payment')),
    status VARCHAR(50) NOT NULL DEFAULT 'pending',
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMP DEFAULT NOW()
);

-- AP2 Mandates
CREATE TABLE intent_mandates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    constraints JSONB NOT NULL,
    natural_language_intent TEXT NOT NULL,
    signature VARCHAR(1000) NOT NULL,
    public_key VARCHAR(500),
    expires_at TIMESTAMP NOT NULL,
    status VARCHAR(50) DEFAULT 'active' CHECK (status IN ('active', 'revoked', 'expired')),
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE cart_mandates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    intent_mandate_id UUID REFERENCES intent_mandates(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    merchant_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    items JSONB NOT NULL,
    total_amount DECIMAL(15,2) NOT NULL CHECK (total_amount > 0),
    currency VARCHAR(3) DEFAULT 'INR' NOT NULL,
    signature VARCHAR(1000) NOT NULL,
    merchant_signature VARCHAR(1000),
    status VARCHAR(50) DEFAULT 'pending' CHECK (status IN ('pending', 'signed', 'rejected', 'expired')),
    expires_at TIMESTAMP NOT NULL,
    created_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE payment_mandates (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    cart_mandate_id UUID NOT NULL REFERENCES cart_mandates(id) ON DELETE CASCADE,
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    payment_method_id UUID REFERENCES payment_credentials(id) ON DELETE SET NULL,
    amount DECIMAL(15,2) NOT NULL CHECK (amount > 0),
    currency VARCHAR(3) NOT NULL,
    signature VARCHAR(1000) NOT NULL,
    razorpay_order_id VARCHAR(100) UNIQUE,
    razorpay_payment_id VARCHAR(100),
    status VARCHAR(50) DEFAULT 'pending' CHECK (status IN ('pending', 'authorized', 'captured', 'failed', 'refunded')),
    processed_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Credential Management
CREATE TABLE payment_credentials (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    credential_type VARCHAR(50) NOT NULL CHECK (credential_type IN ('razorpay_card', 'razorpay_upi', 'razorpay_wallet')),
    razorpay_customer_id VARCHAR(100),
    masked_card_number VARCHAR(50),
    card_brand VARCHAR(50),
    encrypted_data TEXT,
    is_default BOOLEAN DEFAULT FALSE,
    is_active BOOLEAN DEFAULT TRUE,
    expires_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE credential_tokens (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    credential_id UUID NOT NULL REFERENCES payment_credentials(id) ON DELETE CASCADE,
    payment_mandate_id UUID REFERENCES payment_mandates(id) ON DELETE SET NULL,
    token VARCHAR(255) NOT NULL UNIQUE,
    mandate_id VARCHAR(100),
    expires_at TIMESTAMP NOT NULL,
    is_used BOOLEAN DEFAULT FALSE,
    created_at TIMESTAMP DEFAULT NOW()
);

-- Marketplace
CREATE TABLE marketplace_products (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    product_id UUID REFERENCES products(id) ON DELETE SET NULL,
    name VARCHAR(255) NOT NULL,
    description TEXT,
    price DECIMAL(15,2) NOT NULL CHECK (price >= 0),
    currency VARCHAR(3) DEFAULT 'INR' NOT NULL,
    inventory_count INTEGER DEFAULT 0 CHECK (inventory_count >= 0),
    is_available BOOLEAN DEFAULT TRUE,
    images JSONB DEFAULT '[]',
    categories JSONB DEFAULT '[]',
    created_at TIMESTAMP DEFAULT NOW(),
    updated_at TIMESTAMP DEFAULT NOW()
);

CREATE TABLE marketplace_orders (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    user_id UUID NOT NULL REFERENCES users(id) ON DELETE CASCADE,
    shopping_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    merchant_agent_id UUID NOT NULL REFERENCES agents(id) ON DELETE CASCADE,
    cart_mandate_id UUID NOT NULL REFERENCES cart_mandates(id) ON DELETE CASCADE,
    payment_mandate_id UUID REFERENCES payment_mandates(id) ON DELETE SET NULL,
    total_amount DECIMAL(15,2) NOT NULL CHECK (total_amount > 0),
    currency VARCHAR(3) DEFAULT 'INR' NOT NULL,
    status VARCHAR(50) DEFAULT 'pending' CHECK (status IN ('pending', 'confirmed', 'processing', 'shipped', 'delivered', 'cancelled')),
    razorpay_order_id VARCHAR(100),
    razorpay_payment_id VARCHAR(100),
    shipping_address JSONB,
    tracking_number VARCHAR(100),
    estimated_delivery TIMESTAMP,
    delivered_at TIMESTAMP,
    created_at TIMESTAMP DEFAULT NOW()
);

-- A2A Message Logging
CREATE TABLE a2a_messages (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    sender_agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    receiver_agent_id UUID REFERENCES agents(id) ON DELETE SET NULL,
    message_type VARCHAR(100) NOT NULL,
    payload JSONB NOT NULL,
    signature VARCHAR(1000),
    status VARCHAR(50) DEFAULT 'pending' CHECK (status IN ('pending', 'sent', 'delivered', 'failed')),
    response JSONB,
    created_at TIMESTAMP DEFAULT NOW(),
    processed_at TIMESTAMP
);

-- Indexes
CREATE INDEX idx_agents_owner ON agents(owner_id);
CREATE INDEX idx_agents_business ON agents(business_id);
CREATE INDEX idx_agents_type ON agents(type);
CREATE INDEX idx_agents_active ON agents(is_active) WHERE is_active = TRUE;

CREATE INDEX idx_intent_mandates_user ON intent_mandates(user_id);
CREATE INDEX idx_intent_mandates_agent ON intent_mandates(agent_id);
CREATE INDEX idx_intent_mandates_status ON intent_mandates(status);
CREATE INDEX idx_intent_mandates_expires ON intent_mandates(expires_at);

CREATE INDEX idx_cart_mandates_user ON cart_mandates(user_id);
CREATE INDEX idx_cart_mandates_agent ON cart_mandates(agent_id);
CREATE INDEX idx_cart_mandates_merchant ON cart_mandates(merchant_id);
CREATE INDEX idx_cart_mandates_status ON cart_mandates(status);
CREATE INDEX idx_cart_mandates_expires ON cart_mandates(expires_at);

CREATE INDEX idx_payment_mandates_cart ON payment_mandates(cart_mandate_id);
CREATE INDEX idx_payment_mandates_user ON payment_mandates(user_id);
CREATE INDEX idx_payment_mandates_status ON payment_mandates(status);
CREATE INDEX idx_payment_mandates_razorpay ON payment_mandates(razorpay_order_id);

CREATE INDEX idx_payment_credentials_user ON payment_credentials(user_id);
CREATE INDEX idx_payment_credentials_type ON payment_credentials(credential_type);
CREATE INDEX idx_payment_credentials_default ON payment_credentials(user_id, is_default) WHERE is_default = TRUE;

CREATE INDEX idx_credential_tokens_credential ON credential_tokens(credential_id);
CREATE INDEX idx_credential_tokens_payment ON credential_tokens(payment_mandate_id);
CREATE INDEX idx_credential_tokens_expires ON credential_tokens(expires_at) WHERE is_used = FALSE;

CREATE INDEX idx_marketplace_products_agent ON marketplace_products(agent_id);
CREATE INDEX idx_marketplace_products_available ON marketplace_products(is_available) WHERE is_available = TRUE;
CREATE INDEX idx_marketplace_products_name ON marketplace_products USING gin(to_tsvector('english', name));

CREATE INDEX idx_marketplace_orders_user ON marketplace_orders(user_id);
CREATE INDEX idx_marketplace_orders_shopping ON marketplace_orders(shopping_agent_id);
CREATE INDEX idx_marketplace_orders_merchant ON marketplace_orders(merchant_agent_id);
CREATE INDEX idx_marketplace_orders_status ON marketplace_orders(status);

CREATE INDEX idx_a2a_messages_sender ON a2a_messages(sender_agent_id);
CREATE INDEX idx_a2a_messages_receiver ON a2a_messages(receiver_agent_id);
CREATE INDEX idx_a2a_messages_status ON a2a_messages(status);
CREATE INDEX idx_a2a_messages_created ON a2a_messages(created_at);
