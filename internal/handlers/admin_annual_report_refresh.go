package handlers

import (
	"btcpp-web/internal/config"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/jackc/pgx/v5"
)

// Each event is a separate request, so a failed platform/event does not discard
// successful refreshes. Existing event edits remain protected by revision checks.
func refreshAnnualSource(ctx context.Context, w http.ResponseWriter, r *http.Request, app *config.AppContext, p *annualReportPage) {
	if cutoff := p.Saved["cutoff"]; cutoff != "" {
		p.Cutoff = cutoff
	}
	if err := loadAnnualReport(ctx, app, p); err != nil {
		http.Error(w, "Unable to load selected events", 503)
		return
	}
	if r.PostForm.Get("action") == "refresh_annual_posts" {
		if err := refreshReportPosts(ctx, app, p.Year); err != nil {
			http.Error(w, "X post refresh failed; previous data retained. Check the production X session.", 502)
			return
		}
		w.Write([]byte("X post analytics updated. Event attribution still requires review."))
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
