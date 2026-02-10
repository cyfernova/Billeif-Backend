# Agent Configuration & Bargaining System

## Overview

This implementation provides a complete agent configuration and bargaining system with support for buyer and seller agent types, mentee-driven decision making, and A2A protocol communication.

## Components

### 1. Agent Types

#### Buyer Agent
Agents configured for purchasing goods and services with optimal bargaining strategies.

**Configuration Parameters:**
- `max_discount_percent` (float64): Maximum discount to request (default: 25%)
- `min_discount_percent` (float64): Minimum acceptable discount (default: 5%)
- `target_discount` (float64): Target discount to achieve (default: 15%)
- `risk_tolerance` (float64): Risk level 0-1 (default: 0.5)
- `patience_level` (float64): Patience for negotiations 0-1 (default: 0.5)
- `max_rounds` (int): Maximum negotiation rounds (default: 5)
- `preferred_products` ([]string): Preferred product categories
- `blacklisted_vendors` ([]string): Vendors to avoid
- `budget_limit` (*float64): Maximum spend limit
- `payment_terms` ([]string): Accepted payment terms
- `acceptance_threshold` (float64): Auto-accept threshold 0-1 (default: 0.85)

#### Seller Agent
Agents configured for selling goods and services with profit maximization strategies.

**Configuration Parameters:**
- `min_acceptable_price` (float64): Minimum price to accept
- `max_markup_percent` (float64): Maximum markup to apply (default: 30%)
- `inventory_pressure` (float64): Urgency to sell 0-1 (default: 0.5)
- `sales_volume_goal` (*float64): Target sales volume
- `customer_loyalty_factor` (float64): Loyalty multiplier (default: 1.0)
- `max_rounds` (int): Maximum negotiation rounds (default: 5)
- `preferred_customers` ([]string): Priority customers
- `volume_discount_tiers` ([]DiscountTier): Bulk discount structure
- `payment_terms` ([]string): Accepted payment terms
- `acceptance_threshold` (float64): Auto-accept threshold 0-1 (default: 0.85)

### 2. Volatile Mentee System

The mentee is an in-memory learning system that provides bargaining recommendations based on historical data.

**Key Features:**

1. **Opponent Learning**
   - Tracks past negotiations with specific agents
   - Learns average discount/markup patterns
   - Calculates opponent volatility
   - Builds confidence over time

2. **Self Learning**
   - Tracks own negotiation history
   - Learns personal bargaining style
   - Identifies successful strategies
   - Adjusts parameters based on outcomes

3. **Decision Making**
   - Three decision modes:
     - **Informed**: High confidence (>0.7) on opponent patterns
     - **Self-Based**: Moderate confidence (>0.6) on own history
     - **Base**: Low confidence - uses default strategies

4. **Confidence Scoring**
   - Confidence increases with more data (max 0.95 at 20 outcomes)
   - Minimum confidence is 0.1 (initial state)
   - Decay factor: 0.95 per day for old data

**Mentee Operations:**
- `RecordNegotiationOutcome`: Log negotiation results for learning
- `GetBargainingDecision`: Get recommendation for next action
- `GetAgentLearningData`: View learned patterns for an agent
- `ResetAgentLearning`: Clear learning data for an agent
- `ExportLearningData`: Export all learning data as JSON
- `ImportLearningData`: Import learning data from JSON

### 3. A2A Protocol Communication

Agents communicate using the Agent2Agent (A2A) protocol v0.3.

**Communication Types:**

1. **Counter Offer Notification**
```json
{
  "type": "counter_offer",
  "negotiation_id": "uuid",
  "round_number": 1,
  "proposed_amount": 100.50,
  "agent_type": "buyer",
  "volatility_factor": 0.05,
  "reason": "Adjustment based on budget"
}
```

2. **Negotiation Completion**
```json
{
  "type": "negotiation_complete",
  "negotiation_id": "uuid",
  "status": "accepted|rejected",
  "final_amount": 95.00,
  "rounds": 3
}
```

3. **Task Request**
```json
{
  "task_name": "bargaining_notification",
  "description": "Notification from counterparty",
  "parameters": { ... }
}
```

**A2A Features:**
- Automatic retry (3 attempts with 1000ms delay)
- Timeout protection (30 seconds)
- Signature verification for message authenticity
- Error handling and logging

### 4. Agent Registry (.well-known/agents.json)

Public agent registry stored in `.well-known/agents.json`.

**File Structure:**
```json
{
  "version": "1.0.0",
  "agent_types": {
    "buyer": { "default_config": { ... } },
    "seller": { "default_config": { ... } }
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
  ],
  "last_sync": "2024-02-10T00:00:00Z"
}
```

**Public Endpoints:**
- `GET /.well-known/agents.json` - Serve agent registry
- `GET /.well-known/agent.json` - Serve individual agent card

## API Endpoints

### Agent Configuration

All endpoints require Bearer authentication.

#### Create/Update Configuration
```
POST /api/v1/agents/config
Content-Type: application/json
Authorization: Bearer <token>

{
  "agent_id": "uuid",
  "config": {
    "type": "buyer|seller",
    "volatility": 0.5,
    "buyer_config": { ... } | "seller_config": { ... }
  }
}
```

#### Get All Configurations
```
GET /api/v1/agents/config
Authorization: Bearer <token>

Response:
{
  "configs": [ ... ]
}
```

#### Get Specific Configuration
```
GET /api/v1/agents/config/:agent_id
Authorization: Bearer <token>

Response:
{
  "agent_id": "uuid",
  "config": { ... }
}
```

#### Update Configuration
```
PUT /api/v1/agents/config/:agent_id
Content-Type: application/json
Authorization: Bearer <token>

{
  "buyer_config": { ... } | "seller_config": { ... },
  "volatility": 0.7
}
```

#### Delete Configuration
```
DELETE /api/v1/agents/config/:agent_id
Authorization: Bearer <token>

Response: 204 No Content
```

#### Create Default Configuration
```
POST /api/v1/agents/config/default
Content-Type: application/json
Authorization: Bearer <token>

{
  "agent_id": "uuid",
  "type": "buyer|seller"
}
```

### Mentee Operations

#### Get Recommendation
```
GET /api/v1/agents/config/mentee/recommendation/:negotiation_id?agent_type=buyer|seller
Authorization: Bearer <token>

Response:
{
  "action": "counteroffer|accept|reject",
  "proposed_amount": 95.50,
  "reason": "Based on historical opponent patterns",
  "confidence": 0.85,
  "suggested_range": {
    "min": 90.00,
    "max": 100.00
  }
}
```

#### Get Learning Data
```
GET /api/v1/agents/config/mentee/learning/:agent_id
Authorization: Bearer <token>

Response:
{
  "agent_id": "uuid",
  "agent_type": "buyer",
  "outcomes": [ ... ],
  "average_discount": 0.15,
  "average_markup": 0.10,
  "success_rate": 0.85,
  "confidence": 0.90,
  "volatility": 0.45
}
```

#### Reset Learning Data
```
DELETE /api/v1/agents/config/mentee/learning/:agent_id
Authorization: Bearer <token>

Response: 204 No Content
```

#### Export Learning Data
```
GET /api/v1/agents/config/mentee/export
Authorization: Bearer <token>

Response: File Download (mentee_data.json)
```

#### Import Learning Data
```
POST /api/v1/agents/config/mentee/import
Content-Type: multipart/form-data
Authorization: Bearer <token>

file: <JSON file>

Response:
{
  "message": "learning data imported successfully"
}
```

### Bargaining Endpoints

#### Create Negotiation
```
POST /api/v1/bargaining/negotiations
Authorization: Bearer <token>

{
  "buyer_agent_id": "uuid",
  "seller_agent_id": "uuid",
  "initial_amount": 100.00,
  "max_rounds": 5,
  "marketplace_order_id": "uuid"
}
```

#### List Negotiations
```
GET /api/v1/bargaining/negotiations?page=1&limit=10
Authorization: Bearer <token>
```

#### Get Negotiation
```
GET /api/v1/bargaining/negotiations/:id
Authorization: Bearer <token>
```

#### Submit Counter Offer
```
POST /api/v1/bargaining/negotiations/:id/counteroffer
Authorization: Bearer <token>

{
  "agent_id": "uuid",
  "proposed_amount": 95.50,
  "action": "counteroffer|accept|reject",
  "reason": "Price adjustment"
}
```

#### Get Negotiation Rounds
```
GET /api/v1/bargaining/negotiations/:id/rounds
Authorization: Bearer <token>
```

#### Get Suggested Counter Offer
```
GET /api/v1/bargaining/negotiations/:id/suggest?agent_type=buyer|seller
Authorization: Bearer <token>
```

## Implementation Files

### Services
- `internal/services/agent_config_service.go` - Agent configuration management
- `internal/services/bargaining_service.go` - Bargaining logic with A2A
- `internal/services/mentee_service.go` - Volatile mentee learning system

### Handlers
- `internal/handlers/agent_config_handler.go` - HTTP handlers with Swagger annotations

### Models
- `internal/models/agent_config.go` - Agent configuration data structures
- `internal/models/bargaining.go` - Bargaining negotiation models

### Public Files
- `.well-known/agents.json` - Public agent registry
- `.well-known/README.md` - Documentation

## Swagger Documentation

All endpoints are documented with Swagger annotations and available at:
- `GET /swagger/index.html` - Interactive Swagger UI
- `GET /swagger/doc.json` - Swagger JSON specification

Tags:
- **Agent Configuration** - Agent config management endpoints
- **Bargaining** - Negotiation and counter offer endpoints

## Security

1. **Authentication**: All endpoints require Bearer token
2. **Authorization**: Users can only modify their own agents
3. **Data Privacy**: Sensitive config data is protected
4. **A2A Verification**: All A2A messages are signed
5. **CORS**: Configured for allowed origins

## Usage Flow

### Setting up a Buyer Agent

1. Create agent via `/api/v1/agents`
2. Configure via `/api/v1/agents/config/default` with type="buyer"
3. Adjust parameters via `/api/v1/agents/config/:agent_id`
4. Agent appears in `.well-known/agents.json`

### Setting up a Seller Agent

1. Create agent via `/api/v1/agents`
2. Configure via `/api/v1/agents/config/default` with type="seller"
3. Adjust parameters via `/api/v1/agents/config/:agent_id`
4. Agent appears in `.well-known/agents.json`

### Conducting a Bargain

1. Buyer agent initiates negotiation via `/api/v1/bargaining/negotiations`
2. System creates negotiation with buyer/seller volatility factors
3. Seller receives A2A notification with counter offer request
4. Either agent can:
   - Submit counter offer (with mentee recommendation)
   - Accept current offer
   - Reject offer
5. Repeat until acceptance, rejection, or max rounds
6. Outcome recorded in mentee for future learning

### Mentee Learning

1. Each negotiation outcome is recorded
2. Mentee analyzes patterns:
   - Opponent behavior
   - Self performance
   - Volatility factors
3. Confidence increases with more data
4. Future recommendations improve over time

## Testing

Run tests:
```bash
# Run all tests
go test ./...

# Run specific test
go test ./internal/services -run TestMentee

# Run integration tests
go test -tags=integration ./tests/integration/...
```

## Configuration

Environment variables:
```env
# Well-known directory path (default: .well-known)
WELL_KNOWN_DIR=.well-known

# Mentee learning rate (default: 0.05)
MENTEE_LEARNING_RATE=0.05

# Mentee decay factor (default: 0.95)
MENTEE_DECAY_FACTOR=0.95

# A2A timeout (default: 30s)
A2A_TIMEOUT_SECONDS=30
```

## Future Enhancements

1. **Machine Learning**: Replace heuristic learning with ML models
2. **Market Data Integration**: Consider external market conditions
3. **Multi-Agent Bargaining**: Support more than 2 agents
4. **Real-time Analytics**: Dashboard for agent performance
5. **Simulation Mode**: Practice bargaining without real transactions
6. **Config Templates**: Pre-built configurations for different industries
