package auth_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/takutakahashi/memory-server/internal/auth"
)

// fakeStore implements auth.UserStorer for testing.
type fakeStore struct {
	users map[string]*auth.User
}

func newFakeStore(users ...*auth.User) *fakeStore {
	m := make(map[string]*auth.User)
	for _, u := range users {
		m[u.Token] = u
	}
	return &fakeStore{users: m}
}

func (f *fakeStore) PutUser(_ context.Context, u *auth.User) error {
	f.users[u.Token] = u
	return nil
}

func (f *fakeStore) GetUser(_ context.Context, userID string) (*auth.User, error) {
	for _, u := range f.users {
		if u.UserID == userID {
			cp := *u
			return &cp, nil
		}
	}
	return nil, fmt.Errorf("%w: user %s", auth.ErrNotFound, userID)
}

func (f *fakeStore) GetUserByToken(_ context.Context, token string) (*auth.User, error) {
	u, ok := f.users[token]
	if !ok {
		return nil, fmt.Errorf("%w: token", auth.ErrNotFound)
	}
	cp := *u
	return &cp, nil
}

var _ auth.UserStorer = (*fakeStore)(nil)

// captureUserID is a handler that captures the user_id from context.
func captureUserID(captured *string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*captured = auth.UserIDFromContext(r.Context())
		w.WriteHeader(http.StatusOK)
	})
}

func bearerReq(token string) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	return r
}

// -------------------------------------------------------------------------
// BearerAuth tests
// -------------------------------------------------------------------------

func TestBearerAuth_NoToken_Returns401(t *testing.T) {
	store := newFakeStore()
	var uid string
	h := auth.BearerAuth(store)(captureUserID(&uid))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearerReq(""))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestBearerAuth_InvalidToken_Returns401(t *testing.T) {
	store := newFakeStore()
	var uid string
	h := auth.BearerAuth(store)(captureUserID(&uid))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearerReq("unknown-token"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestBearerAuth_ValidUserToken_InjectsUserID(t *testing.T) {
	store := newFakeStore(&auth.User{UserID: "alice", Token: "alice-token"})
	var uid string
	h := auth.BearerAuth(store)(captureUserID(&uid))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearerReq("alice-token"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if uid != "alice" {
		t.Errorf("user_id = %q, want alice", uid)
	}
}

func TestBearerAuth_AdminToken_GrantsAdminAccess(t *testing.T) {
	t.Setenv("ADMIN_TOKEN", "super-secret")
	store := newFakeStore() // no users in store
	var uid string
	h := auth.BearerAuth(store)(captureUserID(&uid))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearerReq("super-secret"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if uid != "admin" {
		t.Errorf("user_id = %q, want admin", uid)
	}
}

func TestBearerAuth_AdminTokenNotSet_FallsBackToUserLookup(t *testing.T) {
	// ADMIN_TOKEN unset — regular user token should still work
	store := newFakeStore(&auth.User{UserID: "bob", Token: "bob-token"})
	var uid string
	h := auth.BearerAuth(store)(captureUserID(&uid))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearerReq("bob-token"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if uid != "bob" {
		t.Errorf("user_id = %q, want bob", uid)
	}
}

func TestBearerAuth_CuratorToken_GrantsCuratorAccess(t *testing.T) {
	t.Setenv("CURATOR_TOKEN", "curator-secret")
	store := newFakeStore() // no users in store
	var uid string
	h := auth.BearerAuth(store)(captureUserID(&uid))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearerReq("curator-secret"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if uid != "curator" {
		t.Errorf("user_id = %q, want curator", uid)
	}
}

func TestBearerAuth_CuratorTokenNotSet_FallsBackToUserLookup(t *testing.T) {
	// CURATOR_TOKEN unset — regular user token should still work
	store := newFakeStore(&auth.User{UserID: "carol", Token: "carol-token"})
	var uid string
	h := auth.BearerAuth(store)(captureUserID(&uid))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearerReq("carol-token"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	if uid != "carol" {
		t.Errorf("user_id = %q, want carol", uid)
	}
}

// -------------------------------------------------------------------------
// AdminTokenAuth tests
// -------------------------------------------------------------------------

func TestAdminTokenAuth_NotSet_Returns503(t *testing.T) {
	h := auth.AdminTokenAuth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearerReq("anything"))
	if w.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503, got %d", w.Code)
	}
}

func TestAdminTokenAuth_WrongToken_Returns401(t *testing.T) {
	t.Setenv("ADMIN_TOKEN", "correct")
	h := auth.AdminTokenAuth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearerReq("wrong"))
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("expected 401, got %d", w.Code)
	}
}

func TestAdminTokenAuth_CorrectToken_Returns200(t *testing.T) {
	t.Setenv("ADMIN_TOKEN", "correct")
	h := auth.AdminTokenAuth()(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	w := httptest.NewRecorder()
	h.ServeHTTP(w, bearerReq("correct"))
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
}
