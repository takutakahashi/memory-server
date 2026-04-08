package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/takutakahashi/memory-server/internal/api"
	"github.com/takutakahashi/memory-server/internal/auth"
	"github.com/takutakahashi/memory-server/internal/curator"
	"github.com/takutakahashi/memory-server/internal/inbox"
	"github.com/takutakahashi/memory-server/internal/kb"
	"github.com/takutakahashi/memory-server/internal/memory"
	"github.com/takutakahashi/memory-server/internal/migrate"
	mcpserver "github.com/takutakahashi/memory-server/internal/mcp"
)

//go:embed web
var webFS embed.FS

func main() {
	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}

	// Run versioned DynamoDB schema migrations before starting the server.
	log.Println("Running DynamoDB schema migrations…")
	if err := migrate.Run(ctx, cfg); err != nil {
		log.Fatalf("DynamoDB migration failed: %v", err)
	}
	log.Println("DynamoDB schema migrations complete.")

	// Initialize services.
	memorySvc := memory.NewService(cfg)
	inboxSvc := inbox.NewService(cfg)
	kbSvc := kb.NewService(cfg)
	cur := curator.New(inboxSvc, memorySvc, kbSvc)

	mux := http.NewServeMux()

	// Web UI — serve embedded static files at /
	webSub, err := fs.Sub(webFS, "web")
	if err != nil {
		log.Fatalf("failed to create web sub-filesystem: %v", err)
	}
	mux.Handle("/", http.FileServer(http.FS(webSub)))

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		// Use a dedicated context with a generous timeout so that the probe's
		// own deadline does not cancel the DynamoDB call prematurely.
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := memorySvc.Store.Ping(ctx); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"status":   "error",
				"dynamodb": err.Error(),
			})
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(map[string]string{"status": "ok"})
	})

	// MCP routes (no auth — MCP clients manage their own auth)
	mcpSrv := mcpserver.NewServerWithServices(memorySvc, inboxSvc, kbSvc, cur)
	mcpSrv.RegisterRoutes(mux)

	// Auth store — always initialised so user routes work.
	// Set AUTH_ENABLED=true to also require Bearer tokens on memory endpoints.
	// Set ADMIN_TOKEN to enable the user management API (POST/GET /api/v1/users).
	authStore := auth.NewStore(cfg)
	authEnabled := os.Getenv("AUTH_ENABLED") == "true"

	// User management routes (require ADMIN_TOKEN via AdminTokenAuth)
	userSrv := api.NewUserServer(authStore)
	userSrv.RegisterUserRoutes(mux, auth.AdminTokenAuth())

	// REST API routes — optionally protected by user Bearer tokens
	apiSrv := api.New(memorySvc)
	inboxSrv := api.NewInboxServer(inboxSvc)
	kbSrv := api.NewKBServer(kbSvc)
	curatorSrv := api.NewCuratorServer(cur)

	if authEnabled {
		log.Println("Auth enabled: API requires Bearer user token")
		authMiddleware := auth.BearerAuth(authStore)
		apiSrv.RegisterRoutes(mux, authMiddleware)
		inboxSrv.RegisterInboxRoutes(mux, authMiddleware)
		kbSrv.RegisterKBRoutes(mux, authMiddleware)
		curatorSrv.RegisterCuratorRoutes(mux, authMiddleware)
	} else {
		log.Println("Auth disabled: API is open (set AUTH_ENABLED=true to enable)")
		apiSrv.RegisterRoutes(mux)
		inboxSrv.RegisterInboxRoutes(mux)
		kbSrv.RegisterKBRoutes(mux)
		curatorSrv.RegisterCuratorRoutes(mux)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting Memory Server on port %s (MCP + REST API)", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}

	_ = fmt.Sprintf // avoid unused import
}
