package handlers

import "btcpp-web/internal/config"

func attachRecordingPublishChanges(app *config.AppContext, rows []*RecordingRow) error {
	byID := map[string]*RecordingRow{}
	ids := []string{}
	for _, row := range rows {
		if row != nil && row.Recording != nil {
			byID[row.Recording.ID] = row
			ids = append(ids, row.Recording.ID)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	changes, err := app.DB.Query(app.DatabaseContext(), `SELECT recording_id::text,target_at,state,error FROM recording_schedule_changes
 WHERE recording_id=ANY($1::uuid[]) AND channel_id='youtube' AND state<>'applied'`, ids)
	if err != nil {
		return err
	}
	defer changes.Close()
	for changes.Next() {
		var id string
		var change RecordingRow
		if err = changes.Scan(&id, &change.YTRequestedAt, &change.YTScheduleState, &change.YTScheduleError); err != nil {
			return err
		}
		if row := byID[id]; row != nil {
			row.YTRequestedAt = change.YTRequestedAt
			row.YTScheduleState = change.YTScheduleState
			row.YTScheduleError = change.YTScheduleError
		}
	}
	return changes.Err()
}
