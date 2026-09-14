package handlers

import (
	"bytes"
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/prizepool"
)

type nodeConfigPage struct {
	MonitorCommand, ProvisionCommand      string
	Host, NodeID, Network, Domain, CFZone string
	MonitorSaved, ProvisionSaved, DNS     bool
	Active                                bool
	Revision                              int64
	CSRF, Error, Message                  string
	Year                                  uint
}

func nodeConfigView(c prizepool.NodeConfig) nodeConfigPage {
	monitor, provision := prizepool.RuneCommands()
	return nodeConfigPage{MonitorCommand: monitor, ProvisionCommand: provision, Host: c.Host, NodeID: c.NodeID, Network: c.Network, Domain: c.Domain, CFZone: c.CFZone,
		MonitorSaved: c.Rune != "", ProvisionSaved: c.ProvisionRune != "", DNS: c.CFToken != "", Active: c.Active, Revision: c.Revision, Year: helpers.CurrentYear()}
}
func canManageNodeConfig(id *auth.Identity) bool { return id != nil && id.IsAccountsAdmin() }
func AdminNodeConfig(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	id := requirePersonIdentity(w, r, app)
	if id == nil {
		return
	}
	serveNodeConfig(w, r, app, id)
}
func serveNodeConfig(w http.ResponseWriter, r *http.Request, app *config.AppContext, id *auth.Identity) {
	if !canManageNodeConfig(id) {
		http.Error(w, "Accounts-admin access required", http.StatusForbidden)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 15*time.Second)
	defer cancel()
	c, err := prizepool.LoadNodeConfig(ctx, app.DB, app.Env.HMACSecret)
	if err != nil {
		http.Error(w, "Unable to load node configuration", http.StatusServiceUnavailable)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare form", 500)
		return
	}
	page := nodeConfigView(c)
	page.CSRF = csrf
	if r.URL.Query().Get("saved") == "1" {
		page.Message = "Configuration saved. Monitoring picks up changes within a few seconds."
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 16384)
		if r.ParseForm() != nil || !secureTokenEqual(csrf, r.PostForm.Get("csrf")) {
			http.Error(w, "Invalid form token", 403)
			return
		}
		revision, e := strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
		if e != nil || revision != c.Revision {
			http.Error(w, "Configuration changed. Reload before continuing.", 409)
			return
		}
		switch r.PostForm.Get("action") {
		case "save":
			c.Host = strings.TrimSpace(r.PostForm.Get("host"))
			c.NodeID = strings.ToLower(strings.TrimSpace(r.PostForm.Get("node_id")))
			c.Network = r.PostForm.Get("network")
			c.Domain = strings.ToLower(strings.TrimSpace(r.PostForm.Get("domain")))
			c.CFZone = strings.TrimSpace(r.PostForm.Get("cf_zone"))
			c.Active = r.PostForm.Get("enabled") == "yes"
			updateSecret := func(name string, value *string) {
				if r.PostForm.Get("clear_"+name) == "yes" {
					*value = ""
				} else if replacement := strings.TrimSpace(r.PostForm.Get(name)); replacement != "" {
					*value = replacement
				}
			}
			updateSecret("monitor_rune", &c.Rune)
			updateSecret("provision_rune", &c.ProvisionRune)
			updateSecret("cf_token", &c.CFToken)
			if err = prizepool.ValidateNodeConfig(c); err != nil {
				page.Error = err.Error()
			} else if err = prizepool.SaveNodeConfig(ctx, app.DB, app.Env.HMACSecret, id.PersonID, c); err != nil {
				// Storage errors and credentials are never reflected or logged.
				page.Error = "Unable to save. Reload to check for another administrator’s changes. Existing pools must retain their node, network, and address domain."
			} else {
				http.Redirect(w, r, "/admin/node-config?saved=1", http.StatusSeeOther)
				return
			}
			// Preserve non-secret edits, but presence flags describe saved credentials.
			page.Host, page.NodeID, page.Network, page.Domain, page.CFZone, page.Active = c.Host, c.NodeID, c.Network, c.Domain, c.CFZone, c.Active
		case "test":
			if !c.Enabled() {
				page.Error = "Save a host, node public key, and monitoring rune first."
				break
			}
			rpc := prizepool.Commando{Host: c.Host, NodeID: c.NodeID, Rune: c.Rune}
			if err = prizepool.VerifyNode(ctx, rpc, c.Settings); err != nil {
				page.Error = "Connection check failed. Check the host, public key, network, and monitoring rune."
			} else {
				page.Message = "Connected: node identity and network match. Invoice-monitoring permissions, provisioning, DNS, and wallet payments still need verification."
			}
		default:
			http.Error(w, "Unknown action", 400)
			return
		}
	}
	var buf bytes.Buffer
	if err = app.TemplateCache.ExecuteTemplate(&buf, "admin/node_config.tmpl", page); err != nil {
		http.Error(w, "Unable to render node configuration", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Write(buf.Bytes())
}
