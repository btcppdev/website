package publishing

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"github.com/jackc/pgx/v5"
)

// RecordingItem is one destination of a recording, with its own schedule and
// revision. ConfirmedAt remains reserved until the provider confirms a move.
type RecordingItem struct {
	YouTubePrivacy    string     `json:"youtube_privacy"`
	RetryConfirmation bool       `json:"retry_confirmation"`
	ID                string     `json:"id"`
	RecordingID       string     `json:"recording_id"`
	ChannelID         string     `json:"channel_id"`
	Conference        string     `json:"conference"`
	Title             string     `json:"title"`
	Kind              string     `json:"kind"`
	At                *time.Time `json:"at"`
	ConfirmedAt       *time.Time `json:"confirmed_at"`
	State             string     `json:"state"`
	Error             string     `json:"error"`
	Revision          string     `json:"revision"`
	DurationSeconds   int64      `json:"duration_seconds"`
	YouTubeURL        string     `json:"youtube_url"`
	FileURI           string     `json:"-"`
	BroadcastID       string     `json:"-"`
	Version           int        `json:"version"`
	Eligible          bool       `json:"eligible"`
	Reason            string     `json:"reason"`
}
type RecordingMove struct {
	ID       string     `json:"id"`
	Revision string     `json:"revision"`
	At       *time.Time `json:"at"`
}
type recordingDB interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func recordingItems(ctx context.Context, db recordingDB) ([]RecordingItem, error) {
	rows, err := db.Query(ctx, `
 SELECT r.id::text,ch.id,c.tag,r.talk_name,r.duration_seconds,r.youtube_url,r.file_uri,
 CASE WHEN ch.id='youtube' THEN r.publish_at ELSE xb.scheduled_at END,
 coalesce(j.state,''),j.target_at,j.previous_at,coalesce(j.error,''),coalesce(j.version,0),
 CASE WHEN ch.id='youtube' THEN coalesce(ys.privacy_status,(SELECT status FROM social_posts WHERE ref='recording:'||r.id::text||':youtube' ORDER BY updated_at DESC LIMIT 1),'') ELSE coalesce(xb.status,'') END,
 coalesce(rb.state,''),coalesce(xb.broadcast_id,''),r.updated_at,xb.updated_at
 FROM recordings r JOIN conf_talks ct ON ct.id=r.conf_talk_id JOIN conferences c ON c.id=ct.conference_id
 CROSS JOIN publishing_channels ch
 LEFT JOIN recording_x_broadcasts xb ON xb.recording_id=r.id
 LEFT JOIN recording_broadcasts rb ON rb.recording_id=r.id
 LEFT JOIN recording_youtube_status ys ON ys.recording_id=r.id AND ys.youtube_url=r.youtube_url
 LEFT JOIN recording_schedule_changes j ON j.recording_id=r.id AND j.channel_id=ch.id
 WHERE ch.id IN ('youtube','broadcasts') ORDER BY c.start_date DESC NULLS LAST,r.talk_name,r.id,ch.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []RecordingItem{}
	for rows.Next() {
		var d RecordingItem
		var jobState, sourceState, live string
		var target, previous *time.Time
		var updated time.Time
		var xupdated *time.Time
		if err = rows.Scan(&d.RecordingID, &d.ChannelID, &d.Conference, &d.Title, &d.DurationSeconds, &d.YouTubeURL, &d.FileURI, &d.ConfirmedAt, &jobState, &target, &previous, &d.Error, &d.Version, &sourceState, &live, &d.BroadcastID, &updated, &xupdated); err != nil {
			return nil, err
		}
		d.Kind = "recording"
		prefix := "youtube:"
		if d.ChannelID == "broadcasts" {
			d.Kind = "stream"
			prefix = "broadcast:"
		}
		d.ID = prefix + d.RecordingID
		d.At = d.ConfirmedAt
		d.State = "draft"
		if d.At != nil {
			d.State = "scheduled"
		}
		if sourceState == "published" || sourceState == "public" {
			d.State = "published"
		}
		if d.ChannelID == "broadcasts" && sourceState != "" && sourceState != "scheduled" {
			d.State = sourceState
		}
		if jobState != "" && jobState != "applied" {
			d.ConfirmedAt = previous
			d.At = target
			d.State = jobState
		}
		if d.ChannelID == "youtube" {
			d.YouTubePrivacy = sourceState
		}
		if d.ChannelID == "broadcasts" && (live == "live" || live == "ended") {
			d.State = live
		}
		encoded, _ := json.Marshal([]any{d.RecordingID, d.ChannelID, d.ConfirmedAt, jobState, target, d.Version, d.YouTubeURL, d.FileURI, d.DurationSeconds, sourceState, live, updated, xupdated})
		d.Revision = fmt.Sprintf("%x", sha256.Sum256(encoded))
		d.RetryConfirmation = d.ChannelID == "broadcasts" && d.State == "needs_review" && d.BroadcastID == ""
		d.Reason = recordingUnavailable(d, time.Now())
		d.Eligible = d.Reason == ""
		out = append(out, d)
	}
	return out, rows.Err()
}
func recordingUnavailable(d RecordingItem, now time.Time) string {
	if d.ChannelID == "youtube" && (d.YouTubePrivacy == "public" || d.YouTubePrivacy == "published") {
		return "This video is already public on YouTube"
	}
	switch d.State {
	case "syncing", "needs_review", "creating", "uploading_poster", "finalizing", "created", "poster_uploaded":
		return "Resolve the in-progress schedule change before moving this item"
	case "live", "ended", "published":
		return "This item is already live or published"
	}
	if d.ConfirmedAt != nil && !d.ConfirmedAt.After(now.Add(time.Minute)) {
		return "The confirmed publication time has already started or is less than a minute away"
	}
	if d.DurationSeconds <= 0 {
		return "Recording runtime is missing; use Refresh recording status"
	}
	if d.ChannelID == "youtube" && strings.TrimSpace(d.YouTubeURL) == "" {
		return "Upload the recording to YouTube first"
	}
	if d.ChannelID == "broadcasts" && strings.TrimSpace(d.FileURI) == "" {
		return "Add the recording source file first"
	}
	return ""
}
func (s Store) RecordingItems(ctx context.Context) ([]RecordingItem, error) {
	return recordingItems(ctx, s.DB)
}
func recordingByID(items []RecordingItem, id string) (RecordingItem, error) {
	for _, d := range items {
		if d.ID == id {
			return d, nil
		}
	}
	return RecordingItem{}, fmt.Errorf("Recording no longer exists")
}

// RecordingSlots preserves each existing YouTube slot's timezone. Broadcasts
// have their own recurring schedule; changing either never shifts existing jobs.
func recordingSlots(ctx context.Context, db recordingDB, channel string, start, end time.Time) ([]time.Time, error) {
	if channel != "youtube" && channel != "broadcasts" {
		return nil, fmt.Errorf("Choose a recording destination")
	}
	if channel == "youtube" {
		rows, e := db.Query(ctx, `SELECT weekday,to_char(publish_time,'HH24:MI'),timezone FROM youtube_publish_slots WHERE active AND channel=$1`, getters.YouTubePublishChannel)
		if e != nil {
			return nil, e
		}
		defer rows.Close()
		out := []time.Time{}
		seen := map[int64]bool{}
		for rows.Next() {
			var sl Slot
			var zone string
			if e = rows.Scan(&sl.Day, &sl.Time, &zone); e != nil {
				return nil, e
			}
			for _, at := range SlotsBetween(zone, []Slot{sl}, start, end) {
				if !seen[at.UnixNano()] {
					out = append(out, at)
					seen[at.UnixNano()] = true
				}
			}
		}
		sort.Slice(out, func(i, j int) bool { return out[i].Before(out[j]) })
		return out, rows.Err()
	}
	var zone string
	var slots []Slot
	if e := db.QueryRow(ctx, `SELECT timezone,slots FROM publishing_channels WHERE id=$1`, channel).Scan(&zone, &slots); e != nil {
		return nil, e
	}
	return SlotsBetween(zone, slots, start, end), nil
}
func (s Store) RecordingSlotEntries(ctx context.Context, channel string, start, end time.Time) ([]Entry, error) {
	ats, e := recordingSlots(ctx, s.DB, channel, start, end)
	if e != nil {
		return nil, e
	}
	out := []Entry{}
	for _, at := range ats {
		at := at
		out = append(out, Entry{ID: channel + at.Format(time.RFC3339), ChannelID: channel, Title: "Publication slot", Kind: "slot", At: &at, Provider: "recordings"})
	}
	return out, nil
}
func (s Store) AvailableRecordingSlots(ctx context.Context, id string, now time.Time) ([]Entry, error) {
	items, e := s.RecordingItems(ctx)
	if e != nil {
		return nil, e
	}
	d, e := recordingByID(items, id)
	if e != nil {
		return nil, e
	}
	if d.Reason != "" {
		return nil, fmt.Errorf("%s", d.Reason)
	}
	reservations, e := getters.RecordingScheduleReservations(ctx, s.DB)
	if e != nil {
		return nil, e
	}
	slots, e := s.RecordingSlotEntries(ctx, d.ChannelID, now.Add(10*time.Minute), now.AddDate(0, 0, 90))
	if e != nil {
		return nil, e
	}
	out := []Entry{}
	for _, sl := range slots {
		if d.At != nil && sl.At.Equal(*d.At) {
			continue
		}
		conflict, e := getters.RecordingScheduleConflictForRecording(d.RecordingID, *sl.At, d.DurationSeconds, reservations)
		if e != nil {
			return nil, e
		}
		if conflict == nil {
			out = append(out, sl)
		}
	}
	return out, nil
}
func lockRecordings(ctx context.Context, tx pgx.Tx) error {
	_, e := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(7330192401)`)
	return e
}

// SaveRecordingMoves reserves every proposed slot in one transaction. It queues
// provider updates, without pretending an external schedule has already moved.
func (s Store) SaveRecordingMoves(ctx context.Context, moves []RecordingMove) error {
	if len(moves) == 0 || len(moves) > 200 {
		return fmt.Errorf("Select between one and 200 recording items")
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = lockRecordings(ctx, tx); e != nil {
		return e
	}
	items, e := recordingItems(ctx, tx)
	if e != nil {
		return e
	}
	reservations, e := getters.RecordingScheduleReservations(ctx, tx)
	if e != nil {
		return e
	}
	seen := map[string]bool{}
	for _, m := range moves {
		d, e := recordingByID(items, m.ID)
		if e != nil {
			return e
		}
		if seen[m.ID] || m.Revision != d.Revision {
			return ErrChanged
		}
		seen[m.ID] = true
		if d.Reason != "" {
			return fmt.Errorf("%s: %s", d.Title, d.Reason)
		}
		if m.At == nil {
			if d.ChannelID != "youtube" {
				return fmt.Errorf("Broadcast cancellation is not supported")
			}
			if d.ConfirmedAt == nil && d.At == nil {
				return fmt.Errorf("This release has no schedule to clear")
			}
		} else {
			if !m.At.After(time.Now().Add(10 * time.Minute)) {
				return fmt.Errorf("Choose a publication slot at least ten minutes away")
			}
			slots, e := recordingSlots(ctx, tx, d.ChannelID, m.At.Add(-time.Second), m.At.Add(time.Second))
			if e != nil {
				return e
			}
			valid := false
			for _, at := range slots {
				valid = valid || at.Equal(*m.At)
			}
			if !valid {
				return fmt.Errorf("Choose a configured publication slot for this channel")
			}
			conflict, e := getters.RecordingScheduleConflictForRecording(d.RecordingID, *m.At, d.DurationSeconds, reservations)
			if e != nil {
				return e
			}
			if conflict != nil {
				return fmt.Errorf("This slot overlaps %s, including the five-minute broadcast gap", conflict.Label())
			}
			reservations = append(reservations, getters.RecordingPublishReservation{RecordingID: d.RecordingID, Title: d.Title, ConferenceTag: d.Conference, PublishAt: *m.At, DurationSeconds: d.DurationSeconds})
		}
		_, e = tx.Exec(ctx, `INSERT INTO recording_schedule_changes(recording_id,channel_id,target_at,state,youtube_url,file_uri,previous_at) VALUES($1,$2,$3,'pending',$4,$5,$6) ON CONFLICT(recording_id,channel_id) DO UPDATE SET target_at=EXCLUDED.target_at,previous_at=EXCLUDED.previous_at,state='pending',youtube_url=EXCLUDED.youtube_url,file_uri=EXCLUDED.file_uri,error='',lease_until=NULL,version=recording_schedule_changes.version+1,updated_at=now()`, d.RecordingID, d.ChannelID, m.At, d.YouTubeURL, d.FileURI, d.ConfirmedAt)
		if e != nil {
			return e
		}
	}
	return tx.Commit(ctx)
}

// PreviewRecordingQueue allocates each selected destination independently. The
// returned revisions and exact times must be submitted for a second validation.
func (s Store) PreviewRecordingQueue(ctx context.Context, ids []string) ([]RecordingMove, error) {
	if len(ids) == 0 || len(ids) > 200 {
		return nil, fmt.Errorf("Select between one and 200 recording items")
	}
	items, e := s.RecordingItems(ctx)
	if e != nil {
		return nil, e
	}
	reservations, e := getters.RecordingScheduleReservations(ctx, s.DB)
	if e != nil {
		return nil, e
	}
	out := []RecordingMove{}
	seen := map[string]bool{}
	now := time.Now()
	for _, id := range ids {
		d, e := recordingByID(items, id)
		if e != nil {
			return nil, e
		}
		if seen[id] {
			return nil, fmt.Errorf("Select each item once")
		}
		seen[id] = true
		if d.Reason != "" {
			return nil, fmt.Errorf("%s: %s", d.Title, d.Reason)
		}
		slots, e := recordingSlots(ctx, s.DB, d.ChannelID, now.Add(10*time.Minute), now.AddDate(1, 0, 0))
		if e != nil {
			return nil, e
		}
		found := false
		for _, at := range slots {
			if d.At != nil && at.Equal(*d.At) {
				continue
			}
			conflict, e := getters.RecordingScheduleConflictForRecording(d.RecordingID, at, d.DurationSeconds, reservations)
			if e != nil {
				return nil, e
			}
			if conflict != nil {
				continue
			}
			at := at
			out = append(out, RecordingMove{ID: d.ID, Revision: d.Revision, At: &at})
			reservations = append(reservations, getters.RecordingPublishReservation{RecordingID: d.RecordingID, Title: d.Title, PublishAt: at, DurationSeconds: d.DurationSeconds})
			found = true
			break
		}
		if !found {
			return nil, fmt.Errorf("No free %s slot for %s in the next year; add slots in Channels & schedules", d.ChannelID, d.Title)
		}
	}
	return out, nil
}
func (s Store) SaveBroadcastSlots(ctx context.Context, c Channel) error {
	if c.ID != "broadcasts" {
		return fmt.Errorf("Choose the broadcasts channel")
	}
	if e := ValidateSlots(c.Timezone, c.Slots); e != nil {
		return e
	}
	tx, e := s.DB.Begin(ctx)
	if e != nil {
		return e
	}
	defer tx.Rollback(ctx)
	if e = lockRecordings(ctx, tx); e != nil {
		return e
	}
	_, e = tx.Exec(ctx, `UPDATE publishing_channels SET timezone=$1,slots=$2,updated_at=now() WHERE id='broadcasts'`, c.Timezone, c.Slots)
	if e != nil {
		return e
	}
	return tx.Commit(ctx)
}
