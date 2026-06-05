package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

const youtubeAutoscheduleChannel = getters.YouTubePublishChannel

type RecordingAutoscheduleItem struct {
	Row        *RecordingRow
	PublishAt  time.Time
	SlotLabel  string
	SkipReason string
}

type RecordingAutoschedulePreviewPage struct {
	Conf               *types.Conf
	Items              []*RecordingAutoscheduleItem
	Skipped            []*RecordingAutoscheduleItem
	Slots              []*types.YouTubePublishSlot
	RescheduleExisting bool
	FlashError         string
	Year               uint
}

type YouTubeSlotDayGroup struct {
	Weekday int
	Name    string
	Times   string
}

type RecordingsYouTubeSlotsPage struct {
	Conf         *types.Conf
	Groups       []YouTubeSlotDayGroup
	Timezone     string
	FlashMessage string
	FlashError   string
	Year         uint
}

func RecordingsAdminAutoschedulePreview(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, ok := requireRecordingsConfAdmin(w, r, ctx)
	if !ok {
		return
	}
	rescheduleExisting := r.URL.Query().Get("reschedule") == "1"
	preview, skipped, slots, err := buildRecordingAutoschedulePreview(ctx, conf, rescheduleExisting)
	page := &RecordingAutoschedulePreviewPage{
		Conf:               conf,
		Items:              preview,
		Skipped:            skipped,
		Slots:              slots,
		RescheduleExisting: rescheduleExisting,
		Year:               uint(time.Now().Year()),
	}
	if err != nil {
		page.FlashError = err.Error()
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/recordings_autoschedule.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/admin/recordings/autoschedule render: %s", conf.Tag, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func RecordingsAdminAutoscheduleApply(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, ok := requireRecordingsConfAdmin(w, r, ctx)
	if !ok {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, recordingsAdminPath(conf.Tag, "?err="+url.QueryEscape("couldn't parse form: "+err.Error())), http.StatusSeeOther)
		return
	}
	rescheduleExisting := r.FormValue("reschedule") == "1"
	items, _, _, err := buildRecordingAutoschedulePreview(ctx, conf, rescheduleExisting)
	if err != nil {
		http.Redirect(w, r, recordingsAdminPath(conf.Tag, "?err="+url.QueryEscape(err.Error())), http.StatusSeeOther)
		return
	}
	if len(items) == 0 {
		http.Redirect(w, r, recordingsAdminPath(conf.Tag, "?flash="+url.QueryEscape("No eligible YouTube recordings to autoschedule")), http.StatusSeeOther)
		return
	}

	saved, youtubeUpdated, publicSkipped, failed := 0, 0, 0, 0
	var firstErr string
	for _, item := range items {
		if item == nil || item.Row == nil || item.Row.Recording == nil {
			continue
		}
		rec := item.Row.Recording
		publishAt := item.PublishAt
		if err := getters.UpdateRecordingPublishAt(ctx, rec.ID, &publishAt); err != nil {
			failed++
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		rec.PublishAt = &publishAt
		saved++
		result, err := updateRecordingYouTubeSchedule(ctx, rec, &publishAt)
		if err != nil {
			failed++
			if firstErr == "" {
				firstErr = err.Error()
			}
			continue
		}
		switch result {
		case "updated":
			youtubeUpdated++
		case "public":
			publicSkipped++
		}
	}

	flash := fmt.Sprintf("Autoscheduled %d recording(s); YouTube updated %d", saved, youtubeUpdated)
	if publicSkipped > 0 {
		flash += fmt.Sprintf("; %d already public", publicSkipped)
	}
	if failed > 0 {
		flash += fmt.Sprintf("; %d failed", failed)
		if firstErr != "" {
			flash += ": " + firstErr
		}
	}
	http.Redirect(w, r, recordingsAdminPath(conf.Tag, "?flash="+url.QueryEscape(flash)), http.StatusSeeOther)
}

func RecordingsYouTubeSlots(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, ok := requireRecordingsConfAdmin(w, r, ctx)
	if !ok {
		return
	}
	if r.Method == http.MethodPost {
		recordingsYouTubeSlotsSave(w, r, ctx, conf)
		return
	}

	slots, err := getters.ListYouTubePublishSlots(ctx)
	page := &RecordingsYouTubeSlotsPage{
		Conf:     conf,
		Groups:   slotDayGroups(slots),
		Timezone: defaultSlotTimezone(slots),
		Year:     uint(time.Now().Year()),
	}
	if err != nil {
		page.FlashError = err.Error()
		page.Groups = slotDayGroups(nil)
		page.Timezone = "America/Chicago"
	}
	if flash := r.URL.Query().Get("flash"); flash != "" {
		page.FlashMessage = flash
		page.FlashError = ""
	}
	if flashErr := r.URL.Query().Get("err"); flashErr != "" {
		page.FlashError = flashErr
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/recordings_youtube_slots.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/admin/recordings/youtube-slots render: %s", conf.Tag, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func recordingsYouTubeSlotsSave(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, conf *types.Conf) {
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, recordingsAdminPath(conf.Tag, "/youtube-slots?err="+url.QueryEscape("couldn't parse form: "+err.Error())), http.StatusSeeOther)
		return
	}
	timezone := strings.TrimSpace(r.FormValue("timezone"))
	if timezone == "" {
		timezone = "America/Chicago"
	}
	if _, err := time.LoadLocation(timezone); err != nil {
		http.Redirect(w, r, recordingsAdminPath(conf.Tag, "/youtube-slots?err="+url.QueryEscape("invalid timezone: "+err.Error())), http.StatusSeeOther)
		return
	}

	var slots []*types.YouTubePublishSlot
	for weekday := 0; weekday < 7; weekday++ {
		raw := r.FormValue(fmt.Sprintf("times_%d", weekday))
		for _, part := range strings.FieldsFunc(raw, func(r rune) bool {
			return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
		}) {
			tod := strings.TrimSpace(part)
			if tod == "" {
				continue
			}
			parsed, err := time.Parse("15:04", tod)
			if err != nil {
				http.Redirect(w, r, recordingsAdminPath(conf.Tag, "/youtube-slots?err="+url.QueryEscape("invalid time "+tod+": use HH:MM")), http.StatusSeeOther)
				return
			}
			slots = append(slots, &types.YouTubePublishSlot{
				Channel:   youtubeAutoscheduleChannel,
				Weekday:   time.Weekday(weekday),
				TimeOfDay: parsed.Format("15:04"),
				Timezone:  timezone,
				Active:    true,
			})
		}
	}
	if len(slots) == 0 {
		http.Redirect(w, r, recordingsAdminPath(conf.Tag, "/youtube-slots?err="+url.QueryEscape("add at least one publish slot")), http.StatusSeeOther)
		return
	}
	if err := getters.ReplaceYouTubePublishSlots(ctx, slots); err != nil {
		http.Redirect(w, r, recordingsAdminPath(conf.Tag, "/youtube-slots?err="+url.QueryEscape(err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, recordingsAdminPath(conf.Tag, "/youtube-slots?flash="+url.QueryEscape("YouTube publish slots saved")), http.StatusSeeOther)
}

func buildRecordingAutoschedulePreview(ctx *config.AppContext, conf *types.Conf, rescheduleExisting bool) ([]*RecordingAutoscheduleItem, []*RecordingAutoscheduleItem, []*types.YouTubePublishSlot, error) {
	if _, err := getters.FetchSocialPostsCached(ctx); err != nil {
		ctx.Err.Printf("/%s/admin/recordings/autoschedule socialposts: %s", conf.Tag, err)
	}
	rows := recordingRowsForConf(ctx, conf.Tag)
	enrichRowsWithYouTubeStatus(ctx, rows)
	sort.SliceStable(rows, func(i, j int) bool {
		return recordingAutoscheduleSortKey(rows[i]) < recordingAutoscheduleSortKey(rows[j])
	})

	var eligible []*RecordingRow
	var skipped []*RecordingAutoscheduleItem
	for _, row := range rows {
		reason := recordingAutoscheduleSkipReason(row, rescheduleExisting)
		if reason != "" {
			skipped = append(skipped, &RecordingAutoscheduleItem{Row: row, SkipReason: reason})
			continue
		}
		eligible = append(eligible, row)
	}

	slots, err := getters.ListYouTubePublishSlots(ctx)
	if err != nil {
		return nil, skipped, nil, err
	}
	active := activeYouTubePublishSlots(slots)
	if len(active) == 0 {
		return nil, skipped, slots, fmt.Errorf("no active YouTube publish slots configured")
	}
	occupied := occupiedYouTubePublishTimes(ctx, rows, rescheduleExisting)
	startAfter := time.Now().Add(10 * time.Minute)
	assignments, err := nextYouTubePublishTimes(active, occupied, startAfter, len(eligible))
	if err != nil {
		return nil, skipped, slots, err
	}

	items := make([]*RecordingAutoscheduleItem, 0, len(eligible))
	for i, row := range eligible {
		items = append(items, &RecordingAutoscheduleItem{
			Row:       row,
			PublishAt: assignments[i],
			SlotLabel: youtubeSlotLabel(assignments[i], active),
		})
	}
	return items, skipped, slots, nil
}

func recordingAutoscheduleSkipReason(row *RecordingRow, rescheduleExisting bool) string {
	if row == nil || row.Recording == nil {
		return "missing recording"
	}
	if strings.TrimSpace(row.YTURL) == "" {
		return "missing YouTube URL"
	}
	if strings.EqualFold(row.YTPrivacyStatus, "public") {
		return "already public on YouTube"
	}
	if !rescheduleExisting {
		if row.Recording.PublishAt != nil && row.Recording.PublishAt.After(time.Now()) {
			return "already scheduled"
		}
		if row.YTPublishAt != nil && row.YTPublishAt.After(time.Now()) {
			return "already scheduled on YouTube"
		}
	}
	return ""
}

func recordingAutoscheduleSortKey(row *RecordingRow) string {
	if row == nil || row.Recording == nil {
		return ""
	}
	if row.ConfTalk != nil && row.ConfTalk.Sched != nil {
		return row.ConfTalk.Sched.Start.UTC().Format(time.RFC3339Nano) + row.Recording.ID
	}
	return strings.ToLower(row.Recording.TalkName) + row.Recording.ID
}

func activeYouTubePublishSlots(slots []*types.YouTubePublishSlot) []*types.YouTubePublishSlot {
	out := make([]*types.YouTubePublishSlot, 0, len(slots))
	for _, slot := range slots {
		if slot == nil || !slot.Active || strings.TrimSpace(slot.TimeOfDay) == "" {
			continue
		}
		out = append(out, slot)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Weekday != out[j].Weekday {
			return out[i].Weekday < out[j].Weekday
		}
		return out[i].TimeOfDay < out[j].TimeOfDay
	})
	return out
}

func occupiedYouTubePublishTimes(ctx *config.AppContext, currentRows []*RecordingRow, rescheduleExisting bool) map[int64]bool {
	occupied := map[int64]bool{}
	all, err := getters.ListRecordings(ctx)
	if err != nil {
		ctx.Err.Printf("autoschedule list recordings for occupied slots: %s", err)
		all = getters.ListRecordingsCached()
	}
	for _, rec := range all {
		if rec == nil || rec.PublishAt == nil || rec.PublishAt.Before(time.Now()) {
			continue
		}
		occupied[rec.PublishAt.UTC().Unix()] = true
	}
	posts, err := getters.FetchSocialPostsCached(ctx)
	if err == nil {
		for _, post := range posts {
			if post == nil || post.ScheduledAt == nil || post.ScheduledAt.Before(time.Now()) {
				continue
			}
			if post.PostedTo == recordingPlatformYouTube || post.PostedTo == youtubeAutoscheduleChannel {
				occupied[post.ScheduledAt.UTC().Unix()] = true
			}
		}
	}
	if rescheduleExisting {
		for _, row := range currentRows {
			if row == nil || row.Recording == nil || row.Recording.PublishAt == nil {
				continue
			}
			delete(occupied, row.Recording.PublishAt.UTC().Unix())
		}
	}
	return occupied
}

func nextYouTubePublishTimes(slots []*types.YouTubePublishSlot, occupied map[int64]bool, after time.Time, count int) ([]time.Time, error) {
	if count <= 0 {
		return nil, nil
	}
	var out []time.Time
	for dayOffset := 0; dayOffset < 730 && len(out) < count; dayOffset++ {
		for _, slot := range slots {
			if slot == nil {
				continue
			}
			loc, err := time.LoadLocation(slot.Timezone)
			if err != nil {
				return nil, fmt.Errorf("load slot timezone %q: %w", slot.Timezone, err)
			}
			day := after.In(loc).AddDate(0, 0, dayOffset)
			if slot.Weekday != day.Weekday() {
				continue
			}
			tod, err := time.Parse("15:04", slot.TimeOfDay)
			if err != nil {
				return nil, fmt.Errorf("parse slot time %q: %w", slot.TimeOfDay, err)
			}
			candidate := time.Date(day.Year(), day.Month(), day.Day(), tod.Hour(), tod.Minute(), 0, 0, loc)
			if !candidate.After(after) {
				continue
			}
			key := candidate.UTC().Unix()
			if occupied[key] {
				continue
			}
			occupied[key] = true
			out = append(out, candidate)
			if len(out) == count {
				break
			}
		}
	}
	if len(out) < count {
		return nil, fmt.Errorf("only found %d publish slots for %d recordings", len(out), count)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Before(out[j]) })
	return out, nil
}

func youtubeSlotLabel(t time.Time, slots []*types.YouTubePublishSlot) string {
	for _, slot := range slots {
		if slot == nil || slot.Weekday != t.In(slotLocation(slot)).Weekday() {
			continue
		}
		local := t.In(slotLocation(slot))
		if local.Format("15:04") == slot.TimeOfDay {
			return fmt.Sprintf("%s %s %s", local.Weekday(), slot.TimeOfDay, slot.Timezone)
		}
	}
	return t.Format("Mon Jan 2 3:04 PM MST")
}

func slotLocation(slot *types.YouTubePublishSlot) *time.Location {
	if slot != nil && slot.Timezone != "" {
		if loc, err := time.LoadLocation(slot.Timezone); err == nil {
			return loc
		}
	}
	return time.UTC
}

func slotDayGroups(slots []*types.YouTubePublishSlot) []YouTubeSlotDayGroup {
	byDay := map[int][]string{}
	for _, slot := range slots {
		if slot == nil || !slot.Active {
			continue
		}
		byDay[int(slot.Weekday)] = append(byDay[int(slot.Weekday)], slot.TimeOfDay)
	}
	groups := make([]YouTubeSlotDayGroup, 0, 7)
	for day := 0; day < 7; day++ {
		times := byDay[day]
		sort.Strings(times)
		groups = append(groups, YouTubeSlotDayGroup{
			Weekday: day,
			Name:    time.Weekday(day).String(),
			Times:   strings.Join(times, "\n"),
		})
	}
	return groups
}

func defaultSlotTimezone(slots []*types.YouTubePublishSlot) string {
	for _, slot := range slots {
		if slot != nil && slot.Timezone != "" {
			return slot.Timezone
		}
	}
	return "America/Chicago"
}
