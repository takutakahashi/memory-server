package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
)

// tagList is a flag.Value that accumulates multiple --tag values.
type tagList []string

func (t *tagList) String() string {
	return strings.Join(*t, ",")
}

func (t *tagList) Set(v string) error {
	*t = append(*t, v)
	return nil
}

func main() {
	// Global flags
	globalFlags := flag.NewFlagSet("memory-cli", flag.ContinueOnError)
	serverURL := globalFlags.String("server", "", "Server base URL (overrides MEMORY_SERVER_URL)")
	rawJSON := globalFlags.Bool("json", false, "Output raw JSON")

	// Parse only up to the subcommand
	if len(os.Args) < 2 {
		printUsage()
		os.Exit(1)
	}

	// Collect global args before subcommand
	var globalArgs []string
	var subcommand string
	var subArgs []string
	for i := 1; i < len(os.Args); i++ {
		arg := os.Args[i]
		if arg == "--server" || arg == "-server" {
			if i+1 < len(os.Args) {
				globalArgs = append(globalArgs, arg, os.Args[i+1])
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--server=") || strings.HasPrefix(arg, "-server=") {
			globalArgs = append(globalArgs, arg)
			continue
		}
		if arg == "--json" || arg == "-json" {
			globalArgs = append(globalArgs, arg)
			continue
		}
		// First non-flag arg is the subcommand
		subcommand = arg
		subArgs = os.Args[i+1:]
		break
	}

	if err := globalFlags.Parse(globalArgs); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	baseURL := *serverURL
	if baseURL == "" {
		baseURL = os.Getenv("MEMORY_SERVER_URL")
	}
	if baseURL == "" {
		baseURL = "http://localhost:8080"
	}
	// Remove trailing slash
	baseURL = strings.TrimRight(baseURL, "/")

	if subcommand == "" {
		printUsage()
		os.Exit(1)
	}

	var result interface{}
	var err error

	switch subcommand {
	case "add":
		result, err = runAdd(baseURL, subArgs)
	case "search":
		result, err = runSearch(baseURL, subArgs)
	case "list":
		result, err = runList(baseURL, subArgs)
	case "get":
		result, err = runGet(baseURL, subArgs)
	case "update":
		result, err = runUpdate(baseURL, subArgs)
	case "delete":
		result, err = runDelete(baseURL, subArgs)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", subcommand)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if *rawJSON {
		b, _ := json.Marshal(result)
		fmt.Println(string(b))
	} else {
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: memory-cli [--server URL] [--json] <command> [options]

Commands:
  add     --user-id ID --content TEXT [--tag TAG ...]
  search  --user-id ID --query TEXT [--tag TAG ...] [--limit N]
  list    --user-id ID [--limit N] [--next-token TOKEN]
  get     <memory-id>
  update  <memory-id> [--content TEXT] [--tag TAG ...]
  delete  <memory-id>

Environment:
  MEMORY_SERVER_URL  Server base URL (default: http://localhost:8080)

Options:
  --server URL    Override server URL
  --json          Output raw JSON (default: pretty-printed)
`)
}

func runAdd(baseURL string, args []string) (interface{}, error) {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	userID := fs.String("user-id", "", "User ID")
	content := fs.String("content", "", "Content of the memory")
	var tags tagList
	fs.Var(&tags, "tag", "Tag (can be specified multiple times)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if *content == "" {
		fs.Usage()
		return nil, fmt.Errorf("--content is required")
	}

	body := map[string]interface{}{
		"user_id":  *userID,
		"content":  *content,
		"tags":     []string(tags),
	}

	return doRequest(http.MethodPost, baseURL+"/api/v1/memories", body)
}

func runSearch(baseURL string, args []string) (interface{}, error) {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	userID := fs.String("user-id", "", "User ID")
	query := fs.String("query", "", "Search query")
	limit := fs.Int("limit", 0, "Number of results")
	var tags tagList
	fs.Var(&tags, "tag", "Tag filter (can be specified multiple times)")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}
	if *query == "" {
		fs.Usage()
		return nil, fmt.Errorf("--query is required")
	}

	body := map[string]interface{}{
		"user_id": *userID,
		"query":   *query,
		"tags":    []string(tags),
		"limit":   *limit,
	}

	return doRequest(http.MethodPost, baseURL+"/api/v1/memories/search", body)
}

func runList(baseURL string, args []string) (interface{}, error) {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	userID := fs.String("user-id", "", "User ID")
	limit := fs.Int("limit", 0, "Number of results per page")
	nextToken := fs.String("next-token", "", "Pagination token")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	url := baseURL + "/api/v1/memories"
	params := []string{}
	if *userID != "" {
		params = append(params, "user_id="+*userID)
	}
	if *limit > 0 {
		params = append(params, fmt.Sprintf("limit=%d", *limit))
	}
	if *nextToken != "" {
		params = append(params, "next_token="+*nextToken)
	}
	if len(params) > 0 {
		url += "?" + strings.Join(params, "&")
	}

	return doRequest(http.MethodGet, url, nil)
}

func runGet(baseURL string, args []string) (interface{}, error) {
	if len(args) == 0 || args[0] == "" {
		return nil, fmt.Errorf("memory-id is required\nUsage: memory-cli get <memory-id>")
	}
	memoryID := args[0]
	return doRequest(http.MethodGet, baseURL+"/api/v1/memories/"+memoryID, nil)
}

func runUpdate(baseURL string, args []string) (interface{}, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return nil, fmt.Errorf("memory-id is required\nUsage: memory-cli update <memory-id> [--content TEXT] [--tag TAG ...]")
	}
	memoryID := args[0]
	rest := args[1:]

	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	content := fs.String("content", "", "New content")
	var tags tagList
	fs.Var(&tags, "tag", "New tag (can be specified multiple times)")
	if err := fs.Parse(rest); err != nil {
		return nil, err
	}

	body := map[string]interface{}{
		"content": *content,
		"tags":    []string(tags),
	}

	return doRequest(http.MethodPut, baseURL+"/api/v1/memories/"+memoryID, body)
}

func runDelete(baseURL string, args []string) (interface{}, error) {
	if len(args) == 0 || args[0] == "" {
		return nil, fmt.Errorf("memory-id is required\nUsage: memory-cli delete <memory-id>")
	}
	memoryID := args[0]
	return doRequest(http.MethodDelete, baseURL+"/api/v1/memories/"+memoryID, nil)
}

// doRequest performs an HTTP request and returns the parsed JSON response.
func doRequest(method, url string, body interface{}) (interface{}, error) {
	var bodyReader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request: %w", err)
		}
		bodyReader = bytes.NewReader(b)
	}

	req, err := http.NewRequest(method, url, bodyReader)
	if err != nil {
		return nil, fmt.Errorf("create request: %w", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("do request: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()

	respBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response: %w", err)
	}

	var result interface{}
	if err := json.Unmarshal(respBytes, &result); err != nil {
		return nil, fmt.Errorf("parse response (status %d): %s", resp.StatusCode, string(respBytes))
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("server error (status %d): %v", resp.StatusCode, result)
	}

	return result, nil
}
