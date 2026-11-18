package publishing

import (
	"context"
	"fmt"
	"sort"
	"time"

	"github.com/google/uuid"
)

// SaveCampaign creates one independent delivery for each selected destination.
// All destinations succeed together; retries cannot create a partial campaign.
func (s Store) SaveCampaign(ctx context.Context, d Delivery, channelIDs []string) ([]string, error) {
	if d.ID != "" {
		return nil, fmt.Errorf("Edit each channel's post separately")
	}
	if len(channelIDs) == 0 || len(channelIDs) > 20 {
		return nil, fmt.Errorf("Choose between one and 20 accounts")
	}
	ids := append([]string(nil), channelIDs...)
	sort.Strings(ids) // Same lock order as other multi-account submissions.
	for i, id := range ids {
		if id == "" || (i > 0 && ids[i-1] == id) {
			return nil, fmt.Errorf("Choose each account only once")
		}
	}
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback(ctx)
	d.CampaignID = uuid.NewString()
	var deliveries []string
	for _, channelID := range ids {
		copy := d
		copy.ChannelID = channelID
		id, err := saveDelivery(ctx, tx, copy)
		if err != nil {
			return nil, err
		}
		deliveries = append(deliveries, id)
	}
	return deliveries, tx.Commit(ctx)
}

func movable(d Delivery) bool { return d.State == "scheduled" && d.RootID == "" && d.At != nil }

// IsPublicationSlot compares exact generated instants, including the channel's
// timezone and DST policy. An arbitrary time on the same day is not a slot.
func IsPublicationSlot(zone string, slots []Slot, at time.Time) bool {
	for _, candidate := range SlotsBetween(zone, slots, at.Add(-time.Second), at.Add(time.Second)) {
		if candidate.Equal(at) {
			return true
		}
	}
	return false
}

// MoveToSlot pins only the selected delivery. Other campaign destinations and
// other queued items retain their times; the released slot becomes available.
func (s Store) MoveToSlot(ctx context.Context, id string, version int, targetChannel string, at time.Time) error {
	tx, err := s.DB.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var channelID string
	if err = tx.QueryRow(ctx, `SELECT channel_id FROM publishing_deliveries WHERE id=$1`, id).Scan(&channelID); err != nil {
		return err
	}
	if channelID != targetChannel {
		return fmt.Errorf("Choose a publication slot for this same account")
	}
	if err = lockChannel(ctx, tx, channelID); err != nil {
		return err
	}
	d, err := scanDelivery(tx.QueryRow(ctx, `SELECT `+deliveryColumns+` FROM publishing_deliveries WHERE id=$1 FOR UPDATE`, id))
	if err != nil {
		return err
	}
	if d.Version != version || !movable(d) {
		return ErrChanged
	}
	var zone, provider, platform string
	var slots []Slot
	if err = tx.QueryRow(ctx, `SELECT timezone,slots,provider,platform FROM publishing_channels WHERE id=$1`, channelID).Scan(&zone, &slots, &provider, &platform); err != nil {
		return err
	}
	if provider != "direct" || platform != "x" {
		return fmt.Errorf("This item must be scheduled through its recording")
	}
	if !at.After(time.Now().Add(time.Minute)) {
		return fmt.Errorf("Choose a future publication slot")
	}
	if !IsPublicationSlot(zone, slots, at) {
		return fmt.Errorf("That publication slot is no longer configured for this account")
	}
	var occupied bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM publishing_deliveries WHERE channel_id=$1 AND id<>$2 AND scheduled_at=$3 AND state IN ('scheduled','sending'))`, channelID, id, at).Scan(&occupied); err != nil {
		return err
	}
	if occupied {
		return fmt.Errorf("That publication slot is already occupied; choose another slot")
	}
	_, err = tx.Exec(ctx, `UPDATE publishing_deliveries SET scheduled_at=$2,mode='fixed',version=version+1,updated_at=now() WHERE id=$1`, id, at)
	if err != nil {
		return err
	}
	return tx.Commit(ctx)
}

// AvailableSlots powers the same move operation for touch and keyboard users.
func (s Store) AvailableSlots(ctx context.Context, id string, now time.Time) ([]Entry, error) {
	d, err := scanDelivery(s.DB.QueryRow(ctx, `SELECT `+deliveryColumns+` FROM publishing_deliveries WHERE id=$1`, id))
	if err != nil {
		return nil, err
	}
	if !movable(d) {
		return nil, ErrChanged
	}
	var zone string
	var slots []Slot
	if err = s.DB.QueryRow(ctx, `SELECT timezone,slots FROM publishing_channels WHERE id=$1 AND provider='direct' AND platform='x'`, d.ChannelID).Scan(&zone, &slots); err != nil {
		return nil, err
	}
	rows, err := s.DB.Query(ctx, `SELECT scheduled_at FROM publishing_deliveries WHERE channel_id=$1 AND state IN ('scheduled','sending') AND scheduled_at IS NOT NULL`, d.ChannelID)
	if err != nil {
		return nil, err
	}
	used := map[int64]bool{}
	for rows.Next() {
		var at time.Time
		if err = rows.Scan(&at); err != nil {
			rows.Close()
			return nil, err
		}
		used[at.UnixNano()] = true
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return nil, err
	}
	out := []Entry{}
	for _, at := range SlotsBetween(zone, slots, now.Add(time.Minute), now.AddDate(0, 0, 90)) {
		if !used[at.UnixNano()] {
			at := at
			out = append(out, Entry{ID: d.ChannelID + at.Format(time.RFC3339), ChannelID: d.ChannelID, Kind: "slot", Title: "Publication slot", At: &at, Provider: "direct"})
		}
	}
	return out, nil
}
