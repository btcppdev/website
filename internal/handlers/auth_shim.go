package handlers

import (
	"net/http"
	"net/url"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

// requireConfAdmin gates a per-conf admin route on the request's
// {conf} mux var. Returns nil identity (with response already
// written) when access is denied — caller should `return` immediately.
//
// Replaces the legacy `helpers.CheckPin(...)` pattern; the role
// check now considers the user's Speakers DB Roles column rather
// than a single shared PIN in the session.
func requireConfAdmin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) *auth.Identity {
	return auth.RequireRole(w, r, ctx, auth.Spec{
		Conf: mux.Vars(r)["conf"],
		Role: auth.RoleAdmin,
	})
}

// requireConfVolcoord gates a per-conf volunteer-admin route on the
// request's {conf} mux var. admin role implies volcoord, so a
// vienna-admin can also access vienna-volcoord paths.
func requireConfVolcoord(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) *auth.Identity {
	return auth.RequireRole(w, r, ctx, auth.Spec{
		Conf: mux.Vars(r)["conf"],
		Role: auth.RoleVolcoord,
	})
}

// requireConfStaff gates a per-conf staff route on the request's
// {conf} mux var. admin covers staff, so a vienna-admin can access
// any vienna-staff path. staff is the read-mostly tier — pages
// gated here surface info (run-of-show, speakers, registrations,
// hotels, downloads) but mutating actions like email blasts and
// calendar fan-outs stay behind requireConfAdmin.
func requireConfStaff(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) *auth.Identity {
	return auth.RequireRole(w, r, ctx, auth.Spec{
		Conf: mux.Vars(r)["conf"],
		Role: auth.RoleStaff,
	})
}

// requireGlobalAdmin gates a route that isn't scoped to a single
// conf (org list, missives DB, etc). Only a global-admin satisfies.
func requireGlobalAdmin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) *auth.Identity {
	return auth.RequireRole(w, r, ctx, auth.Spec{Role: auth.RoleAdmin})
}

// requireHackathonAdmin scopes hackathon administration to the linked
// conference. Global admins still satisfy every scoped check. The unscoped
// hackathon index remains global-admin-only; creating a hackathon is scoped by
// the selected conference in ?conf= or ConferenceID.
func requireHackathonAdmin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) *auth.Identity {
	confRef := ""
	if competitionID := strings.TrimSpace(mux.Vars(r)["competitionID"]); competitionID != "" {
		competition, err := getters.GetCompetitionByID(ctx, competitionID)
		if err != nil || competition == nil {
			handle404(w, r, ctx)
			return nil
		}
		confRef = competition.ConferenceID
	} else if strings.HasSuffix(r.URL.Path, "/new") {
		confRef = strings.TrimSpace(r.URL.Query().Get("conf"))
	} else if r.Method == http.MethodPost && r.URL.Path == "/admin/hackathons" {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "Bad form", http.StatusBadRequest)
			return nil
		}
		confRef = strings.TrimSpace(r.FormValue("ConferenceID"))
	}
	if confRef == "" {
		return requireGlobalAdmin(w, r, ctx)
	}
	conf, err := hackathonAdminConference(ctx, confRef)
	if err != nil || conf == nil {
		handle404(w, r, ctx)
		return nil
	}
	id, err := auth.Resolve(r, ctx)
	if err != nil {
		ctx.Err.Printf("auth resolve %s: %s", r.URL.Path, err)
	}
	if id == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
		return nil
	}
	if id.HasRoleForConf(conf.Tag, auth.RoleAdmin) {
		return id
	}
	if competitionID := strings.TrimSpace(mux.Vars(r)["competitionID"]); competitionID != "" && hackathonIdentityIsCoordinator(ctx, competitionID, id) {
		return id
	}
	msg := "You don't have access to that hackathon admin page. A conference admin can assign you the Coordinator role."
	http.Redirect(w, r, "/dashboard?error="+url.QueryEscape(msg), http.StatusSeeOther)
	return nil
}

func hackathonIdentityIsCoordinator(ctx *config.AppContext, competitionID string, id *auth.Identity) bool {
	if id == nil || strings.TrimSpace(id.Email) == "" {
		return false
	}
	assignments, err := getters.ListCompetitionJudgeAssignmentsByEmail(ctx, id.Email)
	if err != nil {
		ctx.Err.Printf("hackathon coordinator lookup %s: %s", competitionID, err)
		return false
	}
	for _, assignment := range assignments {
		if assignment != nil && assignment.CompetitionID == competitionID && assignment.JudgeType == getters.JudgeTypeCoordinator {
			return true
		}
	}
	return false
}

func hackathonAdminConference(ctx *config.AppContext, refOrTag string) (*types.Conf, error) {
	conf, tagErr := getters.GetConfByTag(ctx, refOrTag)
	if tagErr == nil && conf != nil {
		return conf, nil
	}
	return getters.GetConfByRef(ctx, refOrTag)
}
