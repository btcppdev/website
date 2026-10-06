package types

// RegistrationLocale deliberately treats absent, invalid and legacy values as English.
func RegistrationLocale(locale string) string {
	if locale == "ko" {
		return "ko"
	}
	return "en"
}

// ReminderLocale requires every associated active registration to explicitly
// agree on Korean. Missing or ambiguous registration context defaults to English.
func ReminderLocale(registrations []*Registration) string {
	found := false
	for _, r := range registrations {
		if r == nil {
			return "en"
		}
		if r.Revoked {
			continue
		}
		found = true
		if RegistrationLocale(r.Locale) != "ko" {
			return "en"
		}
	}
	if found {
		return "ko"
	}
	return "en"
}
