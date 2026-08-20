// Package requestid stores one request identifier in a request context.
package requestid

import (
	"context"
	"fmt"
	"sync/atomic"
	"time"
)

type contextKey struct{}

var counter uint64

// New returns a process-unique, log-friendly request identifier.
func New() string {
	n := atomic.AddUint64(&counter, 1)
	return fmt.Sprintf("%x-%06x", time.Now().UnixNano(), n)
}

// With returns a context carrying id.
func With(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, contextKey{}, id)
}

// From returns the request identifier in ctx, or an empty string.
func From(ctx context.Context) string {
	if ctx == nil {
		return ""
	}
	id, _ := ctx.Value(contextKey{}).(string)
	return id
}
