package types

import "testing"

func TestConferenceCampaignSubjectAddsPrefixOnce(t *testing.T) {
	want := "✨ bitcoin++ {{ .Conf.Tag }} {{ .Conf.Emoji }}: Event details"
	if got := ConferenceCampaignSubject("Event details"); got != want {
		t.Fatalf("ConferenceCampaignSubject = %q, want %q", got, want)
	}
	if got := ConferenceCampaignSubject(want); got != want {
		t.Fatalf("ConferenceCampaignSubject duplicated prefix: %q", got)
	}
}
