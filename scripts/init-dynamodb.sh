#!/usr/bin/env bash
# Initialize DynamoDB table for memory-server

set -euo pipefail

TABLE_NAME="${DYNAMODB_TABLE_NAME:-memories}"
ENDPOINT_URL="${DYNAMODB_ENDPOINT_URL:-}"
REGION="${AWS_REGION:-ap-northeast-1}"

ENDPOINT_ARG=""
if [ -n "$ENDPOINT_URL" ]; then
  ENDPOINT_ARG="--endpoint-url $ENDPOINT_URL"
fi

# Keep backward-compatible alias
EXTRA_ARGS="$ENDPOINT_ARG"

echo "Creating DynamoDB table: $TABLE_NAME in region $REGION"

aws dynamodb create-table \
  --region "$REGION" \
  $EXTRA_ARGS \
  --table-name "$TABLE_NAME" \
  --attribute-definitions \
    AttributeName=memory_id,AttributeType=S \
    AttributeName=user_id,AttributeType=S \
    AttributeName=created_at,AttributeType=S \
    AttributeName=last_accessed_at,AttributeType=S \
  --key-schema \
    AttributeName=memory_id,KeyType=HASH \
  --global-secondary-indexes \
    '[
      {
        "IndexName": "user_id-created_at-index",
        "KeySchema": [
          {"AttributeName": "user_id", "KeyType": "HASH"},
          {"AttributeName": "created_at", "KeyType": "RANGE"}
        ],
        "Projection": {"ProjectionType": "ALL"},
        "ProvisionedThroughput": {"ReadCapacityUnits": 5, "WriteCapacityUnits": 5}
      },
      {
        "IndexName": "user_id-last_accessed_at-index",
        "KeySchema": [
          {"AttributeName": "user_id", "KeyType": "HASH"},
          {"AttributeName": "last_accessed_at", "KeyType": "RANGE"}
        ],
        "Projection": {"ProjectionType": "ALL"},
        "ProvisionedThroughput": {"ReadCapacityUnits": 5, "WriteCapacityUnits": 5}
      }
    ]' \
  --provisioned-throughput ReadCapacityUnits=5,WriteCapacityUnits=5

echo "Table $TABLE_NAME created successfully."

# -------------------------------------------------------------------------
# org_tokens table
# -------------------------------------------------------------------------
ORG_TOKENS_TABLE="${ORG_TOKENS_TABLE_NAME:-org_tokens}"
echo "Creating DynamoDB table: $ORG_TOKENS_TABLE in region $REGION"

aws dynamodb create-table \
  --region "$REGION" \
  $EXTRA_ARGS \
  --table-name "$ORG_TOKENS_TABLE" \
  --attribute-definitions \
    AttributeName=token,AttributeType=S \
  --key-schema \
    AttributeName=token,KeyType=HASH \
  --provisioned-throughput ReadCapacityUnits=5,WriteCapacityUnits=5

echo "Table $ORG_TOKENS_TABLE created successfully."

# -------------------------------------------------------------------------
# users table
# -------------------------------------------------------------------------
USERS_TABLE="${USERS_TABLE_NAME:-users}"
echo "Creating DynamoDB table: $USERS_TABLE in region $REGION"

aws dynamodb create-table \
  --region "$REGION" \
  $EXTRA_ARGS \
  --table-name "$USERS_TABLE" \
  --attribute-definitions \
    AttributeName=user_id,AttributeType=S \
    AttributeName=token,AttributeType=S \
  --key-schema \
    AttributeName=user_id,KeyType=HASH \
  --global-secondary-indexes \
    '[
      {
        "IndexName": "token-index",
        "KeySchema": [
          {"AttributeName": "token", "KeyType": "HASH"}
        ],
        "Projection": {"ProjectionType": "ALL"},
        "ProvisionedThroughput": {"ReadCapacityUnits": 5, "WriteCapacityUnits": 5}
      }
    ]' \
  --provisioned-throughput ReadCapacityUnits=5,WriteCapacityUnits=5

echo "Table $USERS_TABLE created successfully."
