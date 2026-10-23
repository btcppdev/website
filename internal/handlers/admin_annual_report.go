package handlers

import (
	"btcpp-web/internal/config"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type annualEvent struct {
	Proposals                                                                   int64
	Applications                                                                *int64
	Posts                                                                       []reportPost
	Media                                                                       []reportMedia
	Event                                                                       reportEvent
	Included, Eligible                                                          bool
	Registrations, Attendees, Projects, Participants, Challenges, Sats, Winners int64
	Talks, Workshops, Panels, Speakers                                          int64
	HackCoverage                                                                string
	Reach                                                                       []reportReachPlatform
	WinningProjects                                                             []reportWinner
}

func (e annualEvent) Sessions() int64     { return e.Talks + e.Workshops + e.Panels }
func (e annualEvent) SatsDisplay() string { return compactReportSats(e.Sats) }
func (e annualEvent) HackKnown() bool {
	return e.HackCoverage == "records" || e.HackCoverage == "automatic"
}

type annualEditionLink struct{ URL, Date string }
type annualReportPage struct {
	Year                                                                    int
	Recipient, Cutoff, Generated, Frozen, CSRF, Message                     string
	Revision                                                                int64
	Saved                                                                   map[string]string
	Sections                                                                []reportSection
	Events                                                                  []annualEvent
	Editions                                                                []annualEditionLink
	UniqueAttendees, ReturningAttendees, UniqueSpeakers, UniqueParticipants int64
	UniqueRegistrants, RepeatRegistrants                                    int64
	RegistrationBased                                                       bool
	Preview                                                                 bool
}

func annualSections(year int, recipient string) []reportSection {
	note := fmt.Sprintf("To our friends at %s,\n\nThank you for helping bring the Bitcoin development community together. Three things stand out for me this year: taking bitcoin++ to Africa, shipping our hackathon platform with help from our summer intern Jake from Tulsa, and the amazing conversations we helped spark at events around the globe.\n\nOur first economics edition was a particular highlight: making room for developers and the people who say ‘developers don’t understand economics’ to talk to one another. Those conversations are part of why we do this.\n\nThis report brings together the gatherings, people and projects behind %d so far. It also gives us a place to be candid about what we’ve learned as we expand.\n\nThank you for being part of it.\n\nnifty", recipient, year)
	if year != 2026 {
		note = fmt.Sprintf("To our friends at %s,\n\n[Add your opening letter for %d.]\n\nnifty", recipient, year)
	}
	program := "[Highlight three technical questions that ran through this year’s talks and workshops. Link to recordings and explain why the work matters beyond the event.]"
	if year == 2026 {
		program = "Our first economics edition brought developers into conversation with the people who say ‘developers don’t understand economics’. Making space for that exchange was one of this year’s highlights.\n\nThe featured recordings in this review offer a way back into the technical work and conversations from our events."
	}
	return []reportSection{
		{"note", "A note from nifty", note},
		{"community", "The people who make it happen", "Each gathering creates space for builders to meet, share their work and find people working on related problems. The counts below describe participation in our events, not the size of the entire Bitcoin development community.\n\n[Add a specific collaboration or participant story, with permission.]"},
		{"program", "The technical conversation", program},
		{"building", "From conversation to code", "The hackathons give participants time to turn ideas into prototypes and get feedback from other builders. A submitted project is a starting point, not proof of adoption.\n\n[Add the strongest project follow-up stories and explain what teams shipped next.]"},
		{"reach", "Beyond the room", ""},
		{"lessons", "What expansion taught us", "[Add the biggest lesson from expanding, the current team size, and the headwinds that still need attention. Be specific about what was hard and what you changed.]"},
		{"next", "Where we go next", "[Describe the experiments planned for next year, what you want to improve, and how you will know whether it worked.]\n\nThank you for supporting the people building Bitcoin’s future."},
	}
}

func (p annualReportPage) Section(key string) reportSection {
	for _, s := range p.Sections {
		if s.Key == key {
			return s
		}
	}
	return reportSection{}
}
func (p annualReportPage) Included() []annualEvent {
	var out []annualEvent
	for _, e := range p.Events {
		if e.Included {
			out = append(out, e)
		}
	}
	return out
}
func (p annualReportPage) Upcoming() []annualEvent {
	var out []annualEvent
	for _, e := range p.Events {
		if !e.Eligible {
			out = append(out, e)
		}
	}
	return out
}
func (p annualReportPage) Platforms() []reportReachPlatform {
	return (sponsorReportPage{Saved: p.Saved}).ReachPlatforms()
}
func (p annualReportPage) URL() string {
	return fmt.Sprintf("/admin/reports/annual?year=%d&recipient=%s", p.Year, url.QueryEscape(p.Recipient))
}
func (p annualReportPage) Scope() string {
	return fmt.Sprintf("annual-v1/%d/%s", p.Year, strings.ToLower(p.Recipient))
}
func (p annualReportPage) ParticipationChart() reportChart { return p.chart("attendance") }
func (p annualReportPage) ProjectChart() reportChart       { return p.chart("projects") }
func (p annualReportPage) ParticipantChart() reportChart   { return p.chart("participants") }
func (p annualReportPage) chart(kind string) reportChart {
	c := reportChart{}
	for _, e := range p.Included() {
		n := e.Attendees
		if p.RegistrationBased {
			n = e.Registrations
		}
		if kind != "attendance" && !e.HackKnown() {
			continue
		}
		if kind == "projects" {
			n = e.Projects
		}
		if kind == "participants" {
			n = e.Participants
		}
		c.Rows = append(c.Rows, reportBar{Label: e.Event.Location, Value: n, Detail: e.Event.Tag})
		if n > c.Maximum {
			c.Maximum = n
		}
	}
	for i := range c.Rows {
		if c.Maximum > 0 {
			c.Rows[i].Width = 100 * float64(c.Rows[i].Value) / float64(c.Maximum)
		}
	}
	return c
}
func (p annualReportPage) ProgramChart() reportChart {
	c := reportChart{Kind: "program", Rows: []reportBar{{Label: "Talks & keynotes"}, {Label: "Workshops"}, {Label: "Panels"}}}
	for _, e := range p.Included() {
		c.Rows[0].Value += e.Talks
		c.Rows[1].Value += e.Workshops
		c.Rows[2].Value += e.Panels
	}
	return c
}
func (p annualReportPage) ReturningChart() reportChart {
	if p.RegistrationBased {
		return reportChart{Kind: "program", Rows: []reportBar{{Label: "Registered for one included event", Value: p.UniqueRegistrants - p.RepeatRegistrants}, {Label: "Registered for multiple included events", Value: p.RepeatRegistrants}}}
	}
	return reportChart{Kind: "program", Rows: []reportBar{{Label: "Attended one included event", Value: p.UniqueAttendees - p.ReturningAttendees}, {Label: "Attended multiple included events", Value: p.ReturningAttendees}}}
}
func (p annualReportPage) ParticipationTotal() int64 {
	var n int64
	for _, e := range p.Included() {
		if p.RegistrationBased {
			n += e.Registrations
		} else {
			n += e.Attendees
		}
	}
	return n
}

func annualEligible(e reportEvent, year int, cutoff string) bool {
	if e.Start == nil || e.End == nil {
		return false
	}
	loc := reportEventLocation(e)
	return e.Start.In(loc).Year() == year && !e.End.Before(*e.Start) && e.End.In(loc).Format("2006-01-02") <= cutoff
}

func AdminAnnualSponsorReport(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	if requireGlobalAdmin(w, r, app) == nil {
		return
	}
	serveAnnualSponsorReport(w, r, app)
}

func serveAnnualSponsorReport(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	ctx, cancel := context.WithTimeout(r.Context(), 110*time.Second)
	defer cancel()
	if app.DB == nil {
		http.Error(w, "Report database unavailable", 503)
		return
	}
	now := time.Now().UTC()
	year := now.Year()
	if raw := r.URL.Query().Get("year"); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 2020 || n > now.Year() {
			http.Error(w, "Invalid report year", 400)
			return
		}
		year = n
	}
	recipient := strings.ToUpper(r.URL.Query().Get("recipient"))
	if recipient == "" {
		recipient = "NYDIG"
	}
	if recipient != "NYDIG" && recipient != "HRF" {
		http.Error(w, "Choose NYDIG or HRF", 400)
		return
	}
	p := annualReportPage{Year: year, Recipient: recipient, Saved: map[string]string{}, Generated: now.Format(time.RFC3339), Cutoff: fmt.Sprintf("%d-12-31", year)}
	if year == now.Year() {
		p.Cutoff = now.Format("2006-01-02")
	}
	if id := r.URL.Query().Get("edition"); id != "" {
		if _, err := uuid.Parse(id); err != nil || r.Method != http.MethodGet {
			http.Error(w, "Invalid frozen edition", 400)
			return
		}
		var content []byte
		err := app.DB.QueryRow(ctx, `SELECT content FROM sponsor_report_drafts WHERE scope=$1`, p.Scope()+"/frozen/"+id).Scan(&content)
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
			return
		}
		if err != nil || json.Unmarshal(content, &p) != nil {
			http.Error(w, "Unable to read frozen edition", 503)
			return
		}
		renderAnnualReport(w, app, p)
		return
	}
	var content []byte
	err := app.DB.QueryRow(ctx, `SELECT content,revision FROM sponsor_report_drafts WHERE scope=$1`, p.Scope()).Scan(&content, &p.Revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Unable to load annual draft", 503)
		return
	}
	if len(content) > 0 && json.Unmarshal(content, &p.Saved) != nil {
		http.Error(w, "Invalid stored draft", 500)
		return
	}
	if p.Saved == nil {
		p.Saved = map[string]string{}
	}
	p.CSRF, err = ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare editor", 500)
		return
	}
	if r.Method == http.MethodPost {
		r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
		if r.ParseForm() != nil {
			http.Error(w, "Invalid form", 400)
			return
		}
		if !secureTokenEqual(p.CSRF, r.PostForm.Get("csrf")) {
			http.Error(w, "Invalid form token", 403)
			return
		}
		if action := r.PostForm.Get("action"); action == "refresh_annual_event" || action == "refresh_annual_posts" {
			refreshAnnualSource(ctx, w, r, app, &p)
			return
		}
		rev, e := strconv.ParseInt(r.PostForm.Get("revision"), 10, 64)
		if e != nil || rev != p.Revision {
			http.Error(w, errReportConflict.Error(), 409)
			return
		}
		if err = annualReportForm(&p, r.PostForm, now); err != nil {
			http.Error(w, err.Error(), 400)
			return
		}
	}
	if s := p.Saved["cutoff"]; s != "" {
		p.Cutoff = s
	}
	p.Sections = annualSections(year, recipient)
	for i, s := range p.Sections {
		if v, ok := p.Saved[s.Key]; ok {
			p.Sections[i].Text = v
		}
	}
	if err = loadAnnualReport(ctx, app, &p); err != nil {
		app.Err.Printf("annual report: %v", err)
		http.Error(w, "Unable to load annual event data", 503)
		return
	}
	if r.Method == http.MethodPost {
		id, err := saveAnnualReport(ctx, app, p, r.PostForm.Get("action") == "freeze")
		if errors.Is(err, errReportConflict) {
			http.Error(w, err.Error(), 409)
			return
		}
		if err != nil {
			http.Error(w, "Unable to save annual report", 503)
			return
		}
		target := p.URL() + "&saved=1"
		if id != "" {
			target = p.URL() + "&edition=" + id
		}
		http.Redirect(w, r, target, http.StatusSeeOther)
		return
	}
	rows, err := app.DB.Query(ctx, `SELECT scope,updated_at FROM sponsor_report_drafts WHERE starts_with(scope,$1) ORDER BY updated_at DESC LIMIT 30`, p.Scope()+"/frozen/")
	if err != nil {
		http.Error(w, "Unable to load editions", 503)
		return
	}
	for rows.Next() {
		var scope string
		var date time.Time
		if err = rows.Scan(&scope, &date); err != nil {
			break
		}
		id := strings.TrimPrefix(scope, p.Scope()+"/frozen/")
		p.Editions = append(p.Editions, annualEditionLink{p.URL() + "&edition=" + id, date.UTC().Format("2 Jan 2006, 15:04 UTC")})
	}
	if err == nil {
		err = rows.Err()
	}
	rows.Close()
	if err != nil {
		http.Error(w, "Unable to load editions", 503)
		return
	}
	if r.URL.Query().Get("saved") == "1" {
		p.Message = "Annual draft saved. Each recipient has an independent draft."
	}
	renderAnnualReport(w, app, p)
}

func annualReportForm(p *annualReportPage, f url.Values, now time.Time) error {
	if f.Get("action") != "save" && f.Get("action") != "freeze" && f.Get("action") != "include_all" {
		return fmt.Errorf("invalid report action")
	}
	cutoff, err := time.Parse("2006-01-02", f.Get("cutoff"))
	if err != nil || cutoff.Year() != p.Year || cutoff.After(now) {
		return fmt.Errorf("choose a cutoff in the report year, not in the future")
	}
	saved := map[string]string{"cutoff": f.Get("cutoff"), "selection_saved": "yes"}
	if f.Get("action") == "include_all" {
		saved["selection_saved"] = "no"
	}
	for _, s := range annualSections(p.Year, p.Recipient) {
		v := strings.TrimSpace(f.Get(s.Key))
		if len(v) > 12000 {
			return fmt.Errorf("%s exceeds 12,000 bytes", s.Title)
		}
		saved[s.Key] = v
	}
	for _, id := range f["event"] {
		if _, err := uuid.Parse(id); err != nil {
			return fmt.Errorf("invalid event selection")
		}
		saved["include_"+id] = "yes"
	}
	// Retain recaps when saving an older form that did not render these fields.
	for key, value := range p.Saved {
		if strings.HasPrefix(key, "event_highlight_") {
			saved[key] = value
		}
	}
	for key, values := range f {
		if strings.HasPrefix(key, "event_highlight_") {
			if _, err := uuid.Parse(strings.TrimPrefix(key, "event_highlight_")); err != nil {
				return fmt.Errorf("invalid event highlight")
			}
			if len(values) != 1 || len(values[0]) > 2000 {
				return fmt.Errorf("event highlights must be at most 2,000 bytes")
			}
			saved[key] = strings.TrimSpace(values[0])
		}
		if strings.HasPrefix(key, "hack_") && len(values) == 1 {
			if _, err := uuid.Parse(strings.TrimPrefix(key, "hack_")); err != nil {
				return fmt.Errorf("invalid event coverage")
			}
			if values[0] != "unknown" && values[0] != "none" && values[0] != "records" && values[0] != "incomplete" {
				return fmt.Errorf("invalid hackathon coverage")
			}
			saved[key] = values[0]
		}
	}
	if err := saveReportReach(f, saved, now); err != nil {
		return err
	}
	p.Saved = saved
	p.Cutoff = saved["cutoff"]
	return nil
}

// Saving a draft and creating its immutable data snapshot is one transaction.
func saveAnnualReport(ctx context.Context, app *config.AppContext, p annualReportPage, freeze bool) (string, error) {
	// Persist default recaps too, so a frozen edition does not pick up future copy changes.
	saved := make(map[string]string, len(p.Saved)+len(p.Events))
	for key, value := range p.Saved {
		saved[key] = value
	}
	for _, h := range p.EventHighlightEditors() {
		saved[h.Copy.Key] = h.Copy.Text
	}
	p.Saved = saved
	tx, err := app.DB.Begin(ctx)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx)
	content, err := json.Marshal(p.Saved)
	if err != nil {
		return "", err
	}
	result, err := tx.Exec(ctx, `INSERT INTO sponsor_report_drafts(scope,content) SELECT $1,$2::jsonb WHERE $3::bigint=0 ON CONFLICT(scope) DO NOTHING`, p.Scope(), string(content), p.Revision)
	if err == nil && p.Revision > 0 {
		result, err = tx.Exec(ctx, `UPDATE sponsor_report_drafts SET content=$2::jsonb,revision=revision+1,updated_at=now() WHERE scope=$1 AND revision=$3`, p.Scope(), string(content), p.Revision)
	}
	if err != nil {
		return "", err
	}
	if result.RowsAffected() != 1 {
		return "", errReportConflict
	}
	id := ""
	if freeze {
		id = uuid.NewString()
		p.Frozen = p.Generated
		p.CSRF = ""
		p.Editions = nil
		p.Message = ""
		content, err = json.Marshal(p)
		if err != nil {
			return "", err
		}
		if _, err = tx.Exec(ctx, `INSERT INTO sponsor_report_drafts(scope,content) VALUES($1,$2::jsonb)`, p.Scope()+"/frozen/"+id, string(content)); err != nil {
			return "", err
		}
	}
	return id, tx.Commit(ctx)
}

func renderAnnualReport(w http.ResponseWriter, app *config.AppContext, p annualReportPage) {
	var out bytes.Buffer
	if err := app.TemplateCache.ExecuteTemplate(&out, "admin/annual_report.tmpl", p); err != nil {
		app.Err.Printf("annual report template: %v", err)
		http.Error(w, "Unable to render annual report", 500)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	_, _ = w.Write(out.Bytes())
}
