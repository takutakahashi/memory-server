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
	case "user":
		if err := runUserCommand(subArgs); err != nil {
			fmt.Fprintf(os.Stderr, "error: %v\n", err)
			os.Exit(1)
		}
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", group)
		printUsage()
		os.Exit(1)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: memory-admin <command> [options]

Commands:
  user create  --user-id USER_ID [--description TEXT] [--token TOKEN]
               Create a new user and generate an API token.

Environment:
  AWS_REGION             AWS region (default: us-east-1)
  DYNAMODB_ENDPOINT_URL  Override DynamoDB endpoint (e.g. http://localhost:4566 for LocalStack)
  USERS_TABLE_NAME       DynamoDB table name for users (default: users)
`)
}

func runUserCommand(args []string) error {
	if len(args) == 0 {
		fmt.Fprintf(os.Stderr, "Usage: memory-admin user <subcommand>\n\nSubcommands:\n  create\n")
		return fmt.Errorf("subcommand required")
	}

	switch args[0] {
	case "create":
		return runUserCreate(args[1:])
	default:
		return fmt.Errorf("unknown user subcommand: %s", args[0])
	}
}

func runUserCreate(args []string) error {
	fs := flag.NewFlagSet("user create", flag.ContinueOnError)
	userID := fs.String("user-id", "", "User ID (optional; auto-generated if not specified)")
	description := fs.String("description", "", "Description of the user")
	token := fs.String("token", "", "API token (optional; auto-generated as 'usr_<uuid>' if not specified)")
	outputJSON := fs.Bool("json", false, "Output result as JSON")

	if err := fs.Parse(args); err != nil {
		return err
	}

	// Auto-generate user_id if not provided
	uid := *userID
	if uid == "" {
		uid = uuid.NewString()
	}

	// Generate token if not provided
	userToken := *token
	if userToken == "" {
		userToken = "usr_" + uuid.NewString()
	}

	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		return fmt.Errorf("load AWS config: %w", err)
	}

	store := auth.NewStore(cfg)

	u := &auth.User{
		UserID:      uid,
		Token:       userToken,
		Description: *description,
		CreatedAt:   time.Now().UTC(),
	}

	if err := store.PutUser(ctx, u); err != nil {
		return fmt.Errorf("create user: %w", err)
	}

	if *outputJSON {
		b, _ := json.MarshalIndent(u, "", "  ")
		fmt.Println(string(b))
	} else {
		fmt.Printf("User created successfully.\n\n")
		fmt.Printf("  user_id:     %s\n", u.UserID)
		fmt.Printf("  token:       %s\n", u.Token)
		fmt.Printf("  description: %s\n", u.Description)
		fmt.Printf("  created_at:  %s\n", u.CreatedAt.Format(time.RFC3339))
		fmt.Printf("\nKeep the token safe — it cannot be retrieved again.\n")
	}

	return nil
}
