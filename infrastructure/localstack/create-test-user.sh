#!/bin/bash
set -e

POOL_ID=$(awslocal cognito-idp list-user-pools --max-results 1 | jq -r '.UserPools[0].Id')
CLIENT_ID=$(awslocal cognito-idp list-user-pool-clients --user-pool-id $POOL_ID | jq -r '.UserPoolClients[0].ClientId')

EMAIL="testuser@example.com"
PASSWORD="TestPass123!"
USERNAME="testuser"

echo "Creating user..."
awslocal cognito-idp sign-up \
  --client-id $CLIENT_ID \
  --username $USERNAME \
  --password $PASSWORD \
  --user-attributes Name=email,Value=$EMAIL

echo "Confirming user..."
CODE=$(docker logs invoice-localstack 2>&1 | grep -oP 'Confirmation code for Cognito user.*: \K\d+' | tail -1)
if [ -z "$CODE" ]; then
  echo "No confirmation code found in logs. Trying alternative method..."
  awslocal cognito-idp admin-confirm-sign-up \
    --user-pool-id $POOL_ID \
    --username $USERNAME
else
  awslocal cognito-idp confirm-sign-up \
    --client-id $CLIENT_ID \
    --username $USERNAME \
    --confirmation-code $CODE
fi

echo "Adding user to admin group..."
awslocal cognito-idp admin-add-user-to-group \
  --user-pool-id $POOL_ID \
  --username $USERNAME \
  --group-name admin

echo "Logging in..."
TOKENS=$(awslocal cognito-idp initiate-auth \
  --client-id $CLIENT_ID \
  --auth-flow ADMIN_NO_SRP_AUTH \
  --auth-parameters USERNAME=$USERNAME,PASSWORD=$PASSWORD)

ID_TOKEN=$(echo $TOKENS | jq -r '.AuthenticationResult.IdToken')
ACCESS_TOKEN=$(echo $TOKENS | jq -r '.AuthenticationResult.AccessToken')
REFRESH_TOKEN=$(echo $TOKENS | jq -r '.AuthenticationResult.RefreshToken')

echo "================================"
echo "Test User Created Successfully!"
echo "================================"
echo "Email: $EMAIL"
echo "Username: $USERNAME"
echo "Password: $PASSWORD"
echo ""
echo "ID Token:"
echo "$ID_TOKEN"
echo ""
echo "Access Token:"
echo "$ACCESS_TOKEN"
echo ""
echo "Refresh Token:"
echo "$REFRESH_TOKEN"
echo ""
echo "Use ID Token as 'Authorization: Bearer $ID_TOKEN' header"
