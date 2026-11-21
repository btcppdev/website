package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"bytes"
	"strings"
	"testing"
)

func TestConferenceCommitteeCards(t *testing.T) {
	t.Chdir("../..")
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	page := &ConfPage{SteeringCommittee: []*types.ConferenceCommitteeMember{
		{Speaker: &types.Speaker{Name: "Ada", Photo: "portrait.avif"}, Track: `Privacy <script>`, ProfileURL: "/whois/ada", Organizations: []*types.Org{
			{Name: `Independent & "Lab"`, LogoLight: "https://example.test/logo.svg", LogoDark: "https://example.test/unused.svg"},
			{Name: "Dark logo", LogoDark: "https://example.test/dark.svg"},
			{Name: "Organization without logo"},
		}},
		{Speaker: &types.Speaker{Name: "Sam", Company: "Profile affiliation", OrgLogo: "profile.svg"}},
	}}
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "generic_conf_committee", page); err != nil {
		t.Fatal(err)
	}
	body := out.String()
	for _, want := range []string{`id="steering-committee"`, "Steering committee", `href="/whois/ada"`, "Privacy &lt;script&gt;", `alt="Independent &amp; &#34;Lab&#34;"`, "Organization without logo", "https://example.test/dark.svg", "Profile affiliation", "sponsors/profile.svg"} {
		if !strings.Contains(body, want) {
			t.Fatalf("missing %q: %s", want, body)
		}
	}
	if strings.Contains(body, "Event sponsor") || strings.Contains(body, "unused.svg") || strings.Count(body, "rebrand-committee-card__track") != 1 {
		t.Fatal("incorrect committee metadata", body)
	}
	out.Reset()
	page.SteeringCommittee = nil
	if err := app.TemplateCache.ExecuteTemplate(&out, "generic_conf_committee", page); err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(out.String()) != "" {
		t.Fatal("empty committee rendered a section")
	}
}
