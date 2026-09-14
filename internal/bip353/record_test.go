package bip353

import "testing"

func TestRecordName(t *testing.T) {
	t.Parallel()
	got, err := RecordName("Conference-2027", "ZAP.Example.COM.")
	if err != nil {
		t.Fatal(err)
	}
	want := "conference-2027.user._bitcoin-payment.zap.example.com"
	if got != want {
		t.Fatalf("RecordName() = %q, want %q", got, want)
	}
}

func TestRecordNameRejectsUnsafeNames(t *testing.T) {
	t.Parallel()
	for _, user := range []string{"", "two words", "-leading", "trailing-", "café", "under_score"} {
		if _, err := RecordName(user, "example.com"); err == nil {
			t.Errorf("RecordName(%q) unexpectedly succeeded", user)
		}
	}
}

func TestValidateURI(t *testing.T) {
	t.Parallel()
	valid := []string{
		"bitcoin:bc1qexample?amount=1.25",
		"bitcoin:?lno=lno1example",
		"BITCOIN:?sp=sp1example",
	}
	for _, value := range valid {
		if err := validateURI(value); err != nil {
			t.Errorf("validateURI(%q): %v", value, err)
		}
	}
	invalid := []string{"", "https://example.com", "bitcoin:bad value", "bitcoin:test#fragment"}
	for _, value := range invalid {
		if err := validateURI(value); err == nil {
			t.Errorf("validateURI(%q) unexpectedly succeeded", value)
		}
	}
}
