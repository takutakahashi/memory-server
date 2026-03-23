package migrate

import (
	"context"
	"errors"
	"fmt"
	"log"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/attributevalue"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// errMigrationSkipped is returned when migrations cannot run due to insufficient
// IAM permissions. The server can still start in this case.
var errMigrationSkipped = errors.New("migration skipped: insufficient permissions")

// isAccessDenied reports whether an error is a DynamoDB AccessDeniedException.
func isAccessDenied(err error) bool {
	return err != nil && strings.Contains(err.Error(), "AccessDeniedException")
}

const schemaMigrationsTable = "schema_migrations"

// appliedRecord represents a row in the schema_migrations table.
type appliedRecord struct {
	Version     int    `dynamodbav:"version"`
	Description string `dynamodbav:"description"`
	AppliedAt   string `dynamodbav:"applied_at"`
}

// Run applies all pending migrations in order.
// It is idempotent: already-applied migrations are tracked in the schema_migrations
// DynamoDB table and will not be re-executed.
// If the caller lacks IAM permissions to access the schema_migrations table,
// migrations are skipped with a warning rather than failing fatally.
func Run(ctx context.Context, cfg aws.Config) error {
	client := newDynamoClient(cfg)

	// 1. Ensure the tracking table exists.
	if err := ensureMigrationsTable(ctx, client); err != nil {
		if errors.Is(err, errMigrationSkipped) {
			log.Printf("[migrate] WARNING: %v – skipping all migrations", err)
			return nil
		}
		return fmt.Errorf("ensure schema_migrations table: %w", err)
	}

	// 2. Load already-applied versions.
	applied, err := loadApplied(ctx, client)
	if err != nil {
		return fmt.Errorf("load applied migrations: %w", err)
	}

	// 3. Apply pending migrations in order.
	for _, m := range allMigrations {
		if _, ok := applied[m.Version]; ok {
			log.Printf("[migrate] version %d already applied – skipping", m.Version)
			continue
		}

		log.Printf("[migrate] applying version %d: %s", m.Version, m.Description)
		if err := m.Up(ctx, client); err != nil {
			return fmt.Errorf("migration %d (%s): %w", m.Version, m.Description, err)
		}

		if err := markApplied(ctx, client, m); err != nil {
			return fmt.Errorf("mark applied for version %d: %w", m.Version, err)
		}

		log.Printf("[migrate] version %d applied successfully", m.Version)
	}

	return nil
}

// newDynamoClient creates a DynamoDB client with optional LocalStack endpoint override.
func newDynamoClient(cfg aws.Config) *dynamodb.Client {
	return dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		if ep := os.Getenv("DYNAMODB_ENDPOINT_URL"); ep != "" {
			o.BaseEndpoint = aws.String(ep)
		}
	})
}

// ensureMigrationsTable creates the schema_migrations table if it does not yet exist.
func ensureMigrationsTable(ctx context.Context, client *dynamodb.Client) error {
	_, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(schemaMigrationsTable),
	})
	if err == nil {
		return nil // already exists
	}

	var notFound *types.ResourceNotFoundException
	if errors.As(err, &notFound) {
		// Table doesn't exist yet — fall through to create it.
	} else if isAccessDenied(err) {
		// Insufficient IAM permissions — skip migrations gracefully.
		return fmt.Errorf("%w: %v", errMigrationSkipped, err)
	} else {
		return fmt.Errorf("describe schema_migrations: %w", err)
	}

	log.Printf("[migrate] creating tracking table %q …", schemaMigrationsTable)
	_, err = client.CreateTable(ctx, &dynamodb.CreateTableInput{
		TableName: aws.String(schemaMigrationsTable),
		AttributeDefinitions: []types.AttributeDefinition{
			{AttributeName: aws.String("version"), AttributeType: types.ScalarAttributeTypeN},
		},
		KeySchema: []types.KeySchemaElement{
			{AttributeName: aws.String("version"), KeyType: types.KeyTypeHash},
		},
		BillingMode: types.BillingModePayPerRequest,
	})
	if err != nil {
		return fmt.Errorf("create schema_migrations: %w", err)
	}

	if err := waitUntilActive(ctx, client, schemaMigrationsTable, 2*time.Minute); err != nil {
		return fmt.Errorf("wait for schema_migrations to become active: %w", err)
	}

	log.Printf("[migrate] tracking table %q is now ACTIVE", schemaMigrationsTable)
	return nil
}

// loadApplied scans the schema_migrations table and returns the set of applied version numbers.
func loadApplied(ctx context.Context, client *dynamodb.Client) (map[int]struct{}, error) {
	applied := make(map[int]struct{})

	var lastKey map[string]types.AttributeValue
	for {
		input := &dynamodb.ScanInput{
			TableName: aws.String(schemaMigrationsTable),
		}
		if lastKey != nil {
			input.ExclusiveStartKey = lastKey
		}

		out, err := client.Scan(ctx, input)
		if err != nil {
			return nil, fmt.Errorf("scan schema_migrations: %w", err)
		}

		for _, item := range out.Items {
			var rec appliedRecord
			if err := attributevalue.UnmarshalMap(item, &rec); err != nil {
				return nil, fmt.Errorf("unmarshal applied record: %w", err)
			}
			applied[rec.Version] = struct{}{}
		}

		if out.LastEvaluatedKey == nil {
			break
		}
		lastKey = out.LastEvaluatedKey
	}

	return applied, nil
}

// markApplied records a migration as applied in the schema_migrations table.
// Uses a conditional write to safely handle concurrent startup scenarios.
func markApplied(ctx context.Context, client *dynamodb.Client, m Migration) error {
	rec := appliedRecord{
		Version:     m.Version,
		Description: m.Description,
		AppliedAt:   time.Now().UTC().Format(time.RFC3339),
	}
	item, err := attributevalue.MarshalMap(rec)
	if err != nil {
		return fmt.Errorf("marshal applied record: %w", err)
	}

	_, err = client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           aws.String(schemaMigrationsTable),
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(version)"),
	})
	if err != nil {
		// If another instance already marked it, treat as success.
		var condFailed *types.ConditionalCheckFailedException
		if errors.As(err, &condFailed) {
			log.Printf("[migrate] version %d already marked by another instance – ignoring", m.Version)
			return nil
		}
		return fmt.Errorf("put applied record: %w", err)
	}
	return nil
}
