package xstudio

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
)

// ScheduledBroadcastState uses the same complete-schedule marker as X Studio's
// live page. An absent row in an incomplete response is never proof of deletion.
func (c *Client) ScheduledBroadcastState(ctx context.Context, sessionID, broadcastID string) (string, error) {
	if strings.TrimSpace(broadcastID) == "" {
		return "", errors.New("broadcast ID is required")
	}
	var response envelope[struct {
		Rows *[]struct {
			BroadcastID string `json:"broadcastId"`
			Kind        string `json:"kind"`
			StateName   string `json:"stateName"`
		} `json:"rows"`
		ScheduledComplete bool `json:"scheduledComplete"`
	}]
	_, err := c.do(ctx, http.MethodGet, "/api/live/get-broadcasts?rwebShell=1&limit=100", sessionID, "", nil, &response)
	if err != nil {
		return "", err
	}
	if err = checkEnvelope(response.Error); err != nil {
		return "", err
	}
	if response.Value.Rows == nil {
		return "", errors.New("X Studio omitted broadcast rows")
	}
	for _, row := range *response.Value.Rows {
		if row.BroadcastID != broadcastID {
			continue
		}
		if row.Kind == "scheduled" && row.StateName == "NotStarted" {
			return "scheduled", nil
		}
		return "", errors.New("X broadcast is no longer an unstarted scheduled broadcast; check X Studio")
	}
	if !response.Value.ScheduledComplete {
		return "", errors.New("X Studio returned an incomplete schedule; cancellation cannot be confirmed")
	}
	return "absent", nil
}

// CancelScheduled deletes only the scheduled broadcast, never a live/replay
// broadcast. Reads before and after the mutation make retry safe and require
// provider confirmation even when a successful response has an empty value.
func (c *Client) CancelScheduled(ctx context.Context, sessionID, broadcastID string) error {
	state, err := c.ScheduledBroadcastState(ctx, sessionID, broadcastID)
	if err != nil || state == "absent" {
		return err
	}
	var response envelope[json.RawMessage]
	_, err = c.doJSON(ctx, http.MethodPost, "/api/live/delete-scheduled-broadcast?rwebShell=1", sessionID, struct {
		BroadcastID string `json:"broadcastId"`
	}{broadcastID}, &response)
	if err != nil {
		return err
	}
	if err = checkEnvelope(response.Error); err != nil {
		return err
	}
	state, err = c.ScheduledBroadcastState(ctx, sessionID, broadcastID)
	if err != nil {
		return err
	}
	if state != "absent" {
		return errors.New("X has not yet confirmed broadcast cancellation")
	}
	return nil
}
