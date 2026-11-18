package publishing

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"btcpp-web/external/secureblob"
	"btcpp-web/external/xapi"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/oauth2"
)

func TestSlotsRespectDST(t *testing.T) {
	start := time.Date(2027, 3, 13, 0, 0, 0, 0, time.UTC)
	slots := SlotsBetween("America/Chicago", []Slot{{Day: 0, Time: "02:30"}}, start, start.AddDate(0, 0, 10))
	if len(slots) != 1 || slots[0].Day() != 21 {
		t.Fatalf("nonexistent local time must be skipped: %v", slots)
	}
	start = time.Date(2027, 11, 6, 0, 0, 0, 0, time.UTC)
	slots = SlotsBetween("America/Chicago", []Slot{{Day: 0, Time: "01:30"}}, start, start.AddDate(0, 0, 2))
	if len(slots) != 1 {
		t.Fatalf("repeated hour must not double post: %v", slots)
	}
}
func TestPostURL(t *testing.T) {
	for _, u := range []string{"http://x.com/me/status/1", "https://x.com.evil/me/status/1", "https://x.com/me/status/no", "javascript:alert(1)"} {
		if ValidPostURL(u) {
			t.Fatal(u)
		}
	}
	if !ValidPostURL("https://x.com/i/status/123") {
		t.Fatal("valid post rejected")
	}
}
func testStore(t *testing.T) Store {
	t.Helper()
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("isolated Postgres required")
	}
	db, e := pgxpool.New(context.Background(), os.Getenv("DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(db.Close)
	return Store{DB: db}
}
func fixtureChannel(t *testing.T, s Store) Channel {
	t.Helper()
	c := Channel{ID: "test:" + uuid.NewString(), Platform: "x", Provider: "direct", Timezone: "UTC", Name: "Test"}
	for day := 0; day < 7; day++ {
		c.Slots = append(c.Slots, Slot{Day: day, Time: "10:00"}, Slot{Day: day, Time: "12:00"})
	}
	_, e := s.DB.Exec(context.Background(), `INSERT INTO publishing_channels(id,platform,name,provider,timezone,slots) VALUES($1,'x','Test','direct','UTC',$2)`, c.ID, c.Slots)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() {
		_, _ = s.DB.Exec(context.Background(), `DELETE FROM publishing_deliveries WHERE channel_id=$1`, c.ID)
		_, _ = s.DB.Exec(context.Background(), `DELETE FROM publishing_channels WHERE id=$1`, c.ID)
	})
	return c
}
func TestStoreConcurrentQueueAndFixedTime(t *testing.T) {
	s := testStore(t)
	c := fixtureChannel(t, s)
	ctx := context.Background()
	fixed := SlotsBetween(c.Timezone, c.Slots, time.Now().Add(time.Minute), time.Now().AddDate(0, 0, 7))[0]
	if _, e := s.Save(ctx, Delivery{ChannelID: c.ID, Body: "fixed", Mode: "fixed", At: &fixed}); e != nil {
		t.Fatal(e)
	}
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, e := s.Save(ctx, Delivery{ChannelID: c.ID, Body: "queued", Mode: "queue"}); e != nil {
				t.Error(e)
			}
		}()
	}
	wg.Wait()
	rows, e := s.DB.Query(ctx, `SELECT scheduled_at,count(*) FROM publishing_deliveries WHERE channel_id=$1 GROUP BY scheduled_at`, c.ID)
	if e != nil {
		t.Fatal(e)
	}
	defer rows.Close()
	count := 0
	for rows.Next() {
		var at time.Time
		var n int
		if e = rows.Scan(&at, &n); e != nil {
			t.Fatal(e)
		}
		if n != 1 {
			t.Fatalf("double booked %v", at)
		}
		count++
	}
	if count != 9 {
		t.Fatal(count)
	}
	entries, e := s.Calendar(ctx, time.Now(), time.Now().AddDate(0, 0, 14))
	if e != nil {
		t.Fatal(e)
	}
	if len(entries) < 9 {
		t.Fatal("calendar lost posts")
	}
}
func TestQueueRollbackAndOptimisticEdit(t *testing.T) {
	s := testStore(t)
	c := fixtureChannel(t, s)
	ctx := context.Background()
	id, e := s.Save(ctx, Delivery{ChannelID: c.ID, SourceRef: "same", Body: "draft", Mode: "draft"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Save(ctx, Delivery{ChannelID: c.ID, SourceRef: "same", Body: "duplicate", Mode: "draft"}); e == nil {
		t.Fatal("duplicate accepted")
	}
	if _, e = s.Save(ctx, Delivery{ID: id, Version: 0, ChannelID: c.ID, Body: "stale", Mode: "draft"}); e != ErrChanged {
		t.Fatalf("stale edit %v", e)
	}
	_, e = s.Save(ctx, Delivery{ChannelID: c.ID, Body: "queued", Mode: "queue"})
	if e != nil {
		t.Fatal(e)
	}
	c.Slots = nil
	if e = s.SaveChannel(ctx, c); e == nil {
		t.Fatal("removed occupied schedule")
	}
	var slots []Slot
	if e = s.DB.QueryRow(ctx, `SELECT slots FROM publishing_channels WHERE id=$1`, c.ID).Scan(&slots); e != nil || len(slots) == 0 {
		t.Fatal("schedule rollback failed", e)
	}
}
func TestWorkerRecoveryAndReplyCheckpoint(t *testing.T) {
	s := testStore(t)
	c := fixtureChannel(t, s)
	ctx := context.Background()
	key := []byte(strings.Repeat("x", 32))
	raw, _ := json.Marshal(oauth2.Token{AccessToken: "test-only", TokenType: "Bearer", Expiry: time.Now().Add(time.Hour)})
	encrypted, _ := secureblob.Encrypt(raw, key)
	_, e := s.DB.Exec(ctx, `UPDATE publishing_channels SET paused=false,credentials=$2 WHERE id=$1`, c.ID, encrypted)
	if e != nil {
		t.Fatal(e)
	}
	id, e := s.Save(ctx, Delivery{ChannelID: c.ID, Body: "root", Reply: "reply", Mode: "queue"})
	if e != nil {
		t.Fatal(e)
	}
	_, e = s.DB.Exec(ctx, `UPDATE publishing_deliveries SET scheduled_at=now()-interval '1 minute' WHERE id=$1`, id)
	if e != nil {
		t.Fatal(e)
	}
	var calls atomic.Int32
	var failReply atomic.Bool
	failReply.Store(true)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := calls.Add(1)
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		if n == 1 {
			if body["text"] != "root" {
				t.Error("wrong root")
			}
			w.Write([]byte(`{"data":{"id":"111"}}`))
			return
		}
		if body["text"] != "reply" || body["reply"] == nil {
			t.Error("root was repeated")
		}
		if failReply.Load() {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(`{"data":{"id":"222"}}`))
	}))
	defer server.Close()
	worker := Worker{Store: s, OAuth: xapi.Config("test", "secret", ""), Key: key, X: xapi.Client{BaseURL: server.URL, HTTP: server.Client()}}
	if e = worker.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	d, e := scanDelivery(s.DB.QueryRow(ctx, `SELECT `+deliveryColumns+` FROM publishing_deliveries WHERE id=$1`, id))
	if e != nil {
		t.Fatal(e)
	}
	if d.State != "needs_review" || d.RootID != "111" || calls.Load() != 2 {
		t.Fatalf("unsafe state: %+v calls=%d", d, calls.Load())
	}
	if e = worker.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	if calls.Load() != 2 {
		t.Fatal("ambiguous POST retried automatically")
	}
	if e = s.Action(ctx, id, d.Version, "confirm_not_sent", ""); e != nil {
		t.Fatal(e)
	}
	_, _ = s.DB.Exec(ctx, `UPDATE publishing_deliveries SET scheduled_at=now()-interval '1 minute' WHERE id=$1`, id)
	failReply.Store(false)
	if e = worker.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	d, e = scanDelivery(s.DB.QueryRow(ctx, `SELECT `+deliveryColumns+` FROM publishing_deliveries WHERE id=$1`, id))
	if e != nil || d.State != "published" || calls.Load() != 3 {
		t.Fatalf("resume failed %+v %v", d, e)
	}
	// A crashed process that began POSTing requires review on recovery.
	_, _ = s.DB.Exec(ctx, `UPDATE publishing_deliveries SET state='sending',lease_until=now()-interval '1 minute',started_at=now() WHERE id=$1`, id)
	if e = worker.RunOnce(ctx); e != nil {
		t.Fatal(e)
	}
	var state string
	_ = s.DB.QueryRow(ctx, `SELECT state FROM publishing_deliveries WHERE id=$1`, id).Scan(&state)
	if state != "needs_review" || calls.Load() != 3 {
		t.Fatal("crash replayed the POST")
	}
}

func TestRetiredChannelsStayOutOfCalendarAndQueue(t *testing.T) {
	s := testStore(t)
	c := fixtureChannel(t, s)
	ctx := context.Background()
	legacyID := "buffer:" + c.ID
	at := SlotsBetween("UTC", c.Slots, time.Now().Add(time.Minute), time.Now().AddDate(0, 0, 7))[0]
	_, err := s.DB.Exec(ctx, `INSERT INTO publishing_channels(id,platform,name,provider,remote_id,slots) VALUES($1,'x','Legacy','buffer',$2,$3)`, legacyID, c.ID, c.Slots)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = s.DB.Exec(ctx, `DELETE FROM publishing_external WHERE channel_id=$1`, legacyID)
		_, _ = s.DB.Exec(ctx, `DELETE FROM publishing_channels WHERE id=$1`, legacyID)
	})
	_, err = s.DB.Exec(ctx, `INSERT INTO publishing_external(provider,remote_id,channel_id,title,state,starts_at) VALUES('buffer',$1,$1,'Legacy post','scheduled',$2)`, legacyID, at)
	if err != nil {
		t.Fatal(err)
	}
	channels, err := s.Channels(ctx)
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range channels {
		if ch.ID == legacyID {
			t.Fatal("retired account exposed as an active channel")
		}
	}
	entries, err := s.Calendar(ctx, at.Add(-time.Hour), at.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.ChannelID == legacyID {
			t.Fatal("retired post exposed in the active calendar")
		}
	}
	if _, err = s.Save(ctx, Delivery{ChannelID: legacyID, Body: "Must not send", Mode: "draft"}); err == nil {
		t.Fatal("retired channel accepted a delivery")
	}
	if err = s.ReplaceExternal(ctx, "buffer", nil, nil); err == nil {
		t.Fatal("retired provider accepted a new import")
	}
	var n int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM publishing_external WHERE channel_id=$1`, legacyID).Scan(&n); err != nil || n != 1 {
		t.Fatalf("historical record lost: %d, %v", n, err)
	}
	id, err := s.Save(ctx, Delivery{ChannelID: c.ID, Body: "New direct post", Mode: "queue"})
	if err != nil {
		t.Fatal(err)
	}
	var got time.Time
	if err = s.DB.QueryRow(ctx, `SELECT scheduled_at FROM publishing_deliveries WHERE id=$1`, id).Scan(&got); err != nil {
		t.Fatal(err)
	}
	if !got.Equal(at) {
		t.Fatal("retired snapshot still controls the local queue")
	}
}

func TestStreamSnapshotAtomicity(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	// This integration test uses an isolated database; preserve any preview fixture.
	// Read the existing streamer projection before replacement so the test can restore it.
	rows, err := s.DB.Query(ctx, `SELECT remote_id,channel_id,conference_tag,title,state,starts_at,ends_at,url FROM publishing_external WHERE provider='streamer'`)
	if err != nil {
		t.Fatal(err)
	}
	var previous []Entry
	for rows.Next() {
		var e Entry
		if err = rows.Scan(&e.ID, &e.ChannelID, &e.Conference, &e.Title, &e.State, &e.At, &e.End, &e.URL); err != nil {
			t.Fatal(err)
		}
		previous = append(previous, e)
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := s.ReplaceExternal(ctx, "streamer", nil, previous); err != nil {
			t.Error(err)
		}
	})
	at := time.Now().Add(time.Hour)
	if err = s.ReplaceExternal(ctx, "streamer", nil, []Entry{{ID: "atomicity", ChannelID: "broadcasts", Title: "Stream", State: "scheduled", At: &at}}); err != nil {
		t.Fatal(err)
	}
	if err = s.ReplaceExternal(ctx, "streamer", nil, []Entry{{ID: "invalid", ChannelID: "missing", Title: "Invalid"}}); err == nil {
		t.Fatal("invalid snapshot accepted")
	}
	var n int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM publishing_external WHERE provider='streamer' AND remote_id='atomicity'`).Scan(&n); err != nil || n != 1 {
		t.Fatalf("failed snapshot erased stream: %d, %v", n, err)
	}
}
