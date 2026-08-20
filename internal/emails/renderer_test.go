package emails

import (
	"bytes"
	htmltemplate "html/template"
	"os"
	"strings"
	"testing"
	texttemplate "text/template"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"
	conferencemissives "btcpp-web/templates/missives"
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
	err := executeMissiveTemplate(ctx, letter, &out, map[string]string{
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

func TestLoginEmailUsesButtonDestination(t *testing.T) {
	link := "https://btcpp.dev/auth/magic?token=test-token"
	if got, want := loginEmailButtonLink(link), "button#"+link; got != want {
		t.Fatalf("login email destination = %q, want %q", got, want)
	}
}

func TestConferenceCampaignPreviewUsesNewsletterWrapper(t *testing.T) {
	rebrand, err := os.ReadFile("../../templates/emails/rebrand.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	ctx := &config.AppContext{
		Env:           &types.EnvConfig{Host: "localhost:8888"},
		EmailCache:    make(map[string]*texttemplate.Template),
		TemplateCache: htmltemplate.Must(htmltemplate.New("").New("emails/rebrand.tmpl").Parse(string(rebrand))),
	}
	letter := &mtypes.Letter{
		UID: 42, OnlyFor: mtypes.OnlyForTemplated,
		Markdown: "---\ntemplate: announce\npalette: ember\nissue: EVENT UPDATE\n---\n\nHello {{ .Name }}",
	}
	html, err := RenderConferenceCampaignPreview(ctx, letter, &ConferenceCampaignData{
		Name: "Ada", SendAt: time.Date(2026, time.August, 13, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	for _, want := range []string{"a dispatch from the frontier", "ISSUE EVENT UPDATE", "Hello Ada"} {
		if !strings.Contains(got, want) {
			t.Fatalf("conference newsletter preview missing %q", want)
		}
	}
}

func TestConferenceCampaignDefaultUsesNewsletterSections(t *testing.T) {
	rebrand, err := os.ReadFile("../../templates/emails/rebrand.tmpl")
	if err != nil {
		t.Fatal(err)
	}
	definition, err := conferencemissives.DefinitionForKind(types.ConferenceCampaignAttendeeReminder70)
	if err != nil {
		t.Fatal(err)
	}
	ctx := &config.AppContext{
		Env:           &types.EnvConfig{Host: "localhost:8888"},
		EmailCache:    make(map[string]*texttemplate.Template),
		TemplateCache: htmltemplate.Must(htmltemplate.New("").New("emails/rebrand.tmpl").Parse(string(rebrand))),
	}
	letter := &mtypes.Letter{UID: 43, Title: "✨ bitcoin++ dev26 ++: We're getting closer", OnlyFor: mtypes.OnlyForTemplated, Markdown: definition.Markdown}
	html, err := RenderConferenceCampaignPreview(ctx, letter, &ConferenceCampaignData{
		Conf: &types.Conf{Desc: "bitcoin++ test", Location: "Test City"}, Name: "Ada",
		DashboardLink: "http://localhost:8888/dashboard", GeneratedUpdates: "### What's new\n\n- The agenda is live.",
		SendAt: time.Date(2026, time.August, 13, 10, 0, 0, 0, time.UTC),
	})
	if err != nil {
		t.Fatal(err)
	}
	got := string(html)
	for _, want := range []string{
		"EVENT UPDATE", "closer</h1>", "getting closer.", "The agenda is live.",
		"background:#F57247", "Get ready for bitcoin++", "Open your dashboard",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("conference default preview missing %q", want)
		}
	}
}

func TestConferenceCampaignHeadlineUsesSubjectSuffix(t *testing.T) {
	if got := conferenceCampaignHeadline("✨ bitcoin++ dev26 ++: Volunteer Orientation Next Week!"); got != "Volunteer Orientation Next Week!" {
		t.Fatalf("conferenceCampaignHeadline = %q", got)
	}
}

func TestTemplatizeTitleReturnsPlainText(t *testing.T) {
	data := struct {
		Tag   string
		Emoji string
		Name  string
	}{Tag: "dev26", Emoji: "++", Name: "O'Brien"}
	want := "✨ bitcoin++ dev26 ++: O'Brien, your moment at bitcoin++ has arrived"
	if got := templatizeTitle("✨ bitcoin++ {{ .Tag }} {{ .Emoji }}: {{ .Name }}, your moment at bitcoin++ has arrived", data); got != want {
		t.Fatalf("templatizeTitle = %q, want %q", got, want)
	}
}

func TestEmailButtonPreservesVerificationToken(t *testing.T) {
	verificationURL := "http://localhost:8888/dashboard/emails/verify?token=abc_123"
	html := string(mdToHTML([]byte("[Add This Email](button#" + verificationURL + ")")))
	if !strings.Contains(html, `href="`+verificationURL+`"`) {
		t.Fatalf("rendered email button dropped verification token: %s", html)
	}
}

func TestNewsletterNestedUpdateBulletsRenderAsNestedLists(t *testing.T) {
	markdown := []byte("- New speakers confirmed:\n    - Alice of ACME. [x.com](https://x.com/alice)\n    - Bob.\n- New sponsors:\n    - Bitco — Gold sponsor\n")
	html := string(mdToHTML(markdown))
	if strings.Count(html, "<ul") != 3 {
		t.Fatalf("nested update bullets rendered with %d lists, want 3: %s", strings.Count(html, "<ul"), html)
	}
	for _, want := range []string{"New speakers confirmed", "Alice of ACME", "New sponsors", "Bitco — Gold sponsor"} {
		if !strings.Contains(html, want) {
			t.Errorf("nested list render missing %q: %s", want, html)
		}
	}
}

func TestRebrandCTARendersMarkdownStrongInSubtitle(t *testing.T) {
	html := rebrandCTA("subscriber offer", "Get your ticket", "Use code **SUBSCRIBER20**", "Get your ticket", "https://btcpp.dev/dev26?code=SUBSCRIBER20#tickets")
	if !strings.Contains(html, "Use code <strong>SUBSCRIBER20</strong>") {
		t.Fatalf("CTA subtitle did not render strong discount code: %s", html)
	}
	if !strings.Contains(html, `href="https://btcpp.dev/dev26?code=SUBSCRIBER20#tickets"`) {
		t.Fatalf("CTA URL lost query parameter or fragment: %s", html)
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

{{ button "Read the full issue" "https://insider.btcpp.dev/p/weekly?from=email&issue=42" }}

{{ button "Get your ticket" (print .URI "/vienna#tickets") }}

{{ cta "NEXT STOP" "Vienna · June 12+13." "Earlybird tickets live." "GRAB A TICKET" "https://btcpp.dev/vienna" }}
`)

	letter := &mtypes.Letter{
		UID:      42,
		OnlyFor:  mtypes.OnlyForTemplated,
		Markdown: string(markdown),
	}
	var rendered bytes.Buffer
	if err := executeMissiveTemplate(&config.AppContext{EmailCache: map[string]*texttemplate.Template{}}, letter, &rendered, &mtypes.EmailContent{URI: "https://btcpp.dev"}); err != nil {
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
	if strings.Count(html, "VIENNA TICKETS LIVE") != 2 || !strings.Contains(html, "btcpp-ticker-track") {
		t.Fatalf("ticker was not repeated for continuous scrolling: %s", html)
	}
	if strings.Contains(html, "&#9654; LIVE") || strings.Contains(html, "▶ LIVE") {
		t.Fatalf("ticker still contains the retired live indicator: %s", html)
	}
	if !strings.Contains(html, "Villain edition.") {
		t.Fatalf("lead was not rendered: %s", html)
	}
	if !strings.Contains(html, "Core 28 ships") {
		t.Fatalf("news list was not rendered: %s", html)
	}
	if !strings.Contains(html, "Read the full issue") || !strings.Contains(html, "https://insider.btcpp.dev/p/weekly?from=email&amp;issue=42") {
		t.Fatalf("inline button was not rendered: %s", html)
	}
	if !strings.Contains(html, `href="https://btcpp.dev/vienna#tickets"`) {
		t.Fatalf(".URI button was not expanded to an absolute URL: %s", html)
	}
	if !strings.Contains(html, ".btcpp-content h3") || !strings.Contains(html, "border-top:1px solid #1C1C1E") {
		t.Fatalf("templated Markdown section-heading styles are missing: %s", html)
	}
	if strings.Contains(html, `{{ button`) {
		t.Fatalf("inline button shortcode leaked into rendered HTML: %s", html)
	}
	if !strings.Contains(html, "https://btcpp.dev/newsletter/unsubscribe/tok") {
		t.Fatalf("unsubscribe URL missing: %s", html)
	}
	if strings.Contains(string(textBody), "---") {
		t.Fatalf("text body should not include frontmatter: %q", textBody)
	}
}

func TestMissiveTemplateReturnsMalformedInlineButtonError(t *testing.T) {
	ctx := &config.AppContext{EmailCache: map[string]*texttemplate.Template{}}
	letter := &mtypes.Letter{
		UID:      43,
		OnlyFor:  mtypes.OnlyForTemplated,
		Markdown: "Before.\n\n{{ button [Read the issue](https://example.com) }}\n\nAfter.",
	}

	var rendered bytes.Buffer
	err := executeMissiveTemplate(ctx, letter, &rendered, &mtypes.EmailContent{})
	if err == nil {
		t.Fatal("malformed inline button unexpectedly rendered")
	}
	if !strings.Contains(err.Error(), `unexpected "[" in operand`) {
		t.Fatalf("error = %q, want parser detail", err)
	}
	if !strings.Contains(err.Error(), `inline buttons use {{ button "Label" "https://example.com" }}`) {
		t.Fatalf("error = %q, want corrected syntax", err)
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

func TestRebrandEmailCSSRemovesOuterBorderOnMobile(t *testing.T) {
	css := string(rebrandEmailCSS("signal"))
	if !strings.Contains(css, ".btcpp-inner { width: 640px; max-width: 100%; table-layout: fixed;") || !strings.Contains(css, "border: 1px solid #1C1C1E;") {
		t.Fatalf("desktop newsletter border missing: %s", css)
	}
	if !strings.Contains(css, "@media only screen and (max-width: 680px)") || !strings.Contains(css, ".btcpp-inner { border: 0 !important; }") {
		t.Fatalf("mobile newsletter border override missing: %s", css)
	}
	if !strings.Contains(css, "@keyframes btcpp-ticker-scroll") || !strings.Contains(css, ".btcpp-ticker { max-width: 0; overflow: hidden; white-space: nowrap; }") || !strings.Contains(css, "width: 100%; height: 14px; max-height: 14px; overflow: hidden; white-space: nowrap;") {
		t.Fatalf("single-line scrolling ticker CSS missing: %s", css)
	}
}

func TestRebrandLeadOmitsEmptyEyebrow(t *testing.T) {
	html := rebrandLead("", "what's new", "Weekly briefing")
	if strings.Contains(html, "btcpp-section-label") || strings.Contains(html, "§ FEATURE") {
		t.Fatalf("empty lead eyebrow rendered a fallback label: %s", html)
	}
	if !strings.Contains(html, "what&#39;s new") {
		t.Fatalf("lead title missing: %s", html)
	}
}
