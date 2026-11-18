package getters

import (
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
)

func TestValidateRecordingSchedule(t *testing.T) {
	now := time.Date(2030, time.June, 3, 9, 0, 0, 0, time.UTC)
	at, later, past := now.Add(time.Hour), now.Add(2*time.Hour), now.Add(-time.Hour)
	sameInstant := at.In(time.FixedZone("KST", 9*60*60))
	reservations := []RecordingPublishReservation{
		{RecordingID: "berlin", Title: "Berlin talk", ConferenceTag: "berlin26", PublishAt: at, DurationSeconds: 1800},
		{RecordingID: "seoul", Title: "Seoul talk", ConferenceTag: "seoul", PublishAt: at, DurationSeconds: 1800},
	}
	tests := []struct {
		name    string
		updates []RecordingScheduleUpdate
		wantErr string
	}{
		{"other event keeps shared slot", []RecordingScheduleUpdate{{"seoul", &at, 1800}}, "Berlin talk (berlin26)"},
		{"same instant in another timezone", []RecordingScheduleUpdate{{"seoul", &sameInstant, 1800}}, "Berlin talk (berlin26)"},
		{"move to free slot", []RecordingScheduleUpdate{{"seoul", &later, 1800}}, ""},
		{"clear schedule", []RecordingScheduleUpdate{{"seoul", nil, 1800}}, ""},
		{"historical metadata", []RecordingScheduleUpdate{{"seoul", &past, 1800}}, ""},
		{"batch releases only its own reservations", []RecordingScheduleUpdate{{"berlin", &at, 1800}, {"seoul", &later, 1800}}, ""},
		{"duplicate proposed slots", []RecordingScheduleUpdate{{"berlin", &later, 1800}, {"seoul", &later, 1800}}, "already reserved"},
		{"duplicate ID", []RecordingScheduleUpdate{{"seoul", &later, 1800}, {"seoul", nil, 1800}}, "duplicate recording ID"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validateRecordingSchedule(tt.updates, reservations, now)
			if tt.wantErr == "" && err != nil {
				t.Fatal(err)
			}
			if tt.wantErr != "" && (err == nil || !strings.Contains(err.Error(), tt.wantErr)) {
				t.Fatalf("error = %v, want %q", err, tt.wantErr)
			}
		})
	}
}

func TestRecordingScheduleFailsClosedWithoutDatabase(t *testing.T) {
	ctx := &config.AppContext{}
	if _, err := ListRecordingPublishReservations(ctx); err == nil {
		t.Fatal("unavailable database was treated as an empty calendar")
	}
	if err := SaveRecordingSchedule(ctx, []RecordingScheduleUpdate{{RecordingID: "seoul"}}); err == nil {
		t.Fatal("saved a schedule without a database")
	}
}

func TestRecordingScheduleConflictRuntimeAndGap(t *testing.T) {
	start := time.Date(2030, 6, 3, 23, 30, 0, 0, time.UTC)
	bookings := []RecordingPublishReservation{{RecordingID: "berlin", Title: "Workshop", PublishAt: start, DurationSeconds: 5400}}
	for _, tt := range []struct {
		name     string
		start    time.Time
		seconds  int64
		conflict bool
	}{
		{"duplicate starts", start, 1800, true},
		{"during long workshop after midnight", start.Add(time.Hour), 1800, true},
		{"inside turnaround gap", start.Add(92 * time.Minute), 1800, true},
		{"exactly after runtime plus gap", start.Add(95 * time.Minute), 1800, false},
		{"earlier talk runs into workshop", start.Add(-time.Hour), 7200, true},
		{"ends and clears gap just before workshop", start.Add(-35 * time.Minute), 1800, false},
		{"same time expressed in Seoul", start.In(time.FixedZone("KST", 9*3600)), 1800, true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			conflict, err := RecordingScheduleConflict(tt.start, tt.seconds, bookings)
			if err != nil || (conflict != nil) != tt.conflict {
				t.Fatalf("conflict = %+v, err = %v", conflict, err)
			}
		})
	}
	bookings[0].DurationSeconds = 0
	if _, err := RecordingScheduleConflict(start, 1800, bookings); err == nil {
		t.Fatal("unknown runtime allowed a booking")
	}
}

func TestRecordingScheduleKeepsExistingXBroadcastsDuringReschedule(t *testing.T) {
	now := time.Now()
	at, later := now.Add(time.Hour), now.Add(3*time.Hour)
	reservations := []RecordingPublishReservation{{RecordingID: "first", PublishAt: at, DurationSeconds: 1800, Persistent: true}}
	updates := []RecordingScheduleUpdate{{"first", &later, 1800}, {"second", &at, 1800}}
	if err := validateRecordingSchedule(updates, reservations, now); err == nil {
		t.Fatal("rescheduling released an X broadcast that has not been moved yet")
	}
	if err := validateRecordingSchedule([]RecordingScheduleUpdate{{"first", &at, 1800}}, reservations, now); err != nil {
		t.Fatalf("own X broadcast blocks unchanged schedule: %v", err)
	}
}

func TestDatabaseSmokeRecordingScheduleDuration(t *testing.T) {
	ctx := databaseSmokeContext(t)
	berlin, _ := insertScheduleTestRecording(t, ctx)
	seoul, _ := insertScheduleTestRecording(t, ctx)
	at := time.Now().UTC().Add(96 * time.Hour).Truncate(time.Second)
	if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE recordings SET duration_seconds = 5400 WHERE id = $1::uuid`, berlin); err != nil {
		t.Fatal(err)
	}
	if err := UpdateRecordingPublishAt(ctx, berlin, &at); err != nil {
		t.Fatal(err)
	}
	overlap := at.Add(time.Hour)
	if err := UpdateRecordingPublishAt(ctx, seoul, &overlap); err == nil || !strings.Contains(err.Error(), "overlaps") {
		t.Fatalf("runtime conflict = %v", err)
	}
	clear := at.Add(95 * time.Minute)
	if err := UpdateRecordingPublishAt(ctx, seoul, &clear); err != nil {
		t.Fatal(err)
	}
	if err := UpdateRecordingPublishAt(ctx, seoul, nil); err != nil {
		t.Fatal(err)
	}
	if err := UpdateRecordingYTLink(ctx, seoul, "https://youtu.be/new-video"); err != nil {
		t.Fatal(err)
	}
	if err := UpdateRecordingPublishAt(ctx, seoul, &clear); err == nil || !strings.Contains(err.Error(), "runtime is unknown") {
		t.Fatalf("unknown runtime = %v", err)
	}
	if err := CacheRecordingYouTubeDuration(ctx, seoul, "https://youtu.be/wrong-video", "", 900); err == nil {
		t.Fatal("cached runtime for a stale video")
	}
	if err := CacheRecordingYouTubeDuration(ctx, seoul, "https://youtu.be/new-video", "", 900); err != nil {
		t.Fatal(err)
	}
	rec, err := GetRecordingByID(ctx, seoul)
	if err != nil || rec.DurationSeconds != 900 {
		t.Fatalf("duration = %+v, err = %v", rec, err)
	}
	if err := UpdateRecordingPublishAt(ctx, seoul, &clear); err != nil {
		t.Fatal(err)
	}
}

func TestDatabaseSmokeRecordingSchedule(t *testing.T) {
	ctx := databaseSmokeContext(t)
	berlin, _ := insertScheduleTestRecording(t, ctx)
	seoul, seoulTalk := insertScheduleTestRecording(t, ctx)
	at := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Second)
	later := at.Add(time.Hour)
	if err := UpdateRecordingPublishAt(ctx, berlin, &at); err != nil {
		t.Fatal(err)
	}
	if err := UpdateRecordingPublishAt(ctx, seoul, &at); err == nil || !strings.Contains(err.Error(), "already reserved") {
		t.Fatalf("manual scheduling conflict = %v", err)
	}
	if _, err := UpsertRecordingForConfTalk(ctx, seoulTalk, RecordingUpsert{SetPublishAt: true, PublishAt: &at}); err == nil || !strings.Contains(err.Error(), "already reserved") {
		t.Fatalf("API scheduling conflict = %v", err)
	}
	if _, _, err := ClaimRecordingXBroadcastStep(ctx, seoul, at, "session", "poster", time.Now()); err == nil || !strings.Contains(err.Error(), "already reserved") {
		t.Fatalf("X scheduling conflict = %v", err)
	}
	// A stale batch must save nothing, even if some requested slots are free.
	if err := SaveRecordingSchedule(ctx, []RecordingScheduleUpdate{{seoul, &later, 1800}, {berlin, &later, 1800}}); err == nil {
		t.Fatal("conflicting batch was accepted")
	}
	assertScheduleTestTime(t, ctx, berlin, &at)
	assertScheduleTestTime(t, ctx, seoul, nil)
	// A later database failure must also roll back an earlier valid update.
	if err := SaveRecordingSchedule(ctx, []RecordingScheduleUpdate{{seoul, &later, 1800}, {"00000000-0000-0000-0000-000000000000", nil, 0}}); err == nil {
		t.Fatal("batch with missing recording was accepted")
	}
	assertScheduleTestTime(t, ctx, seoul, nil)
	if err := SaveRecordingSchedule(ctx, []RecordingScheduleUpdate{{berlin, &later, 1800}, {seoul, &at, 1800}}); err != nil {
		t.Fatalf("move own reservations as a batch: %v", err)
	}
}

func TestDatabaseSmokeRecordingScheduleConcurrentEvents(t *testing.T) {
	ctx := databaseSmokeContext(t)
	berlin, _ := insertScheduleTestRecording(t, ctx)
	seoul, _ := insertScheduleTestRecording(t, ctx)
	at := time.Now().UTC().Add(48 * time.Hour).Truncate(time.Second)
	start := make(chan struct{})
	results := make(chan error, 2)
	for _, id := range []string{berlin, seoul} {
		go func(id string) {
			<-start
			results <- UpdateRecordingPublishAt(ctx, id, &at)
		}(id)
	}
	close(start)
	succeeded := 0
	for range 2 {
		if err := <-results; err == nil {
			succeeded++
		} else if !strings.Contains(err.Error(), "already reserved") {
			t.Fatal(err)
		}
	}
	if succeeded != 1 {
		t.Fatalf("concurrent bookings succeeded = %d, want 1", succeeded)
	}
}

func TestDatabaseSmokeRecordingScheduleIncludesExternalReservations(t *testing.T) {
	ctx := databaseSmokeContext(t)
	berlin, _ := insertScheduleTestRecording(t, ctx)
	seoul, _ := insertScheduleTestRecording(t, ctx)
	at := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Second)
	later := at.Add(time.Hour)
	// Legacy social posts can identify their recording only through ref.
	ref := "recording:" + berlin + ":youtube"
	if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `INSERT INTO social_posts (ref, posted_to, scheduled_at) VALUES ($1, 'youtube', $2)`, ref, at); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = ctx.DB.Exec(ctx.DatabaseContext(), `DELETE FROM social_posts WHERE ref = $1`, ref) })
	if err := UpdateRecordingPublishAt(ctx, seoul, &at); err == nil {
		t.Fatal("ignored legacy YouTube reservation")
	}
	if err := UpdateRecordingPublishAt(ctx, berlin, &at); err != nil {
		t.Fatalf("own legacy post blocked its recording: %v", err)
	}
	if _, _, err := ClaimRecordingXBroadcastStep(ctx, berlin, later, "session", "poster", time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := UpdateRecordingPublishAt(ctx, seoul, &later); err == nil {
		t.Fatal("ignored existing X broadcast start")
	}
	reservations, err := ListRecordingPublishReservations(ctx)
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, reservation := range reservations {
		if reservation.RecordingID == berlin {
			count++
			if reservation.ConferenceTag == "" {
				t.Fatal("reservation lost its event")
			}
		}
	}
	if count != 3 {
		t.Fatalf("recording, legacy post and X reservations = %d, want 3", count)
	}
}

func insertScheduleTestRecording(t *testing.T, ctx *config.AppContext) (string, string) {
	t.Helper()
	confID, _ := insertSmokeConference(t, ctx)
	var recordingID, talkID string
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		WITH proposal AS (
			INSERT INTO proposals (conference_id, title) VALUES ($1::uuid, 'Schedule test') RETURNING id
		), talk AS (
			INSERT INTO conf_talks (conference_id, proposal_id) SELECT $1::uuid, id FROM proposal RETURNING id
		)
		INSERT INTO recordings (conf_talk_id, talk_name, duration_seconds) SELECT id, 'Schedule test', 1800 FROM talk RETURNING id::text, conf_talk_id::text
	`, confID).Scan(&recordingID, &talkID); err != nil {
		t.Fatal(err)
	}
	return recordingID, talkID
}

func assertScheduleTestTime(t *testing.T, ctx *config.AppContext, id string, want *time.Time) {
	t.Helper()
	var got *time.Time
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT publish_at FROM recordings WHERE id = $1::uuid`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if (got == nil) != (want == nil) || (got != nil && !got.Equal(*want)) {
		t.Fatalf("recording %s publish_at = %v, want %v", id, got, want)
	}
}
