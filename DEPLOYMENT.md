# Deployment Guide - Invoice Tracker + AI Agent Marketplace

## Table of Contents
1. [Prerequisites](#prerequisites)
2. [Local Development](#local-development)
3. [Docker Deployment](#docker-deployment)
4. [Kubernetes Deployment](#kubernetes-deployment)
5. [Environment Configuration](#environment-configuration)
6. [Database Migrations](#database-migrations)
7. [Monitoring & Observability](#monitoring--observability)
8. [Security Best Practices](#security-best-practices)

## Prerequisites

### Required Tools
- Go 1.21+ ([Download](https://golang.org/dl))
- Docker & Docker Compose ([Download](https://www.docker.com/products/docker-desktop))
- PostgreSQL 14+ or use Docker
- Redis 7+ or use Docker
- kubectl (for Kubernetes deployment)

### Required API Keys
- **Razorpay**: Sandbox and Live keys from [Razorpay Dashboard](https://dashboard.razorpay.com)
- **Claude API**: From [Anthropic Console](https://console.anthropic.com)
- **AWS**: Access keys for S3 and SES
- **Cognito**: User pool and client ID from AWS Cognito

## Local Development

### 1. Clone Repository
```bash
git clone <repository>
cd invoice-backend
```

### 2. Setup Environment
```bash
cp .env.example .env
# Edit .env with your configuration
nano .env
```

### 3. Install Dependencies
```bash
go mod download
go mod tidy
```

### 4. Run Database
```bash
# Using Docker
docker-compose up postgres redis

# Or install PostgreSQL locally
psql -U postgres -c "CREATE DATABASE invoice_db;"
```

### 5. Run Migrations
```bash
go run cmd/migrate/main.go up
```

### 6. Start API Server
```bash
go run cmd/api/main.go
```

The API will be available at `http://localhost:8080`

### 7. Test Health Endpoint
```bash
curl http://localhost:8080/health
```

## Docker Deployment

### Build Docker Image
```bash
docker build -t invoice-backend:latest .
```

### Run with Docker Compose (Development)
```bash
docker-compose up -d
```

Check logs:
```bash
docker-compose logs -f api
```

### Stop Services
```bash
docker-compose down
```

### Run Production Build
```bash
docker build -t invoice-backend:v1.0.0 .
docker tag invoice-backend:v1.0.0 your-registry/invoice-backend:v1.0.0
docker push your-registry/invoice-backend:v1.0.0
```

## Kubernetes Deployment

### Prerequisites
- Kubernetes cluster 1.24+
- kubectl configured
- Container registry (Docker Hub, ECR, GCR)

### 1. Create Namespace
```bash
kubectl create namespace invoice-system
```

### 2. Create Secrets
```bash
kubectl create secret generic invoice-secrets \
  --from-literal=database-url=$DATABASE_URL \
  --from-literal=redis-url=$REDIS_URL \
  --from-literal=razorpay-key=$RAZORPAY_KEY_ID \
  --from-literal=razorpay-secret=$RAZORPAY_KEY_SECRET \
  --from-literal=claude-api-key=$CLAUDE_API_KEY \
  -n invoice-system
```

### 3. Create ConfigMap
```bash
kubectl create configmap invoice-config \
  --from-literal=nlp-provider=claude \
  --from-literal=log-level=info \
  -n invoice-system
```

### 4. Deploy Using Helm (Recommended)
```bash
helm repo add invoice https://charts.example.com
helm repo update
helm install invoice-backend invoice/invoice-backend \
  -n invoice-system \
  --values values.yaml
```

### 5. Deploy Using kubectl
```bash
kubectl apply -f k8s/namespace.yaml
kubectl apply -f k8s/postgres.yaml
kubectl apply -f k8s/redis.yaml
kubectl apply -f k8s/api-deployment.yaml
kubectl apply -f k8s/api-service.yaml
kubectl apply -f k8s/ingress.yaml
```

### 6. Monitor Deployment
```bash
kubectl get pods -n invoice-system
kubectl describe pod invoice-api-0 -n invoice-system
kubectl logs -f deployment/invoice-api -n invoice-system
```

### 7. Access Application
```bash
# Port forward
kubectl port-forward svc/invoice-api 8080:8080 -n invoice-system

# Or access via Ingress
curl https://api.yourdomain.com/health
```

## Environment Configuration

### Development
```bash
ENV=development
LOG_LEVEL=debug
CORS_ENABLED=true
ALLOWED_ORIGINS=http://localhost:3000,http://localhost:5000
```

### Staging
```bash
ENV=staging
LOG_LEVEL=info
ALLOWED_ORIGINS=https://staging.yourdomain.com
```

### Production
```bash
ENV=production
LOG_LEVEL=warn
SSL_ENABLED=true
RATE_LIMIT_REQUESTS=100
RATE_LIMIT_WINDOW_SECONDS=60
```

## Database Migrations

### Create Migration
```bash
go run cmd/migrate/main.go create add_new_table
```

### Run Migrations
```bash
go run cmd/migrate/main.go up

# Run specific number
go run cmd/migrate/main.go up 5
```

### Rollback Migrations
```bash
go run cmd/migrate/main.go down

# Rollback specific number
go run cmd/migrate/main.go down 3
```

## Monitoring & Observability

### Prometheus Metrics
Metrics available at `http://localhost:9090`

Key metrics:
- `http_request_duration_seconds`
- `http_request_total`
- `database_query_duration_seconds`
- `websocket_connections_active`

### Grafana Dashboards
Access at `http://localhost:3000` (admin/admin)

Dashboards:
- API Performance
- Database Queries
- WebSocket Connections
- Error Rates

### Logs
```bash
# Docker logs
docker-compose logs -f api

# Kubernetes logs
kubectl logs -f deployment/invoice-api -n invoice-system

# Structured logging
curl http://localhost:8080/logs?level=error
```

### Distributed Tracing (Optional)
```bash
# Enable OTEL tracing
OTEL_EXPORTER_OTLP_ENDPOINT=http://localhost:4318
```

## Security Best Practices

### 1. Environment Variables
- Never commit `.env` files
- Rotate secrets regularly
- Use secure secret management (AWS Secrets Manager, HashiCorp Vault)

### 2. Database
```bash
# Use strong password
POSTGRES_PASSWORD=$(openssl rand -base64 32)

# Enable SSL
DATABASE_URL=postgresql://user:pass@host:5432/db?sslmode=require
```

### 3. API Security
- Enable HTTPS only in production
- Set secure CORS headers
- Implement rate limiting
- Validate all inputs

### 4. Razorpay Integration
- Store keys in secure vault
- Use webhook secret for verification
- Enable IP whitelisting
- Monitor payment logs

### 5. Authentication
- Rotate JWT secrets regularly
- Implement OAuth2 for third-party access
- Use short-lived tokens with refresh tokens
- Enable MFA for admin accounts

### 6. Network Security
- Use VPC for database/cache
- Implement network policies
- Enable WAF (Web Application Firewall)
- Use TLS 1.3 for all connections

## Troubleshooting

### API Won't Start
```bash
# Check logs
docker-compose logs api

# Verify environment variables
echo $DATABASE_URL

# Test database connection
psql $DATABASE_URL -c "SELECT 1"
```

### Database Connection Issues
```bash
# Check PostgreSQL is running
docker-compose ps postgres

# Verify credentials
psql -U invoice_user -d invoice_db -h localhost

# Run migrations
go run cmd/migrate/main.go up
```

### Memory Issues
```bash
# Increase limits
docker-compose down
# Edit docker-compose.yml and increase memory limits

# Check resource usage
docker stats
```

### WebSocket Issues
```bash
# Verify port is open
netstat -tuln | grep 8080

# Check connection
wscat -c ws://localhost:8080/ws

# Monitor WebSocket stats
curl http://localhost:8080/ws/stats
```

## Scaling Considerations

### Horizontal Scaling
1. Run multiple API instances behind load balancer
2. Use sticky sessions for WebSocket connections
3. Share state in Redis/PostgreSQL

### Vertical Scaling
```bash
# Docker compose increase resources
docker-compose down
# Edit docker-compose.yml
docker-compose up -d
```

### Database Optimization
- Enable query logging
- Create indexes for frequent queries
- Use connection pooling
- Monitor slow queries

## Health Checks

### API Health
```bash
curl http://localhost:8080/health

# Detailed health
curl http://localhost:8080/health/detailed
```

### Database Health
```bash
curl http://localhost:8080/health/db
```

### Cache Health
```bash
curl http://localhost:8080/health/cache
```

## Rollback Strategy

### Docker Rollback
```bash
# Use previous image version
docker-compose down
docker-compose up -d  # Previous compose file
```

### Kubernetes Rollback
```bash
# View rollout history
kubectl rollout history deployment/invoice-api -n invoice-system

# Rollback to previous
kubectl rollout undo deployment/invoice-api -n invoice-system

# Rollback to specific revision
kubectl rollout undo deployment/invoice-api --to-revision=2 -n invoice-system
```

## Support

For issues or questions:
1. Check logs: `docker-compose logs -f api`
2. Review documentation: See `/docs`
3. Contact support: api-support@example.com
4. Report issues: https://github.com/yourorg/invoice-backend/issues
