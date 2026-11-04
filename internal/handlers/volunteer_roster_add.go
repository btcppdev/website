package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
	"errors"
	"net/http"
	"net/url"
	"strings"
)

func VolAdminAdd(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireConfVolcoord(w, r, ctx) == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil || !secureTokenEqual(ctx.Session.GetString(r.Context(), authMethodsCSRFKey), r.FormValue("csrf")) {
		http.Error(w, "Invalid form token", 403)
		return
	}
	if r.FormValue("confirm_email") != "yes" {
		http.Error(w, "Confirm that the email belongs to this person", 400)
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	vol := &types.Volunteer{Name: strings.TrimSpace(r.FormValue("name")), Email: strings.TrimSpace(r.FormValue("email")), ScheduleFor: []*types.Conf{conf}}
	if len(vol.Name) > 200 {
		http.Error(w, "Name is too long", 400)
		return
	}
	err = getters.AddVolunteerToRoster(ctx, vol)
	message := "Added to roster. Assign shifts, then mark Scheduled when ready to send onboarding and issue a ticket."
	if errors.Is(err, getters.ErrVolunteerAlreadyApplied) {
		message = "This person is already on the event roster."
	} else if err != nil {
		ctx.Err.Printf("add volunteer to roster: %s", err)
		message = "Could not add this person. Check their name and email; accounts awaiting a merge must be resolved first."
	}
	http.Redirect(w, r, "/"+conf.Tag+"/volcoord?flash="+url.QueryEscape(message), http.StatusSeeOther)
}
