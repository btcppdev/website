package handlers

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"html/template"
	"log"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// Opt-in, read-only product UI fixtures for the local annual-report concept.
// These render the real templates with sample data and never mount mutations.
func mountAnnualSoftwarePreview(t *testing.T, mux *http.ServeMux) {
	t.Helper()
	mux.Handle("/software-preview/", http.StripPrefix("/software-preview/", http.FileServer(http.Dir("../../docs/annual-software-preview"))))
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	if err = os.Chdir("../.."); err != nil {
		t.Fatal(err)
	}
	app := &config.AppContext{Env: &types.EnvConfig{}, Err: log.New(os.Stderr, "", 0)}
	err = loadTemplates(app)
	if restoreErr := os.Chdir(wd); restoreErr != nil {
		t.Fatal(restoreErr)
	}
	if err != nil {
		t.Fatal(err)
	}
	// Capture the product workspace without unrelated navigation or live widgets.
	if _, err = app.TemplateCache.Parse(`{{define "mainnav"}}<!-- Workspace-only local capture. -->{{end}}`); err != nil {
		t.Fatal(err)
	}
	app.TemplateCache.Funcs(template.FuncMap{"speakerPhoto": func(string) string { return "/static/img/ninja_default.jpeg" }})
	date := time.Date(2026, time.November, 5, 0, 0, 0, 0, time.UTC)
	conf := &types.Conf{Tag: "example26", Desc: "Example event", StartDate: date, EndDate: date.AddDate(0, 0, 1)}
	proposal := func(id, title, kind, status string, start, duration int) *ScheduleProposal {
		return &ScheduleProposal{Proposal: &types.Proposal{ID: id, Title: title, TalkType: kind, Status: status}, ConfTalkID: id, DesiredMin: duration, ActualMin: duration, StartMin: start, TopPx: (start - 540) * 2, HeightPx: duration * 2}
	}
	schedule := &AdminSchedulePage{Conf: conf, PxPerMin: 2, SnapMin: 5, HackathonSetupURL: "#", HackathonButton: "Hackathon schedule", Year: 2026,
		Unscheduled: []*ScheduleProposal{proposal("four", "Building with ecash", "workshop", "Accepted", 0, 45), proposal("five", "The next generation of wallets", "talk", "Accepted", 0, 30)},
		Days: []*ScheduleDay{{Idx: 1, Date: date, Venues: []string{"Main stage", "Workshop room"}, OpensMin: 540, ClosesMin: 780, HeightPx: 480,
			Placed: map[string][]*ScheduleProposal{
				"Main stage":    {proposal("one", "Opening keynote", "keynote", "Scheduled", 560, 30), proposal("two", "Privacy in practice", "panel", "Scheduled", 600, 45), proposal("three", "Lightning payments", "talk", "Scheduled", 660, 30)},
				"Workshop room": {proposal("six", "Build your first Bitcoin tool", "workshop", "Scheduled", 570, 75), proposal("seven", "Hackathon project clinic", "workshop", "Scheduled", 675, 60)},
			}}},
	}
	speakers := []*types.Speaker{{ID: "sample-a", Name: "Alex", Email: "alex@example.test", Photo: "sample"}, {ID: "sample-b", Name: "Sam", Email: "sam@example.test", Photo: "sample"}}
	list := &ProposalAdminPage{Conf: conf, EmailCompose: &EmailComposeData{}, Rows: []*ProposalAdminRow{
		{Proposal: schedule.Days[0].Placed["Main stage"][0].Proposal, Speakers: speakers[:1]},
		{Proposal: schedule.Days[0].Placed["Main stage"][1].Proposal, Speakers: speakers},
		{Proposal: schedule.Unscheduled[0].Proposal, Speakers: speakers[1:]},
	}}
	for path, item := range map[string]struct {
		name string
		data any
	}{
		"/software-ui/schedule": {"admin/schedule.tmpl", schedule},
		"/software-ui/people":   {"talks/applicants.tmpl", list},
	} {
		mux.HandleFunc("GET "+path, func(w http.ResponseWriter, r *http.Request) {
			if err := app.TemplateCache.ExecuteTemplate(w, item.name, item.data); err != nil {
				t.Log(err)
			}
		})
	}
	// The streaming source is optional; the example uses only sample schedules.
	if root := os.Getenv("ANNUAL_REPORT_STREAMER_TEMPLATES"); root != "" {
		tmpl, err := template.New("stream-preview").Funcs(template.FuncMap{"csrfField": func() template.HTML { return "" }, "currentUser": func() any { return nil }}).ParseFiles(filepath.Join(root, "layout.html"), filepath.Join(root, "streams.html"))
		if err != nil {
			t.Fatal(err)
		}
		var streams []map[string]any
		for i, name := range []string{"Conference main stage", "Workshop recordings", "Hackathon demos"} {
			streams = append(streams, map[string]any{"ID": i + 1, "Name": name, "Enabled": true, "Videos": []string{"opening.mp4", "session.mp4"}, "OnCalendar": "2026-11-05 10:00:00", "ScheduleType": "once", "Endpoints": []map[string]string{{"Name": "YouTube"}, {"Name": "X"}}, "Status": "scheduled", "NextTrigger": "Nov 5 · 10:00", "NextTriggerUnix": 0, "LiveURL": "#"})
		}
		mux.HandleFunc("GET /software-ui/streaming", func(w http.ResponseWriter, r *http.Request) {
			if err := tmpl.ExecuteTemplate(w, "streams.html", map[string]any{"Streams": streams}); err != nil {
				t.Log(err)
			}
		})
	}
}
