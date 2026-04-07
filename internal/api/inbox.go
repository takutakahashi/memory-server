package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/takutakahashi/memory-server/internal/inbox"
)

// InboxServer is the REST API server for Inbox operations.
type InboxServer struct {
	svc *inbox.Service
}

// NewInboxServer creates a new InboxServer.
func NewInboxServer(svc *inbox.Service) *InboxServer {
	return &InboxServer{svc: svc}
}

// RegisterInboxRoutes registers inbox REST API routes.
//
// Routes:
//
//	POST   /api/v1/inbox           - Add inbox entry
//	GET    /api/v1/inbox           - List inbox entries (?status=&limit=&next_token=)
//	GET    /api/v1/inbox/{id}      - Get inbox entry
//	POST   /api/v1/inbox/{id}/archive - Archive inbox entry
//	DELETE /api/v1/inbox/{id}      - Delete inbox entry
func (s *InboxServer) RegisterInboxRoutes(mux *http.ServeMux, authMiddleware ...func(http.Handler) http.Handler) {
	wrap := func(h http.Handler) http.Handler {
		if len(authMiddleware) > 0 && authMiddleware[0] != nil {
			return authMiddleware[0](h)
		}
		return h
	}

	mux.Handle("POST /api/v1/inbox", wrap(http.HandlerFunc(s.handleAdd)))
	mux.Handle("GET /api/v1/inbox", wrap(http.HandlerFunc(s.handleList)))
	mux.Handle("GET /api/v1/inbox/{id}", wrap(http.HandlerFunc(s.handleGet)))
	mux.Handle("POST /api/v1/inbox/{id}/archive", wrap(http.HandlerFunc(s.handleArchive)))
	mux.Handle("DELETE /api/v1/inbox/{id}", wrap(http.HandlerFunc(s.handleDelete)))
}

func (s *InboxServer) handleAdd(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID  string   `json:"user_id"`
		Content string   `json:"content"`
		Source  string   `json:"source"`
		Tags    []string `json:"tags"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	e, err := s.svc.Add(r.Context(), inbox.AddInput{
		UserID:  resolveUserID(r, req.UserID),
		Content: req.Content,
		Source:  req.Source,
		Tags:    req.Tags,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusCreated, e)
}

func (s *InboxServer) handleList(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	userID := resolveUserID(r, q.Get("user_id"))
	statusStr := q.Get("status")
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

	result, err := s.svc.List(r.Context(), inbox.ListInput{
		UserID:    userID,
		Status:    inbox.Status(statusStr),
		Limit:     limit,
		NextToken: nextToken,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *InboxServer) handleGet(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "inbox id is required")
		return
	}

	e, err := s.svc.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, inbox.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, e)
}

func (s *InboxServer) handleArchive(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "inbox id is required")
		return
	}

	if err := s.svc.Archive(r.Context(), id); err != nil {
		if errors.Is(err, inbox.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *InboxServer) handleDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if id == "" {
		writeError(w, http.StatusBadRequest, "inbox id is required")
		return
	}

	if err := s.svc.Delete(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}
