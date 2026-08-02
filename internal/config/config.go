package config

import (
	"context"
	htmltemplate "html/template"
	"log"
	"sync"
	texttemplate "text/template"
	"time"

	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
	"github.com/jackc/pgx/v5/pgxpool"
)

const DatabaseOperationTimeout = 15 * time.Second

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
}

// TextTemplateCache safely shares parsed email templates between HTTP requests
// and background mail workers. Its zero value is ready for use.
type TextTemplateCache struct {
	mu        sync.RWMutex
	templates map[string]*texttemplate.Template
}

func (c *TextTemplateCache) LoadOrStore(key string, parse func() (*texttemplate.Template, error)) (*texttemplate.Template, error) {
	c.mu.RLock()
	tmpl := c.templates[key]
	c.mu.RUnlock()
	if tmpl != nil {
		return tmpl, nil
	}

	parsed, err := parse()
	if err != nil {
		return nil, err
	}

	c.mu.Lock()
	defer c.mu.Unlock()
	if tmpl = c.templates[key]; tmpl != nil {
		return tmpl, nil
	}
	if c.templates == nil {
		c.templates = make(map[string]*texttemplate.Template)
	}
	c.templates[key] = parsed
	return parsed, nil
}

// DatabaseContext bounds both pool acquisition and query execution. Most of
// the data layer predates request-scoped contexts, so this is a hard safety
// boundary until callers can pass r.Context() all the way through.
func (c *AppContext) DatabaseContext() context.Context {
	return DatabaseContext()
}

func DatabaseContext() context.Context {
	ctx, _ := context.WithTimeout(context.Background(), DatabaseOperationTimeout)
	return ctx
}
