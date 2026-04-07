package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/takutakahashi/memory-server/internal/kb"
)

// KBServer is the REST API server for Knowledge Base operations.
type KBServer struct {
	svc *kb.Service
}

// NewKBServer creates a new KBServer.
func NewKBServer(svc *kb.Service) *KBServer {
	return &KBServer{svc: svc}
}

// RegisterKBRoutes registers Knowledge Base REST API routes.
//
// Routes:
//
//	POST   /api/v1/kb               - Create KB page
//	GET    /api/v1/kb               - List KB pages (?category=&limit=&next_token=)
//	POST   /api/v1/kb/search        - Search KB pages
//	GET    /api/v1/kb/{id}          - Get KB page by ID
//	PUT    /api/v1/kb/{id}          - Update KB page
//	DELETE /api/v1/kb/{id}          - Delete KB page
//	GET    /api/v1/kb/slug/{slug}   - Get KB page by slug
func (s *KBServer) RegisterKBRoutes(mux *http.ServeMux, authMiddleware ...func(http.Handler) http.Handler) {
	wrap := func(h http.Handler) http.Handler {
		if len(authMiddleware) > 0 && authMiddleware[0] != nil {
			return authMiddleware[0](h)
		}
		return h
	}

	mux.Handle("POST /api/v1/kb", wrap(http.HandlerFunc(s.handleCreate)))
	mux.Handle("GET /api/v1/kb", wrap(http.HandlerFunc(s.handleList)))
	mux.Handle("POST /api/v1/kb/search", wrap(http.HandlerFunc(s.handleSearch)))
	mux.Handle("GET /api/v1/kb/slug/{slug}", wrap(http.HandlerFunc(s.handleGetBySlug)))
	mux.Handle("GET /api/v1/kb/{id}", wrap(http.HandlerFunc(s.handleGet)))
	mux.Handle("PUT /api/v1/kb/{id}", wrap(http.HandlerFunc(s.handleUpdate)))
	mux.Handle("DELETE /api/v1/kb/{id}", wrap(http.HandlerFunc(s.handleDelete)))
}

func (s *KBServer) handleCreate(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID          string   `json:"user_id"`
		Title           string   `json:"title"`
		Slug            string   `json:"slug"`
		Content         string   `json:"content"`
		Summary         string   `json:"summary"`
		Category        string   `json:"category"`
		Scope           kb.Scope `json:"scope"`
		Tags            []string `json:"tags"`
		SourceMemoryIDs []string `json:"source_memory_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	p, err := s.svc.Create(r.Context(), kb.CreateInput{
		UserID:          resolveUserID(r, req.UserID),
		Title:           req.Title,
		Slug:            req.Slug,
		Content:         req.Content,
		Summary:         req.Summary,
		Category:        req.Category,
		Scope:           req.Scope,
		Tags:            req.Tags,
		SourceMemoryIDs: req.SourceMemoryIDs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, p)
}

func (s *KBServer) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	userID := resolveUserID(r, q.Get("user_id"))
	category := q.Get("category")
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

	result, err := s.svc.List(r.Context(), kb.ListInput{
		UserID:    userID,
		Category:  category,
		Limit:     limit,
		NextToken: nextToken,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *KBServer) handleSearch(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID string `json:"user_id"`
		Query  string `json:"query"`
		Limit  int    `json:"limit"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	results, err := s.svc.Search(r.Context(), resolveUserID(r, req.UserID), req.Query, req.Limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, results)
}

func (s *KBServer) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "page id is required")
		return
	}

	p, err := s.svc.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, kb.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *KBServer) handleGetBySlug(w http.ResponseWriter, r *http.Request) {
	slug := r.PathValue("slug")
	if slug == "" {
		writeError(w, http.StatusBadRequest, "slug is required")
		return
	}

	p, err := s.svc.GetBySlug(r.Context(), slug)
	if err != nil {
		if errors.Is(err, kb.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *KBServer) handleUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "page id is required")
		return
	}

	var req struct {
		Title           string   `json:"title"`
		Slug            string   `json:"slug"`
		Content         string   `json:"content"`
		Summary         string   `json:"summary"`
		Category        string   `json:"category"`
		Scope           kb.Scope `json:"scope"`
		Tags            []string `json:"tags"`
		SourceMemoryIDs []string `json:"source_memory_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	p, err := s.svc.Update(r.Context(), kb.UpdateInput{
		PageID:          id,
		Title:           req.Title,
		Slug:            req.Slug,
		Content:         req.Content,
		Summary:         req.Summary,
		Category:        req.Category,
		Scope:           req.Scope,
		Tags:            req.Tags,
		SourceMemoryIDs: req.SourceMemoryIDs,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, p)
}

func (s *KBServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "page id is required")
		return
	}

	if err := s.svc.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
