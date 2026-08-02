package config

import (
	"context"
	"sync/atomic"
	"testing"
	texttemplate "text/template"
)

func TestWithDatabaseContextPropagatesCancellation(t *testing.T) {
	base := &AppContext{}
	parent, cancel := context.WithCancel(context.Background())
	scoped := base.WithDatabaseContext(parent)

	cancel()
	select {
	case <-scoped.DatabaseContext().Done():
	default:
		t.Fatal("request-scoped database context was not canceled")
	}
	if err := base.DatabaseContext().Err(); err != nil {
		t.Fatalf("base database context was canceled: %v", err)
	}
}

func TestDetachedDatabaseContextOutlivesRequest(t *testing.T) {
	parent, cancel := context.WithCancel(context.Background())
	scoped := (&AppContext{}).WithDatabaseContext(parent)
	detached := scoped.Detached()

	cancel()
	if err := scoped.DatabaseContext().Err(); err == nil {
		t.Fatal("request-scoped database context was not canceled")
	}
	if err := detached.DatabaseContext().Err(); err != nil {
		t.Fatalf("detached database context was canceled: %v", err)
	}
}

func TestRequestScopedCopiesShareEmailCache(t *testing.T) {
	base := &AppContext{}
	base.EmailCache.Initialize()
	scoped := base.WithDatabaseContext(context.Background())
	var parses atomic.Int32
	parse := func() (*texttemplate.Template, error) {
		parses.Add(1)
		return texttemplate.New("email").Parse("hello")
	}

	if _, err := base.EmailCache.LoadOrStore("shared", parse); err != nil {
		t.Fatal(err)
	}
	if _, err := scoped.EmailCache.LoadOrStore("shared", parse); err != nil {
		t.Fatal(err)
	}
	if got := parses.Load(); got != 1 {
		t.Fatalf("parsed template %d times, want 1", got)
	}
}
