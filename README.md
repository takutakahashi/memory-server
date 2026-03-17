# Memory MCP Server

A Remote MCP Server (HTTP/SSE transport) that provides long-term memory capabilities using AWS S3 Vectors for vector similarity search and DynamoDB for metadata storage.

## Architecture

```
Claude (MCP Client)
  ↓ HTTP/SSE
Memory MCP Server (Go)
  ├── S3 Vectors     ← Vector similarity search
  ├── DynamoDB       ← Metadata ledger, listing, filtering
  └── Bedrock        ← Embedding generation (Titan Embeddings V2)
```

## MCP Tools

| Tool | Description |
|------|-------------|
| `add_memory` | Store a new memory with embedding |
| `search_memories` | Semantic similarity search with optional tag filters |
| `list_memories` | Paginated list of memories for a user |
| `get_memory` | Retrieve a single memory by ID |
| `update_memory` | Update content and/or tags of a memory |
| `delete_memory` | Delete a memory |

## Environment Variables

```
PORT=8080
AWS_REGION=ap-northeast-1
AWS_ACCESS_KEY_ID=...               # Optional (not needed with IAM role)
AWS_SECRET_ACCESS_KEY=...           # Optional
DYNAMODB_TABLE_NAME=memories
S3_VECTORS_BUCKET_NAME=my-memory-bucket
S3_VECTORS_INDEX_NAME=memories
BEDROCK_REGION=ap-northeast-1      # Bedrock region (if different from S3)
DECAY_LAMBDA=0.01                  # Scoring: creation age decay factor
DECAY_MU=0.005                     # Scoring: last access age decay factor
MCP_SERVER_NAME=memory-server
MCP_SERVER_VERSION=1.0.0
```

## DynamoDB Table Setup

Run the initialization script to create the DynamoDB table:

```bash
# Production (AWS)
./scripts/init-dynamodb.sh

# LocalStack (local development)
DYNAMODB_ENDPOINT_URL=http://localhost:4566 \
AWS_ACCESS_KEY_ID=test \
AWS_SECRET_ACCESS_KEY=test \
./scripts/init-dynamodb.sh
```

## Local Development

Uses LocalStack for DynamoDB (S3 Vectors and Bedrock require real AWS):

```bash
docker-compose up
```

## Building

```bash
go build ./cmd/server
```

## Scoring Algorithm

Search results are ranked using a composite score:

```
finalScore = similarityScore
           × exp(-lambda × daysSinceCreated)
           × log1p(accessCount)
           × exp(-mu × daysSinceAccessed)
```

## S3 Vectors

Since `github.com/aws/aws-sdk-go-v2/service/s3vectors` is not yet available in the AWS SDK, this server uses a custom HTTP client with SigV4 signing to call the S3 Vectors API directly.

API endpoint: `https://s3vectors.{region}.amazonaws.com`
