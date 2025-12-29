#!/bin/bash
set -e

echo "=== Initializing AWS Resources with awslocal ==="

export AWS_DEFAULT_REGION=us-east-1

echo "Creating test user in Cognito..."
USER_POOL_ID=$(awslocal cognito-idp list-user-pools --max-results 10 --query 'UserPools[0].Id' --output text)

if [ "$USER_POOL_ID" != "None" ] && [ -n "$USER_POOL_ID" ]; then
    echo "Found User Pool: $USER_POOL_ID"
    
    awslocal cognito-idp admin-create-user \
        --user-pool-id "$USER_POOL_ID" \
        --username "test@example.com" \
        --user-attributes Name=email,Value=test@example.com Name=email_verified,Value=true Name=name,Value="Test User" \
        --message-action SUPPRESS || true
    
    awslocal cognito-idp admin-set-user-password \
        --user-pool-id "$USER_POOL_ID" \
        --username "test@example.com" \
        --password "Test123!" \
        --permanent || true
    
    echo "Test user created: test@example.com / Test123!"
else
    echo "No user pool found. Run 'make infra-apply' first."
fi

echo "Verifying S3 buckets..."
awslocal s3 ls

echo "Verifying DynamoDB tables..."
awslocal dynamodb list-tables

echo "Verifying SQS queues..."
awslocal sqs list-queues

echo "Verifying SNS topics..."
awslocal sns list-topics

echo "=== AWS Resources Ready ==="
