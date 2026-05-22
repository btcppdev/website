package emails

import (
	"bytes"
	"strings"
	"testing"
	texttemplate "text/template"

	"btcpp-web/internal/config"
	"btcpp-web/internal/mtypes"
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
