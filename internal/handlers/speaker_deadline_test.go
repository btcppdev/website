package handlers

import (
	"bytes"
	"html"
	"os"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

func TestSpeakerDeadlineAdminAndPublicTemplates(t *testing.T) {
	loc, err := time.LoadLocation("Asia/Seoul")
	if err != nil {
		t.Fatal(err)
	}
	deadline, err := parseOptionalDatetimeLocal("2026-09-26T00:00", loc)
	if err != nil {
		t.Fatal(err)
	}
	if got := deadline.UTC().Format(time.RFC3339); got != "2026-09-25T15:00:00Z" {
		t.Fatalf("wrong deadline instant: %s", got)
	}
	if blank, err := parseOptionalDatetimeLocal("", loc); err != nil || blank != nil {
		t.Fatal("blank should clear override")
	}
	if _, err := parseOptionalDatetimeLocal("2026-09-31T00:00", loc); err == nil {
		t.Fatal("invalid date accepted")
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chdir(wd) })
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	conf := &types.Conf{Tag: "seoul", Desc: "Bitcoin++ Seoul", Timezone: "Asia/Seoul", TZ: loc, StartDate: deadline.AddDate(0, 0, 20), EndDate: deadline.AddDate(0, 0, 22), SpeakerApplicationsClose: deadline}
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "admin/event_details.tmpl", &EventDetailsPage{Conf: conf, MerchUpsellSlots: make([]*types.MerchProduct, 3), SpeakerApplicationsCloseInput: datetimeLocalInput(deadline.In(conf.Loc()))}); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{`name="speaker_applications_close"`, `value="2026-09-26T00:00"`, "Asia/Seoul", "Leave blank to use 45 days", "KST (UTC+09:00)"} {
		if !strings.Contains(html.UnescapeString(out.String()), want) {
			t.Errorf("admin form omitted %q", want)
		}
	}
	out.Reset()
	if err := app.TemplateCache.ExecuteTemplate(&out, "embeds/talk_closed.tmpl", &SpeakerPage{Conf: conf, DueDate: conf.TalksDueLabel()}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(html.UnescapeString(out.String()), conf.TalksDueLabel()) || strings.Contains(html.UnescapeString(out.String()), "midnight on") {
		t.Fatal("closed page must show the effective time and timezone")
	}
}
