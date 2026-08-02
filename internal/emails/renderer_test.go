package emails

import (
	"bytes"
	"fmt"
	htmltemplate "html/template"
	"os"
	"strings"
	"sync"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"
)

func TestMissiveTemplateDoesNotHTMLEscapePlainTextURLs(t *testing.T) {
	ctx := &config.AppContext{}
	letter := &mtypes.Letter{
		UID:      1,
		Markdown: "Open {{ .URL }}",
	}

	var out bytes.Buffer
	err := executeMissiveTemplate(ctx, &out, letter, map[string]string{
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
	if err := executeMissiveTemplate(&config.AppContext{}, &rendered, letter, &mtypes.EmailContent{}); err != nil {
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

func TestMissiveTemplateCacheIsSafeForConcurrentUse(t *testing.T) {
	ctx := &config.AppContext{}
	letter := &mtypes.Letter{UID: 7, Markdown: "Hello {{ .Name }}"}

	const workers = 64
	errCh := make(chan error, workers)
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			var out bytes.Buffer
			if err := executeMissiveTemplate(ctx, &out, letter, map[string]string{"Name": "Nifty"}); err != nil {
				errCh <- err
				return
			}
			if got := out.String(); got != "Hello Nifty" {
				errCh <- fmt.Errorf("rendered %q", got)
			}
		}()
	}
	wg.Wait()
	close(errCh)
	for err := range errCh {
		t.Errorf("concurrent render: %v", err)
	}
}

func TestMissiveTemplateReturnsParseErrors(t *testing.T) {
	_, err := missiveTemplate(&config.AppContext{}, &mtypes.Letter{UID: 8, Markdown: "{{ broken"})
	if err == nil {
		t.Fatal("missiveTemplate returned nil error for invalid template")
	}
}

func TestTemplatedNewsletterDisplayDateCanUseSendAt(t *testing.T) {
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
issue: "42"
date: "JAN 24, 2026"
---

Body.
`)
	sendAt := time.Date(2026, time.May, 25, 9, 0, 0, 0, time.UTC)
	htmlBody, _, err := BuildTemplatedNewsletterEmailAt(ctx, "/static/img/newsletter/logo_blk.svg", markdown, "", sendAt)
	if err != nil {
		t.Fatalf("build templated newsletter: %v", err)
	}
	html := string(htmlBody)
	if !strings.Contains(html, "MAY 25, 2026") {
		t.Fatalf("rendered email did not use sendAt date: %s", html)
	}
	if strings.Contains(html, "JAN 24, 2026") {
		t.Fatalf("rendered email used stale frontmatter date: %s", html)
	}
}
