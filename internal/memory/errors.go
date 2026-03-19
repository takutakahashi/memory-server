package memory

import "errors"

// ErrNotFound is returned when a memory is not found.
var ErrNotFound = errors.New("not found")

// ErrUnauthorized is returned when a token is invalid.
var ErrUnauthorized = errors.New("unauthorized")
