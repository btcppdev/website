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

func TestNewsletterMobileMarkup(t *testing.T) {
	source, err := os.ReadFile("../../templates/emails/rebrand.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &config.AppContext{
		Env:           &types.EnvConfig{Host: "btcpp.dev", Prod: true},
		EmailCache:    map[string]*texttemplate.Template{},
		TemplateCache: htmltemplate.Must(htmltemplate.New("emails/rebrand.tmpl").Parse(string(source))),
	}
	letter := &mtypes.Letter{UID: 99, OnlyFor: mtypes.OnlyForTemplated, Markdown: `---
template: roundup
issue: "99"
hero: ""
ticker:
  - CONFERENCE TICKETS AVAILABLE
  - NEW TALKS AND COMMUNITY UPDATES
---
{{ lead "THIS WEEK" "Dispatches from the frontier" "The latest from the bitcoin++ community." }}

Read this week's updates, meet the builders, and find your next event.

{{ newsList "A new chapter for bitcoin builders | Meet the community and explore the latest projects. | COMMUNITY | https://btcpp.dev/news" }}

{{ stats "12 | New talks" "300 | Builders" "4 | Events" }}

{{ cta "UP NEXT" "Join the community" "Catch the latest developments in bitcoin." "Explore upcoming conferences" "https://btcpp.dev/#events" }}

{{ button "Read all updates" "https://btcpp.dev/news" }}
`}
	var body bytes.Buffer
	if err := executeMissiveTemplate(ctx, letter, &body, &mtypes.EmailContent{URI: "https://btcpp.dev"}); err != nil {
		t.Fatal(err)
	}
	html, _, err := BuildTemplatedNewsletterEmail(ctx, "", body.Bytes(), "test-unsubscribe")
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		`class="btcpp-inner" style="width:100%;max-width:640px;`,
		`class="btcpp-cta"`, `class="btcpp-cta-title"`, `class="btcpp-stat"`,
		`class="btcpp-news-cell btcpp-news-tag"`, `class="btcpp-header-cell btcpp-issue"`,
		`padding-left: 16px !important`, `font-size: 32px !important`,
		`display: block !important; width: auto !important;`,
	} {
		if !strings.Contains(string(html), want) {
			t.Errorf("mobile markup missing %s", want)
		}
	}
	if strings.Contains(string(html), `class="btcpp-inner" style="width:640px`) {
		t.Fatal("fixed desktop width returned")
	}
	// Optional standalone fixture for visual checks; never sends an email.
	if target := os.Getenv("BTCPP_NEWSLETTER_PREVIEW_HTML"); target != "" {
		if err := os.WriteFile(target, html, 0600); err != nil {
			t.Fatal(err)
		}
	}
}
