package publishing

import (
	"context"
	"github.com/google/uuid"
	"testing"
	"time"
)

func TestPastStreamsStayInHistoryAndInactiveStreamsDoNotNeedAttention(t *testing.T) {
	s := testStore(t)
	ctx := context.Background()
	past := time.Now().AddDate(0, 0, -20)
	prefix := uuid.NewString()
	for _, tc := range []struct {
		id, state string
		at        *time.Time
	}{{"past", "scheduled", &past}, {"unknown", "scheduled", nil}, {"disabled", "disabled", nil}, {"ended", "ended", nil}} {
		_, err := s.DB.Exec(ctx, `INSERT INTO publishing_external(provider,remote_id,channel_id,title,state,starts_at) VALUES('streamer',$1,'broadcasts',$1,$2,$3)`, prefix+tc.id, tc.state, tc.at)
		if err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() {
		s.DB.Exec(ctx, `DELETE FROM publishing_external WHERE provider='streamer' AND remote_id LIKE $1`, prefix+"%")
	})
	entries, err := s.Calendar(ctx, time.Now().Add(-time.Hour), time.Now().AddDate(0, 0, 7))
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, e := range entries {
		if len(e.ID) >= len("streamer:"+prefix) && e.ID[:len("streamer:"+prefix)] == "streamer:"+prefix {
			count++
			if e.ID != "streamer:"+prefix+"unknown" {
				t.Fatalf("historical/inactive stream in current attention tray: %+v", e)
			}
		}
	}
	if count != 1 {
		t.Fatalf("missing unknown active stream: %d", count)
	}
	entries, err = s.Calendar(ctx, past.Add(-time.Hour), past.Add(time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, e := range entries {
		if e.ID == "streamer:"+prefix+"past" {
			found = true
			if e.State != "past" || e.At == nil {
				t.Fatalf("bad history: %+v", e)
			}
		}
	}
	if !found {
		t.Fatal("past stream missing from history")
	}
}
