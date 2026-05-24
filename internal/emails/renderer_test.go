package emails

import (
	"bytes"
	htmltemplate "html/template"
	"os"
	"strings"
	"testing"
	texttemplate "text/template"

	"btcpp-web/internal/config"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"
)

func TestMissiveTemplateDoesNotHTMLEscapePlainTextURLs(t *testing.T) {
	ctx := &config.AppContext{
		EmailCache: make(map[string]*texttemplate.Template),
	}
	letter := &mtypes.Letter{
		UID:      1,
		Markdown: "Open {{ .URL }}",
	}

	var out bytes.Buffer
	err := missiveTemplate(ctx, letter).Execute(&out, map[string]string{
		"URL": "https://btcpp.dev/dashboard?email=test@example.com&token=abc123",
	})
	if err != nil {
		t.Fatalf("execute template: %v", err)
	}

	got := out.String()
	if strings.Contains(got, "&amp;") {
		t.Fatalf("plain text email body contains HTML entity: %q", got)
	}
	if !strings.Contains(got, "email=test@example.com&token=abc123") {
		t.Fatalf("plain text email body lost raw query separator: %q", got)
	}
}

func TestTemplatedNewsletterFrontmatterAndShortcodes(t *testing.T) {
	ctx := &config.AppContext{
		Env: &types.EnvConfig{Host: "btcpp.dev", Prod: true},
	}
	rebrandTmpl, err := os.ReadFile("../../templates/emails/rebrand.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	ctx.TemplateCache = htmltemplate.Must(htmltemplate.New("").New("emails/rebrand.tmpl").Parse(string(rebrandTmpl)))
	markdown := []byte(`---
template: roundup
palette: signal
issue: "42"
date: "APR 18, 2026"
hero: "https://btcpp.dev/hero.png"
ticker:
  - VIENNA TICKETS LIVE
  - NAIROBI CFP OPEN
---

{{ lead "§ FEATURE" "Villain edition." "A short deck." }}

{{ newsList "Core 28 ships | Cleanup landed | CORE | https://btcpp.dev/core?x=1&y=2" }}

{{ cta "NEXT STOP" "Vienna · June 12+13." "Earlybird tickets live." "GRAB A TICKET" "https://btcpp.dev/vienna" }}
`)

	letter := &mtypes.Letter{
		UID:      42,
		OnlyFor:  mtypes.OnlyForTemplated,
		Markdown: string(markdown),
	}
	var rendered bytes.Buffer
	if err := missiveTemplate(&config.AppContext{EmailCache: map[string]*texttemplate.Template{}}, letter).Execute(&rendered, &mtypes.EmailContent{}); err != nil {
		t.Fatalf("execute templated missive: %v", err)
	}

	htmlBody, textBody, err := BuildTemplatedNewsletterEmail(ctx, "/static/img/newsletter/logo_blk.svg", rendered.Bytes(), "tok")
	if err != nil {
		t.Fatalf("build templated newsletter: %v", err)
	}
	html := string(htmlBody)
	if !strings.Contains(html, "VIENNA TICKETS LIVE") {
		t.Fatalf("ticker was not rendered: %s", html)
	}
	if !strings.Contains(html, "Villain edition.") {
		t.Fatalf("lead was not rendered: %s", html)
	}
	if !strings.Contains(html, "Core 28 ships") {
		t.Fatalf("news list was not rendered: %s", html)
	}
	if !strings.Contains(html, "https://btcpp.dev/newsletter/unsubscribe/tok") {
		t.Fatalf("unsubscribe URL missing: %s", html)
	}
	if strings.Contains(string(textBody), "---") {
		t.Fatalf("text body should not include frontmatter: %q", textBody)
	}
}
