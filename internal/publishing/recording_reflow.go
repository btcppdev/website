package publishing

import (
	"context"
	"fmt"
	"time"

	"btcpp-web/external/getters"
)

func recordingDestinationID(recordingID, channel string) string {
	switch channel {
	case "youtube":
		return "youtube:" + recordingID
	case "broadcasts":
		return "broadcast:" + recordingID
	default:
		return "" // Live/unidentified reservations must never be released by a batch.
	}
}

func recordingReservationsOutsideBatch(reservations []getters.RecordingPublishReservation, selected map[string]bool) []getters.RecordingPublishReservation {
	out := make([]getters.RecordingPublishReservation, 0, len(reservations))
	for _, r := range reservations {
		if !selected[recordingDestinationID(r.RecordingID, r.ChannelID)] {
			out = append(out, r)
		}
	}
	return out
}

// Plan a sequence that can be applied without overlapping confirmed schedules.
// Usually moves simply run in dependency order. For a cycle (A takes B's time,
// B takes A's), reserve a spare future slot for one member before its final move.
// This keeps both provider identities and visibility intact, and persists enough
// information for failures/restarts to retain every potentially occupied time.
func planRecordingTransitions(ctx context.Context, db recordingDB, moves []RecordingMove, items map[string]RecordingItem, reservations []getters.RecordingPublishReservation) (map[string]*time.Time, error) {
	selected := map[string]bool{}
	for _, m := range moves {
		selected[m.ID] = true
	}
	physical := make([]getters.RecordingPublishReservation, 0, len(reservations))
	for _, r := range reservations {
		if !r.Requested || !selected[recordingDestinationID(r.RecordingID, r.ChannelID)] {
			physical = append(physical, r)
		}
	}
	final := []getters.RecordingPublishReservation{}
	for _, m := range moves {
		if m.At != nil {
			d := items[m.ID]
			final = append(final, getters.RecordingPublishReservation{RecordingID: d.RecordingID, PublishAt: *m.At, DurationSeconds: d.DurationSeconds})
		}
	}
	advance := func(m RecordingMove, at *time.Time) {
		physical = recordingReservationsOutsideBatch(physical, map[string]bool{m.ID: true})
		if at != nil {
			d := items[m.ID]
			physical = append(physical, getters.RecordingPublishReservation{RecordingID: d.RecordingID, ChannelID: d.ChannelID, PublishAt: *at, DurationSeconds: d.DurationSeconds})
		}
	}
	staging := map[string]*time.Time{}
	pending := append([]RecordingMove(nil), moves...)
	for len(pending) > 0 {
		progress := false
		for i, m := range pending {
			d := items[m.ID]
			if m.At != nil {
				conflict, err := getters.RecordingScheduleConflictForRecording(d.RecordingID, *m.At, d.DurationSeconds, physical)
				if err != nil {
					return nil, err
				}
				if conflict != nil {
					continue
				}
			}
			advance(m, m.At)
			pending = append(pending[:i], pending[i+1:]...)
			progress = true
			break
		}
		if progress {
			continue
		}
		// All remaining items depend on another selected item. Pick a spare
		// configured time beyond both the old and reviewed final schedules.
		var candidate *RecordingMove
		for i := range pending {
			if staging[pending[i].ID] == nil {
				candidate = &pending[i]
				break
			}
		}
		if candidate == nil {
			return nil, fmt.Errorf("Could not safely reorder this batch; refresh the schedule")
		}
		d := items[candidate.ID]
		after := time.Now().Add(10 * time.Minute)
		for _, group := range [][]getters.RecordingPublishReservation{physical, final} {
			for _, r := range group {
				if until := r.ReservedUntil(); until.After(after) {
					after = until
				}
			}
		}
		slots, err := recordingSlots(ctx, db, d.ChannelID, after, after.AddDate(1, 0, 0))
		if err != nil {
			return nil, err
		}
		if len(slots) == 0 {
			return nil, fmt.Errorf("Add a future %s slot before swapping these recordings", d.ChannelID)
		}
		at := slots[0]
		staging[candidate.ID] = &at
		final = append(final, getters.RecordingPublishReservation{RecordingID: d.RecordingID, PublishAt: at, DurationSeconds: d.DurationSeconds})
		advance(*candidate, &at)
	}
	return staging, nil
}
