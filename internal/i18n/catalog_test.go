package i18n

import (
	"bytes"
	"html/template"
	"regexp"
	"strings"
	"testing"
)

func TestCatalog(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct{ locale, key, want string }{
		{"en", "tickets.remaining", "Only 7 tickets are left."},
		{"ko", "tickets.remaining", "7"},
		{"unknown", "tickets.remaining", "Only 7 tickets are left."},
	} {
		got, err := c.Message(tc.locale, tc.key, map[string]any{"Count": 7})
		if err != nil || !strings.Contains(got, tc.want) {
			t.Fatalf("%+v: %q %v", tc, got, err)
		}
	}
	if _, err := c.Message("en", "missing"); err == nil {
		t.Fatal("unknown key accepted")
	}
	if _, err := c.Message("ko", "tickets.remaining"); err == nil {
		t.Fatal("missing placeholder accepted")
	}
	// A partially translated locale must fall back to the English message.
	delete(c.Messages["ko"], "tickets.remaining")
	if got, err := c.Message("ko", "tickets.remaining", map[string]any{"Count": 3}); err != nil || got != "Only 3 tickets are left." {
		t.Fatalf("fallback: %q %v", got, err)
	}
}

func TestCatalogPlaceholderParity(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	fields := regexp.MustCompile(`\.[A-Z][A-Za-z0-9]*`)
	for locale, messages := range c.Messages {
		for key, translation := range messages {
			expected := map[string]bool{}
			actual := map[string]bool{}
			for _, f := range fields.FindAllString(c.Messages["en"][key].Tree.Root.String(), -1) {
				expected[f] = true
			}
			for _, f := range fields.FindAllString(translation.Tree.Root.String(), -1) {
				actual[f] = true
			}
			for f := range expected {
				if !actual[f] {
					t.Errorf("%s/%s missing %s", locale, key, f)
				}
			}
			for f := range actual {
				if !expected[f] {
					t.Errorf("%s/%s unexpected %s", locale, key, f)
				}
			}
		}
	}
}

func TestCatalogEscapesInterpolatedValues(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	tmpl := template.Must(template.New("test").Funcs(template.FuncMap{"msg": func(key string, data map[string]any) (string, error) { return c.Message("ko", key, data) }}).Parse(`{{msg "venue.default_title" .}}`))
	var b bytes.Buffer
	if err := tmpl.Execute(&b, map[string]any{"Location": "<script>alert(1)</script>"}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "<script>") || !strings.Contains(b.String(), "&lt;script&gt;") {
		t.Fatalf("unsafe interpolation: %s", b.String())
	}
}

func TestEventCatalog(t *testing.T) {
	c, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if !c.Supports("seoul", "ko") || c.Supports("berlin26", "ko") {
		t.Fatal("wrong event eligibility")
	}
	if c.Content("seoul", "ko", "Seoul, South Korea") == "Seoul, South Korea" {
		t.Fatal("event translation missing")
	}
	if c.Content("seoul", "ko", "Updated event copy") != "Updated event copy" {
		t.Fatal("changed source not preserved")
	}
	if len(c.Languages("berlin26", "en")) != 0 {
		t.Fatal("untranslated event has switch")
	}
	for input, want := range map[string]string{"/seoul": "/ko/seoul", "/seoul#tickets": "/ko/seoul#tickets", "/seoul?ref=abc#tickets": "/ko/seoul?ref=abc#tickets", "/seoul/agenda": "/seoul/agenda", "/seoul/hackathon": "/seoul/hackathon", "/tix/123/checkout": "/tix/123/checkout", "/berlin26": "/berlin26", "https://example.com/seoul": "https://example.com/seoul"} {
		if got := c.URL("ko", input); got != want {
			t.Errorf("URL(%q) = %q; want %q", input, got, want)
		}
	}
}
