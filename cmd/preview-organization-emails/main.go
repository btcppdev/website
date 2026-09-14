// preview-organization-emails serves the real email renderer with sample data.
// Run from the repository root. It never loads credentials or sends mail.
package main

import (
	"flag"
	"fmt"
	"html/template"
	"log"
	"net/http"
	"os"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/types"
)

func main() {
	addr := flag.String("addr", "127.0.0.1:8091", "preview listen address")
	flag.Parse()
	mux := http.NewServeMux()
	mux.Handle("/static/img/", http.StripPrefix("/static/img/", http.FileServer(http.Dir("static/img"))))
	mux.HandleFunc("/email/", func(w http.ResponseWriter, r *http.Request) {
		role := strings.TrimPrefix(r.URL.Path, "/email/")
		if role != "member" && role != "manager" && role != "owner" {
			http.NotFound(w, r)
			return
		}
		source, err := os.ReadFile("templates/emails/rebrand.tmpl")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		tmpl, err := template.New("emails/rebrand.tmpl").Parse(string(source))
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		ctx := &config.AppContext{Env: &types.EnvConfig{Prod: true, Host: "btcpp.dev"}, TemplateCache: tmpl}
		orgName := "Base58"
		if r.URL.Query().Get("long") == "1" {
			orgName = "Bitcoin Open Source Builders Collective"
		}
		m := &types.OrganizationMembership{OrganizationID: "preview-org", PersonID: "preview-person", Role: role, CreatedAt: time.Now(), Organization: &types.Org{Name: orgName, LogoLight: "http://" + r.Host + "/static/img/base58_purple_new.png"}}
		mail, err := emails.BuildOrganizationWelcomeMail(ctx, m, "Lisa", "lisa@example.com", "https://btcpp.dev/dashboard/orgs/base58", "/static/img/berlin26/leading.png", "Alex")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write(mail.HTMLBody)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/" {
			http.NotFound(w, r)
			return
		}
		role := r.URL.Query().Get("role")
		if role != "member" && role != "owner" {
			role = "manager"
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprintf(w, `<!doctype html><html lang="en"><meta charset="utf-8"><meta name="viewport" content="width=device-width,initial-scale=1"><title>Organization email preview</title><style>body{margin:0;padding:28px;background:#eeeae1;color:#1c1c1e;font:16px system-ui}h1{font-size:26px}a{color:inherit}nav{display:flex;gap:20px;margin:24px 0}.previews{display:flex;gap:28px;align-items:flex-start;flex-wrap:wrap}h2{font-size:15px}iframe{display:block;border:1px solid #aaa;width:100%%;height:1100px;background:white}.desktop{width:720px;max-width:100%%}.mobile{width:375px;max-width:100%%}p{max-width:750px;line-height:1.5}</style><h1>Organization welcome email</h1><p>Sample: Base58 · Berlin conference artwork. These previews use the actual email template. Browser previews do not simulate every email app.</p><nav><a href="/?role=member">Member</a><a href="/?role=manager">Manager</a><a href="/?role=owner">Owner</a><a href="/email/%s">Open email directly</a></nav><main class="previews"><section class="desktop"><h2>Desktop · %s</h2><iframe title="Desktop email" src="/email/%s"></iframe></section><section class="mobile"><h2>Mobile · 375px</h2><iframe title="Mobile email" src="/email/%s"></iframe></section></main></html>`, role, role, role, role)
	})
	log.Printf("Organization email preview: http://%s", *addr)
	log.Fatal((&http.Server{Addr: *addr, Handler: mux, ReadHeaderTimeout: 5 * time.Second}).ListenAndServe())
}
