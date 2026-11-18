package publishing

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
)

func readDelivery(t *testing.T, s Store, id string) Delivery {
	t.Helper()
	d, err := scanDelivery(s.DB.QueryRow(context.Background(), `SELECT `+deliveryColumns+` FROM publishing_deliveries WHERE id=$1`, id))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
func TestCampaignHasIndependentChannelItems(t *testing.T) {
	s := testStore(t)
	a := fixtureChannel(t, s)
	b := fixtureChannel(t, s)
	ctx := context.Background()
	body := Delivery{Body: "Shared announcement", Title: "Conference news", Mode: "queue", SourceRef: "composer:" + uuid.NewString()}
	ids, err := s.SaveCampaign(ctx, body, []string{a.ID, b.ID})
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 2 {
		t.Fatal(ids)
	}
	first, second := readDelivery(t, s, ids[0]), readDelivery(t, s, ids[1])
	if first.CampaignID != second.CampaignID || first.ChannelID == second.ChannelID || first.ID == second.ID {
		t.Fatal("destinations are not separate deliveries in one campaign")
	}
	otherID, err := s.Save(ctx, Delivery{ChannelID: first.ChannelID, Body: "Keep this time", Mode: "queue"})
	if err != nil {
		t.Fatal(err)
	}
	// Creating another queue item can legitimately reflow; use current versions.
	first = readDelivery(t, s, first.ID)
	other := readDelivery(t, s, otherID)
	slots, err := s.AvailableSlots(ctx, first.ID, time.Now())
	if err != nil || len(slots) < 2 {
		t.Fatalf("slots: %v %v", slots, err)
	}
	target := *slots[1].At
	if err = s.MoveToSlot(ctx, first.ID, first.Version, first.ChannelID, target); err != nil {
		t.Fatal(err)
	}
	moved := readDelivery(t, s, first.ID)
	untouched := readDelivery(t, s, second.ID)
	sameAccount := readDelivery(t, s, otherID)
	if !moved.At.Equal(target) || moved.Mode != "fixed" || moved.CampaignID != first.CampaignID {
		t.Fatal("move did not pin the selected delivery")
	}
	if !untouched.At.Equal(*second.At) || untouched.Version != second.Version || untouched.Body != second.Body {
		t.Fatal("moving one channel changed its sibling")
	}
	if !sameAccount.At.Equal(*other.At) || sameAccount.Version != other.Version {
		t.Fatal("move unexpectedly reflowed other posts")
	}
	if err = s.MoveToSlot(ctx, first.ID, first.Version, first.ChannelID, *slots[0].At); err != ErrChanged {
		t.Fatalf("stale edit accepted: %v", err)
	}
	if _, err = s.SaveCampaign(ctx, body, []string{a.ID, b.ID}); err == nil {
		t.Fatal("duplicate submission accepted")
	}
	var n int
	if err = s.DB.QueryRow(ctx, `SELECT count(*) FROM publishing_deliveries WHERE source_ref=$1`, body.SourceRef).Scan(&n); err != nil || n != 2 {
		t.Fatalf("duplicate or missing destinations: %d %v", n, err)
	}
}
func TestCampaignRollsBackEveryDestination(t *testing.T) {
	s := testStore(t)
	c := fixtureChannel(t, s)
	ctx := context.Background()
	ref := "composer:" + uuid.NewString()
	if _, err := s.SaveCampaign(ctx, Delivery{Body: "Atomic", Mode: "draft", SourceRef: ref}, []string{c.ID, "zz-missing"}); err == nil {
		t.Fatal("missing destination accepted")
	}
	var n int
	if err := s.DB.QueryRow(ctx, `SELECT count(*) FROM publishing_deliveries WHERE source_ref=$1`, ref).Scan(&n); err != nil || n != 0 {
		t.Fatalf("partial campaign: %d %v", n, err)
	}
	if _, err := s.SaveCampaign(ctx, Delivery{Body: "Duplicate", Mode: "draft"}, []string{c.ID, c.ID}); err == nil {
		t.Fatal("duplicate account accepted")
	}
}
func TestMoveOnlyAcceptsFreeConfiguredSameChannelSlots(t *testing.T) {
	s := testStore(t)
	c := fixtureChannel(t, s)
	other := fixtureChannel(t, s)
	ctx := context.Background()
	id, err := s.Save(ctx, Delivery{ChannelID: c.ID, Body: "Move me", Mode: "queue"})
	if err != nil {
		t.Fatal(err)
	}
	occupied, err := s.Save(ctx, Delivery{ChannelID: c.ID, Body: "Occupied queue slot", Mode: "queue"})
	if err != nil {
		t.Fatal(err)
	}
	d := readDelivery(t, s, id)
	taken := readDelivery(t, s, occupied)
	slots, err := s.AvailableSlots(ctx, id, time.Now())
	if err != nil || len(slots) == 0 {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, channel string
		at            time.Time
	}{
		{"other channel", other.ID, *slots[0].At},
		{"arbitrary time", c.ID, slots[0].At.Add(5 * time.Minute)},
		{"occupied slot", c.ID, *taken.At},
		{"past slot", c.ID, time.Now().Add(-time.Hour)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if err := s.MoveToSlot(ctx, id, d.Version, tc.channel, tc.at); err == nil {
				t.Fatal("invalid move accepted")
			}
			got := readDelivery(t, s, id)
			if !got.At.Equal(*d.At) || got.Version != d.Version {
				t.Fatal("rejected move changed delivery")
			}
		})
	}
	for _, state := range []string{"sending", "published", "needs_review", "canceled"} {
		if _, err = s.DB.Exec(ctx, `UPDATE publishing_deliveries SET state=$2 WHERE id=$1`, id, state); err != nil {
			t.Fatal(err)
		}
		if err = s.MoveToSlot(ctx, id, d.Version, c.ID, *slots[0].At); err != ErrChanged {
			t.Fatalf("moved %s item: %v", state, err)
		}
	}
	if _, err = s.DB.Exec(ctx, `UPDATE publishing_deliveries SET state='scheduled',root_id='already-posted' WHERE id=$1`, id); err != nil {
		t.Fatal(err)
	}
	if err = s.MoveToSlot(ctx, id, d.Version, c.ID, *slots[0].At); err != ErrChanged {
		t.Fatal("partially published item moved")
	}
}
func TestConcurrentMovesCannotClaimOneSlot(t *testing.T) {
	s := testStore(t)
	c := fixtureChannel(t, s)
	ctx := context.Background()
	var ids []string
	for i := 0; i < 2; i++ {
		id, err := s.Save(ctx, Delivery{ChannelID: c.ID, Body: "Concurrent move", Mode: "queue"})
		if err != nil {
			t.Fatal(err)
		}
		ids = append(ids, id)
	}
	slots, err := s.AvailableSlots(ctx, ids[0], time.Now())
	if err != nil || len(slots) == 0 {
		t.Fatal(err)
	}
	var success atomic.Int32
	var wg sync.WaitGroup
	for _, id := range ids {
		d := readDelivery(t, s, id)
		wg.Add(1)
		go func() {
			defer wg.Done()
			if s.MoveToSlot(ctx, d.ID, d.Version, c.ID, *slots[0].At) == nil {
				success.Add(1)
			}
		}()
	}
	wg.Wait()
	if success.Load() != 1 {
		t.Fatalf("accepted %d concurrent moves", success.Load())
	}
}
func TestPublicationSlotTimezoneAndDST(t *testing.T) {
	slots := []Slot{{Day: 0, Time: "02:30"}}
	gap := time.Date(2027, 3, 14, 8, 30, 0, 0, time.UTC)
	if IsPublicationSlot("America/Chicago", slots, gap) {
		t.Fatal("nonexistent spring-forward slot accepted")
	}
	slots = []Slot{{Day: 0, Time: "01:30"}}
	start := time.Date(2027, 11, 7, 0, 0, 0, 0, time.UTC)
	candidates := SlotsBetween("America/Chicago", slots, start, start.Add(24*time.Hour))
	if len(candidates) != 1 || !IsPublicationSlot("America/Chicago", slots, candidates[0]) {
		t.Fatal("configured fall-back occurrence rejected")
	}
	if IsPublicationSlot("America/Chicago", slots, candidates[0].Add(time.Hour)) {
		t.Fatal("second fall-back occurrence accepted")
	}
	if IsPublicationSlot("America/Chicago", slots, candidates[0].Add(time.Second)) {
		t.Fatal("non-slot seconds accepted")
	}
}
