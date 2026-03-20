// Package migrate provides a versioned DynamoDB schema migration system.
// Migrations are defined in migrations.go and tracked in the schema_migrations
// DynamoDB table, so each migration is applied exactly once.
package migrate

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
)

// EnsureTables is kept for backward compatibility.
// New code should call Run() directly.
func EnsureTables(ctx context.Context, cfg aws.Config) error {
	return Run(ctx, cfg)
}

// tableDefinition bundles a CreateTableInput for use by migration functions.
type tableDefinition struct {
	input *dynamodb.CreateTableInput
}

// ensureTable creates the table if it does not exist, then waits until it is ACTIVE.
func ensureTable(ctx context.Context, client *dynamodb.Client, def tableDefinition) error {
	tableName := aws.ToString(def.input.TableName)

	_, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: def.input.TableName,
	})
	if err == nil {
		log.Printf("[migrate] table %q already exists – skipping", tableName)
		return nil
	}

	var notFound *types.ResourceNotFoundException
	if !errors.As(err, &notFound) {
		return fmt.Errorf("describe table: %w", err)
	}

	log.Printf("[migrate] creating table %q …", tableName)
	if _, err := client.CreateTable(ctx, def.input); err != nil {
		return fmt.Errorf("create table: %w", err)
	}

	if err := waitUntilActive(ctx, client, tableName, 2*time.Minute); err != nil {
		return fmt.Errorf("wait for table %q to become active: %w", tableName, err)
	}

	log.Printf("[migrate] table %q is now ACTIVE", tableName)
	return nil
}

// waitUntilActive polls DescribeTable until the table status is ACTIVE or the timeout expires.
func waitUntilActive(ctx context.Context, client *dynamodb.Client, tableName string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		out, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
			TableName: aws.String(tableName),
		})
		if err != nil {
			return fmt.Errorf("describe table: %w", err)
		}
		if out.Table.TableStatus == types.TableStatusActive {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
	return fmt.Errorf("timed out waiting for table %q to become ACTIVE", tableName)
}

// ---------------------------------------------------------------------------
// Table definitions (used by migration functions in migrations.go)
// ---------------------------------------------------------------------------

func memoriesTableDef(tableName string) tableDefinition {
	return tableDefinition{
		input: &dynamodb.CreateTableInput{
			TableName: aws.String(tableName),
			AttributeDefinitions: []types.AttributeDefinition{
				{AttributeName: aws.String("memory_id"), AttributeType: types.ScalarAttributeTypeS},
				{AttributeName: aws.String("user_id"), AttributeType: types.ScalarAttributeTypeS},
				{AttributeName: aws.String("created_at"), AttributeType: types.ScalarAttributeTypeS},
				{AttributeName: aws.String("last_accessed_at"), AttributeType: types.ScalarAttributeTypeS},
			},
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String("memory_id"), KeyType: types.KeyTypeHash},
			},
			GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
				{
					IndexName: aws.String("user_id-created_at-index"),
					KeySchema: []types.KeySchemaElement{
						{AttributeName: aws.String("user_id"), KeyType: types.KeyTypeHash},
						{AttributeName: aws.String("created_at"), KeyType: types.KeyTypeRange},
					},
					Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
					ProvisionedThroughput: &types.ProvisionedThroughput{
						ReadCapacityUnits:  aws.Int64(5),
						WriteCapacityUnits: aws.Int64(5),
					},
				},
				{
					IndexName: aws.String("user_id-last_accessed_at-index"),
					KeySchema: []types.KeySchemaElement{
						{AttributeName: aws.String("user_id"), KeyType: types.KeyTypeHash},
						{AttributeName: aws.String("last_accessed_at"), KeyType: types.KeyTypeRange},
					},
					Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
					ProvisionedThroughput: &types.ProvisionedThroughput{
						ReadCapacityUnits:  aws.Int64(5),
						WriteCapacityUnits: aws.Int64(5),
					},
				},
			},
			ProvisionedThroughput: &types.ProvisionedThroughput{
				ReadCapacityUnits:  aws.Int64(5),
				WriteCapacityUnits: aws.Int64(5),
			},
		},
	}
}

func usersTableDef(tableName string) tableDefinition {
	return tableDefinition{
		input: &dynamodb.CreateTableInput{
			TableName: aws.String(tableName),
			AttributeDefinitions: []types.AttributeDefinition{
				{AttributeName: aws.String("user_id"), AttributeType: types.ScalarAttributeTypeS},
				{AttributeName: aws.String("token"), AttributeType: types.ScalarAttributeTypeS},
			},
			KeySchema: []types.KeySchemaElement{
				{AttributeName: aws.String("user_id"), KeyType: types.KeyTypeHash},
			},
			GlobalSecondaryIndexes: []types.GlobalSecondaryIndex{
				{
					IndexName: aws.String("token-index"),
					KeySchema: []types.KeySchemaElement{
						{AttributeName: aws.String("token"), KeyType: types.KeyTypeHash},
					},
					Projection: &types.Projection{ProjectionType: types.ProjectionTypeAll},
					ProvisionedThroughput: &types.ProvisionedThroughput{
						ReadCapacityUnits:  aws.Int64(5),
						WriteCapacityUnits: aws.Int64(5),
					},
				},
			},
			ProvisionedThroughput: &types.ProvisionedThroughput{
				ReadCapacityUnits:  aws.Int64(5),
				WriteCapacityUnits: aws.Int64(5),
			},
		},
	}
}
