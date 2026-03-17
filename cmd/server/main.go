package main

import (
	"context"
	"log"
	"os"

	mcpserver "github.com/takutakahashi/memory-server/internal/mcp"
)

func main() {
	ctx := context.Background()

	srv, err := mcpserver.NewServer(ctx)
	if err != nil {
		log.Fatalf("failed to create server: %v", err)
	}

	port := os.Getenv("PORT")
	if port == "" {
		port = "8080"
	}

	log.Printf("Starting Memory MCP Server on port %s", port)
	if err := srv.Start(port); err != nil {
		log.Fatalf("server error: %v", err)
	}
}
