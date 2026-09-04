package handlers

import (
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestCanSelfScheduleVolunteer(t *testing.T) {
	start := time.Date(2026, time.October, 1, 9, 0, 0, 0, time.UTC)
	open := func() *types.WorkShift {
		return &types.WorkShift{MaxVols: 2, AssigneesRef: []string{"one"}, ShiftTime: &types.Times{Start: start}}
	}
	full := &types.WorkShift{MaxVols: 1, AssigneesRef: []string{"one"}, ShiftTime: &types.Times{Start: start}}
	unscheduled := &types.WorkShift{MaxVols: 2}

	tests := []struct {
		name   string
		conf   *types.Conf
		shifts []*types.WorkShift
		want   bool
	}{
		{name: "setting disabled", conf: &types.Conf{}, shifts: []*types.WorkShift{open(), open(), open()}},
		{name: "no shifts", conf: &types.Conf{VolunteerSelfSchedule: true}},
		{name: "unscheduled shift", conf: &types.Conf{VolunteerSelfSchedule: true}, shifts: []*types.WorkShift{unscheduled}},
		{name: "only full shifts", conf: &types.Conf{VolunteerSelfSchedule: true}, shifts: []*types.WorkShift{full}},
		{name: "too few open shifts", conf: &types.Conf{VolunteerSelfSchedule: true}, shifts: []*types.WorkShift{open(), open()}},
		{name: "enough open shifts", conf: &types.Conf{VolunteerSelfSchedule: true}, shifts: []*types.WorkShift{full, open(), open(), open()}, want: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := canSelfScheduleVolunteer(test.conf, test.shifts); got != test.want {
				t.Fatalf("canSelfScheduleVolunteer() = %t, want %t", got, test.want)
			}
		})
	}
}
