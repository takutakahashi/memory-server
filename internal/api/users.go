package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/takutakahashi/memory-server/internal/auth"
)

// UserServer handles user registration endpoints.
type UserServer struct {
	store *auth.Store
}

// NewUserServer creates a new UserServer.
func NewUserServer(store *auth.Store) *UserServer {
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

// handleCreateUser handles POST /api/v1/users
//
// Request body:
//
//	{
//	  "user_id": "alice",        // optional; auto-generated if empty
//	  "description": "..."       // optional
//	}
//
// Response (201 Created):
//
//	{
//	  "user_id": "alice",
//	  "token": "usr_<uuid>",
//	  "org_id": "my-org",
//	  "description": "...",
//	  "created_at": "..."
//	}
func (us *UserServer) handleCreateUser(w http.ResponseWriter, r *http.Request) {
	var req struct {
		UserID      string `json:"user_id"`
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

	// Check if user already exists
	existing, err := us.store.GetUser(r.Context(), userID)
	if err != nil && !errors.Is(err, auth.ErrNotFound) {
		writeError(w, http.StatusInternalServerError, "check user: "+err.Error())
		return
	}
	if existing != nil {
		writeError(w, http.StatusConflict, fmt.Sprintf("user %q already exists", userID))
		return
	}

	// Generate a user API token: "usr_<uuid>"
	token := "usr_" + uuid.NewString()

	user := &auth.User{
		UserID:      userID,
		Token:       token,
		Description: req.Description,
		OrgID:       orgID,
		CreatedAt:   time.Now().UTC(),
	}

	if err := us.store.PutUser(r.Context(), user); err != nil {
		writeError(w, http.StatusInternalServerError, "create user: "+err.Error())
		return
	}

	writeJSON(w, http.StatusCreated, user)
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
