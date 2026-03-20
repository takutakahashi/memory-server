package auth

import (
	"context"
	"errors"
	"net/http"
	"strings"
)

type contextKey string

const userIDKey contextKey = "user_id"

// UserIDFromContext extracts the authenticated user_id from the request context.
// Returns empty string if not authenticated.
func UserIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(userIDKey).(string)
	return v
}

// BearerAuth returns an HTTP middleware that validates Bearer tokens using the
// provided UserStorer. On success it injects the user_id into the request context.
// On failure it responds with 401 Unauthorized.
func BearerAuth(store UserStorer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				http.Error(w, `{"error":"missing Authorization Bearer token"}`, http.StatusUnauthorized)
				return
			}

			user, err := store.GetUserByToken(r.Context(), token)
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					http.Error(w, `{"error":"invalid token"}`, http.StatusUnauthorized)
				} else {
					http.Error(w, `{"error":"auth error"}`, http.StatusInternalServerError)
				}
				return
			}

			ctx := context.WithValue(r.Context(), userIDKey, user.UserID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

// OrgTokenAuth returns an HTTP middleware that validates org-level Bearer tokens.
// On success it injects the org_id into the request context via orgIDKey.
func OrgTokenAuth(store UserStorer) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			token := extractBearerToken(r)
			if token == "" {
				http.Error(w, `{"error":"missing Authorization Bearer token"}`, http.StatusUnauthorized)
				return
			}

			orgToken, err := store.GetOrgToken(r.Context(), token)
			if err != nil {
				if errors.Is(err, ErrNotFound) {
					http.Error(w, `{"error":"invalid org token"}`, http.StatusUnauthorized)
				} else {
					http.Error(w, `{"error":"auth error"}`, http.StatusInternalServerError)
				}
				return
			}

			ctx := context.WithValue(r.Context(), orgIDKey, orgToken.OrgID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

type orgContextKey string

const orgIDKey orgContextKey = "org_id"

// OrgIDFromContext extracts the authenticated org_id from the request context.
func OrgIDFromContext(ctx context.Context) string {
	v, _ := ctx.Value(orgIDKey).(string)
	return v
}

func extractBearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	if h == "" {
		return ""
	}
	parts := strings.SplitN(h, " ", 2)
	if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
		return ""
	}
	return strings.TrimSpace(parts[1])
}
