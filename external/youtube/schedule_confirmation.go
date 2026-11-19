package youtube

import (
	"context"
	"fmt"
	"time"
)

// MatchesSchedule compares the provider's state with the requested operation.
// Clearing a schedule must also restore unlisted visibility.
func (s *VideoStatus) MatchesSchedule(at *time.Time) bool {
	if s == nil {
		return false
	}
	if at == nil {
		return s.PublishAt == nil && s.PrivacyStatus == "unlisted"
	}
	return s.PrivacyStatus == "private" && s.PublishAt != nil && s.PublishAt.Equal(*at)
}

// WaitForVideoSchedule tolerates YouTube reads briefly lagging behind an update.
// It never repeats the update itself.
func WaitForVideoSchedule(ctx context.Context, id string, at *time.Time) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	return waitForVideoSchedule(ctx, id, at, []time.Duration{0, time.Second, 2 * time.Second, 4 * time.Second, 8 * time.Second}, GetVideoStatus)
}

func waitForVideoSchedule(ctx context.Context, id string, at *time.Time, delays []time.Duration, get func(context.Context, string) (*VideoStatus, error)) error {
	var lastErr error
	for _, delay := range delays {
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		case <-timer.C:
		}
		status, err := get(ctx, id)
		if err == nil && status.MatchesSchedule(at) {
			return nil
		}
		lastErr = err
	}
	if lastErr != nil {
		return fmt.Errorf("Could not confirm YouTube schedule: %w", lastErr)
	}
	if at == nil {
		return fmt.Errorf("YouTube has not confirmed the cleared schedule")
	}
	return fmt.Errorf("YouTube has not confirmed the requested publication time")
}
