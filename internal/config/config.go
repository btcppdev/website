package config

import (
	htmltemplate "html/template"
	"log"
	texttemplate "text/template"

	"btcpp-web/internal/types"
	"github.com/alexedwards/scs/v2"
)

/* application configuration settings */
type AppContext struct {
	Env    *types.EnvConfig
	Notion *types.Notion

	InProduction  bool
	Err           *log.Logger
	Infos         *log.Logger
	Session       *scs.SessionManager
	TemplateCache *htmltemplate.Template
	EmailCache    map[string]*texttemplate.Template
}
