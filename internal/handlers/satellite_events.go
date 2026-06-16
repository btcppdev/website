package handlers

import (
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func SatelliteEventSuggest(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if _, ok := requireSatelliteSubmitterSession(w, r, ctx); !ok {
		return
	}
	renderSatelliteSuggestForm(w, ctx, conf, r.URL.Query().Get("flash"), r.URL.Query().Get("error"), nil)
}

func renderSatelliteSuggestForm(w http.ResponseWriter, ctx *config.AppContext, conf *types.Conf, flash string, flashErr string, form *SatelliteEventFormValues) {
	page := &SatelliteEventFormPage{
		Conf:         conf,
		FlashMessage: flash,
		FlashError:   flashErr,
		Form:         form,
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "satellites/suggest.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/satellites/new render: %s", conf.Tag, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func satelliteSubmitterSessionEmail(r *http.Request, ctx *config.AppContext) string {
	return strings.TrimSpace(ctx.Session.GetString(r.Context(), auth.SessionEmailKey))
}

func requireSatelliteSubmitterSession(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (string, bool) {
	email := satelliteSubmitterSessionEmail(r, ctx)
	if email != "" {
		return email, true
	}
	next := auth.SafeNext(r.URL.RequestURI(), "/dashboard")
	http.Redirect(w, r, "/login?next="+url.QueryEscape(next), http.StatusSeeOther)
	return "", false
}

func SatelliteEventSuggestSubmit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	email, ok := requireSatelliteSubmitterSession(w, r, ctx)
	if !ok {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	form := satelliteEventFormValuesFromRequest(r)
	input, err := satelliteEventInputFromForm(r, conf.Ref, "submitted")
	if err != nil {
		renderSatelliteSuggestForm(w, ctx, conf, "", err.Error(), form)
		return
	}
	input.SubmitterEmail = email
	event, err := getters.CreateSatelliteEvent(ctx, input)
	if err != nil {
		ctx.Err.Printf("/%s/satellites/new create: %s", conf.Tag, err)
		renderSatelliteSuggestForm(w, ctx, conf, "", "Unable to save the suggestion. Please try again.", form)
		return
	}
	if err := sendSatelliteSubmittedNotifications(ctx, conf, event); err != nil {
		ctx.Err.Printf("/%s/satellites/new notify: %s", conf.Tag, err)
	}
	http.Redirect(w, r, fmt.Sprintf("/%s/satellites/new?flash=%s", conf.Tag, url.QueryEscape("Thanks - we saved your satellite event suggestion for review.")), http.StatusSeeOther)
}

func SatelliteEventSuggestImageUpload(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if satelliteSubmitterSessionEmail(r, ctx) == "" {
		http.Error(w, "login required", http.StatusUnauthorized)
		return
	}
	satelliteImageUpload(w, r, ctx, conf)
}

func SatelliteEventsAdmin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfStaff(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	events, err := getters.ListSatelliteEvents(ctx, conf.Ref, true)
	if err != nil {
		ctx.Err.Printf("/%s/admin/satellites list: %s", conf.Tag, err)
		events = nil
	}
	page := &SatelliteEventAdminPage{
		Conf:         conf,
		Events:       events,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/satellites.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/admin/satellites render: %s", conf.Tag, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func SatelliteEventsAdminSave(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
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

	action := r.FormValue("action")
	eventID := strings.TrimSpace(r.FormValue("id"))
	dest := fmt.Sprintf("/%s/admin/satellites", conf.Tag)
	switch action {
	case "delete":
		if eventID == "" {
			http.Redirect(w, r, dest+"?error="+url.QueryEscape("Missing satellite event id."), http.StatusSeeOther)
			return
		}
		if err := getters.DeleteSatelliteEvent(ctx, eventID); err != nil {
			ctx.Err.Printf("/%s/admin/satellites delete %s: %s", conf.Tag, eventID, err)
			http.Redirect(w, r, dest+"?error="+url.QueryEscape("Delete failed: "+err.Error()), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Satellite event deleted."), http.StatusSeeOther)
		return

	case "create":
		input, err := satelliteEventInputFromForm(r, conf.Ref, "draft")
		if err != nil {
			http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		if _, err := getters.CreateSatelliteEvent(ctx, input); err != nil {
			ctx.Err.Printf("/%s/admin/satellites create: %s", conf.Tag, err)
			http.Redirect(w, r, dest+"?error="+url.QueryEscape("Create failed: "+err.Error()), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Satellite event added."), http.StatusSeeOther)
		return

	case "update":
		if eventID == "" {
			http.Redirect(w, r, dest+"?error="+url.QueryEscape("Missing satellite event id."), http.StatusSeeOther)
			return
		}
		oldEvent, _ := getters.GetSatelliteEvent(ctx, eventID)
		status := strings.TrimSpace(r.FormValue("Status"))
		if status == "" {
			status = "draft"
		}
		input, err := satelliteEventInputFromForm(r, conf.Ref, status)
		if err != nil {
			http.Redirect(w, r, dest+"?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
			return
		}
		if err := getters.UpdateSatelliteEvent(ctx, eventID, input); err != nil {
			ctx.Err.Printf("/%s/admin/satellites update %s: %s", conf.Tag, eventID, err)
			http.Redirect(w, r, dest+"?error="+url.QueryEscape("Update failed: "+err.Error()), http.StatusSeeOther)
			return
		}
		if input.Status == "published" && (oldEvent == nil || oldEvent.Status != "published") {
			event, err := getters.GetSatelliteEvent(ctx, eventID)
			if err != nil {
				ctx.Err.Printf("/%s/admin/satellites published lookup %s: %s", conf.Tag, eventID, err)
			} else if err := sendSatellitePublishedNotification(ctx, conf, event); err != nil {
				ctx.Err.Printf("/%s/admin/satellites published notify %s: %s", conf.Tag, eventID, err)
			}
		}
		http.Redirect(w, r, dest+"?flash="+url.QueryEscape("Satellite event saved."), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, dest+"?error="+url.QueryEscape("Unknown action."), http.StatusSeeOther)
}

func SatelliteEventsAdminImageUpload(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	satelliteImageUpload(w, r, ctx, conf)
}

func DashboardSatelliteEventEdit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, encHMAC, err := validateVolEmail(r, ctx)
	if err != nil {
		ctx.Infos.Printf("/dashboard/satellites auth: %s", err)
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	encEmail := r.URL.Query().Get("em")
	event, conf, err := dashboardSatelliteEventForOwner(r, ctx, email)
	if err != nil {
		ctx.Err.Printf("/dashboard/satellites edit: %s", err)
		http.Redirect(w, r, dashboardRedirect(encHMAC, encEmail, "Satellite event not found."), http.StatusSeeOther)
		return
	}
	page := &SatelliteEventFormPage{
		Conf:         conf,
		Event:        event,
		HMAC:         encHMAC,
		Email:        encEmail,
		FlashMessage: r.URL.Query().Get("flash"),
		FlashError:   r.URL.Query().Get("error"),
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "satellites/edit.tmpl", page); err != nil {
		ctx.Err.Printf("/dashboard/satellites edit render: %s", err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func renderDashboardSatelliteEditForm(w http.ResponseWriter, ctx *config.AppContext, conf *types.Conf, event *types.SatelliteEvent, encHMAC string, encEmail string, flash string, flashErr string, form *SatelliteEventFormValues) {
	page := &SatelliteEventFormPage{
		Conf:         conf,
		Event:        event,
		Form:         form,
		HMAC:         encHMAC,
		Email:        encEmail,
		FlashMessage: flash,
		FlashError:   flashErr,
		Year:         helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "satellites/edit.tmpl", page); err != nil {
		ctx.Err.Printf("/dashboard/satellites edit render: %s", err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func DashboardSatelliteEventSave(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, encHMAC, err := validateVolEmail(r, ctx)
	if err != nil {
		ctx.Infos.Printf("/dashboard/satellites save auth: %s", err)
		http.Redirect(w, r, "/dashboard", http.StatusSeeOther)
		return
	}
	encEmail := r.URL.Query().Get("em")
	event, conf, err := dashboardSatelliteEventForOwner(r, ctx, email)
	if err != nil {
		ctx.Err.Printf("/dashboard/satellites save lookup: %s", err)
		http.Redirect(w, r, dashboardRedirect(encHMAC, encEmail, "Satellite event not found."), http.StatusSeeOther)
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	form := satelliteEventFormValuesFromRequest(r)
	action := strings.TrimSpace(r.FormValue("action"))
	status := event.Status
	if action == "submit" {
		status = "submitted"
	}
	if event.Status == "published" {
		status = "submitted"
	}
	input, err := satelliteEventInputFromForm(r, conf.Ref, status)
	if err != nil {
		renderDashboardSatelliteEditForm(w, ctx, conf, event, encHMAC, encEmail, "", err.Error(), form)
		return
	}
	input.SubmitterEmail = email
	input.Status = status
	if err := getters.UpdateSatelliteEvent(ctx, event.ID, input); err != nil {
		ctx.Err.Printf("/dashboard/satellites save %s: %s", event.ID, err)
		renderDashboardSatelliteEditForm(w, ctx, conf, event, encHMAC, encEmail, "", "Update failed: "+err.Error(), form)
		return
	}
	if input.Status == "submitted" && event.Status != "submitted" {
		updated, err := getters.GetSatelliteEvent(ctx, event.ID)
		if err != nil {
			ctx.Err.Printf("/dashboard/satellites submitted lookup %s: %s", event.ID, err)
		} else if err := sendSatelliteSubmittedNotifications(ctx, conf, updated); err != nil {
			ctx.Err.Printf("/dashboard/satellites submitted notify %s: %s", event.ID, err)
		}
	}
	http.Redirect(w, r, dashboardRedirect(encHMAC, encEmail, "Satellite event saved."), http.StatusSeeOther)
}

func DashboardSatelliteEventImageUpload(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, _, err := validateVolEmail(r, ctx)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	_, conf, err := dashboardSatelliteEventForOwner(r, ctx, email)
	if err != nil {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	satelliteImageUpload(w, r, ctx, conf)
}

func dashboardSatelliteEventForOwner(r *http.Request, ctx *config.AppContext, email string) (*types.SatelliteEvent, *types.Conf, error) {
	eventID := strings.TrimSpace(mux.Vars(r)["eventID"])
	if eventID == "" {
		return nil, nil, fmt.Errorf("missing event id")
	}
	event, err := getters.GetSatelliteEvent(ctx, eventID)
	if err != nil {
		return nil, nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(event.SubmitterEmail), strings.TrimSpace(email)) {
		return nil, nil, fmt.Errorf("satellite event %s is not owned by %s", eventID, email)
	}
	conf := satelliteConfByRef(ctx, event.ConfRef)
	if conf == nil {
		return nil, nil, fmt.Errorf("conference %s not found", event.ConfRef)
	}
	return event, conf, nil
}

func satelliteConfByRef(ctx *config.AppContext, confRef string) *types.Conf {
	confs, _ := getters.FetchConfsCached(ctx)
	for _, conf := range confs {
		if conf != nil && conf.Ref == confRef {
			return conf
		}
	}
	return nil
}

func satelliteImageUpload(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, conf *types.Conf) {
	limitRequestBody(w, r, maxMultipartBodyBytes)
	raw, contentType, ext, err := readMultipartFile(r, "file")
	if err != nil {
		http.Error(w, "missing or unreadable file", http.StatusBadRequest)
		return
	}
	kind := strings.TrimSpace(r.FormValue("kind"))
	if kind != "logo" {
		kind = "event"
	}
	key, publicURL, err := newPhotoPipeline(ctx).uploadSatelliteImage(conf.Tag, kind, raw, contentType, ext)
	if err != nil {
		ctx.Err.Printf("/%s/satellites/upload-img: %s", conf.Tag, err)
		http.Error(w, "upload failed", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{
		"path": key,
		"url":  publicURL,
	})
}

func sendSatelliteSubmittedNotifications(ctx *config.AppContext, conf *types.Conf, event *types.SatelliteEvent) error {
	if event == nil || conf == nil {
		return nil
	}
	var firstErr error
	adminEmails := satelliteAdminEmails(ctx, conf)
	adminURL := ctx.Env.GetURI() + "/" + conf.Tag + "/admin/satellites"
	submitter := strings.TrimSpace(event.SubmitterEmail)
	when := satelliteEventTimeLabel(event, conf)
	title := fmt.Sprintf("Satellite event submitted: %s", event.Title)
	text := fmt.Sprintf(
		"A satellite event was submitted for %s.\n\nTitle: %s\nStatus: %s\nWhen: %s\nLocation: %s\nHost: %s\nSubmitter: %s\n\nReview it: %s\n",
		conf.Desc,
		event.Title,
		event.Status,
		when,
		event.Location,
		event.HostName,
		submitter,
		adminURL,
	)
	html := fmt.Sprintf(
		"<p>A satellite event was submitted for <strong>%s</strong>.</p><ul><li><strong>Title:</strong> %s</li><li><strong>Status:</strong> %s</li><li><strong>When:</strong> %s</li><li><strong>Location:</strong> %s</li><li><strong>Host:</strong> %s</li><li><strong>Submitter:</strong> %s</li></ul><p><a href=\"%s\">Review satellite events</a></p>",
		template.HTMLEscapeString(conf.Desc),
		template.HTMLEscapeString(event.Title),
		template.HTMLEscapeString(event.Status),
		template.HTMLEscapeString(when),
		template.HTMLEscapeString(event.Location),
		template.HTMLEscapeString(event.HostName),
		template.HTMLEscapeString(submitter),
		template.HTMLEscapeString(adminURL),
	)
	for _, adminEmail := range adminEmails {
		if err := sendSatelliteMail(ctx, adminEmail, submitter, title, html, text, "submitted-admin", event.ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	if submitter != "" {
		copyTitle := fmt.Sprintf("We received your satellite event: %s", event.Title)
		copyText := fmt.Sprintf("Thanks for submitting a satellite event for %s.\n\nTitle: %s\nStatus: submitted\n\nWe'll review it and let you know when it is published.\n", conf.Desc, event.Title)
		copyHTML := fmt.Sprintf("<p>Thanks for submitting a satellite event for <strong>%s</strong>.</p><p><strong>%s</strong> is now marked <strong>submitted</strong>. We'll review it and let you know when it is published.</p>",
			template.HTMLEscapeString(conf.Desc),
			template.HTMLEscapeString(event.Title),
		)
		if err := sendSatelliteMail(ctx, submitter, "hello@btcpp.dev", copyTitle, copyHTML, copyText, "submitted-copy", event.ID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

func sendSatellitePublishedNotification(ctx *config.AppContext, conf *types.Conf, event *types.SatelliteEvent) error {
	if event == nil || conf == nil || strings.TrimSpace(event.SubmitterEmail) == "" {
		return nil
	}
	publicURL := ctx.Env.GetURI() + "/" + conf.Tag + "#satellites"
	title := fmt.Sprintf("Your satellite event is published: %s", event.Title)
	text := fmt.Sprintf("Your satellite event for %s has been published.\n\nTitle: %s\nView it: %s\n", conf.Desc, event.Title, publicURL)
	html := fmt.Sprintf("<p>Your satellite event for <strong>%s</strong> has been published.</p><p><strong>%s</strong></p><p><a href=\"%s\">View it on the event page</a></p>",
		template.HTMLEscapeString(conf.Desc),
		template.HTMLEscapeString(event.Title),
		template.HTMLEscapeString(publicURL),
	)
	return sendSatelliteMail(ctx, event.SubmitterEmail, "hello@btcpp.dev", title, html, text, "published", event.ID)
}

func satelliteAdminEmails(ctx *config.AppContext, conf *types.Conf) []string {
	var speakers []*types.Speaker
	if cached, err := getters.FetchSpeakersCached(ctx); err == nil {
		speakers = cached
	}
	if len(speakers) == 0 {
		if listed, err := getters.ListSpeakers(ctx); err == nil {
			speakers = listed
		}
	}
	seen := map[string]bool{}
	var out []string
	for _, sp := range speakers {
		if sp == nil || strings.TrimSpace(sp.Email) == "" {
			continue
		}
		id := &auth.Identity{Roles: auth.ParseRoles(sp.Roles)}
		if !id.HasRoleForConf(conf.Tag, auth.RoleAdmin) {
			continue
		}
		email := strings.TrimSpace(sp.Email)
		key := strings.ToLower(email)
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, email)
	}
	if len(out) == 0 {
		out = append(out, "hello@btcpp.dev")
	}
	return out
}

func sendSatelliteMail(ctx *config.AppContext, to string, replyTo string, title string, html string, text string, keyPrefix string, eventID string) error {
	to = strings.TrimSpace(to)
	if to == "" {
		return nil
	}
	return emails.ComposeAndSendMail(ctx, &emails.Mail{
		JobKey:   fmt.Sprintf("satellite-%s-%s-%d", keyPrefix, eventID, time.Now().UnixNano()),
		Email:    to,
		ReplyTo:  replyTo,
		Title:    title,
		SendAt:   time.Now(),
		HTMLBody: []byte(html),
		TextBody: []byte(text),
	})
}

func satelliteEventInputFromForm(r *http.Request, confRef string, fallbackStatus string) (getters.SatelliteEventInput, error) {
	title := strings.TrimSpace(r.FormValue("Title"))
	if title == "" {
		return getters.SatelliteEventInput{}, fmt.Errorf("Event title is required.")
	}
	status := strings.TrimSpace(r.FormValue("Status"))
	if status == "" {
		status = fallbackStatus
	}
	if status != "draft" && status != "submitted" && status != "published" {
		return getters.SatelliteEventInput{}, fmt.Errorf("Status must be draft, submitted, or published.")
	}
	startsAt, err := parseSatelliteFormTime(r.FormValue("StartsAt"), r.FormValue("Timezone"))
	if err != nil {
		return getters.SatelliteEventInput{}, err
	}
	endsAt, err := parseSatelliteFormTime(r.FormValue("EndsAt"), r.FormValue("Timezone"))
	if err != nil {
		return getters.SatelliteEventInput{}, err
	}
	return getters.SatelliteEventInput{
		Title:          title,
		Description:    strings.TrimSpace(r.FormValue("Description")),
		EventURL:       strings.TrimSpace(r.FormValue("EventURL")),
		EventType:      strings.TrimSpace(r.FormValue("EventType")),
		StartsAt:       startsAt,
		EndsAt:         endsAt,
		Location:       strings.TrimSpace(r.FormValue("Location")),
		ImageURL:       strings.TrimSpace(r.FormValue("ImageURL")),
		HostName:       strings.TrimSpace(r.FormValue("HostName")),
		HostURL:        strings.TrimSpace(r.FormValue("HostURL")),
		HostLogoURL:    strings.TrimSpace(r.FormValue("HostLogoURL")),
		SubmitterEmail: strings.TrimSpace(r.FormValue("SubmitterEmail")),
		Status:         status,
		Notes:          strings.TrimSpace(r.FormValue("Notes")),
		ConfRef:        confRef,
	}, nil
}

func satelliteEventFormValuesFromRequest(r *http.Request) *SatelliteEventFormValues {
	return &SatelliteEventFormValues{
		Title:       strings.TrimSpace(r.FormValue("Title")),
		Description: strings.TrimSpace(r.FormValue("Description")),
		EventURL:    strings.TrimSpace(r.FormValue("EventURL")),
		EventType:   strings.TrimSpace(r.FormValue("EventType")),
		StartsAt:    strings.TrimSpace(r.FormValue("StartsAt")),
		EndsAt:      strings.TrimSpace(r.FormValue("EndsAt")),
		Location:    strings.TrimSpace(r.FormValue("Location")),
		ImageURL:    strings.TrimSpace(r.FormValue("ImageURL")),
		HostName:    strings.TrimSpace(r.FormValue("HostName")),
		HostURL:     strings.TrimSpace(r.FormValue("HostURL")),
		HostLogoURL: strings.TrimSpace(r.FormValue("HostLogoURL")),
	}
}

func satelliteFormValue(form *SatelliteEventFormValues, event *types.SatelliteEvent, field string) string {
	if form != nil {
		switch field {
		case "Title":
			return form.Title
		case "Description":
			return form.Description
		case "EventURL":
			return form.EventURL
		case "EventType":
			return form.EventType
		case "StartsAt":
			return form.StartsAt
		case "EndsAt":
			return form.EndsAt
		case "Location":
			return form.Location
		case "ImageURL":
			return form.ImageURL
		case "HostName":
			return form.HostName
		case "HostURL":
			return form.HostURL
		case "HostLogoURL":
			return form.HostLogoURL
		}
	}
	if event == nil {
		return ""
	}
	switch field {
	case "Title":
		return event.Title
	case "Description":
		return event.Description
	case "EventURL":
		return event.EventURL
	case "EventType":
		return event.EventType
	case "StartsAt":
		return satelliteEventInputTime(event.StartsAt, nil)
	case "EndsAt":
		return satelliteEventInputTime(event.EndsAt, nil)
	case "Location":
		return event.Location
	case "ImageURL":
		return event.ImageURL
	case "HostName":
		return event.HostName
	case "HostURL":
		return event.HostURL
	case "HostLogoURL":
		return event.HostLogoURL
	}
	return ""
}

func satelliteFormStartsAt(form *SatelliteEventFormValues, event *types.SatelliteEvent, conf *types.Conf) string {
	if form != nil {
		return form.StartsAt
	}
	if event == nil {
		return ""
	}
	return satelliteEventInputTime(event.StartsAt, conf)
}

func satelliteFormEndsAt(form *SatelliteEventFormValues, event *types.SatelliteEvent, conf *types.Conf) string {
	if form != nil {
		return form.EndsAt
	}
	if event == nil {
		return ""
	}
	return satelliteEventInputTime(event.EndsAt, conf)
}

func satelliteEventInputTime(t *time.Time, conf *types.Conf) string {
	if t == nil || t.IsZero() {
		return ""
	}
	loc := time.Local
	if conf != nil {
		loc = conf.Loc()
	}
	return t.In(loc).Format("2006-01-02T15:04")
}

func satelliteEventTimeLabel(event *types.SatelliteEvent, conf *types.Conf) string {
	if event == nil || event.StartsAt == nil || event.StartsAt.IsZero() {
		return ""
	}
	loc := time.Local
	if conf != nil {
		loc = conf.Loc()
	}
	start := event.StartsAt.In(loc)
	label := start.Format("Mon Jan 2 · 3:04 PM MST")
	if event.EndsAt == nil || event.EndsAt.IsZero() {
		return label
	}
	end := event.EndsAt.In(loc)
	if start.Year() == end.Year() && start.YearDay() == end.YearDay() {
		return label + " - " + end.Format("3:04 PM MST")
	}
	return label + " - " + end.Format("Mon Jan 2 · 3:04 PM MST")
}

func parseSatelliteFormTime(raw string, tzName string) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	loc := time.Local
	if tzName != "" {
		if loaded, err := time.LoadLocation(tzName); err == nil {
			loc = loaded
		}
	}
	parsed, err := time.ParseInLocation("2006-01-02T15:04", raw, loc)
	if err != nil {
		return nil, fmt.Errorf("Event times must use the date/time picker format.")
	}
	return &parsed, nil
}
