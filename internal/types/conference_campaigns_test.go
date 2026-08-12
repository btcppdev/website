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

func TestConferenceEmailCampaignsDefaultOnAndAllowOptOut(t *testing.T) {
	conf := &Conf{}
	if !conf.UsesConferenceEmailCampaigns() {
		t.Fatal("nil campaign setting should preserve default-on behavior")
	}
	disabled := false
	conf.ConferenceEmailCampaignsEnabled = &disabled
	if conf.UsesConferenceEmailCampaigns() {
		t.Fatal("explicit false campaign setting should disable automation")
	}
	enabled := true
	conf.ConferenceEmailCampaignsEnabled = &enabled
	if !conf.UsesConferenceEmailCampaigns() {
		t.Fatal("explicit true campaign setting should enable automation")
	}
}
