package config

import (
	"context"
	htmltemplate "html/template"
	"log"
	texttemplate "text/template"
	"time"

	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
)

const DatabaseOperationTimeout = 15 * time.Second

/* application configuration settings */
type AppContext struct {
	Env *types.EnvConfig
	DB  *Database

	InProduction  bool
	Err           *log.Logger
	Infos         *log.Logger
	Session       *scs.SessionManager
	TemplateCache *htmltemplate.Template
	EmailCache    map[string]*texttemplate.Template
}

// DatabaseContext supplies the parent context used by the legacy data layer.
// Database owns and promptly cancels the per-operation timeout derived from
// this context.
func (c *AppContext) DatabaseContext() context.Context {
	return DatabaseContext()
}

func DatabaseContext() context.Context {
	return context.Background()
}
