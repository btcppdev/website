package expedition

import "time"

// DueAt's UTC date is the contract's calendar date, not a midnight payment cutoff.
// Deadline is exclusive: guests have the entire named date in the policy timezone.
func InstallmentDeadline(due time.Time, zone string) (time.Time, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, err
	}
	y, m, d := due.UTC().Date()
	return time.Date(y, m, d+1, 0, 0, 0, 0, loc), nil
}

func InstallmentReminderAt(due time.Time, zone string, offset int) (time.Time, error) {
	loc, err := time.LoadLocation(zone)
	if err != nil {
		return time.Time{}, err
	}
	y, m, d := due.UTC().Date()
	return time.Date(y, m, d+offset, 9, 0, 0, 0, loc), nil
}

// LatestReminder intentionally skips obsolete reminders after downtime instead
// of delivering a burst of historical notices. Subsequent runs retry the same event.
func LatestReminder(due, now time.Time, zone string) (int, bool, error) {
	offsets := []int{-14, -4, 0, 2, 4, 6, 8, 10, 12, 14, 16, 18, 20, 22, 24, 26, 28}
	result, ok := 0, false
	for _, offset := range offsets {
		at, err := InstallmentReminderAt(due, zone, offset)
		if err != nil {
			return 0, false, err
		}
		if !now.Before(at) {
			result, ok = offset, true
		}
	}
	return result, ok, nil
}
