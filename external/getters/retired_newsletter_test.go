package getters

import "testing"

func TestRetiredCentralNewsletterTargets(t *testing.T) {
	for _, names := range [][]string{{"insider"}, {"newsletter", "!insider"}, {" ! insider "}} {
		if err := ValidateCentralNewsletterTargets(names); err == nil {
			t.Fatalf("accepted retired target %q", names)
		}
	}
	for _, names := range [][]string{nil, {"newsletter"}, {"test-news", "!newsletter"}, {"berlin"}} {
		if err := ValidateCentralNewsletterTargets(names); err != nil {
			t.Fatalf("rejected valid targets %q: %v", names, err)
		}
	}
}
