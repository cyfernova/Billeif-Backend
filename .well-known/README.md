# .well-known Directory

This directory contains public agent configuration files that can be discovered and used by other systems.

## Files

### agents.json
Public agent registry containing all registered bargaining agents with their configurations. This file includes:

- **Buyer Agents**: Agents configured for purchasing goods and services
  - Bargaining parameters (max discount, min discount, target discount)
  - Risk tolerance and patience levels
  - Preferred products and blacklisted vendors
  - Budget limits and payment terms

- **Seller Agents**: Agents configured for selling goods and services
  - Pricing strategies (min acceptable price, max markup)
  - Inventory pressure and sales goals
  - Preferred customers and volume discounts
  - Customer loyalty factors

### File Structure

```json
{
  "version": "1.0.0",
  "agent_types": {
    "buyer": { ... },
    "seller": { ... }
  },
  "agents": [
    {
      "agent_id": "uuid",
      "name": "Agent Name",
      "type": "buyer|seller",
      "config": { ... },
      "capabilities": [ ... ],
      "a2a_endpoint": "https://...",
      "created_at": "2024-01-01T00:00:00Z"
    }
  ]
}
```

## Agent2Agent (A2A) Protocol

Agents in this registry support the A2A protocol for inter-agent communication:

1. **Counter Offer Notifications**: Agents send/receive counter offers via A2A messages
2. **Negotiation Completion**: Agents are notified when negotiations are accepted/rejected
3. **Task Execution**: Agents can request tasks from each other

### A2A Message Format

```json
{
  "type": "counter_offer",
  "negotiation_id": "uuid",
  "round_number": 1,
  "proposed_amount": 100.50,
  "agent_type": "buyer",
  "volatility_factor": 0.05,
  "reason": "Offer adjustment based on market conditions"
}
```

## Mentee Integration

The volatile mentee system uses historical negotiation data to provide recommendations:

- **Opponent Learning**: Learns from past negotiations with specific agents
- **Self Learning**: Learns from own negotiation patterns
- **Adaptive Strategy**: Adjusts recommendations based on confidence levels
- **Confidence Threshold**: Only uses learned patterns when confidence > 0.7

## Configuration Updates

Agent configurations are updated via:
1. API endpoints: `/api/v1/agents/config`
2. Direct file modifications for static agents
3. Real-time updates via A2A notifications

## Security

- All endpoints are protected with Bearer authentication
- Agent owners can only modify their own configurations
- Public agents expose limited information (no sensitive data)
- A2A endpoints must be registered and validated

## Endpoints

Public:
- `GET /.well-known/agents.json` - Public agent registry

Protected (requires authentication):
- `POST /api/v1/agents/config` - Create/update agent config
- `GET /api/v1/agents/config` - List all configs
- `GET /api/v1/agents/config/:agent_id` - Get specific config
- `PUT /api/v1/agents/config/:agent_id` - Update config
- `DELETE /api/v1/agents/config/:agent_id` - Delete config
- `POST /api/v1/agents/config/default` - Create default config

Mentee:
- `GET /api/v1/agents/config/mentee/recommendation/:negotiation_id` - Get mentee recommendation
- `GET /api/v1/agents/config/mentee/learning/:agent_id` - Get learning data
- `DELETE /api/v1/agents/config/mentee/learning/:agent_id` - Reset learning data
- `GET /api/v1/agents/config/mentee/export` - Export learning data
- `POST /api/v1/agents/config/mentee/import` - Import learning data

## See Also

- [A2A Protocol Specification](../../docs/a2a-spec.md)
- [Agent Configuration API](../../docs/agent-config-api.md)
- [Mentee Documentation](../../docs/mentee.md)
