package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/takutakahashi/memory-server/internal/auth"
	"github.com/takutakahashi/memory-server/internal/memory"
)

// Server is the REST API server backed by a memory.Service.
type Server struct {
	svc     *memory.Service
	userSvc *memory.UserService
}

// New creates a new REST API Server.
func New(svc *memory.Service, userSvc *memory.UserService) *Server {
	return &Server{svc: svc, userSvc: userSvc}
}

// RegisterRoutes registers the REST API routes on the given mux.
//
// Routes:
//
//	POST   /api/v1/users            - Create user (no auth)
//	POST   /api/v1/memories          - Add memory
//	GET    /api/v1/memories          - List memories (?limit=&next_token=)
//	POST   /api/v1/memories/search   - Search memories
//	GET    /api/v1/memories/{id}     - Get memory
//	PUT    /api/v1/memories/{id}     - Update memory
//	DELETE /api/v1/memories/{id}     - Delete memory
func (s *Server) RegisterRoutes(mux *http.ServeMux) {
	// User registration (no auth required)
	mux.HandleFunc("POST /api/v1/users", s.handleCreateUser)

	// Build the resolve function wrapping userSvc
	resolve := auth.ResolveFunc(func(ctx context.Context, token string) (string, error) {
		user, err := s.userSvc.ResolveUserByToken(ctx, token)
		if err != nil {
			return "", err
		}
		return user.UserID, nil
	})

	memoriesMux := http.NewServeMux()
	memoriesMux.HandleFunc("POST /api/v1/memories", s.handleAdd)
	memoriesMux.HandleFunc("GET /api/v1/memories", s.handleList)
	memoriesMux.HandleFunc("POST /api/v1/memories/search", s.handleSearch)
	memoriesMux.HandleFunc("GET /api/v1/memories/{id}", s.handleGet)
	memoriesMux.HandleFunc("PUT /api/v1/memories/{id}", s.handleUpdate)
	memoriesMux.HandleFunc("DELETE /api/v1/memories/{id}", s.handleDelete)

	mux.Handle("/api/v1/memories", auth.BearerAuth(resolve, memoriesMux))
	mux.Handle("/api/v1/memories/", auth.BearerAuth(resolve, memoriesMux))
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

// handleCreateUser handles POST /api/v1/users
func (s *Server) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	result, err := s.userSvc.CreateUser(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

// handleAdd handles POST /api/v1/memories
func (s *Server) handleAdd(w http.ResponseWriter, r *http.Request) {
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "authorization required")
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

	result, err := s.svc.Add(r.Context(), memory.AddInput{
		UserID:  userID,
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
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "authorization required")
		return
	}

	q := r.URL.Query()
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
	userID, ok := auth.UserIDFromContext(r.Context())
	if !ok || userID == "" {
		writeError(w, http.StatusUnauthorized, "authorization required")
		return
	}

	var req struct {
		Query string   `json:"query"`
		Tags  []string `json:"tags"`
		Limit int      `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	results, err := s.svc.Search(r.Context(), memory.SearchInput{
		UserID: userID,
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
