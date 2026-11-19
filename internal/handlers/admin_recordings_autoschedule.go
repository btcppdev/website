package handlers

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

const youtubeAutoscheduleChannel = getters.YouTubePublishChannel

type RecordingAutoscheduleItem struct {
	Row                *RecordingRow
	PublishAt          time.Time
	SlotLabel          string
	DurationLabel      string
	ReservedUntil      time.Time
	SkipReason         string
	XBroadcastEligible bool
	XBroadcastLabel    string
}

type RecordingAutoschedulePreviewPage struct {
	Conf                  *types.Conf
	Items                 []*RecordingAutoscheduleItem
	Skipped               []*RecordingAutoscheduleItem
	Slots                 []*types.YouTubePublishSlot
	RescheduleExisting    bool
	YouTubeUpdatesEnabled bool
	XStudioEnabled        bool
	XEligibleCount        int
	FlashError            string
	ScheduleFingerprint   string
	Year                  uint
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
	redirectRecordingPublishing(w, r, conf.Tag, "", "youtube")
}

func RecordingsAdminAutoscheduleApply(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, ok := requireRecordingsConfAdmin(w, r, ctx)
	if !ok {
		return
	}
	redirectRecordingPublishing(w, r, conf.Tag, "", "youtube")
}

func recordingScheduleFingerprint(items []*RecordingAutoscheduleItem) string {
	var text strings.Builder
	for _, item := range items {
		if item != nil && item.Row != nil && item.Row.Recording != nil {
			fmt.Fprintf(&text, "%s:%d:%d|", item.Row.Recording.ID, item.PublishAt.Unix(), item.Row.Recording.DurationSeconds)
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256([]byte(text.String())))
}

// reorderRecordingAutoscheduleItems assigns the existing configured slot
// sequence to the administrator's requested recording order. Unknown,
// duplicate, and stale IDs are ignored; newly eligible rows are appended in
// their normal stage/agenda order so a stale form cannot drop recordings.
func reorderRecordingAutoscheduleItems(items []*RecordingAutoscheduleItem, requestedIDs []string) []*RecordingAutoscheduleItem {
	if len(items) < 2 || len(requestedIDs) == 0 {
		return items
	}
	byID := make(map[string]*RecordingAutoscheduleItem, len(items))
	for _, item := range items {
		if item != nil && item.Row != nil && item.Row.Recording != nil {
			byID[item.Row.Recording.ID] = item
		}
	}
	seen := make(map[string]bool, len(items))
	ordered := make([]*RecordingAutoscheduleItem, 0, len(items))
	for _, rawID := range requestedIDs {
		id := strings.TrimSpace(rawID)
		if id == "" || seen[id] || byID[id] == nil {
			continue
		}
		seen[id] = true
		ordered = append(ordered, byID[id])
	}
	for _, item := range items {
		if item == nil || item.Row == nil || item.Row.Recording == nil || seen[item.Row.Recording.ID] {
			continue
		}
		seen[item.Row.Recording.ID] = true
		ordered = append(ordered, item)
	}
	if len(ordered) != len(items) {
		return items
	}
	result := make([]*RecordingAutoscheduleItem, len(ordered))
	for i, item := range ordered {
		copyItem := *item
		copyItem.PublishAt = items[i].PublishAt
		copyItem.SlotLabel = items[i].SlotLabel
		result[i] = &copyItem
	}
	return result
}

func RecordingsYouTubeSlots(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if _, ok := requireRecordingsConfAdmin(w, r, ctx); !ok {
		return
	}
	http.Redirect(w, r, "/admin/publishing#channels", http.StatusSeeOther)
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

func buildRecordingAutoschedulePreview(ctx *config.AppContext, conf *types.Conf, rescheduleExisting bool, requestedIDs []string) ([]*RecordingAutoscheduleItem, []*RecordingAutoscheduleItem, []*types.YouTubePublishSlot, error) {
	rows := recordingRowsForConf(ctx, conf.Tag)
	if err := refreshRecordingScheduleDurations(ctx, rows); err != nil {
		return nil, nil, nil, err
	}
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

	orderItems := make([]*RecordingAutoscheduleItem, len(eligible))
	for i, row := range eligible {
		orderItems[i] = &RecordingAutoscheduleItem{Row: row}
	}
	for i, item := range reorderRecordingAutoscheduleItems(orderItems, requestedIDs) {
		eligible[i] = item.Row
	}

	slots, err := getters.ListYouTubePublishSlots(ctx)
	if err != nil {
		return nil, skipped, nil, err
	}
	active := activeYouTubePublishSlots(slots)
	if len(active) == 0 {
		return nil, skipped, slots, fmt.Errorf("no active YouTube publish slots configured")
	}
	reservations, err := getters.ListRecordingPublishReservations(ctx)
	if err != nil {
		return nil, skipped, slots, err
	}
	occupied := occupiedYouTubePublishTimes(reservations, eligible, rescheduleExisting)
	startAfter := time.Now().Add(10 * time.Minute)
	assignments, err := nextRecordingPublishTimes(active, occupied, startAfter, eligible)
	if err != nil {
		return nil, skipped, slots, err
	}

	items := make([]*RecordingAutoscheduleItem, 0, len(eligible))
	for i, row := range eligible {
		item := &RecordingAutoscheduleItem{
			Row:           row,
			PublishAt:     assignments[i],
			SlotLabel:     youtubeSlotLabel(assignments[i], active),
			DurationLabel: (time.Duration(row.Recording.DurationSeconds) * time.Second).String(),
			ReservedUntil: assignments[i].Add(time.Duration(row.Recording.DurationSeconds)*time.Second + getters.RecordingBroadcastGap),
		}
		decorateRecordingAutoscheduleX(ctx, conf, item)
		items = append(items, item)
	}
	return items, skipped, slots, nil
}

func decorateRecordingAutoscheduleX(ctx *config.AppContext, conf *types.Conf, item *RecordingAutoscheduleItem) {
	if item == nil || item.Row == nil || item.Row.Recording == nil {
		return
	}
	item.XBroadcastEligible = false
	if !ctx.Env.Recordings.X.Enabled {
		item.XBroadcastLabel = "X Studio disabled"
		return
	}
	broadcast := item.Row.XBroadcast
	if broadcast != nil && broadcast.Status == "scheduled" && broadcast.ScheduledAt.Equal(item.PublishAt) {
		item.XBroadcastLabel = "Already scheduled"
		return
	}
	if broadcast == nil || strings.TrimSpace(broadcast.PosterMediaID) == "" {
		cardKey := recordingNotificationCardKey(item.Row, conf)
		if cardKey == "" {
			item.XBroadcastLabel = "Missing social card"
			return
		}
		if !spaces.IsConfigured() || !spaces.Exists(cardKey) {
			item.XBroadcastLabel = "Poster unavailable"
			return
		}
	}
	item.XBroadcastEligible = true
	switch {
	case broadcast == nil:
		item.XBroadcastLabel = "Ready to create"
	case broadcast.Status == "scheduled":
		item.XBroadcastLabel = "Ready to update time"
	case broadcast.Status == "failed":
		item.XBroadcastLabel = "Ready to resume"
	default:
		item.XBroadcastLabel = "In progress or resumable"
	}
}

func recordingAutoscheduleXEligibleCount(items []*RecordingAutoscheduleItem) int {
	count := 0
	for _, item := range items {
		if item != nil && item.XBroadcastEligible {
			count++
		}
	}
	return count
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
	if row.Recording.DurationSeconds <= 0 {
		return "runtime unavailable; wait for YouTube processing, then refresh"
	}
	return ""
}

func recordingAutoscheduleSortKey(row *RecordingRow) string {
	if row == nil || row.Recording == nil {
		return "99"
	}
	stage := recordingAutoscheduleStageRank(row)
	if row.ConfTalk != nil && row.ConfTalk.Sched != nil {
		return fmt.Sprintf("%02d|0|%s|%s", stage, row.ConfTalk.Sched.Start.UTC().Format(time.RFC3339Nano), row.Recording.ID)
	}
	return fmt.Sprintf("%02d|1|%s|%s", stage, strings.ToLower(row.Recording.TalkName), row.Recording.ID)
}

func recordingAutoscheduleStageRank(row *RecordingRow) int {
	if row == nil || row.ConfTalk == nil {
		return 3
	}
	switch strings.ToLower(strings.TrimSpace(row.ConfTalk.Venue)) {
	case "one", "p2pkh", "main", "main stage":
		return 0
	case "two", "p2wsh", "p2sh-p2wpkh", "talk", "talks", "talking", "talks stage", "talking stage", "talking two":
		return 1
	case "three", "multisig", "p2tr", "workshop", "workshops", "workshop stage", "workshops stage", "workshops 2":
		return 2
	default:
		return 3
	}
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

func occupiedYouTubePublishTimes(reservations []getters.RecordingPublishReservation, eligibleRows []*RecordingRow, rescheduleExisting bool) []getters.RecordingPublishReservation {
	var occupied []getters.RecordingPublishReservation
	moving := map[string]bool{}
	if rescheduleExisting {
		for _, row := range eligibleRows {
			if row != nil && row.Recording != nil {
				moving[row.Recording.ID] = true
			}
		}
	}
	// Exclude reservations by owner, never by timestamp: another event (or a
	// skipped recording in this event) may still own the same slot.
	for _, reservation := range reservations {
		if !moving[reservation.RecordingID] || reservation.Persistent {
			occupied = append(occupied, reservation)
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
