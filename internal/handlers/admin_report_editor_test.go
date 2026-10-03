package handlers

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestReportMarkdownPreview(t *testing.T) {
	text := "## Nairobi\n\n**Builders** and *ideas*.\n\n- One\n- Two\n\n[Watch](https://btcpp.dev/nairobi)\n\n<script>alert(1)</script>\n\n[Bad](javascript:alert(1))"
	r := httptest.NewRequest(http.MethodPost, "/admin/reports/preview", strings.NewReader(url.Values{"text": {text}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w := httptest.NewRecorder()
	renderReportMarkdownPreview(w, r)
	if w.Code != 200 {
		t.Fatal(w.Code)
	}
	got := w.Body.String()
	for _, want := range []string{"<h2>Nairobi</h2>", "<strong>Builders</strong>", "<em>ideas</em>", "<ul>", `href="https://btcpp.dev/nairobi"`} {
		if !strings.Contains(got, want) {
			t.Fatalf("missing %s in %s", want, got)
		}
	}
	for _, bad := range []string{"<script", `href="javascript:`, "onclick="} {
		if strings.Contains(got, bad) {
			t.Fatalf("unsafe preview: %s", got)
		}
	}
	if got != string((reportSection{Text: text}).Markdown()) {
		t.Fatal("preview differs from saved report renderer")
	}
	r = httptest.NewRequest(http.MethodPost, "/admin/reports/preview", strings.NewReader(url.Values{"text": {strings.Repeat("a", 12001)}}.Encode()))
	r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	w = httptest.NewRecorder()
	renderReportMarkdownPreview(w, r)
	if w.Code != 400 {
		t.Fatal("oversized section accepted")
	}
}
