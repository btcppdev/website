package handlers

import (
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

func reportSourceRecent(collected string, now time.Time) bool {
	t, err := time.Parse(time.RFC3339, collected)
	return err == nil && !t.After(now) && now.Sub(t) < 6*time.Hour
}

// Each event is a separate request, so a failed platform/event does not discard
// successful refreshes. Existing event edits remain protected by revision checks.
func refreshAnnualSource(ctx context.Context, w http.ResponseWriter, r *http.Request, app *config.AppContext, p *annualReportPage) {
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	automatic := r.PostForm.Get("automatic") == "yes"
	if cutoff := p.Saved["cutoff"]; cutoff != "" {
		p.Cutoff = cutoff
	}
	if err := loadAnnualReport(ctx, app, p); err != nil {
		http.Error(w, "Unable to load selected events", 503)
		return
	}
	if r.PostForm.Get("action") == "refresh_annual_posts" {
		if automatic {
			posts, err := loadReportPosts(ctx, app.DB, p.Year)
			if err != nil {
				http.Error(w, "Unable to read X post analytics; previous data retained.", 503)
				return
			}
			if reportSourceRecent(posts.Collected, time.Now()) {
				w.Write([]byte("Using X post analytics collected " + posts.Collected + ". Matching posts are included automatically."))
				return
			}
		}
		if err := refreshReportPosts(ctx, app, p.Year); err != nil {
			http.Error(w, "X post refresh failed; previous data retained. Check the production X session.", 502)
			return
		}
		w.Write([]byte("X post analytics updated. Matching posts are included automatically; you can exclude incorrect matches."))
		return
	}
	var event *reportEvent
	for _, e := range p.Included() {
		if e.Event.ID == r.PostForm.Get("event_id") {
			v := e.Event
			event = &v
			break
		}
	}
	if event == nil {
		http.Error(w, "Event is not selected in the saved annual report", 400)
		return
	}
	page := sponsorReportPage{Event: *event, Scope: "event-v2/" + event.ID, Saved: map[string]string{}}
	if err := loadEventReport(ctx, app, &page); err != nil {
		http.Error(w, "Unable to read event records", 503)
		return
	}
	var raw []byte
	err := app.DB.QueryRow(ctx, `SELECT content,revision FROM sponsor_report_drafts WHERE scope=$1`, page.Scope).Scan(&raw, &page.Revision)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		http.Error(w, "Unable to read event draft", 503)
		return
	}
	if len(raw) > 0 {
		if json.Unmarshal(raw, &page.Saved) != nil {
			http.Error(w, "Invalid event draft", 500)
			return
		}
	} else if strings.Contains(strings.ToLower(event.Tag+" "+event.Location), "nairobi") {
		if applyNairobiReportDefaults(&page) != nil {
			http.Error(w, "Unable to load event defaults", 500)
			return
		}
	}
	if page.Saved == nil {
		page.Saved = map[string]string{}
	}
	if automatic && page.Saved["media_inventory"] == reportMediaInventoryVersion && reportSourceRecent(page.Saved["media_refresh_attempted"], time.Now()) {
		w.Write([]byte("Last checked " + page.Saved["media_refresh_attempted"] + ". " + page.Saved["media_status"]))
		return
	}
	refreshReportMedia(ctx, app, &page, page.Saved)
	if err = saveEventReportDraft(ctx, app, page.Scope, page.Revision, page.Saved); err != nil {
		if errors.Is(err, errReportConflict) {
			http.Error(w, "Event draft changed during refresh. Retry to preserve the other editor’s changes.", 409)
		} else {
			http.Error(w, "Unable to save refreshed analytics", 503)
		}
		return
	}
	w.Header().Set("Content-Type", "text/plain; charset=utf-8")
	w.Write([]byte(page.Saved["media_status"]))
}
