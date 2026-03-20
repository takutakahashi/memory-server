package auth

import (
	"context"
	"errors"
	"net/http"
	"os"
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

// AdminTokenAuth returns an HTTP middleware that validates a static admin token
// set via the ADMIN_TOKEN environment variable. If ADMIN_TOKEN is not set,
// all requests are rejected with 503.
func AdminTokenAuth() func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			adminToken := os.Getenv("ADMIN_TOKEN")
			if adminToken == "" {
				http.Error(w, `{"error":"admin API is disabled (ADMIN_TOKEN not set)"}`, http.StatusServiceUnavailable)
				return
			}

			token := extractBearerToken(r)
			if token == "" {
				http.Error(w, `{"error":"missing Authorization Bearer token"}`, http.StatusUnauthorized)
				return
			}
			if token != adminToken {
				http.Error(w, `{"error":"invalid admin token"}`, http.StatusUnauthorized)
				return
			}

			next.ServeHTTP(w, r)
		})
	}
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
