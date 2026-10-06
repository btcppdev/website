package emails

import (
	"btcpp-web/internal/types"
	"strings"
	"testing"
)

func TestRegistrationEmailEnglishFallback(t *testing.T) {
	original := []byte("Organizer's edited announcement\n\nKeep this exact copy.")
	for _, tc := range []struct{ locale, event string }{{"", "seoul"}, {"en", "seoul"}, {"fr", "seoul"}, {"ko", "berlin26"}} {
		body, err := registrationEmailBody(tc.locale, "ticket", &types.Conf{Tag: tc.event}, "https://example.test/dashboard", "https://btcpp.dev", "", original)
		if err != nil || string(body) != string(original) {
			t.Fatalf("%+v: %s %v", tc, body, err)
		}
	}
}
func TestRegistrationEmailKoreanFirstPreservesOriginal(t *testing.T) {
	conf := &types.Conf{Tag: "seoul", Desc: "bitcoin++ Seoul, privacy edition", DateDesc: "Nov 5 - 6, 2026", Location: "Seoul, South Korea", Venue: "Masil"}
	english := []byte("Custom organizer update: meet at the north entrance.\n\n[Dashboard](https://example.test/dashboard?token=test&next=event)")
	for _, kind := range []string{"ticket", "reminder"} {
		body, err := registrationEmailBody("ko", kind, conf, "https://example.test/dashboard?token=test&next=event", "https://btcpp.dev", "10:00 KST", english)
		if err != nil {
			t.Fatal(err)
		}
		text := string(body)
		if !strings.HasPrefix(text, "## 한국어") || !strings.HasSuffix(text, string(english)) || strings.Count(text, string(english)) != 1 {
			t.Fatalf("language order or original changed: %s", text)
		}
		for _, want := range []string{"2026년 11월 5–6일", "대한민국 서울", "https://btcpp.dev/ko/seoul", "10:00 KST", "## English", "token=test&next=event"} {
			if !strings.Contains(text, want) {
				t.Errorf("missing %s", want)
			}
		}
		html := string(mdToHTML(body))
		if !strings.Contains(html, "한국어") || !strings.Contains(html, "/ko/seoul") {
			t.Fatal("HTML rendering lost localized section")
		}
	}
}

func TestRegistrationEmailPreservesNewsletterFrontmatter(t *testing.T) {
	original := []byte("---\ntemplate: announce\npalette: ocean\nissue: EVENT DETAILS\n---\n\nLatest organizer message")
	body, err := registrationEmailBody("ko", "reminder", &types.Conf{Tag: "seoul"}, "https://example.test/dashboard", "https://btcpp.dev", "", original)
	if err != nil {
		t.Fatal(err)
	}
	cfg, content := parseTemplatedNewsletterFrontmatter(string(body))
	if cfg.Template != "announce" || cfg.Palette != "ocean" || cfg.Issue != "EVENT DETAILS" {
		t.Fatalf("email style metadata lost: %+v", cfg)
	}
	if !strings.Contains(content, "한국어") || !strings.HasSuffix(content, "Latest organizer message") {
		t.Fatalf("email content lost: %s", content)
	}
}
