package api

import (
	"encoding/json"
	"net/http"

	"github.com/takutakahashi/memory-server/internal/auth"
	"github.com/takutakahashi/memory-server/internal/memory"
)

// InjectServer handles CLAUDE.md injection and summarization endpoints.
type InjectServer struct {
	svc *memory.Service
}

// NewInjectServer creates a new InjectServer.
func NewInjectServer(svc *memory.Service) *InjectServer {
	return &InjectServer{svc: svc}
}

// RegisterInjectRoutes registers the inject/summarize routes on the given mux.
// All routes require admin token authentication.
//
// Routes:
//
//	GET  /api/v1/users/{user_id}/claude-md  - Generate CLAUDE.md snippet from user's memories
//	POST /api/v1/users/{user_id}/summarize  - Summarize user's memories into one entry
func (is *InjectServer) RegisterInjectRoutes(mux *http.ServeMux, adminMiddleware func(http.Handler) http.Handler) {
	mux.Handle("GET /api/v1/users/{user_id}/claude-md",
		adminMiddleware(http.HandlerFunc(is.handleClaudeMD)))
	mux.Handle("POST /api/v1/users/{user_id}/summarize",
		adminMiddleware(http.HandlerFunc(is.handleSummarize)))
}

// handleClaudeMD handles GET /api/v1/users/{user_id}/claude-md
//
// Returns a CLAUDE.md-compatible markdown snippet containing all memories for the user.
// The caller is responsible for injecting this content into the target CLAUDE.md file.
//
// Query parameters:
//
//	format=text  (default) - Returns plain text/markdown
//	format=json            - Returns {"content": "<markdown>"}
func (is *InjectServer) handleClaudeMD(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("user_id")
	if userID == "" {
		// Fall back to authenticated user when available.
		userID = auth.UserIDFromContext(r.Context())
	}
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	md, err := is.svc.GenerateClaudeMD(r.Context(), userID)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	format := r.URL.Query().Get("format")
	if format == "json" {
		writeJSON(w, http.StatusOK, map[string]string{"content": md})
		return
	}

	// Default: plain text/markdown
	w.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write([]byte(md))
}

// handleSummarize handles POST /api/v1/users/{user_id}/summarize
//
// Fetches all memories for the user, generates a summary using Bedrock, stores the
// summary as a new memory, and optionally deletes the originals.
//
// Request body (all fields optional):
//
//	{
//	  "min_count":        3,    // minimum memories required (default: 3)
//	  "delete_originals": false // whether to delete source memories after summarization
//	}
//
// Response:
//
//	{
//	  "summarized_memory_id": "...",
//	  "merged_count": 5,
//	  "summary": "..."
//	}
func (is *InjectServer) handleSummarize(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("user_id")
	if userID == "" {
		userID = auth.UserIDFromContext(r.Context())
	}
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	var req struct {
		MinCount        int  `json:"min_count"`
		DeleteOriginals bool `json:"delete_originals"`
	}
	if r.Body != nil && r.ContentLength != 0 {
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
			return
		}
	}

	result, err := is.svc.RunSummarize(r.Context(), memory.SummarizeInput{
		UserID:          userID,
		MinCount:        req.MinCount,
		DeleteOriginals: req.DeleteOriginals,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}
