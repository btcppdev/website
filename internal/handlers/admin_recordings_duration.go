package handlers

import (
	"fmt"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
)

// Resolve missing runtimes for both the event being edited and existing
// reservations elsewhere. YouTube metadata is batched and cached locally.
func refreshRecordingScheduleDurations(ctx *config.AppContext, rows []*RecordingRow) error {
	reservations, err := getters.ListRecordingPublishReservations(ctx)
	if err != nil {
		return err
	}
	seen := make(map[string]bool)
	allRows := append([]*RecordingRow(nil), rows...)
	for _, row := range rows {
		if row != nil && row.Recording != nil {
			seen[row.Recording.ID] = true
		}
	}
	for _, reservation := range reservations {
		if reservation.RecordingID == "" || reservation.DurationSeconds > 0 || seen[reservation.RecordingID] {
			continue
		}
		seen[reservation.RecordingID] = true
		rec, err := getters.GetRecordingByID(ctx, reservation.RecordingID)
		if err != nil {
			return err
		}
		if rec != nil {
			allRows = append(allRows, &RecordingRow{Recording: rec})
		}
	}
	enrichRowsWithYouTubeStatus(ctx, allRows)
	return nil
}

func nextRecordingPublishTimes(slots []*types.YouTubePublishSlot, occupied []getters.RecordingPublishReservation, after time.Time, rows []*RecordingRow) ([]time.Time, error) {
	reservations := append([]getters.RecordingPublishReservation(nil), occupied...)
	cursor := after
	limit := after.AddDate(2, 0, 0)
	assignments := make([]time.Time, 0, len(rows))
	for _, row := range rows {
		if row == nil || row.Recording == nil || row.Recording.DurationSeconds <= 0 {
			return nil, fmt.Errorf("recording runtime is required before scheduling")
		}
		for {
			times, err := nextYouTubePublishTimes(slots, map[int64]bool{}, cursor, 1)
			if err != nil {
				return nil, err
			}
			candidate := times[0]
			if candidate.After(limit) {
				return nil, fmt.Errorf("no non-overlapping publishing slots available in the next two years")
			}
			cursor = candidate
			conflict, err := getters.RecordingScheduleConflictForRecording(row.Recording.ID, candidate, row.Recording.DurationSeconds, reservations)
			if err != nil {
				return nil, err
			}
			if conflict != nil {
				continue
			}
			assignments = append(assignments, candidate)
			reservations = append(reservations, getters.RecordingPublishReservation{
				RecordingID: row.Recording.ID, Title: row.Recording.TalkName,
				PublishAt: candidate, DurationSeconds: row.Recording.DurationSeconds,
			})
			break
		}
	}
	return assignments, nil
}
