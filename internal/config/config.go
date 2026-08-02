package config

import (
	"context"
	htmltemplate "html/template"
	"log"
	"sync"
	texttemplate "text/template"

	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

/* application configuration settings */
type AppContext struct {
	Env *types.EnvConfig
	DB  *pgxpool.Pool

	InProduction  bool
	Err           *log.Logger
	Infos         *log.Logger
	Session       *scs.SessionManager
	TemplateCache *htmltemplate.Template
	EmailCache    TextTemplateCache

	databaseContext context.Context
}

// TextTemplateCache safely shares parsed email templates between HTTP requests
// and background mail workers. Its zero value is ready for use.
type TextTemplateCache struct {
	state *textTemplateCacheState
}

type textTemplateCacheState struct {
	mu        sync.RWMutex
	templates map[string]*texttemplate.Template
}

var textTemplateCacheInitMu sync.Mutex

// Initialize ensures copies of this cache share the same synchronized state.
// AppContext initializes it before creating request-scoped copies; the method
// also keeps the zero value usable in tests and standalone tools.
func (c *TextTemplateCache) Initialize() {
	textTemplateCacheInitMu.Lock()
	defer textTemplateCacheInitMu.Unlock()
	if c.state == nil {
		c.state = &textTemplateCacheState{templates: make(map[string]*texttemplate.Template)}
	}
}

func (c *TextTemplateCache) LoadOrStore(key string, parse func() (*texttemplate.Template, error)) (*texttemplate.Template, error) {
	c.Initialize()
	state := c.state
	state.mu.RLock()
	tmpl := state.templates[key]
	state.mu.RUnlock()
	if tmpl != nil {
		return tmpl, nil
	}

	parsed, err := parse()
	if err != nil {
		return nil, err
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	if tmpl = state.templates[key]; tmpl != nil {
		return tmpl, nil
	}
	state.templates[key] = parsed
	return parsed, nil
}

// WithDatabaseContext returns a request-scoped view of the application. The
// shared fields remain pointers; only the database parent context differs.
func (c *AppContext) WithDatabaseContext(parent context.Context) *AppContext {
	if c == nil {
		return nil
	}
	if parent == nil {
		parent = context.Background()
	}
	scoped := *c
	scoped.databaseContext = parent
	return &scoped
}

// Detached removes a request lifetime from work that intentionally continues
// after its HTTP handler returns.
func (c *AppContext) Detached() *AppContext {
	return c.WithDatabaseContext(context.Background())
}

// DatabaseContext propagates request cancellation through the existing data
// layer. PostgreSQL's statement_timeout remains the per-statement time bound.
func (c *AppContext) DatabaseContext() context.Context {
	if c != nil && c.databaseContext != nil {
		return c.databaseContext
	}
	return context.Background()
}
