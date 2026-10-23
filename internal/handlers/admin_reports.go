package handlers

import (
	"btcpp-web/internal/config"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
)

type reportSection struct{ Key, Title, Text string }
type reportEvent struct {
	LeadImage                                            string
	VenueImages                                          []string
	ID, Tag, Title, Location, Period, Timezone, DateDesc string
	Start, End                                           *time.Time
}
type reportPerson struct {
	ID, Name, Photo, Company, Role, Note, ComingFrom string
	Highlight                                        bool
}
type reportSession struct {
	ID, Title, Kind, Speakers, YouTube, X, Note, WatchURL, Venue, SocialCard string
	Start, End                                                               *time.Time
	Delivered, Core                                                          bool
	Views                                                                    *int64
}
type reportWinner struct {
	Rank                                                         int
	Portraits                                                    []reportPerson
	ID, Title, Description, Awards, Members, Image, GitHub, Demo string
	Sats                                                         int64
	AwardedSats                                                  int64
	Significance                                                 string
}
type reportDay struct {
	Date  string
	Count *int64
}
type reportMetric struct{ Label, Value, Note string }
type sponsorReportPage struct {
	Sponsors                                                                               []reportSponsor
	DemoApplications                                                                       bool
	ProposalStatuses                                                                       []reportBar
	PostLibrary                                                                            reportPostLibrary
	PublicEvidence                                                                         []reportEvidence
	EvidenceDate                                                                           string
	PublicProgramSource, PublicVideoSource                                                 string
	Preview, ProgramReviewed, Synthetic, DemoAttendance                                    bool
	Event                                                                                  reportEvent
	Options                                                                                []reportEvent
	Scope, CSRF, Generated, Message                                                        string
	Revision                                                                               int64
	Sections                                                                               []reportSection
	Speakers, Judges                                                                       []reportPerson
	Sessions, Popular, Panels                                                              []reportSession
	Winners, Projects                                                                      []reportWinner
	Days                                                                                   []reportDay
	TeamSizes                                                                              []reportBar
	Metrics                                                                                []reportMetric
	Saved                                                                                  map[string]string
	Registrations, Attendees, Teams, Participants, Challenges, Sats, Volunteers, Proposals int64
	Talks, Workshops, PanelCount, AttendeeDays                                             int64
	ReportedDays                                                                           int
	Applications                                                                           *int64
}

func eventReportSections(event reportEvent) []reportSection {
	note := "To our partners,\n\nThank you for helping make this bitcoin++ gathering possible. This report brings together the people, technical conversations, and projects at the heart of the event.\n\n[Add what made this event special: a conversation that stayed with you, a builder’s breakthrough, or something you were proud to help make happen.]\n\nThank you for supporting the people building Bitcoin’s future.\n\nnifty"
	if strings.Contains(strings.ToLower(event.Location+" "+event.Tag), "nairobi") {
		note = "To our partners,\n\nBringing bitcoin++ to Africa is one of the things I’m proudest of this year. Nairobi deserves a report of its own: a chance to look closely at the people who came together, the ideas they exchanged, and the projects they built.\n\nSome of what I value most about bitcoin++ happens in conversation. I’m proud of the conversations we help spark around the globe, and excited to make room for more people to take part in shaping Bitcoin’s technical future.\n\nThe pages that follow bring this gathering into focus: speakers and panel discussions, talks and workshops, hackathon teams and winning projects, and the judges and volunteers who helped make it possible.\n\nThank you for supporting bitcoin++ and for being part of this step into Africa.\n\nnifty"
	}
	sections := []reportSection{
		{"note", "A note from nifty", note},
		{"program", "Ideas worth taking home", "[Introduce this event’s technical theme. Explain what the highlighted speakers brought to it and what sponsors should understand about the work discussed.]"},
		{"panels", "The conversations that mattered", "[For each featured panel, describe the central question, the competing perspectives, and what attendees could take away.]"},
		{"hackathon", "Built here. Worth watching.", "[Introduce the hackathon: what teams set out to solve, what impressed the judges, and which projects deserve follow-up.]"},
		{"judges", "The people behind the decisions", "[Highlight the experience the judges brought and their contribution. Add individual notes beside their profiles in the editor.]"},
		{"closing", "What this gathering made possible", "[Close with an event-specific win, a lesson, and the next step for participants or projects. Recognize local collaborators and volunteers.]"},
	}
	if strings.Contains(strings.ToLower(event.Location+" "+event.Tag), "nairobi") {
		sections[1].Text = "Nairobi’s Open Source Edition put contributing in public at the center of the program. The selected speakers connect Bitcoin protocol development, developer education, and tools for using bitcoin in everyday life. Together, these themes offer sponsors a view of the different kinds of work that sustain the ecosystem.\n\nSpeaker highlights are editorial selections from the published program."
		sections[2].Text = "The selected panels frame questions about building open-source organizations, sustaining Bitcoin development, and growing developer communities. Add the strongest takeaways after reviewing the discussions.\n\nProgram source: https://btcpp.dev/nairobi"
		sections[3].Text = "Nairobi’s three overall winners explored cross-border payments, a first step into open-source contribution, and permissionless ridesharing. These prototypes show the range of problems builders brought to the event; their longer-term impact will depend on continued development and use.\n\nResults source: https://btcpp.dev/nairobi/hackathon"
		sections[4].Text = "The public hackathon page lists the judges pictured below. Their names and affiliations are reproduced from the event’s profiles; individual contributions and approved quotes can be added in the editor.\n\nJudge roster: https://btcpp.dev/nairobi/hackathon"
		sections[5].Text = "Nairobi brought the open-source conversation to Africa, with a program spanning protocol work, contributor communities, and practical applications. The winning projects give sponsors concrete work to explore beyond the event.\n\nOur proposed follow-up: ask teams what they shipped next, identify where contributors need mentorship, and make recordings and workshop materials easier to find. These are priorities for the next chapter, not outcomes we have measured yet.\n\nThank you to our speakers, judges, volunteers, and partners for helping make this gathering possible."
	}
	sections = append(sections,
		reportSection{"speakers_intro", "Speaker mosaic introduction", "The published program’s speakers, panelists and workshop presenters. Names and affiliations as listed; this roster is not a check-in record."},
		reportSection{"timeline_intro", "Program timeline introduction", "The published schedule, in time order. Matching times across stages show parallel sessions. Colors distinguish talks/keynotes, workshops and panels; the schedule alone does not confirm delivery."},
		reportSection{"projects_intro", "Project gallery introduction", "A gallery of the event’s prototypes, using the builders’ published descriptions and artwork. Explore each entry to see its team and work. These are submissions, not claims of production readiness."},
		reportSection{"geography_intro", "Speaker map introduction", "Where speakers said they would be traveling from, using their event-specific speaker forms. Both hemispheres are shown so every mapped origin can appear."},
		reportSection{"reach_intro", "Online reach introduction", "How far did the event’s talks, workshops and conversations travel? These figures track the event’s published videos and posts, with each platform reported separately."},
	)
	return sections
}

func AdminSponsorReport(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	if requireGlobalAdmin(w, r, app) == nil {
		return
	}
	serveSponsorReport(w, r, app)
}

func serveSponsorReport(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Minute)
	defer cancel()
	if app.DB == nil {
		http.Error(w, "Report database unavailable", 503)
		return
	}
	page := sponsorReportPage{Generated: time.Now().UTC().Format("2 January 2006, 15:04 UTC"), Saved: map[string]string{}}
	event := strings.TrimSpace(r.URL.Query().Get("event"))
	if len(event) > 100 {
		http.Error(w, "Invalid event", 400)
		return
	}
	rows, err := app.DB.Query(ctx, `SELECT id::text,tag,coalesce(nullif(description,''),tag),location,timezone,date_desc,start_date,end_date FROM conferences WHERE publication_status='published' ORDER BY start_date DESC NULLS LAST,tag`)
	if err != nil {
		http.Error(w, "Unable to load events", 503)
		return
	}
	for rows.Next() {
		var e reportEvent
		if err = rows.Scan(&e.ID, &e.Tag, &e.Title, &e.Location, &e.Timezone, &e.DateDesc, &e.Start, &e.End); err != nil {
			rows.Close()
			http.Error(w, "Unable to read events", 500)
			return
		}
		e.Period = reportEventPeriod(e)
		page.Options = append(page.Options, e)
		if e.Tag == event {
			page.Event = e
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		http.Error(w, "Unable to read events", 500)
		return
	}
	if event == "" {
		if r.Method == http.MethodPost {
			http.Error(w, "Choose an event before saving", 400)
			return
		}
		renderSponsorReport(w, app, page)
		return
	}
	if page.Event.ID == "" {
		http.Error(w, "Published event not found", 404)
		return
	}
	if err = loadReportSponsors(ctx, app, &page); err != nil {
		http.Error(w, "Unable to load event sponsors", 503)
		return
	}
	// Preserve existing annual drafts under their original keys.
	page.Scope = "event-v2/" + page.Event.ID
	page.Sections = eventReportSections(page.Event)
	if err = loadEventReport(ctx, app, &page); err != nil {
		app.Err.Printf("event report %s: %s", event, err)
		http.Error(w, "Unable to load event report", 503)
		return
	}
	var content []byte
	err = app.DB.QueryRow(ctx, `SELECT content,revision FROM sponsor_report_drafts WHERE scope=$1`, page.Scope).Scan(&content, &page.Revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Unable to load draft; check migrations", 503)
		return
	}
	if len(content) > 0 && json.Unmarshal(content, &page.Saved) != nil {
		http.Error(w, "Unable to read draft", 500)
		return
	}
	if page.Saved == nil {
		page.Saved = map[string]string{}
	}
	if len(content) == 0 && strings.Contains(strings.ToLower(page.Event.Tag+" "+page.Event.Location), "nairobi") {
		if err := applyNairobiReportDefaults(&page); err != nil {
			http.Error(w, "Unable to load Nairobi report defaults", 500)
			return
		}
		for _, p := range page.Speakers {
			switch p.Name {
			case "Fabian Jahr", "Naiyoma", "Femi Longe", "Sabina Waithira Gitau", "Rita Anene", "Sy⚡":
				page.Saved["speaker_"+p.ID] = "yes"
			}
		}
		for _, s := range page.Sessions {
			if s.Kind == "panel" {
				page.Saved["panel_"+s.ID] = "yes"
			}
		}
	}
	page.PostLibrary, err = loadReportPosts(ctx, app.DB, reportPostYear(page.Event))
	if err != nil {
		http.Error(w, "Unable to load X post analytics", 503)
		return
	}
	page.CSRF, err = ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare editor", 500)
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid or oversized form", 400)
			return
		}
		if !secureTokenEqual(page.CSRF, r.PostForm.Get("csrf")) {
			http.Error(w, "Invalid form token", 403)
			return
		}
		revision, e := strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
		if e != nil || revision != page.Revision {
			http.Error(w, "Another editor changed this draft. Reload before saving.", 409)
			return
		}
		saved, e := eventReportForm(r.PostForm, &page, time.Now())
		if e != nil {
			http.Error(w, e.Error(), 400)
			return
		}
		if r.PostForm.Get("action") == "refresh_media" {
			refreshReportMedia(ctx, app, &page, saved)
		}
		if r.PostForm.Get("action") == "refresh_x_posts" {
			if err := refreshReportPosts(ctx, app, reportPostYear(page.Event)); err != nil {
				saved["x_posts_status"] = "X post refresh unavailable; previous snapshot retained. Check the session and retry."
			} else {
				saved["x_posts_status"] = "X post analytics refreshed. Review the suggested event matches below."
			}
		}
		if e = saveEventReportDraft(ctx, app, page.Scope, revision, saved); e != nil {
			if errors.Is(e, errReportConflict) {
				http.Error(w, e.Error(), 409)
			} else {
				http.Error(w, "Unable to save draft", 500)
			}
			return
		}
		http.Redirect(w, r, "/"+url.PathEscape(event)+"/admin/report?saved=1", http.StatusSeeOther)
		return
	}
	hydrateEventReport(&page)
	if r.URL.Query().Get("saved") == "1" {
		page.Message = "Event draft saved. Print / Save as PDF preserves this edition. " + page.Saved["media_status"] + " " + page.Saved["x_posts_status"]
	}
	renderSponsorReport(w, app, page)
}

var errReportConflict = errors.New("another editor changed this draft; reload before saving")

func saveEventReportDraft(ctx context.Context, app *config.AppContext, scope string, revision int64, saved map[string]string) error {
	content, err := json.Marshal(saved)
	if err != nil {
		return err
	}
	result, err := app.DB.Exec(ctx, `INSERT INTO sponsor_report_drafts(scope,content) SELECT $1,$2::jsonb WHERE $3::bigint=0 ON CONFLICT(scope) DO NOTHING`, scope, string(content), revision)
	if err == nil && revision > 0 {
		result, err = app.DB.Exec(ctx, `UPDATE sponsor_report_drafts SET content=$2::jsonb,revision=revision+1,updated_at=now() WHERE scope=$1 AND revision=$3`, scope, string(content), revision)
	}
	if err != nil {
		return err
	}
	if result.RowsAffected() != 1 {
		return errReportConflict
	}
	return nil
}
func renderSponsorReport(w http.ResponseWriter, app *config.AppContext, page sponsorReportPage) {
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "admin/reports.tmpl", page); err != nil {
		app.Err.Printf("event report template: %s", err)
		http.Error(w, "Unable to render report", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(out.Bytes())
}
func reportEventLocation(e reportEvent) *time.Location {
	if loc, err := time.LoadLocation(e.Timezone); err == nil {
		return loc
	}
	return time.UTC
}
func reportEventPeriod(e reportEvent) string {
	if strings.TrimSpace(e.DateDesc) != "" {
		return e.DateDesc
	}
	if e.Start == nil {
		return "Event dates to be confirmed"
	}
	loc := reportEventLocation(e)
	start := e.Start.In(loc).Format("2 January 2006")
	if e.End == nil || e.Start.In(loc).Format("2006-01-02") == e.End.In(loc).Format("2006-01-02") {
		return start
	}
	return start + " – " + e.End.In(loc).Format("2 January 2006")
}
func eventReportDays(e reportEvent) []reportDay {
	if e.Start == nil || e.End == nil || e.End.Before(*e.Start) {
		return nil
	}
	loc := reportEventLocation(e)
	start, end := e.Start.In(loc), e.End.In(loc)
	date := time.Date(start.Year(), start.Month(), start.Day(), 0, 0, 0, 0, loc)
	var days []reportDay
	for !date.After(end) && len(days) < 31 {
		days = append(days, reportDay{Date: date.Format("2006-01-02")})
		date = date.AddDate(0, 0, 1)
	}
	return days
}
func reportOptionalCount(raw string) (*int64, error) {
	if raw == "" {
		return nil, nil
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil || n < 0 || n > 1_000_000_000_000 {
		return nil, fmt.Errorf("counts must be whole numbers between 0 and 1,000,000,000,000")
	}
	return &n, nil
}
func eventReportForm(form url.Values, page *sponsorReportPage, now time.Time) (map[string]string, error) {
	saved := map[string]string{}
	if err := saveReportPostChoices(form, saved, page); err != nil {
		return nil, err
	}
	if err := saveReportMediaFields(form, saved, page.Saved); err != nil {
		return nil, err
	}
	if err := saveReportReach(form, saved, now); err != nil {
		return nil, err
	}
	textField := func(key string) error {
		value := strings.TrimSpace(form.Get(key))
		if len(value) > 12000 {
			return fmt.Errorf("%s exceeds 12,000 bytes", key)
		}
		saved[key] = value
		return nil
	}
	if len(strings.TrimSpace(form.Get("prepared_for"))) > 200 {
		return nil, fmt.Errorf("prepared-for organization must be at most 200 bytes")
	}
	for _, s := range page.Sections {
		if err := textField(s.Key); err != nil {
			return nil, err
		}
	}
	for _, key := range []string{"prepared_for", "attendance_source", "applications_source", "analytics_source", "analytics_period", "analytics_metric", "analytics_date"} {
		if err := textField(key); err != nil {
			return nil, err
		}
	}
	if err := saveReportRecipient(form, page, saved); err != nil {
		return nil, err
	}
	countField := func(key string) (*int64, error) {
		value := strings.TrimSpace(form.Get(key))
		n, err := reportOptionalCount(value)
		saved[key] = value
		return n, err
	}
	_, err := countField("applications")
	if err != nil {
		return nil, err
	}
	hasAttendance := false
	for _, d := range page.Days {
		n, err := countField("day_" + d.Date)
		if err != nil {
			return nil, err
		}
		if n != nil {
			hasAttendance = true
		}
	}
	if hasAttendance && saved["attendance_source"] == "" {
		return nil, fmt.Errorf("add a source for daily attendance")
	}
	if form.Get("program_reviewed") == "yes" {
		saved["program_reviewed"] = "yes"
	}
	hasViews := false
	for _, s := range page.Sessions {
		if form.Get("delivered_"+s.ID) == "yes" {
			if s.End == nil || s.End.After(now) {
				return nil, fmt.Errorf("cannot confirm a session that has not ended: %s", s.Title)
			}
			saved["delivered_"+s.ID] = "yes"
		}
		if s.Kind == "panel" && form.Get("panel_"+s.ID) == "yes" {
			saved["panel_"+s.ID] = "yes"
		}
		if err := textField("session_note_" + s.ID); err != nil {
			return nil, err
		}
		n, err := countField("views_" + s.ID)
		if err != nil {
			return nil, err
		}
		if n != nil {
			hasViews = true
		}
	}
	if hasViews {
		if saved["analytics_metric"] != "YouTube views" && saved["analytics_metric"] != "X video views" {
			return nil, fmt.Errorf("choose one comparable view metric for the talk ranking")
		}
		if saved["analytics_source"] == "" || saved["analytics_period"] == "" {
			return nil, fmt.Errorf("add the source and reporting period for talk views")
		}
		date, err := time.Parse("2006-01-02", saved["analytics_date"])
		if err != nil || date.After(now.UTC()) {
			return nil, fmt.Errorf("add a valid analytics export date, not in the future")
		}
	}
	for _, p := range page.Speakers {
		if err := saveReportSpeakerLocation(form, saved, p.ID); err != nil {
			return nil, err
		}
		if form.Get("speaker_"+p.ID) == "yes" {
			saved["speaker_"+p.ID] = "yes"
		}
	}
	for _, p := range page.Judges {
		if err := textField("judge_note_" + p.ID); err != nil {
			return nil, err
		}
	}
	return saved, nil
}
func hydrateEventReport(page *sponsorReportPage) {
	if page.Event.LeadImage == "" {
		page.Event.LeadImage = confImagePath(page.Event.Tag, "leading")
	}
	if len(page.Event.VenueImages) == 0 {
		page.Event.VenueImages = confVenueImages(page.Event.Tag)
	}
	for i := range page.Sections {
		if value, ok := page.Saved[page.Sections[i].Key]; ok {
			page.Sections[i].Text = value
		}
	}
	if len(page.Days) == 0 {
		page.Days = eventReportDays(page.Event)
	}
	for i := range page.Days {
		page.Days[i].Count, _ = reportOptionalCount(page.Saved["day_"+page.Days[i].Date])
		if page.Days[i].Count != nil {
			page.ReportedDays++
			page.AttendeeDays += *page.Days[i].Count
		}
	}
	page.Applications, _ = reportOptionalCount(page.Saved["applications"])
	page.ProgramReviewed = page.Saved["program_reviewed"] == "yes"
	for i := range page.Speakers {
		page.Speakers[i].Highlight = page.Saved["speaker_"+page.Speakers[i].ID] == "yes"
	}
	for i := range page.Judges {
		page.Judges[i].Note = page.Saved["judge_note_"+page.Judges[i].ID]
	}
	for i := range page.Sessions {
		s := &page.Sessions[i]
		s.Delivered = page.Saved["delivered_"+s.ID] == "yes"
		s.Core = page.Saved["panel_"+s.ID] == "yes"
		s.Note = page.Saved["session_note_"+s.ID]
		s.Views, _ = reportOptionalCount(page.Saved["views_"+s.ID])
		if s.Delivered && page.ProgramReviewed {
			switch s.Kind {
			case "talk", "keynote":
				page.Talks++
			case "workshop":
				page.Workshops++
			case "panel":
				page.PanelCount++
			}
		}
		if s.Views != nil {
			page.Popular = append(page.Popular, *s)
		}
		if s.Core {
			page.Panels = append(page.Panels, *s)
		}
	}
	sort.SliceStable(page.Popular, func(i, j int) bool { return *page.Popular[i].Views > *page.Popular[j].Views })
	if len(page.Popular) > 5 {
		page.Popular = page.Popular[:5]
	}
	display := func(n int64) string {
		if page.Preview {
			return "—"
		}
		return strconv.FormatInt(n, 10)
	}
	confirmed := func(n int64) string {
		if !page.ProgramReviewed {
			return "—"
		}
		return display(n)
	}
	apps := "—"
	if page.Applications != nil {
		apps = display(*page.Applications)
	}
	page.Metrics = []reportMetric{
		{"Unique checked-in attendees", display(page.Attendees), "Matched by person or email; preserves attendance after account deletion and excludes other revoked tickets."},
		{"Talks delivered", confirmed(page.Talks), "Editor-confirmed talks and keynotes."},
		{"Workshops delivered", confirmed(page.Workshops), "Editor-confirmed workshops."},
		{"Panels delivered", confirmed(page.PanelCount), "Editor-confirmed panel sessions."},
		{"Volunteer crew", display(page.Volunteers), "Unique people assigned to shifts; not verified turnout."},
		{"Talk applications received", apps, "Verified intake count supplied by the editor."},
		{"Submitted project teams", display(page.Teams), "One team entry per submitted/advanced project."},
		{"Hackathon participants", display(page.Participants), "Unique members of submitted/advanced projects."},
		{"Sponsor challenges", display(page.Challenges), "Published sponsor-backed challenges."},
		{"Sats distributed", func() string {
			if page.Preview {
				return "—"
			}
			return compactReportSats(page.Sats)
		}(), "Sats distributions recorded as sent or claimed."},
	}
}
