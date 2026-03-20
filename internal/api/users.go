package api

import (
	"encoding/json"
	"errors"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/memory-server/internal/auth"
)

// UserServer handles user registration endpoints.
type UserServer struct {
	store auth.UserStorer
}

// NewUserServer creates a new UserServer.
func NewUserServer(store auth.UserStorer) *UserServer {
	return &UserServer{store: store}
}

// RegisterUserRoutes registers user-related routes on the mux.
// All routes require org token authentication via the provided middleware.
//
//	POST /api/v1/users  - Register a new user (org token required)
//	GET  /api/v1/users/{user_id} - Get user info (org token required)
func (us *UserServer) RegisterUserRoutes(mux *http.ServeMux, authMiddleware func(http.Handler) http.Handler) {
	mux.Handle("POST /api/v1/users", authMiddleware(http.HandlerFunc(us.handleCreateUser)))
	mux.Handle("GET /api/v1/users/{user_id}", authMiddleware(http.HandlerFunc(us.handleGetUser)))
}

// handleCreateUser handles POST /api/v1/users (upsert).
//
// Creates a new user or updates an existing one.
// If the user already exists the token and description are overwritten.
//
// Request body:
//
//	{
//	  "user_id": "alice",        // optional; auto-generated if empty
//	  "token":   "my-token",     // optional; auto-generated as "usr_<uuid>" if empty
//	  "description": "..."       // optional
//	}
//
// Response:
//
//	201 Created  – new user registered
//	200 OK       – existing user updated
//
//	{
//	  "user_id": "alice",
//	  "token": "my-token",
//	  "org_id": "my-org",
//	  "description": "...",
//	  "created_at": "..."
//	}
func (us *UserServer) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID      string `json:"user_id"`
		Token       string `json:"token"`
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body: "+err.Error())
		return
	}

	orgID := auth.OrgIDFromContext(r.Context())

	// Auto-generate user_id if not provided
	userID := req.UserID
	if userID == "" {
		userID = uuid.NewString()
	}

	// Use caller-supplied token, or auto-generate one
	token := req.Token
	if token == "" {
		token = "usr_" + uuid.NewString()
	}

	// Determine if this is a create or update
	existing, err := us.store.GetUser(r.Context(), userID)
	if err != nil && !errors.Is(err, auth.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "check user: "+err.Error())
		return
	}

	isNew := existing == nil

	// Preserve original created_at when updating
	createdAt := time.Now().UTC()
	if !isNew {
		createdAt = existing.CreatedAt
	}

	user := &auth.User{
		UserID:      userID,
		Token:       token,
		Description: req.Description,
		OrgID:       orgID,
		CreatedAt:   createdAt,
	}

	if err := us.store.PutUser(r.Context(), user); err != nil {
		writeError(w, http.StatusInternalServerError, "upsert user: "+err.Error())
		return
	}

	writeJSON(w, http.StatusOK, user)
}

// handleGetUser handles GET /api/v1/users/{user_id}
func (us *UserServer) handleGetUser(w http.ResponseWriter, r *http.Request) {
	userID := r.PathValue("user_id")
	if userID == "" {
		writeError(w, http.StatusBadRequest, "user_id is required")
		return
	}

	user, err := us.store.GetUser(r.Context(), userID)
	if err != nil {
		if errors.Is(err, auth.ErrNotFound) {
			writeError(w, http.StatusNotFound, err.Error())
		} else {
			writeError(w, http.StatusInternalServerError, err.Error())
		}
		return
	}

	writeJSON(w, http.StatusOK, user)
}
