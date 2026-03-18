package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/takutakahashi/memory-server/internal/api"
	"github.com/takutakahashi/memory-server/internal/memory"
	mcpserver "github.com/takutakahashi/memory-server/internal/mcp"
)

func main() {
	ctx := context.Background()

	cfg, err := config.LoadDefaultConfig(ctx)
	if err != nil {
		log.Fatalf("failed to load AWS config: %v", err)
	}

	svc := memory.NewService(cfg)

	mux := http.NewServeMux()

	// Health check
	mux.HandleFunc("/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprint(w, `{"status":"ok"}`)
	})

	// MCP routes
	mcpSrv := mcpserver.NewServerWithService(svc)
	mcpSrv.RegisterRoutes(mux)

	// REST API routes
	apiSrv := api.New(svc)
	apiSrv.RegisterRoutes(mux)

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting Memory Server on port %s (MCP + REST API)", port)
	if err := http.ListenAndServe(":"+port, mux); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
