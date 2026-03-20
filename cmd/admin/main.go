package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/google/uuid"
	"github.com/takutakahashi/memory-server/internal/auth"
)

func main() {
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	group := os.Args[1]
	subArgs := os.Args[2:]

	switch group {
	case "org":
		if err := runOrgCommand(subArgs); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command group: %s\n", group)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: memory-admin <command> [options]

Commands:
  org create  --org-id ORG_ID [--description TEXT] [--token TOKEN]
              Create a new organization and generate an org token.

Environment:
  AWS_REGION             AWS region (default: us-east-1)
  DYNAMODB_ENDPOINT_URL  Override DynamoDB endpoint (e.g. http://localhost:4566 for LocalStack)
  ORG_TOKENS_TABLE_NAME  DynamoDB table name for org tokens (default: org_tokens)
`)
}

func runOrgCommand(args []string) error {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: memory-admin org <subcommand>\n\nSubcommands:\n  create\n")
		return fmt.Errorf("subcommand required")
	}

	switch args[0] {
	case "create":
		return runOrgCreate(args[1:])
	default:
		return fmt.Errorf("unknown org subcommand: %s", args[0])
	}
}

func runOrgCreate(args []string) error {
	fs := flag.NewFlagSet("org create", flag.ContinueOnError)
	orgID := fs.String("org-id", "", "Organization ID (required)")
	description := fs.String("description", "", "Description of the organization")
	token := fs.String("token", "", "Org token (optional; auto-generated as 'org_<uuid>' if not specified)")
	outputJSON := fs.Bool("json", false, "Output result as JSON")

	if err := fs.Parse(args); err != nil {
		return err
	}

	if *orgID == "" {
		fs.Usage()
		return fmt.Errorf("--org-id is required")
	}

	// Generate token if not provided
	orgToken := *token
	if orgToken == "" {
		orgToken = "org_" + uuid.New().String()
	}

	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	store := auth.NewStore(cfg)

	t := &auth.OrgToken{
		Token:       orgToken,
		OrgID:       *orgID,
		Description: *description,
		CreatedAt:   time.Now().UTC(),
	}

	if err := store.PutOrgToken(ctx, t); err != nil {
		return fmt.Errorf("create org token: %w", err)
	}

	if *outputJSON {
		b, _ := json.MarshalIndent(t, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("Organization created successfully.\n\n")
		fmt.Printf("  org_id:      %s\n", t.OrgID)
		fmt.Printf("  token:       %s\n", t.Token)
		fmt.Printf("  description: %s\n", t.Description)
		fmt.Printf("  created_at:  %s\n", t.CreatedAt.Format(time.RFC3339))
		fmt.Printf("\nKeep the token safe — it cannot be retrieved again.\n")
	}

	return nil
}
