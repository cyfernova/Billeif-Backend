# AWS Deployment Guide - Swagger UI Fix

## Issue Fixed

The Swagger UI was not showing all endpoints due to swagger annotation issues. This has been resolved by:
1. Fixing `models.User` references in auth_handler.go (changed to `map[string]interface{}`)
2. Ensuring all agent config endpoints have proper swagger annotations
3. Regenerating swagger docs with `--parseInternal` flag

## Verified Endpoints

All endpoints are now properly documented in Swagger UI:

### Agent Configuration Routes
- ✅ `POST /api/v1/agents/config` - Create agent configuration
- ✅ `GET /api/v1/agents/config` - Get all agent configurations  
- ✅ `GET /api/v1/agents/config/{agent_id}` - Get specific agent configuration
- ✅ `PUT /api/v1/agents/config/{agent_id}` - Update agent configuration
- ✅ `DELETE /api/v1/agents/config/{agent_id}` - Delete agent configuration
- ✅ `POST /api/v1/agents/config/default` - Create default configuration

### Mentee Routes
- ✅ `GET /api/v1/agents/mentee/recommendation/{negotiation_id}` - Get mentee recommendation
- ✅ `GET /api/v1/agents/mentee/learning/{agent_id}` - Get mentee learning data
- ✅ `DELETE /api/v1/agents/mentee/learning/{agent_id}` - Reset mentee learning data
- ✅ `GET /api/v1/agents/mentee/export` - Export mentee learning data
- ✅ `POST /api/v1/agents/mentee/import` - Import mentee learning data

### Bargaining Routes
- ✅ `POST /api/v1/bargaining/negotiations` - Create negotiation
- ✅ `GET /api/v1/bargaining/negotiations` - List negotiations
- ✅ `GET /api/v1/bargaining/negotiations/{id}` - Get negotiation details
- ✅ `POST /api/v1/bargaining/negotiations/{id}/counteroffer` - Submit counter offer
- ✅ `GET /api/v1/bargaining/negotiations/{id}/rounds` - Get negotiation rounds
- ✅ `GET /api/v1/bargaining/negotiations/{id}/suggest` - Get suggested counter offer

## Deployment Steps

### 1. Pre-deployment Checks

```bash
# Build the application
go build -o .build/api ./cmd/api

# Test locally (optional)
./build/api

# Verify swagger files exist
ls -lh docs/
# Should show: docs.go, swagger.json, swagger.yaml
```

### 2. AWS Deployment (ECS)

#### Option A: Using AWS CLI

```bash
# Build Docker image
docker build -t invoice-backend:latest .

# Tag for ECR
docker tag invoice-backend:latest <your-ecr-repo>/invoice-backend:latest

# Push to ECR
aws ecr get-login-password --region us-east-1 | docker login --username AWS --password-stdin <your-ecr-repo>
docker push <your-ecr-repo>/invoice-backend:latest

# Update ECS service
aws ecs update-service --cluster invoice-backend-cluster --service invoice-backend-service --force-new-deployment
```

#### Option B: Using CI/CD Pipeline

Ensure your CI/CD pipeline includes:
1. Build step: `go build ./cmd/api`
2. Swagger generation: `swag init -g cmd/api/main.go -o docs/`
3. Package and deploy to ECS

### 3. Environment Variables for AWS

Ensure these are set in ECS task definition or AWS Systems Manager Parameter Store:

```env
DATABASE_HOST=invoice-backend-postgres.c01i2mcsikp8.us-east-1.rds.amazonaws.com
DATABASE_PORT=5432
DATABASE_USER=invoice_user
DATABASE_PASSWORD=<secure-password>
DATABASE_NAME=invoice_db
DATABASE_SSL_MODE=require

REDIS_HOST=invoice-backend-redis.oijf1i.0001.use1.cache.amazonaws.com
REDIS_PORT=6379
REDIS_DB=0

AWS_REGION=us-east-1

ENVIRONMENT=production
SERVER_PORT=8080

S3_BUCKET_LOGOS=invoice-backend-dev-business-logos
S3_BUCKET_INVOICES=invoice-backend-dev-invoices-pdf
S3_BUCKET_PRODUCTS=invoice-backend-dev-product-images

SQS_INVOICE_QUEUE=https://sqs.us-east-1.amazonaws.com/<account-id>/invoice-processing-queue
SQS_PAYMENT_QUEUE=https://sqs.us-east-1.amazonaws.com/<account-id>/payment-processing-queue

SNS_ALERTS_TOPIC_ARN=arn:aws:sns:us-east-1:<account-id>:invoice-backend-alerts
```

### 4. Security Groups and NACLs

Ensure your RDS and ElastiCache security groups allow:
- Inbound from ECS task security group on ports 5432 and 6379
- ECS task security group allows outbound on all necessary ports

### 5. ALB Configuration

```json
{
  "TargetGroups": [
    {
      "TargetGroupArn": "arn:aws:elasticloadbalancing:...:targetgroup/invoice-backend",
      "HealthCheckPath": "/health",
      "HealthCheckIntervalSeconds": 30,
      "HealthCheckTimeoutSeconds": 5,
      "HealthyThresholdCount": 2,
      "UnhealthyThresholdCount": 3,
      "Matcher": {
        "HttpCode": "200"
      }
    }
  ]
}
```

## Verification

After deployment, verify Swagger UI is accessible:

```bash
# Get ALB DNS name
aws elbv2 describe-load-balancers --names invoice-backend-alb --query 'LoadBalancers[0].DNSName' --output text

# Test Swagger UI
curl https://<alb-dns>/swagger/index.html

# Test Swagger JSON
curl https://<alb-dns>/swagger/doc.json

# Test specific API endpoint
curl https://<alb-dns>/api/v1/health
```

## Swagger UI Access

Swagger UI will be available at:
```
https://<your-alb-dns-name>/swagger/index.html
```

Or through API Gateway:
```
https://<api-gateway-id>.execute-api.us-east-1.amazonaws.com/swagger/index.html
```

## Troubleshooting

### Swagger UI Not Loading

1. Check if docs are included in the Docker image:
   ```bash
   docker run <image> ls -la /app/docs/
   ```

2. Check if swagger route is registered in main.go:
   ```bash
   grep -n "swagger" cmd/api/main.go
   ```

3. Check ECS task logs:
   ```bash
   aws logs get-log-events --log-group /ecs/invoice-backend --log-stream-name ecs/invoice-backend/<task-id>
   ```

### Missing Endpoints

1. Verify swagger annotations exist:
   ```bash
   grep -n "@Router" internal/handlers/agent_config_handler.go
   ```

2. Regenerate docs:
   ```bash
   swag init -g cmd/api/main.go -o docs/ --parseInternal
   ```

3. Rebuild and redeploy

### Database Connection Issues

1. Check RDS is accessible from ECS:
   ```bash
   # From ECS task
   nc -zv <rds-endpoint> 5432
   ```

2. Verify security group rules

3. Check RDS parameter group (if needed)

## Monitoring

Enable logging for:
- Swagger API requests
- Agent configuration changes
- Bargaining negotiations
- Mentee learning data

## Rolling Back

If issues occur:
```bash
# Rollback to previous ECS task definition
aws ecs update-service --cluster invoice-backend-cluster --service invoice-backend-service --task-definition <previous-task-def>
```

## Success Indicators

✅ Swagger UI loads and displays all endpoints
✅ Agent Configuration section shows all CRUD operations
✅ Bargaining section shows all negotiation endpoints
✅ Mentee section shows all learning endpoints
✅ "Try it out" buttons work for authenticated endpoints
✅ Health check returns 200 OK

## Next Steps

1. Deploy to ECS using the steps above
2. Verify Swagger UI is accessible
3. Test agent configuration endpoints
4. Test bargaining workflow
5. Test mentee learning data management
