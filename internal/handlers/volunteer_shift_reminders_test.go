package handlers

import (
	"testing"

	"btcpp-web/internal/types"
)

func TestVolNeedsShiftReminderTargetsOnlyPendingWithNoAssignments(t *testing.T) {
	assigned := &types.WorkShift{Ref: "shift-one", AssigneesRef: []string{"assigned-vol"}}
	shifts := []*types.WorkShift{assigned}

	tests := []struct {
		name string
		vol  *types.Volunteer
		want bool
	}{
		{name: "pending without shifts", vol: &types.Volunteer{Ref: "waiting-vol", Status: "PendingShifts"}, want: true},
		{name: "pending with shift", vol: &types.Volunteer{Ref: "assigned-vol", Status: "PendingShifts"}},
		{name: "applied without shifts", vol: &types.Volunteer{Ref: "applied-vol", Status: "Applied"}},
		{name: "scheduled without shifts", vol: &types.Volunteer{Ref: "scheduled-vol", Status: "Scheduled"}},
		{name: "nil volunteer"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := volNeedsShiftReminder(test.vol, shifts); got != test.want {
				t.Fatalf("volNeedsShiftReminder() = %v, want %v", got, test.want)
			}
		})
	}
}
