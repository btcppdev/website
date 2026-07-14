package config

import (
	"context"
	htmltemplate "html/template"
	"log"
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
	EmailCache    map[string]*texttemplate.Template
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
