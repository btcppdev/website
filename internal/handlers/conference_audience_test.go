package handlers

import (
	"bytes"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestConferenceAudienceLabelsAreEventScoped(t *testing.T) {
	t.Chdir(findRepoRoot(t))
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	conf := &types.Conf{Tag: "seoul", Desc: "Seoul"}
	for _, audience := range []string{"speakers", "attendees", "volunteers"} {
		campaign := &types.ConferenceEmailCampaign{ID: "campaign", Audience: audience}
		occurrence := &types.ConferenceEmailOccurrence{ID: "occurrence", ConferenceTag: conf.Tag, Audience: audience}
		for _, tc := range []struct {
			template string
			data     any
		}{
			{"admin/conference_missives.tmpl", &ConferenceMissivesPage{Conf: conf, View: conferenceMissiveViewTemplates, Campaigns: []*types.ConferenceEmailCampaign{campaign}}},
			{"admin/templated_missives.tmpl", &TemplatedMissivesPage{Conf: conf, IsCampaign: true, Campaign: campaign}},
			{"admin/templated_missives.tmpl", &TemplatedMissivesPage{Conf: conf, IsOccurrence: true, Occurrence: occurrence}},
		} {
			var out bytes.Buffer
			if err := ctx.TemplateCache.ExecuteTemplate(&out, tc.template, tc.data); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(out.String(), types.ConferenceAudienceList(conf.Tag, audience)) {
				t.Fatalf("%s missing scoped %s audience", tc.template, audience)
			}
		}
	}
}
