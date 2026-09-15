package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"bytes"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"
)

func TestAdminProposalSpeakerDetails(t *testing.T) {
	wd, _ := os.Getwd()
	if err := os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	defer os.Chdir(wd)
	app := &config.AppContext{Env: &types.EnvConfig{}}
	if err := loadTemplates(app); err != nil {
		t.Fatal(err)
	}
	conf := &types.Conf{Tag: "preview26", Desc: "Proposal preview", StartDate: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC), EndDate: time.Date(2026, 10, 3, 0, 0, 0, 0, time.UTC)}
	proposal := &types.Proposal{ID: "proposal-one", Title: "Payment security panel", Status: "Waitlisted", TalkType: "panel"}
	speakers := []*types.SpeakerConf{
		{ID: "speaker-one", Speaker: &types.Speaker{Name: "First panelist", Email: "first@example.test"}, Availability: []string{"10/01/2026"}, ComingFrom: "Berlin", Company: "First company", RecordOK: "NoRecord", Visa: "No"},
		{ID: "speaker-two", Speaker: &types.Speaker{Name: "Second panelist", Email: "second@example.test"}, Availability: []string{"10/02/2026", "10/03/2026"}, ComingFrom: "Paris", Company: "Second <company>", RecordOK: "AudioOnly", Visa: "IDK", DinnerRSVP: true, Sponsor: true},
	}
	edit := &AdminEditProposalPage{Conf: conf, Proposal: proposal, Speakers: speakers, TalkTypes: adminTalkTypes("panel"), Durations: adminTalkDurations, ReturnURL: "/preview26/admin/applicants"}
	review := &ReviewProposalPage{Conf: conf, Current: proposal, Speakers: speakers}
	for name, page := range map[string]any{"admin/edit_proposal.tmpl": edit, "admin/review_proposal.tmpl": review} {
		var b bytes.Buffer
		if err := app.TemplateCache.ExecuteTemplate(&b, name, page); err != nil {
			t.Fatal(err)
		}
		for _, want := range []string{"First panelist", "Second panelist", "Thu. Oct 1, 2026", "Fri. Oct 2, 2026", "Sat. Oct 3, 2026", "Do not record", "Audio only", "Second &lt;company&gt;", "speakerconfs/speaker-one/edit", "speakerconfs/speaker-two/edit"} {
			if !strings.Contains(b.String(), want) {
				t.Errorf("%s missing %q", name, want)
			}
		}
	}
	if os.Getenv("APPLICANT_PREVIEW") != "1" {
		return
	}
	list := &ProposalAdminPage{Conf: conf, EmailCompose: &EmailComposeData{}, Rows: []*ProposalAdminRow{
		{Proposal: proposal, Speakers: []*types.Speaker{speakers[0].Speaker, speakers[1].Speaker}},
		{Proposal: &types.Proposal{ID: "proposal-two", Title: "Confirmed workshop", Status: "Accepted", TalkType: "workshop"}},
	}}
	var listHTML bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&listHTML, "talks/applicants.tmpl", list); err != nil {
		t.Fatal(err)
	}
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("static"))))
	mux.HandleFunc("/preview26/admin/applicants", func(w http.ResponseWriter, r *http.Request) {
		app.TemplateCache.ExecuteTemplate(w, "talks/applicants.tmpl", list)
	})
	mux.HandleFunc("/other26/admin/applicants", func(w http.ResponseWriter, r *http.Request) {
		app.TemplateCache.ExecuteTemplate(w, "talks/applicants.tmpl", list)
	})
	mux.HandleFunc("/preview26/admin/proposal/proposal-one/edit", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			http.Redirect(w, r, edit.ReturnURL+"?flash=Preview+saved", 303)
			return
		}
		app.TemplateCache.ExecuteTemplate(w, "admin/edit_proposal.tmpl", edit)
	})
	t.Log("Local fixture: http://127.0.0.1:8113/preview26/admin/applicants")
	t.Fatal(http.ListenAndServe("127.0.0.1:8113", mux))
}
