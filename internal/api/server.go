package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/takutakahashi/memory-server/internal/auth"
	"github.com/takutakahashi/memory-server/internal/memory"
)

// Server is the REST API server backed by a memory.Service.
type Server struct {
	svc *memory.Service
}

// New creates a new REST API Server.
func New(svc *memory.Service) *Server {
	return &Server{svc: svc}
}

// RegisterRoutes registers the REST API routes on the given mux.
// If authMiddleware is non-nil it is applied to all memory routes and
// user_id is derived from the authenticated token (request-body user_id is ignored).
//
// Routes:
//
//	POST   /api/v1/memories          - Add memory
//	GET    /api/v1/memories          - List memories (?limit=&next_token=)
//	POST   /api/v1/memories/search   - Search memories
//	GET    /api/v1/memories/{id}     - Get memory
//	PUT    /api/v1/memories/{id}     - Update memory
//	DELETE /api/v1/memories/{id}     - Delete memory
func (s *Server) RegisterRoutes(mux *http.ServeMux, authMiddleware ...func(http.Handler) http.Handler) {
	wrap := func(h http.Handler) http.Handler {
		if len(authMiddleware) > 0 && authMiddleware[0] != nil {
			return authMiddleware[0](h)
		}
		return h
	}

	mux.Handle("POST /api/v1/memories", wrap(http.HandlerFunc(s.handleAdd)))
	mux.Handle("GET /api/v1/memories", wrap(http.HandlerFunc(s.handleList)))
	mux.Handle("POST /api/v1/memories/search", wrap(http.HandlerFunc(s.handleSearch)))
	mux.Handle("GET /api/v1/memories/{id}", wrap(http.HandlerFunc(s.handleGet)))
	mux.Handle("PUT /api/v1/memories/{id}", wrap(http.HandlerFunc(s.handleUpdate)))
	mux.Handle("DELETE /api/v1/memories/{id}", wrap(http.HandlerFunc(s.handleDelete)))
}

// resolveUserID returns the user_id from the auth context (when auth is enabled)
// or falls back to the supplied fallback value (unauthenticated mode).
func resolveUserID(r *http.Request, fallback string) string {
	if uid := auth.UserIDFromContext(r.Context()); uid != "" {
		return uid
	}
	return fallback
}

// writeJSON writes v as a JSON response with the given status code.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeError writes an error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	writeJSON(w, status, map[string]string{"error": msg})
}

// handleAdd handles POST /api/v1/memories
func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID  string       `json:"user_id"`
		OrgID   string       `json:"org_id"`
		Content string       `json:"content"`
		Tags    []string     `json:"tags"`
		Scope   memory.Scope `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	result, err := s.svc.Add(r.Context(), memory.AddInput{
		UserID:  resolveUserID(r, req.UserID),
		OrgID:   req.OrgID,
		Content: req.Content,
		Tags:    req.Tags,
		Scope:   req.Scope,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, result)
}

// handleList handles GET /api/v1/memories
func (s *Server) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	userID := resolveUserID(r, q.Get("user_id"))
	orgID := q.Get("org_id")
	limitStr := q.Get("limit")
	nextTokenStr := q.Get("next_token")

	limit := 20
	if limitStr != "" {
		n, err := strconv.Atoi(limitStr)
		if err != nil || n <= 0 {
			writeError(w, http.StatusBadRequest, "invalid limit parameter")
			return
		}
		limit = n
	}

	var nextToken *string
	if nextTokenStr != "" {
		nextToken = &nextTokenStr
	}

	result, err := s.svc.List(r.Context(), memory.ListInput{
		UserID:    userID,
		OrgID:     orgID,
		Limit:     limit,
		NextToken: nextToken,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, result)
}

// handleSearch handles POST /api/v1/memories/search
func (s *Server) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string   `json:"user_id"`
		OrgID  string   `json:"org_id"`
		Query  string   `json:"query"`
		Tags   []string `json:"tags"`
		Limit  int      `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	results, err := s.svc.Search(r.Context(), memory.SearchInput{
		UserID: resolveUserID(r, req.UserID),
		OrgID:  req.OrgID,
		Query:  req.Query,
		Tags:   req.Tags,
		Limit:  req.Limit,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, results)
}

// handleGet handles GET /api/v1/memories/{id}
func (s *Server) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "memory id is required")
		return
	}

	m, err := s.svc.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, memory.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, m)
}

// handleUpdate handles PUT /api/v1/memories/{id}
func (s *Server) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "memory id is required")
		return
	}

	var req struct {
		Content string       `json:"content"`
		Tags    []string     `json:"tags"`
		Scope   memory.Scope `json:"scope"`
		OrgID   string       `json:"org_id"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	m, err := s.svc.Update(r.Context(), memory.UpdateInput{
		MemoryID: id,
		Content:  req.Content,
		Tags:     req.Tags,
		Scope:    req.Scope,
		OrgID:    req.OrgID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, m)
}

// handleDelete handles DELETE /api/v1/memories/{id}
func (s *Server) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "memory id is required")
		return
	}

	if err := s.svc.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
