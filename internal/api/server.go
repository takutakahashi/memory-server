package api

import (
	"encoding/json"
	"net/http"
	"strconv"

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
//
// Routes:
//
//	POST   /api/v1/memories          - Add memory
//	GET    /api/v1/memories          - List memories (?user_id=&limit=&next_token=)
//	POST   /api/v1/memories/search   - Search memories
//	GET    /api/v1/memories/{id}     - Get memory
//	PUT    /api/v1/memories/{id}     - Update memory
//	DELETE /api/v1/memories/{id}     - Delete memory
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	mux.HandleFunc("POST /api/v1/memories", s.handleAdd)
	mux.HandleFunc("GET /api/v1/memories", s.handleList)
	mux.HandleFunc("POST /api/v1/memories/search", s.handleSearch)
	mux.HandleFunc("GET /api/v1/memories/{id}", s.handleGet)
	mux.HandleFunc("PUT /api/v1/memories/{id}", s.handleUpdate)
	mux.HandleFunc("DELETE /api/v1/memories/{id}", s.handleDelete)
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
		UserID  string        `json:"user_id"`
		Content string        `json:"content"`
		Tags    []string      `json:"tags"`
		Scope   memory.Scope  `json:"scope"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	result, err := s.svc.Add(r.Context(), memory.AddInput{
		UserID:  req.UserID,
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
	userID := q.Get("user_id")
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
		Query  string   `json:"query"`
		Tags   []string `json:"tags"`
		Limit  int      `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	results, err := s.svc.Search(r.Context(), memory.SearchInput{
		UserID: req.UserID,
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
		writeError(w, http.StatusNotFound, err.Error())
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
