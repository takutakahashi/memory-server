package main

import (
	"bytes"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
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

// tokenFilePath returns the path to the stored token file.
func tokenFilePath() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return filepath.Join(home, ".config", "memory-cli", "token")
}

// loadToken resolves token in priority order: flag -> env -> file.
func loadToken(flagToken string) string {
	if flagToken != "" {
		return flagToken
	}
	if v := os.Getenv("MEMORY_TOKEN"); v != "" {
		return v
	}
	p := tokenFilePath()
	if p == "" {
		return ""
	}
	b, err := os.ReadFile(p)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// saveToken saves the token to the config file.
func saveToken(token string) error {
	p := tokenFilePath()
	if p == "" {
		return fmt.Errorf("cannot determine home directory")
	}
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}
	return os.WriteFile(p, []byte(token+"\n"), 0600)
}

func main() {
	// Global flags
	globalFlags := flag.NewFlagSet("memory-cli", flag.ContinueOnError)
	serverURL := globalFlags.String("server", "", "Server base URL (overrides MEMORY_SERVER_URL)")
	rawJSON := globalFlags.Bool("json", false, "Output raw JSON")
	tokenFlag := globalFlags.String("token", "", "Auth token (overrides MEMORY_TOKEN env and ~/.config/memory-cli/token)")
	format := globalFlags.String("format", "", "Output format: table, json, pretty")

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
		if arg == "--token" || arg == "-token" {
			if i+1 < len(os.Args) {
				globalArgs = append(globalArgs, arg, os.Args[i+1])
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--token=") || strings.HasPrefix(arg, "-token=") {
			globalArgs = append(globalArgs, arg)
			continue
		}
		if arg == "--format" || arg == "-format" {
			if i+1 < len(os.Args) {
				globalArgs = append(globalArgs, arg, os.Args[i+1])
				i++
			}
			continue
		}
		if strings.HasPrefix(arg, "--format=") || strings.HasPrefix(arg, "-format=") {
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

	// Resolve token
	token := loadToken(*tokenFlag)

	if subcommand == "" {
		printUsage()
		os.Exit(1)
	}

	var result interface{}
	var err error

	switch subcommand {
	case "register":
		result, err = runRegister(baseURL, subArgs)
		if err == nil {
			// Save token if registration succeeded
			if m, ok := result.(map[string]interface{}); ok {
				if t, ok := m["token"].(string); ok && t != "" {
					if saveErr := saveToken(t); saveErr != nil {
						fmt.Fprintf(os.Stderr, "warning: could not save token: %v\n", saveErr)
					} else {
						fmt.Fprintf(os.Stderr, "Token saved to %s\n", tokenFilePath())
					}
				}
			}
		}
	case "add":
		result, err = runAdd(baseURL, token, subArgs)
	case "search":
		result, err = runSearch(baseURL, token, subArgs)
	case "list":
		result, err = runList(baseURL, token, subArgs)
	case "get":
		result, err = runGet(baseURL, token, subArgs)
	case "update":
		result, err = runUpdate(baseURL, token, subArgs)
	case "delete":
		result, err = runDelete(baseURL, token, subArgs)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n", subcommand)
		printUsage()
		os.Exit(1)
	}

	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	// Determine output format
	outputFormat := *format
	if outputFormat == "" {
		if *rawJSON {
			outputFormat = "json"
		} else {
			outputFormat = "table"
		}
	}

	switch outputFormat {
	case "json":
		b, _ := json.Marshal(result)
		fmt.Println(string(b))
	case "pretty":
		b, _ := json.MarshalIndent(result, "", "  ")
		fmt.Println(string(b))
	default: // table
		printTable(subcommand, result)
	}
}

func printUsage() {
	fmt.Fprintf(os.Stderr, `Usage: memory-cli [--server URL] [--token TOKEN] [--format table|json|pretty] [--json] <command> [options]

Commands:
  register
  add     --content TEXT [--tag TAG ...] [--scope SCOPE]
  search  --query TEXT [--tag TAG ...] [--limit N]
  list    [--limit N] [--next-token TOKEN]
  get     <memory-id>
  update  <memory-id> [--content TEXT] [--tag TAG ...] [--add-tag TAG ...] [--remove-tag TAG ...] [--scope SCOPE]
  delete  <memory-id>

Scope values:
  private  Only visible to the owner (default)
  public   Visible to all users

Environment:
  MEMORY_SERVER_URL  Server base URL (default: http://localhost:8080)
  MEMORY_TOKEN       Auth token
  MEMORY_USER_ID     Default user ID (legacy, for backward compatibility)

Options:
  --server URL              Override server URL
  --token TOKEN             Auth token (overrides MEMORY_TOKEN env and ~/.config/memory-cli/token)
  --format table|json|pretty  Output format (default: table)
  --json                    Output raw JSON (backward compatible, same as --format json)
`)
}

// printTable prints results in table format.
func printTable(subcommand string, result interface{}) {
	b, _ := json.Marshal(result)
	var raw interface{}
	_ = json.Unmarshal(b, &raw)

	switch subcommand {
	case "list":
		printMemoryList(raw)
	case "search":
		printSearchResults(raw)
	case "get":
		printSingleMemory(raw)
	case "add", "register", "delete", "update":
		// For these, print key-value
		if m, ok := raw.(map[string]interface{}); ok {
			printKeyValue(m)
		} else {
			b2, _ := json.MarshalIndent(raw, "", "  ")
			fmt.Println(string(b2))
		}
	default:
		b2, _ := json.MarshalIndent(raw, "", "  ")
		fmt.Println(string(b2))
	}
}

func printMemoryList(raw interface{}) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		b, _ := json.MarshalIndent(raw, "", "  ")
		fmt.Println(string(b))
		return
	}
	memories, _ := m["memories"].([]interface{})
	printMemoriesTable(memories)
}

func printSearchResults(raw interface{}) {
	results, ok := raw.([]interface{})
	if !ok {
		b, _ := json.MarshalIndent(raw, "", "  ")
		fmt.Println(string(b))
		return
	}

	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tCONTENT\tTAGS\tSCOPE\tSCORE\tCREATED_AT")
	for _, item := range results {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		mem, _ := m["memory"].(map[string]interface{})
		if mem == nil {
			continue
		}
		id := shortStr(strVal(mem["memory_id"]), 8)
		content := shortStr(strVal(mem["content"]), 50)
		tags := tagsStr(mem["tags"])
		scope := strVal(mem["scope"])
		score := fmt.Sprintf("%.4f", floatVal(m["final_score"]))
		createdAt := shortStr(strVal(mem["created_at"]), 19)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\t%s\n", id, content, tags, scope, score, createdAt)
	}
	_ = w.Flush()
}

func printMemoriesTable(memories []interface{}) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	fmt.Fprintln(w, "ID\tCONTENT\tTAGS\tSCOPE\tCREATED_AT")
	for _, item := range memories {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		id := shortStr(strVal(m["memory_id"]), 8)
		content := shortStr(strVal(m["content"]), 50)
		tags := tagsStr(m["tags"])
		scope := strVal(m["scope"])
		createdAt := shortStr(strVal(m["created_at"]), 19)
		fmt.Fprintf(w, "%s\t%s\t%s\t%s\t%s\n", id, content, tags, scope, createdAt)
	}
	_ = w.Flush()
}

func printSingleMemory(raw interface{}) {
	m, ok := raw.(map[string]interface{})
	if !ok {
		b, _ := json.MarshalIndent(raw, "", "  ")
		fmt.Println(string(b))
		return
	}
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	keys := []string{"memory_id", "user_id", "content", "tags", "scope", "created_at", "updated_at", "access_count"}
	for _, k := range keys {
		v, ok := m[k]
		if !ok {
			continue
		}
		var val string
		switch tv := v.(type) {
		case []interface{}:
			strs := make([]string, 0, len(tv))
			for _, s := range tv {
				strs = append(strs, fmt.Sprintf("%v", s))
			}
			val = strings.Join(strs, ", ")
		default:
			val = fmt.Sprintf("%v", v)
		}
		fmt.Fprintf(w, "%s\t%s\n", k, val)
	}
	_ = w.Flush()
}

func printKeyValue(m map[string]interface{}) {
	w := tabwriter.NewWriter(os.Stdout, 0, 0, 2, ' ', 0)
	for k, v := range m {
		fmt.Fprintf(w, "%s\t%v\n", k, v)
	}
	_ = w.Flush()
}

// helpers
func shortStr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

func strVal(v interface{}) string {
	if v == nil {
		return ""
	}
	return fmt.Sprintf("%v", v)
}

func floatVal(v interface{}) float64 {
	if v == nil {
		return 0
	}
	f, _ := v.(float64)
	return f
}

func tagsStr(v interface{}) string {
	arr, ok := v.([]interface{})
	if !ok {
		return ""
	}
	strs := make([]string, 0, len(arr))
	for _, s := range arr {
		strs = append(strs, fmt.Sprintf("%v", s))
	}
	return strings.Join(strs, ",")
}

func runRegister(baseURL string, _ []string) (interface{}, error) {
	return doRequest(http.MethodPost, baseURL+"/api/v1/users", nil, "")
}

func runAdd(baseURL, token string, args []string) (interface{}, error) {
	fs := flag.NewFlagSet("add", flag.ContinueOnError)
	userID := fs.String("user-id", os.Getenv("MEMORY_USER_ID"), "User ID (legacy, ignored when token is set)")
	content := fs.String("content", "", "Content of the memory")
	scope := fs.String("scope", "", "Visibility scope: 'private' (default) or 'public'")
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
		"content": *content,
		"tags":    []string(tags),
	}
	if token == "" && *userID != "" {
		body["user_id"] = *userID
	}
	if *scope != "" {
		body["scope"] = *scope
	}

	return doRequest(http.MethodPost, baseURL+"/api/v1/memories", body, token)
}

func runSearch(baseURL, token string, args []string) (interface{}, error) {
	fs := flag.NewFlagSet("search", flag.ContinueOnError)
	userID := fs.String("user-id", os.Getenv("MEMORY_USER_ID"), "User ID (legacy, ignored when token is set)")
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
		"query": *query,
		"tags":  []string(tags),
		"limit": *limit,
	}
	if token == "" && *userID != "" {
		body["user_id"] = *userID
	}

	return doRequest(http.MethodPost, baseURL+"/api/v1/memories/search", body, token)
}

func runList(baseURL, token string, args []string) (interface{}, error) {
	fs := flag.NewFlagSet("list", flag.ContinueOnError)
	userID := fs.String("user-id", os.Getenv("MEMORY_USER_ID"), "User ID (legacy, ignored when token is set)")
	limit := fs.Int("limit", 0, "Number of results per page")
	nextToken := fs.String("next-token", "", "Pagination token")
	if err := fs.Parse(args); err != nil {
		return nil, err
	}

	url := baseURL + "/api/v1/memories"
	params := []string{}
	if token == "" && *userID != "" {
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

	return doRequest(http.MethodGet, url, nil, token)
}

func runGet(baseURL, token string, args []string) (interface{}, error) {
	if len(args) == 0 || args[0] == "" {
		return nil, fmt.Errorf("memory-id is required\nUsage: memory-cli get <memory-id>")
	}
	memoryID := args[0]
	return doRequest(http.MethodGet, baseURL+"/api/v1/memories/"+memoryID, nil, token)
}

func runUpdate(baseURL, token string, args []string) (interface{}, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		return nil, fmt.Errorf("memory-id is required\nUsage: memory-cli update <memory-id> [--content TEXT] [--tag TAG ...] [--add-tag TAG ...] [--remove-tag TAG ...] [--scope SCOPE]")
	}
	memoryID := args[0]
	rest := args[1:]

	fs := flag.NewFlagSet("update", flag.ContinueOnError)
	content := fs.String("content", "", "New content")
	scope := fs.String("scope", "", "New visibility scope: 'private' or 'public'")
	var tags tagList
	fs.Var(&tags, "tag", "New tag (can be specified multiple times)")
	var addTags tagList
	fs.Var(&addTags, "add-tag", "Tag to add (can be specified multiple times)")
	var removeTags tagList
	fs.Var(&removeTags, "remove-tag", "Tag to remove (can be specified multiple times)")
	if err := fs.Parse(rest); err != nil {
		return nil, err
	}

	// If add-tag or remove-tag specified, fetch current memory first
	finalTags := []string(tags)
	if len(addTags) > 0 || len(removeTags) > 0 {
		current, err := doRequest(http.MethodGet, baseURL+"/api/v1/memories/"+memoryID, nil, token)
		if err != nil {
			return nil, fmt.Errorf("fetch current memory: %w", err)
		}
		b, _ := json.Marshal(current)
		var mem map[string]interface{}
		_ = json.Unmarshal(b, &mem)

		// Get existing tags
		existingTags := []string{}
		if rawTags, ok := mem["tags"].([]interface{}); ok {
			for _, t := range rawTags {
				if s, ok := t.(string); ok {
					existingTags = append(existingTags, s)
				}
			}
		}

		// Merge with --tag if specified, otherwise start from existing
		if len(finalTags) == 0 {
			finalTags = existingTags
		}

		// Add new tags
		tagSet := make(map[string]bool)
		for _, t := range finalTags {
			tagSet[t] = true
		}
		for _, t := range addTags {
			tagSet[t] = true
		}

		// Remove tags
		removeSet := make(map[string]bool)
		for _, t := range removeTags {
			removeSet[t] = true
		}

		merged := []string{}
		for t := range tagSet {
			if !removeSet[t] {
				merged = append(merged, t)
			}
		}
		finalTags = merged
	}

	body := map[string]interface{}{
		"content": *content,
		"tags":    finalTags,
	}
	if *scope != "" {
		body["scope"] = *scope
	}

	return doRequest(http.MethodPut, baseURL+"/api/v1/memories/"+memoryID, body, token)
}

func runDelete(baseURL, token string, args []string) (interface{}, error) {
	if len(args) == 0 || args[0] == "" {
		return nil, fmt.Errorf("memory-id is required\nUsage: memory-cli delete <memory-id>")
	}
	memoryID := args[0]
	return doRequest(http.MethodDelete, baseURL+"/api/v1/memories/"+memoryID, nil, token)
}

// doRequest performs an HTTP request and returns the parsed JSON response.
func doRequest(method, url string, body interface{}, token string) (interface{}, error) {
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
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
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
