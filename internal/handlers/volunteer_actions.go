package handlers

import (
	"fmt"
	"net/http"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/ics"
	"btcpp-web/internal/types"
)

// Keep the status editor and bulk actions on the same lifecycle path.
// Dependencies are passed explicitly so failures and side effects can be tested
// without sending email or changing real volunteer records.
type volunteerStatusActions struct {
	update   func(string) error
	notify   func(string) error
	schedule func() error
	leave    func() error
}

func applyVolunteerStatus(vol *types.Volunteer, status string, actions volunteerStatusActions) error {
	switch status {
	case "Applied", "Waitlist", "PendingShifts", "Scheduled", "Declined":
	default:
		return fmt.Errorf("unknown volunteer status %q", status)
	}
	if status == vol.Status {
		return nil
	}
	if status == "Scheduled" {
		if vol.Status != "PendingShifts" || len(vol.WorkShifts) == 0 {
			return fmt.Errorf("scheduling requires Pending Shifts status and at least one assigned shift")
		}
		return actions.schedule()
	}
	// Leaving a scheduled role must not leave an active volunteer ticket or shifts.
	if status == "Declined" || vol.Status == "Scheduled" || ((status == "Applied" || status == "Waitlist") && len(vol.WorkShifts) > 0) {
		if err := actions.leave(); err != nil {
			return fmt.Errorf("volunteer cleanup failed; status unchanged: %w", err)
		}
	}
	if err := actions.update(status); err != nil {
		return err
	}
	vol.Status = status
	switch status {
	case "PendingShifts", "Waitlist", "Declined":
		if err := actions.notify(status); err != nil {
			return fmt.Errorf("status saved as %s, but notification failed: %w", status, err)
		}
	}
	return nil
}

func changeVolunteerStatus(ctx *config.AppContext, vol *types.Volunteer, conf *types.Conf, shifts []*types.WorkShift, status string) error {
	return applyVolunteerStatus(vol, status, volunteerStatusActions{
		update: func(status string) error { return getters.UpdateVolunteerStatus(ctx, vol.Ref, status) },
		notify: func(status string) error {
			var err error
			switch status {
			case "PendingShifts":
				_, err = emails.OnlyForVolSignup(ctx, vol, conf)
			case "Waitlist":
				_, err = emails.OnlyForVolWaitlist(ctx, vol, conf)
			case "Declined":
				_, err = emails.OnlyForVolCancel(ctx, vol, conf)
			}
			return err
		},
		schedule: func() error { return runScheduledFlow(ctx, vol, conf) },
		leave: func() error {
			if vol.Status == "Scheduled" {
				if err := cancelVolunteerOrientation(ctx, vol, conf); err != nil {
					return err
				}
				if err := getters.RevokeVolunteerTicket(ctx, vol.RegisID()); err != nil {
					return err
				}
			}
			return releaseVolunteerShifts(ctx, conf, vol, shifts, "status")
		},
	})
}

func volunteerBelongsToConf(vol *types.Volunteer, conf *types.Conf) bool {
	if vol == nil || conf == nil {
		return false
	}
	for _, event := range vol.ScheduleFor {
		if event != nil && event.Ref == conf.Ref {
			return true
		}
	}
	return false
}

func volunteerShiftInEvent(shifts []*types.WorkShift, ref string) bool {
	for _, shift := range shifts {
		if shift != nil && shift.Ref == ref {
			return true
		}
	}
	return false
}

func VolAdminResendSignup(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireConfVolcoord(w, r, ctx) == nil {
		return
	}
	conf, vol, _ := volAdminLoadVol(w, r, ctx)
	if vol == nil {
		return
	}
	if vol.Status != "PendingShifts" {
		http.Error(w, "Signup invitations require Pending Shifts status", http.StatusConflict)
		return
	}
	if _, err := emails.OnlyForVolSignup(ctx, vol, conf); err != nil {
		ctx.Err.Printf("volunteer signup resend: %s", err)
		http.Error(w, "Signup invitation could not be queued", http.StatusInternalServerError)
		return
	}
	http.Redirect(w, r, "/"+conf.Tag+"/volcoord?flash=Signup+invitation+queued", http.StatusSeeOther)
}

func notifyScheduledShiftAssignment(ctx *config.AppContext, vol *types.Volunteer, conf *types.Conf, shiftRef string) error {
	if vol.Status != "Scheduled" {
		return nil
	}
	shift, err := getters.GetWorkShiftByRef(ctx, shiftRef)
	if err != nil {
		return err
	}
	if shift == nil {
		return fmt.Errorf("shift not found")
	}
	return dispatchVolunteerShiftInvite(ctx, shift, conf, ics.Attendee{Email: vol.Email, Name: vol.Name})
}

func cancelDeletedShift(ctx *config.AppContext, conf *types.Conf, shifts []*types.WorkShift, ref string) error {
	for _, shift := range shifts {
		if shift == nil || shift.Ref != ref || shift.CalNotif == "" {
			continue
		}
		vols, err := getters.ListVolunteersForConf(ctx, conf.Ref)
		if err != nil {
			return err
		}
		var recipients []ics.Attendee
		for _, vol := range vols {
			for _, assigned := range shift.AssigneesRef {
				if assigned == vol.Ref {
					recipients = append(recipients, ics.Attendee{Email: vol.Email, Name: vol.Name})
				}
			}
		}
		return DispatchShiftICS(ctx, shift, conf, recipients, kindCancel, false)
	}
	return nil
}

// A shift's calendar stamp is shared by all its assignees. A new recipient
// must receive an invite even when someone else already received this shift.
func dispatchVolunteerShiftInvite(ctx *config.AppContext, shift *types.WorkShift, conf *types.Conf, recipient ics.Attendee) error {
	return DispatchShiftICS(ctx, shift, conf, []ics.Attendee{recipient}, kindRequest, true)
}

func cancelVolunteerOrientation(ctx *config.AppContext, vol *types.Volunteer, conf *types.Conf) error {
	if conf.OrientCalNotif == "" {
		return nil
	}
	// Staff may still need the orientation after leaving their volunteer role.
	for _, staff := range orientationStaffRecipients(ctx, conf.Tag) {
		if strings.EqualFold(staff.Email, vol.Email) {
			return nil
		}
	}
	info, err := getters.GetVolInfo(ctx, conf.Ref)
	if err != nil {
		return err
	}
	if info == nil || info.OrientTimes == nil || info.OrientTimes.End == nil {
		return nil
	}
	return dispatchOrientICS(ctx, conf, ics.Attendee{Email: vol.Email, Name: vol.Name}, info.OrientTimes.Start, *info.OrientTimes.End, info.OrientLink, kindCancel)
}
