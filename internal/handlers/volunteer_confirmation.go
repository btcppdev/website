package handlers

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/missives"
	"btcpp-web/internal/types"
)

type VolunteerApplicationConfirmationPage struct {
	Volunteer *types.Volunteer
	Conf      *types.Conf
	Token     string
	Confirmed bool
	Notice    string
	Error     string
	Year      uint
}

func sendVolunteerApplicationConfirmationEmail(ctx *config.AppContext, vol *types.Volunteer, conf *types.Conf, token string) error {
	if vol == nil || conf == nil || strings.TrimSpace(token) == "" {
		return fmt.Errorf("volunteer application confirmation data is incomplete")
	}
	confirmationURL := strings.TrimRight(ctx.Env.GetURI(), "/") + "/volunteer/confirm?token=" + url.QueryEscape(token)
	if !ctx.InProduction {
		ctx.Infos.Printf("[dev] volunteer application confirmation for %s: %s", vol.Email, confirmationURL)
	}
	markdown := fmt.Sprintf("# Confirm your volunteer application\n\nWe received a request for %s to volunteer at %s.\n\n[Review and confirm your application](button#%s)\n\nYour request will not be added to the volunteer roster or your bitcoin++ profile until you confirm. This link expires in 30 minutes. If you did not submit this request, ignore this email.\n\n— bitcoin++", markdownEmailText(vol.Email), markdownEmailText(conf.Desc), confirmationURL)
	return sendPersonAccountEmail(ctx, vol.Email, "Confirm your bitcoin++ volunteer application", markdown, "volunteer-confirm-"+token[:12])
}

func VolunteerApplicationConfirmation(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	token := strings.TrimSpace(r.URL.Query().Get("token"))
	if r.Method == http.MethodPost {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			renderVolunteerApplicationConfirmation(w, ctx, &VolunteerApplicationConfirmationPage{Error: "Invalid confirmation request.", Year: helpers.CurrentYear()})
			return
		}
		token = strings.TrimSpace(r.FormValue("token"))
		vol, err := getters.ConfirmVolunteerApplication(ctx, token)
		if err != nil {
			message := err.Error()
			if errors.Is(err, getters.ErrVolunteerAlreadyApplied) {
				message = "You already have a volunteer application for this event. A volunteer coordinator can reopen it if it was declined."
			}
			renderVolunteerApplicationConfirmation(w, ctx, volunteerApplicationConfirmationPage(ctx, token, message))
			return
		}
		conf, err := getters.GetConfByRef(ctx, vol.ScheduleFor[0].Ref)
		if err != nil || conf == nil {
			renderVolunteerApplicationConfirmation(w, ctx, &VolunteerApplicationConfirmationPage{Volunteer: vol, Conf: &types.Conf{Desc: "this event"}, Confirmed: true, Notice: "Your application was saved, but its acknowledgment email could not be prepared.", Year: helpers.CurrentYear()})
			return
		}
		page := &VolunteerApplicationConfirmationPage{Volunteer: vol, Conf: conf, Confirmed: true, Year: helpers.CurrentYear()}
		var notices []string
		if volinfo, err := getters.GetVolInfo(ctx, conf.Ref); err != nil {
			ctx.Err.Printf("volunteer confirmation load volinfo: %s", err)
			notices = append(notices, "The application was saved, but the acknowledgment email could not be prepared.")
		} else if _, err := emails.OnlyForVolApp(ctx, vol, conf, volinfo); err != nil {
			ctx.Err.Printf("volunteer confirmation acknowledgment: %s", err)
			notices = append(notices, "The application was saved, but the acknowledgment email could not be sent.")
		}
		newslist := missives.MakeApplicationSublist(conf.Tag, "volapp", vol.Subscribe)
		if err := missives.NewSubs(ctx, vol.Email, newslist); err != nil {
			ctx.Err.Printf("volunteer confirmation missives subscription: %s", err)
			notices = append(notices, "The application was saved, but its email-list setup needs staff attention.")
		}
		page.Notice = strings.Join(notices, " ")
		renderVolunteerApplicationConfirmation(w, ctx, page)
		return
	}
	renderVolunteerApplicationConfirmation(w, ctx, volunteerApplicationConfirmationPage(ctx, token, ""))
}

func volunteerApplicationConfirmationPage(ctx *config.AppContext, token, message string) *VolunteerApplicationConfirmationPage {
	page := &VolunteerApplicationConfirmationPage{Token: token, Error: message, Year: helpers.CurrentYear()}
	if message != "" {
		return page
	}
	vol, err := getters.GetPendingVolunteerApplication(ctx, token)
	if err != nil {
		page.Error = err.Error()
		return page
	}
	page.Volunteer = vol
	if len(vol.ScheduleFor) > 0 && vol.ScheduleFor[0] != nil {
		page.Conf, err = getters.GetConfByRef(ctx, vol.ScheduleFor[0].Ref)
	}
	if err != nil || page.Conf == nil {
		page.Error = "The event for this volunteer application is no longer available."
	}
	return page
}

func renderVolunteerApplicationConfirmation(w http.ResponseWriter, ctx *config.AppContext, page *VolunteerApplicationConfirmationPage) {
	w.Header().Set("Cache-Control", "private, no-store")
	if err := ctx.TemplateCache.ExecuteTemplate(w, "volunteer_confirmation.tmpl", page); err != nil {
		ctx.Err.Printf("volunteer confirmation render: %s", err)
		http.Error(w, "Unable to render volunteer confirmation", http.StatusInternalServerError)
	}
}
