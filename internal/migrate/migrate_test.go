package migrate_test

import (
	"context"
	"fmt"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/takutakahashi/memory-server/internal/migrate"
)

// ---------------------------------------------------------------------------
// Unit tests (no external dependencies)
// ---------------------------------------------------------------------------

// TestMigrations_VersionsAreUniqueAndAscending verifies that allMigrations
// (exposed via migrate.All()) has unique, monotonically increasing version numbers.
func TestMigrations_VersionsAreUniqueAndAscending(t *testing.T) {
	migrations := migrate.All()

	if len(migrations) == 0 {
		t.Fatal("allMigrations must not be empty")
	}

	seen := make(map[int]struct{})
	prev := 0
	for _, m := range migrations {
		if m.Version <= 0 {
			t.Errorf("migration version must be > 0, got %d (%s)", m.Version, m.Description)
		}
		if m.Version <= prev {
			t.Errorf("migrations must be ascending: version %d follows %d", m.Version, prev)
		}
		if _, dup := seen[m.Version]; dup {
			t.Errorf("duplicate migration version: %d", m.Version)
		}
		if m.Description == "" {
			t.Errorf("migration version %d has empty description", m.Version)
		}
		if m.Up == nil {
			t.Errorf("migration version %d has nil Up function", m.Version)
		}
		seen[m.Version] = struct{}{}
		prev = m.Version
	}
}

// TestMigrations_Sorted verifies that allMigrations is sorted by version ascending.
func TestMigrations_Sorted(t *testing.T) {
	migrations := migrate.All()
	if !sort.SliceIsSorted(migrations, func(i, j int) bool {
		return migrations[i].Version < migrations[j].Version
	}) {
		t.Error("allMigrations must be sorted by Version ascending")
	}
}

// ---------------------------------------------------------------------------
// Integration tests (require LocalStack or real DynamoDB)
//
// These tests are skipped unless DYNAMODB_ENDPOINT_URL is set.
// Run them locally with:
//
//	docker compose up -d localstack
//	DYNAMODB_ENDPOINT_URL=http://localhost:4566 go test ./internal/migrate/... -v
// ---------------------------------------------------------------------------

// newTestConfig returns an AWS config pointing at the LocalStack endpoint.
func newTestConfig(t *testing.T) (aws.Config, string) {
	t.Helper()
	endpoint := os.Getenv("DYNAMODB_ENDPOINT_URL")
	if endpoint == "" {
		t.Skip("DYNAMODB_ENDPOINT_URL not set – skipping integration test")
	}

	cfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion("ap-northeast-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider("test", "test", "")),
	)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	return cfg, endpoint
}

// newTestClient returns a DynamoDB client pointed at LocalStack.
func newTestClient(t *testing.T, cfg aws.Config, endpoint string) *dynamodb.Client {
	t.Helper()
	return dynamodb.NewFromConfig(cfg, func(o *dynamodb.Options) {
		o.BaseEndpoint = aws.String(endpoint)
	})
}

// uniquePrefix returns a test-scoped prefix to avoid table name collisions
// between parallel or repeated test runs.
func uniquePrefix(t *testing.T) string {
	t.Helper()
	return fmt.Sprintf("test_%d_", time.Now().UnixNano())
}

// deleteTable removes a table and ignores "not found" errors.
func deleteTable(ctx context.Context, client *dynamodb.Client, name string) {
	_, _ = client.DeleteTable(ctx, &dynamodb.DeleteTableInput{
		TableName: aws.String(name),
	})
}

// tableExists returns true if the named table is ACTIVE.
func tableExists(ctx context.Context, client *dynamodb.Client, name string) bool {
	out, err := client.DescribeTable(ctx, &dynamodb.DescribeTableInput{
		TableName: aws.String(name),
	})
	if err != nil {
		return false
	}
	return out.Table.TableStatus == types.TableStatusActive
}

// appliedVersions returns the set of versions recorded in schema_migrations.
func appliedVersions(ctx context.Context, t *testing.T, client *dynamodb.Client, migrationsTable string) map[int]struct{} {
	t.Helper()
	out, err := client.Scan(ctx, &dynamodb.ScanInput{
		TableName: aws.String(migrationsTable),
	})
	if err != nil {
		t.Fatalf("scan schema_migrations: %v", err)
	}
	applied := make(map[int]struct{})
	for _, item := range out.Items {
		v, ok := item["version"]
		if !ok {
			continue
		}
		nv, ok := v.(*types.AttributeValueMemberN)
		if !ok {
			continue
		}
		var n int
		fmt.Sscanf(nv.Value, "%d", &n)
		applied[n] = struct{}{}
	}
	return applied
}

// TestRun_FirstStartup verifies that Run creates all tables and records
// every migration in schema_migrations.
func TestRun_FirstStartup(t *testing.T) {
	cfg, endpoint := newTestConfig(t)
	ctx := context.Background()
	prefix := uniquePrefix(t)
	client := newTestClient(t, cfg, endpoint)

	// Set up unique table names for this test run.
	t.Setenv("DYNAMODB_TABLE_NAME", prefix+"memories")
	t.Setenv("ORG_TOKENS_TABLE_NAME", prefix+"org_tokens")
	t.Setenv("USERS_TABLE_NAME", prefix+"users")
	t.Setenv("DYNAMODB_ENDPOINT_URL", endpoint)

	// Cleanup after test.
	t.Cleanup(func() {
		deleteTable(ctx, client, prefix+"memories")
		deleteTable(ctx, client, prefix+"org_tokens")
		deleteTable(ctx, client, prefix+"users")
		deleteTable(ctx, client, "schema_migrations")
	})

	if err := migrate.Run(ctx, cfg); err != nil {
		t.Fatalf("Run() error: %v", err)
	}

	// All data tables must exist.
	for _, table := range []string{prefix + "memories", prefix + "org_tokens", prefix + "users"} {
		if !tableExists(ctx, client, table) {
			t.Errorf("table %q does not exist after Run()", table)
		}
	}

	// schema_migrations must have one row per migration.
	applied := appliedVersions(ctx, t, client, "schema_migrations")
	for _, m := range migrate.All() {
		if _, ok := applied[m.Version]; !ok {
			t.Errorf("migration version %d not recorded in schema_migrations", m.Version)
		}
	}
}

// TestRun_Idempotent verifies that calling Run() twice does not fail or
// create duplicate entries in schema_migrations.
func TestRun_Idempotent(t *testing.T) {
	cfg, endpoint := newTestConfig(t)
	ctx := context.Background()
	prefix := uniquePrefix(t)
	client := newTestClient(t, cfg, endpoint)

	t.Setenv("DYNAMODB_TABLE_NAME", prefix+"memories")
	t.Setenv("ORG_TOKENS_TABLE_NAME", prefix+"org_tokens")
	t.Setenv("USERS_TABLE_NAME", prefix+"users")
	t.Setenv("DYNAMODB_ENDPOINT_URL", endpoint)

	t.Cleanup(func() {
		deleteTable(ctx, client, prefix+"memories")
		deleteTable(ctx, client, prefix+"org_tokens")
		deleteTable(ctx, client, prefix+"users")
		deleteTable(ctx, client, "schema_migrations")
	})

	// First run.
	if err := migrate.Run(ctx, cfg); err != nil {
		t.Fatalf("first Run() error: %v", err)
	}

	// Second run must succeed without error.
	if err := migrate.Run(ctx, cfg); err != nil {
		t.Fatalf("second Run() error: %v", err)
	}

	// Exactly one row per migration version must exist.
	applied := appliedVersions(ctx, t, client, "schema_migrations")
	allMigrations := migrate.All()
	if len(applied) != len(allMigrations) {
		t.Errorf("schema_migrations has %d rows, want %d", len(applied), len(allMigrations))
	}
}
