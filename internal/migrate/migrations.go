package migrate

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
)

// Migration represents a single, versioned schema change.
// Migrations are applied in ascending Version order and tracked in the
// schema_migrations DynamoDB table so they are never re-executed.
//
// To add a new migration:
//  1. Implement a migrationNNN function below.
//  2. Append a new Migration entry at the END of allMigrations.
//     Never change the Version of an existing entry.
type Migration struct {
	Version     int // Unique, monotonically increasing (1, 2, 3 …)
	Description string
	Up          func(ctx context.Context, client *dynamodb.Client) error
}

// All returns a copy of the ordered migration list.
// Intended for use in tests and diagnostic tooling.
func All() []Migration {
	cp := make([]Migration, len(allMigrations))
	copy(cp, allMigrations)
	return cp
}

// allMigrations is the ordered list of all schema migrations.
// New migrations must always be appended at the end.
var allMigrations = []Migration{
	{
		Version:     1,
		Description: "create memories table with GSIs user_id-created_at-index and user_id-last_accessed_at-index",
		Up:          migration001CreateMemories,
	},
	{
		Version:     2,
		Description: "create org_tokens table",
		Up:          migration002CreateOrgTokens,
	},
	{
		Version:     3,
		Description: "create users table with GSI token-index",
		Up:          migration003CreateUsers,
	},
}

// ---------------------------------------------------------------------------
// Migration implementations
// ---------------------------------------------------------------------------

func migration001CreateMemories(ctx context.Context, client *dynamodb.Client) error {
	tableName := os.Getenv("DYNAMODB_TABLE_NAME")
	if tableName == "" {
		tableName = "memories"
	}
	return ensureTable(ctx, client, memoriesTableDef(tableName))
}

func migration002CreateOrgTokens(ctx context.Context, client *dynamodb.Client) error {
	tableName := os.Getenv("ORG_TOKENS_TABLE_NAME")
	if tableName == "" {
		tableName = "org_tokens"
	}
	return ensureTable(ctx, client, orgTokensTableDef(tableName))
}

func migration003CreateUsers(ctx context.Context, client *dynamodb.Client) error {
	tableName := os.Getenv("USERS_TABLE_NAME")
	if tableName == "" {
		tableName = "users"
	}
	return ensureTable(ctx, client, usersTableDef(tableName))
}
