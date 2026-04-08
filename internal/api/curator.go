package api

import (
	"net/http"

	"github.com/takutakahashi/memory-server/internal/curator"
)

// CuratorServer is the REST API server for Curator operations.
type CuratorServer struct {
	cur *curator.Curator
}

// NewCuratorServer creates a new CuratorServer.
func NewCuratorServer(cur *curator.Curator) *CuratorServer {
	return &CuratorServer{cur: cur}
}

// RegisterCuratorRoutes registers Curator REST API routes.
//
// Routes:
//
//	POST /api/v1/curator/run    - Run the Curator
//	GET  /api/v1/curator/status - Get the last Curator run status
func (s *CuratorServer) RegisterCuratorRoutes(mux *http.ServeMux, authMiddleware ...func(http.Handler) http.Handler) {
	wrap := func(h http.Handler) http.Handler {
		if len(authMiddleware) > 0 && authMiddleware[0] != nil {
			return authMiddleware[0](h)
		}
		return h
	}

	mux.Handle("POST /api/v1/curator/run", wrap(http.HandlerFunc(s.handleRun)))
	mux.Handle("GET /api/v1/curator/status", wrap(http.HandlerFunc(s.handleStatus)))
}

func (s *CuratorServer) handleRun(w http.ResponseWriter, r *http.Request) {
	// X-Anthropic-Key header takes priority over the server-level ANTHROPIC_API_KEY.
	// The curator subprocess will receive this key via its environment.
	anthropicKey := r.Header.Get("X-Anthropic-Key")

	result, err := s.cur.Run(r.Context(), curator.RunInput{
		AnthropicAPIKey: anthropicKey,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *CuratorServer) handleStatus(w http.ResponseWriter, r *http.Request) {
	result := s.cur.GetLastResult()
	if result == nil {
		writeJSON(w, http.StatusOK, map[string]string{
			"status":  "never_run",
			"message": "Curator has not been run yet",
		})
		return
	}
	writeJSON(w, http.StatusOK, result)
}
