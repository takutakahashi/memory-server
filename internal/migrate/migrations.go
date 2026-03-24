package migrate

import (
	"context"
	"os"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
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
		Description: "no-op: org_tokens table removed (org concept replaced by users)",
		Up:          migration002Noop,
	},
	{
		Version:     3,
		Description: "create users table with GSI token-index",
		Up:          migration003CreateUsers,
	},
	{
		Version:     4,
		Description: "add GSI org_id-created_at-index to memories table for org-scoped queries",
		Up:          migration004AddOrgGSI,
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

// migration002Noop is a no-op placeholder that preserves the migration version
// sequence for existing deployments that already ran migration002.
func migration002Noop(_ context.Context, _ *dynamodb.Client) error {
	return nil
}

func migration003CreateUsers(ctx context.Context, client *dynamodb.Client) error {
	tableName := os.Getenv("USERS_TABLE_NAME")
	if tableName == "" {
		tableName = "users"
	}
	return ensureTable(ctx, client, usersTableDef(tableName))
}

func migration004AddOrgGSI(ctx context.Context, client *dynamodb.Client) error {
	tableName := os.Getenv("DYNAMODB_TABLE_NAME")
	if tableName == "" {
		tableName = "memories"
	}
	return addGSIToTable(ctx, client, tableName, "org_id-created_at-index",
		[]types.KeySchemaElement{
			{AttributeName: aws.String("org_id"), KeyType: types.KeyTypeHash},
			{AttributeName: aws.String("created_at"), KeyType: types.KeyTypeRange},
		},
		[]types.AttributeDefinition{
			{AttributeName: aws.String("org_id"), AttributeType: types.ScalarAttributeTypeS},
			// created_at is already defined in the table from migration 1
		},
		&types.Projection{ProjectionType: types.ProjectionTypeAll},
		&types.ProvisionedThroughput{ReadCapacityUnits: aws.Int64(5), WriteCapacityUnits: aws.Int64(5)},
	)
}
