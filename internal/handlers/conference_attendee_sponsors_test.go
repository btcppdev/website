package handlers

import (
	"bytes"
	"strings"
	"testing"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestConferenceAttendeeSponsorCards(t *testing.T) {
	t.Chdir("../..")
	ctx := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(ctx); err != nil {
		t.Fatal(err)
	}
	speaker := &types.Speaker{Name: "Mara", Company: "An employer", OrgLogo: "unverified.svg"}
	for _, name := range []string{"generic_conf_featured_speaker", "generic_conf_speaker", "generic_conf_large_speaker"} {
		t.Run(name, func(t *testing.T) {
			data := map[string]any{"Speaker": speaker, "Idx": 0, "Sponsors": []*types.Org{
				{Name: `Copper & "Co"`, LogoLight: "https://example.test/light.svg", LogoDark: "https://example.test/unused.svg"},
				{Name: "Second sponsor", LogoDark: "https://example.test/dark.svg"},
			}}
			var out bytes.Buffer
			if err := ctx.TemplateCache.ExecuteTemplate(&out, name, data); err != nil {
				t.Fatal(err)
			}
			page := out.String()
			for _, want := range []string{`class="rebrand-attendee-sponsors"`, `src="https://example.test/light.svg"`, `src="https://example.test/dark.svg"`, `rebrand-attendee-sponsor--dark-logo`, `alt="Copper &amp; &#34;Co&#34; · Event sponsor"`} {
				if !strings.Contains(page, want) {
					t.Fatalf("missing %q in %s", want, page)
				}
			}
			if strings.Contains(page, "unused.svg") {
				t.Fatal("preferred dark logo over the supplied light logo")
			}
			out.Reset()
			data["Sponsors"] = nil
			if err := ctx.TemplateCache.ExecuteTemplate(&out, name, data); err != nil {
				t.Fatal(err)
			}
			if strings.Contains(out.String(), "rebrand-attendee-sponsor") {
				t.Fatal("company name or speaker-uploaded logo was treated as a sponsor membership")
			}
		})
	}
}
