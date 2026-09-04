package handlers

import (
	"fmt"
	"net/http"
	"net/url"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

func VolAdminSelfScheduleUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	enabled := r.FormValue("volunteer_self_schedule") == "1"
	if err := getters.UpdateConferenceVolunteerSelfSchedule(ctx, conf.Ref, enabled); err != nil {
		ctx.Err.Printf("/%s/volcoord self-schedule update: %s", conf.Tag, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("Could not update volunteer signup settings.")), http.StatusSeeOther)
		return
	}
	invalidateConferenceCache(ctx)
	message := "New volunteer applications will wait for coordinator review."
	if enabled {
		message = "New volunteers can pick shifts immediately after confirming their email when enough shifts are available."
	}
	http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape(message)), http.StatusSeeOther)
}

func canSelfScheduleVolunteer(conf *types.Conf, shifts []*types.WorkShift) bool {
	if conf == nil || !conf.VolunteerSelfSchedule {
		return false
	}
	openShifts := 0
	for _, shift := range shifts {
		if shift != nil && shift.ShiftTime != nil && !shift.IsFull() && shift.MaxVols > 0 {
			openShifts++
			if openShifts >= VolShiftQuota {
				return true
			}
		}
	}
	return false
}
