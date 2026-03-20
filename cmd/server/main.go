package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/takutakahashi/memory-server/internal/api"
	"github.com/takutakahashi/memory-server/internal/auth"
	"github.com/takutakahashi/memory-server/internal/memory"
	"github.com/takutakahashi/memory-server/internal/migrate"
	mcpserver "github.com/takutakahashi/memory-server/internal/mcp"
)

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

	svc := memory.NewService(cfg)

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()
		if err := svc.Store.Ping(ctx); err != nil {
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
	mcpSrv := mcpserver.NewServerWithService(svc)
	mcpSrv.RegisterRoutes(mux)

	// Auth store — always initialised so user/org-token routes work.
	// Set AUTH_ENABLED=true to also require Bearer tokens on memory endpoints.
	authStore := auth.NewStore(cfg)
	authEnabled := os.Getenv("AUTH_ENABLED") == "true"

	// User management routes (always require org token)
	userSrv := api.NewUserServer(authStore)
	userSrv.RegisterUserRoutes(mux, auth.OrgTokenAuth(authStore))

	// REST API memory routes — optionally protected by user Bearer tokens
	apiSrv := api.New(svc)
	if authEnabled {
		log.Println("Auth enabled: memory API requires Bearer user token")
		apiSrv.RegisterRoutes(mux, auth.BearerAuth(authStore))
	} else {
		log.Println("Auth disabled: memory API is open (set AUTH_ENABLED=true to enable)")
		apiSrv.RegisterRoutes(mux)
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
