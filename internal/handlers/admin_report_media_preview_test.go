package handlers

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"os"
	"strconv"
	"testing"
	"time"
)

// Opt-in visual review using a local, sanitized Studio export. No database writes.
func TestReportMediaPreview(t *testing.T) {
	path := os.Getenv("REPORT_MEDIA_EXPORT")
	if path == "" {
		t.Skip("local media preview")
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var export struct {
		Retrieved  string `json:"retrieved_at"`
		Broadcasts []struct {
			ID        string `json:"id"`
			Title     string `json:"title"`
			URL       string `json:"url"`
			Thumbnail string `json:"thumbnail_url"`
			Views     int64  `json:"views"`
		} `json:"broadcasts"`
	}
	if err = json.Unmarshal(raw, &export); err != nil {
		t.Fatal(err)
	}

	attributionRaw, err := os.ReadFile("../../docs/2026-report-broadcasts.json")
	if err != nil {
		t.Fatal(err)
	}
	var attributed []struct {
		EventTag string
		reportMedia
	}
	if err = json.Unmarshal(attributionRaw, &attributed); err != nil {
		t.Fatal(err)
	}
	byID := map[string]string{}
	for _, m := range attributed {
		byID[m.ID] = m.EventTag
	}
	eventMedia := map[string][]reportMedia{}
	var media []reportMedia
	for _, b := range export.Broadcasts {
		tag := byID[b.ID]
		if tag == "" {
			continue
		}
		m := reportMedia{Platform: "X", ID: b.ID, Title: b.Title, URL: b.URL, Thumbnail: reportThumbnail(context.Background(), b.Thumbnail), Views: b.Views, Collected: export.Retrieved, Basis: "X Media Studio · lifetime broadcast views (live + replay)"}
		eventMedia[tag] = append(eventMedia[tag], m)
		if tag == "nairobi" {
			m.Event = "Nairobi"
			media = append(media, m)
		}
	}
	if len(media) == 0 {
		t.Fatal("no Nairobi broadcasts in export")
	}
	postRaw, err := os.ReadFile("../../docs/2026-report-x-post-impressions.json")
	if err != nil {
		t.Fatal(err)
	}
	var postStats struct {
		Impressions      int64
		Source, Coverage string
		Retrieved        string `json:"retrieved_at"`
		From, Through    string
	}
	if err := json.Unmarshal(postRaw, &postStats); err != nil {
		t.Fatal(err)
	}
	p := reportFixturePage(t)
	snapshot, _ := json.Marshal(media)
	p.Saved["media_snapshot"] = string(snapshot)
	p.PostLibrary = defaultReportPosts(2026)
	p.DemoApplications = true
	p.Saved["applications"] = "84"
	p.Saved["applications_source"] = "Illustrative CFP count for local layout review only; not Nairobi data"
	p.Applications = verifiedReportApplications(p.Saved)
	// Local layout preview preselects suggestions; production requires explicit review.
	for _, row := range p.PostReview() {
		if row.Suggested {
			p.Saved["x_post_"+row.ID] = "include"
		}
	}
	p.Message = "LOCAL PREVIEW: speaking-application counts are illustrative. Suggested X post matches are preselected to demonstrate the layout. Review attribution before sharing."
	t.Logf("Nairobi suggested posts: %d; impressions: %d", len(p.ApprovedPosts()), postImpressions(p.ApprovedPosts()))
	mux := http.NewServeMux()
	mux.Handle("/static/", http.StripPrefix("/static/", http.FileServer(http.Dir("../../static"))))
	mux.HandleFunc("/", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "GET" {
			http.Error(w, "Preview only", 405)
			return
		}
		tmpl, err := template.ParseFiles("../../templates/admin/reports.tmpl", "../../templates/admin/annual_report.tmpl")
		if err != nil {
			http.Error(w, err.Error(), 500)
			return
		}
		if r.URL.Path == "/annual" {
			annual := annualFixture()
			annual.Saved["reach_x_impressions"] = strconv.FormatInt(postStats.Impressions, 10)
			annual.Saved["reach_x_source"] = postStats.Source
			annual.Saved["reach_x_period"] = postStats.From + " to " + postStats.Through
			annual.Saved["reach_x_date"] = postStats.Retrieved[:10]
			annual.Saved["reach_x_coverage"] = postStats.Coverage
			for i := range annual.Events {
				e := &annual.Events[i]
				titles := map[string]string{"floripa26": "Exploits edition", "vienna": "Economics edition", "vegas": "Villains edition", "nairobi": "Open-source edition", "toronto": "Consensus edition", "berlin26": "Payments edition"}
				if title := titles[e.Event.Tag]; title != "" {
					e.Event.Title = title
				}
				dates := map[string][2]string{"floripa26": {"2026-02-26T12:00:00-03:00", "2026-02-28T12:00:00-03:00"}, "vienna": {"2026-05-27T16:00:00+02:00", "2026-05-28T16:00:00+02:00"}, "vegas": {"2026-04-23T07:00:00-07:00", "2026-04-24T07:00:00-07:00"}, "nairobi": {"2026-06-17T17:00:00+03:00", "2026-06-20T02:00:00+03:00"}, "toronto": {"2026-07-22T10:00:00-04:00", "2026-07-24T10:00:00-04:00"}, "berlin26": {"2026-10-01T11:00:00+02:00", "2026-10-03T16:00:00+02:00"}}
				if dates, ok := dates[e.Event.Tag]; ok {
					start, _ := time.Parse(time.RFC3339, dates[0])
					end, _ := time.Parse(time.RFC3339, dates[1])
					e.Event.Start = &start
					e.Event.End = &end
					e.Event.Period = reportEventPeriod(e.Event)
				}
				e.Media = append(e.Media, eventMedia[e.Event.Tag]...)
				for j := range e.Media {
					e.Media[j].Event = e.Event.Location
				}
			}
			annual.Events[3].WinningProjects = p.Winners
			annual.Events[3].Posts = p.ApprovedPosts()
			err = tmpl.ExecuteTemplate(w, "annual_report.tmpl", annual)
		} else {
			err = tmpl.ExecuteTemplate(w, "reports.tmpl", p)
		}
		if err != nil {
			t.Log(err)
		}
	})
	t.Log("Media preview: http://127.0.0.1:8141/ and /annual")
	t.Fatal(http.ListenAndServe("127.0.0.1:8141", mux))
}

func TestReportBroadcastInventoryRollup(t *testing.T) {
	raw, err := os.ReadFile("../../docs/2026-report-broadcasts.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []struct {
		EventTag string
		reportMedia
	}
	if err := json.Unmarshal(raw, &rows); err != nil {
		t.Fatal(err)
	}
	p := annualFixture()
	assigned := 0
	for i := range p.Events {
		for _, row := range rows {
			if row.EventTag == p.Events[i].Event.Tag {
				p.Events[i].Media = append(p.Events[i].Media, row.reportMedia)
				assigned++
			}
		}
	}
	if assigned != len(rows) {
		t.Fatal("unmatched event in broadcast inventory")
	}
	totals := p.MediaTotals()
	if len(totals) != 1 || totals[0].Platform != "X" || totals[0].Views != 12306 || totals[0].Recordings != 17 {
		t.Fatalf("unexpected rollup: %+v", totals)
	}
	p.Events[3].Included = false
	if got := p.MediaTotals()[0].Views; got != 11606 {
		t.Fatalf("excluded Nairobi still counted: %d", got)
	}
}
