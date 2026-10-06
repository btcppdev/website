package types

import "testing"

func TestRegistrationLocaleDefaults(t *testing.T) {
	for _, locale := range []string{"", "en", "fr", "KO", " ko "} {
		if RegistrationLocale(locale) != "en" {
			t.Fatalf("unknown %q was not English", locale)
		}
	}
	if RegistrationLocale("ko") != "ko" {
		t.Fatal("explicit Korean lost")
	}
}
func TestReminderLocaleRequiresUnambiguousRegistrations(t *testing.T) {
	for _, tc := range []struct {
		name string
		regs []*Registration
		want string
	}{
		{"missing", nil, "en"}, {"nil", []*Registration{nil}, "en"},
		{"partially missing", []*Registration{{Locale: "ko"}, nil}, "en"},
		{"legacy", []*Registration{{}}, "en"},
		{"Korean", []*Registration{{Locale: "ko"}, {Locale: "ko"}}, "ko"},
		{"mixed", []*Registration{{Locale: "ko"}, {Locale: "en"}}, "en"},
		{"unknown", []*Registration{{Locale: "ko"}, {}}, "en"},
		{"revoked only", []*Registration{{Locale: "ko", Revoked: true}}, "en"},
		{"revoked ignored", []*Registration{{Locale: "ko"}, {Locale: "en", Revoked: true}}, "ko"},
	} {
		if got := ReminderLocale(tc.regs); got != tc.want {
			t.Errorf("%s = %s want %s", tc.name, got, tc.want)
		}
	}
}
