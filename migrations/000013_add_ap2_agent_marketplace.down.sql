-- Migration: Add AP2 Agent Marketplace Tables (rollback)
-- Created at: 2025-01-20
-- Description: Rollback for AP2 protocol tables

DROP INDEX IF EXISTS idx_a2a_messages_created;
DROP INDEX IF EXISTS idx_a2a_messages_status;
DROP INDEX IF EXISTS idx_a2a_messages_receiver;
DROP INDEX IF EXISTS idx_a2a_messages_sender;

DROP INDEX IF EXISTS idx_marketplace_orders_status;
DROP INDEX IF EXISTS idx_marketplace_orders_merchant;
DROP INDEX IF EXISTS idx_marketplace_orders_shopping;
DROP INDEX IF EXISTS idx_marketplace_orders_user;

DROP INDEX IF EXISTS idx_marketplace_products_name;
DROP INDEX IF EXISTS idx_marketplace_products_available;
DROP INDEX IF EXISTS idx_marketplace_products_agent;

DROP INDEX IF EXISTS idx_credential_tokens_expires;
DROP INDEX IF EXISTS idx_credential_tokens_payment;
DROP INDEX IF EXISTS idx_credential_tokens_credential;

DROP INDEX IF EXISTS idx_payment_credentials_default;
DROP INDEX IF EXISTS idx_payment_credentials_type;
DROP INDEX IF EXISTS idx_payment_credentials_user;

DROP INDEX IF EXISTS idx_payment_mandates_razorpay;
DROP INDEX IF EXISTS idx_payment_mandates_status;
DROP INDEX IF EXISTS idx_payment_mandates_user;
DROP INDEX IF EXISTS idx_payment_mandates_cart;

DROP INDEX IF EXISTS idx_cart_mandates_expires;
DROP INDEX IF EXISTS idx_cart_mandates_status;
DROP INDEX IF EXISTS idx_cart_mandates_merchant;
DROP INDEX IF EXISTS idx_cart_mandates_agent;
DROP INDEX IF EXISTS idx_cart_mandates_user;

DROP INDEX IF EXISTS idx_intent_mandates_expires;
DROP INDEX IF EXISTS idx_intent_mandates_status;
DROP INDEX IF EXISTS idx_intent_mandates_agent;
DROP INDEX IF EXISTS idx_intent_mandates_user;

DROP INDEX IF EXISTS idx_agents_active;
DROP INDEX IF EXISTS idx_agents_type;
DROP INDEX IF EXISTS idx_agents_business;
DROP INDEX IF EXISTS idx_agents_owner;

DROP TABLE IF EXISTS a2a_messages CASCADE;
DROP TABLE IF EXISTS marketplace_orders CASCADE;
DROP TABLE IF EXISTS marketplace_products CASCADE;
DROP TABLE IF EXISTS credential_tokens CASCADE;
DROP TABLE IF EXISTS payment_credentials CASCADE;
DROP TABLE IF EXISTS payment_mandates CASCADE;
DROP TABLE IF EXISTS cart_mandates CASCADE;
DROP TABLE IF EXISTS intent_mandates CASCADE;
DROP TABLE IF EXISTS agent_transactions CASCADE;
DROP TABLE IF EXISTS agent_capabilities CASCADE;
DROP TABLE IF EXISTS agents CASCADE;
