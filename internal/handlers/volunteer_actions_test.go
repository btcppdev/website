package handlers

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"btcpp-web/internal/types"
)

func TestVolunteerStatusLifecycle(t *testing.T) {
	tests := []struct {
		name, from, to string
		shifts         int
		want           []string
		fail           bool
	}{
		{"individual invitation", "Applied", "PendingShifts", 0, []string{"update:PendingShifts", "notify:PendingShifts"}, false},
		{"waitlisted invitation", "Waitlist", "PendingShifts", 0, []string{"update:PendingShifts", "notify:PendingShifts"}, false},
		{"waitlist notification", "Applied", "Waitlist", 0, []string{"update:Waitlist", "notify:Waitlist"}, false},
		{"dropdown schedules full lifecycle", "PendingShifts", "Scheduled", 1, []string{"schedule"}, false},
		{"no shifts cannot schedule", "PendingShifts", "Scheduled", 0, nil, true},
		{"declined cannot schedule", "Declined", "Scheduled", 3, nil, true},
		{"applied cannot schedule", "Applied", "Scheduled", 3, nil, true},
		{"cancel scheduled", "Scheduled", "Declined", 3, []string{"leave", "update:Declined", "notify:Declined"}, false},
		{"decline pending", "PendingShifts", "Declined", 2, []string{"leave", "update:Declined", "notify:Declined"}, false},
		{"reopen", "Declined", "Applied", 0, []string{"update:Applied"}, false},
		{"demote scheduled", "Scheduled", "PendingShifts", 3, []string{"leave", "update:PendingShifts", "notify:PendingShifts"}, false},
		{"repeat update does not resend", "PendingShifts", "PendingShifts", 0, nil, false},
		{"repeat scheduled does not reissue", "Scheduled", "Scheduled", 3, nil, false},
		{"invalid status", "Applied", "Typo", 0, nil, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			vol := &types.Volunteer{Status: tt.from, WorkShifts: make([]*types.WorkShift, tt.shifts)}
			var calls []string
			err := applyVolunteerStatus(vol, tt.to, volunteerStatusActions{
				update: func(s string) error { calls = append(calls, "update:"+s); return nil },
				notify: func(s string) error {
					if vol.Status != s {
						t.Error("notification received stale volunteer status")
					}
					calls = append(calls, "notify:"+s)
					return nil
				},
				schedule: func() error { calls = append(calls, "schedule"); return nil },
				leave:    func() error { calls = append(calls, "leave"); return nil },
			})
			if (err != nil) != tt.fail {
				t.Fatalf("error = %v", err)
			}
			if !reflect.DeepEqual(calls, tt.want) {
				t.Fatalf("actions = %v; want %v", calls, tt.want)
			}
		})
	}
}

func TestVolunteerStatusFailuresAreReported(t *testing.T) {
	broken := errors.New("service unavailable")
	for _, step := range []string{"update", "notify", "leave", "schedule"} {
		t.Run(step, func(t *testing.T) {
			vol := &types.Volunteer{Status: "Applied", WorkShifts: []*types.WorkShift{{Ref: "s"}}}
			target := "PendingShifts"
			if step == "leave" {
				vol.Status = "Scheduled"
				target = "Declined"
			}
			if step == "schedule" {
				vol.Status = "PendingShifts"
				target = "Scheduled"
			}
			original := vol.Status
			notified := false
			err := applyVolunteerStatus(vol, target, volunteerStatusActions{
				update: func(string) error {
					if step == "update" {
						return broken
					}
					return nil
				},
				notify: func(string) error {
					notified = true
					if step == "notify" {
						return broken
					}
					return nil
				},
				leave: func() error {
					if step == "leave" {
						return broken
					}
					return nil
				},
				schedule: func() error { return broken },
			})
			if !errors.Is(err, broken) {
				t.Fatalf("missing failure: %v", err)
			}
			if step == "notify" {
				if vol.Status != target || !strings.Contains(err.Error(), "status saved") {
					t.Fatalf("must report partial success: %v, %s", err, vol.Status)
				}
			} else if notified || vol.Status != original {
				t.Fatalf("continued after failure: notified=%v status=%s", notified, vol.Status)
			}
		})
	}
}

func TestVolunteerEventBoundaries(t *testing.T) {
	conf := &types.Conf{Ref: "berlin"}
	vol := &types.Volunteer{ScheduleFor: []*types.Conf{nil, {Ref: "other"}}}
	if volunteerBelongsToConf(vol, conf) {
		t.Fatal("accepted volunteer from another event")
	}
	vol.ScheduleFor = append(vol.ScheduleFor, conf)
	if !volunteerBelongsToConf(vol, conf) {
		t.Fatal("rejected matching event")
	}
	if volunteerShiftInEvent([]*types.WorkShift{nil, {Ref: "one"}}, "two") {
		t.Fatal("accepted shift outside event")
	}
}
