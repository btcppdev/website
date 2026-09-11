package handlers

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

func TestBitcoinWordmarkStyleContract(t *testing.T) {
	stylesheet, err := os.ReadFile("../../static/css/site-chrome.css")
	if err != nil {
		t.Fatalf("read shared site stylesheet: %v", err)
	}
	css := string(stylesheet)
	rule := func(selector string) string {
		t.Helper()
		match := regexp.MustCompile(`(?s)` + regexp.QuoteMeta(selector) + `\s*\{([^}]*)\}`).FindStringSubmatch(css)
		if len(match) != 2 {
			t.Fatalf("missing %s wordmark rule", selector)
		}
		return match[1]
	}

	for _, selector := range []string{"btcpp-wordmark", ".font-bitcoin"} {
		declarations := rule(selector)
		for _, expected := range []string{
			`font-family: "Ubuntu-BoldItalic", Ubuntu, sans-serif`,
			"font-style: italic",
			"font-weight: 700",
			"letter-spacing: normal",
		} {
			if !strings.Contains(declarations, expected) {
				t.Errorf("%s does not enforce %q: %s", selector, expected, declarations)
			}
		}
	}

	joinedPluses := rule("btcpp-plus + btcpp-plus")
	if !strings.Contains(joinedPluses, "margin-left: -.16em") {
		t.Errorf("only the second plus should overlap the first: %s", joinedPluses)
	}
	if strings.Contains(rule("btcpp-wordmark"), "letter-spacing: -") {
		t.Error("the bitcoin letters must retain natural spacing")
	}
}
