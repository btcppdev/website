package handlers

import (
	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/eventplan"
	"btcpp-web/internal/types"
	"bytes"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/gorilla/mux"
	"html/template"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"time"
)

type eventWizardPage struct {
	Data   template.JS
	CSRF   string
	Title  string
	Conf   *types.Conf
	Confs  []*types.Conf
	New    bool
	Index  bool
	Invite bool
	Token  string
	Error  string
}

func registerEventWizard(r *mux.Router, app *config.AppContext) {
	r.HandleFunc("/admin/events", func(w http.ResponseWriter, r *http.Request) { EventWizardIndex(w, r, app) }).Methods("GET")
	r.HandleFunc("/admin/events/new", func(w http.ResponseWriter, r *http.Request) { EventWizardNew(w, r, app) }).Methods("GET", "POST")
	r.HandleFunc("/{conf}/admin/setup", func(w http.ResponseWriter, r *http.Request) { EventWizard(w, r, app) }).Methods("GET", "POST")
	r.HandleFunc("/{conf}/admin/setup/artwork", func(w http.ResponseWriter, r *http.Request) { EventWizardArtwork(w, r, app) }).Methods("POST")
	r.HandleFunc("/event-invites/{token}", func(w http.ResponseWriter, r *http.Request) { EventWizardInvite(w, r, app) }).Methods("GET", "POST")
}
func renderEventWizard(w http.ResponseWriter, r *http.Request, app *config.AppContext, p eventWizardPage) {
	token, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare form", 500)
		return
	}
	p.CSRF = token
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("X-Robots-Tag", "noindex")
	w.Header().Set("Referrer-Policy", "same-origin")
	if err = app.TemplateCache.ExecuteTemplate(w, "admin/event_wizard.tmpl", p); err != nil {
		app.Err.Printf("event wizard render: %s", err)
	}
}
func wizardJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "private, no-store")
	w.WriteHeader(status)
	json.NewEncoder(w).Encode(data)
}
func wizardError(w http.ResponseWriter, err error) {
	status := http.StatusBadRequest
	if errors.Is(err, getters.ErrEventPlanConflict) {
		status = http.StatusConflict
	}
	wizardJSON(w, status, map[string]any{"error": err.Error()})
}
func wizardCSRF(w http.ResponseWriter, r *http.Request, app *config.AppContext) bool {
	token := app.Session.GetString(r.Context(), authMethodsCSRFKey)
	supplied := r.Header.Get("X-CSRF-Token")
	if supplied == "" {
		supplied = r.FormValue("csrf")
	}
	if token == "" || subtle.ConstantTimeCompare([]byte(token), []byte(supplied)) != 1 {
		wizardJSON(w, 403, map[string]string{"error": "Your form expired. Reload and try again."})
		return false
	}
	return true
}
func EventWizardIndex(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	if requireGlobalAdmin(w, r, app) == nil {
		return
	}
	confs, e := getters.ListConfs(app)
	if e != nil {
		http.Error(w, "Unable to load events", 500)
		return
	}
	renderEventWizard(w, r, app, eventWizardPage{Title: "Event planning", Index: true, Confs: confs})
}
func EventWizardNew(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	id := requireGlobalAdmin(w, r, app)
	if id == nil {
		return
	}
	if r.Method == "GET" {
		data, _ := json.Marshal(map[string]any{"new": true, "basics": eventplan.Basics{Timezone: "America/Chicago", Edition: "global"}})
		renderEventWizard(w, r, app, eventWizardPage{Title: "Create an event", New: true, Data: template.JS(data)})
		return
	}
	limitRequestBody(w, r, 256<<10)
	if !wizardCSRF(w, r, app) {
		return
	}
	var b eventplan.Basics
	if e := json.NewDecoder(r.Body).Decode(&b); e != nil {
		wizardError(w, fmt.Errorf("invalid event details"))
		return
	}
	_, e := getters.CreatePlannedEvent(app, b, id.PersonID)
	if e != nil {
		wizardError(w, e)
		return
	}
	invalidateConferenceCache(app)
	wizardJSON(w, 201, map[string]string{"url": "/" + b.Tag + "/admin/setup?step=artwork"})
}
func wizardIdentity(w http.ResponseWriter, r *http.Request, app *config.AppContext) (*auth.Identity, *types.Conf, bool) {
	id, e := auth.Resolve(r, app)
	if e != nil || id == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), 303)
		return nil, nil, false
	}
	conf, e := getters.GetConfByTag(app, mux.Vars(r)["conf"])
	if e != nil || conf == nil {
		http.NotFound(w, r)
		return nil, nil, false
	}
	canEdit := id.HasRoleForConf(conf.Tag, auth.RoleAdmin)
	if !canEdit && !id.HasRoleForConf(conf.Tag, auth.RoleStaff) && !id.HasRoleForConf(conf.Tag, auth.RoleVolcoord) && !getters.EventTeamCanRead(app, conf.Ref, id.PersonID) {
		http.Error(w, "Event planning access required", 403)
		return nil, nil, false
	}
	if r.Method != "GET" && !canEdit {
		http.Error(w, "Only event administrators can change this plan", 403)
		return nil, nil, false
	}
	return id, conf, canEdit
}
func EventWizard(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	id, conf, canEdit := wizardIdentity(w, r, app)
	if id == nil {
		return
	}
	if r.Method == "POST" {
		limitRequestBody(w, r, 1<<20)
		if !wizardCSRF(w, r, app) {
			return
		}
		var in struct {
			Section  string          `json:"section"`
			Revision int64           `json:"revision"`
			Data     json.RawMessage `json:"data"`
		}
		if e := json.NewDecoder(r.Body).Decode(&in); e != nil {
			wizardError(w, fmt.Errorf("invalid setup form"))
			return
		}
		if in.Section == "team" {
			var m getters.EventTeamMember
			if e := json.Unmarshal(in.Data, &m); e != nil {
				wizardError(w, e)
				return
			}
			token, e := getters.CreateEventTeamInvite(app, conf.Ref, id.PersonID, m)
			if e != nil {
				wizardError(w, e)
				return
			}
			wizardJSON(w, 200, map[string]string{"invite_url": app.Env.GetURI() + "/event-invites/" + token})
			return
		}
		if in.Section == "revoke-invite" {
			var m struct {
				ID string `json:"id"`
			}
			if e := json.Unmarshal(in.Data, &m); e != nil {
				wizardError(w, e)
				return
			}
			if e := getters.RevokeEventTeamInvite(app, conf.Ref, m.ID); e != nil {
				wizardError(w, e)
				return
			}
			wizardJSON(w, 200, map[string]bool{"saved": true})
			return
		}
		if in.Section == "staffing-preview" || in.Section == "staffing-apply" {
			eventWizardStaffing(w, r, app, conf, in.Revision, in.Section == "staffing-apply")
			return
		}
		revision, e := getters.SaveEventPlanning(app, conf, in.Revision, id.PersonID, in.Section, in.Data)
		if e != nil {
			wizardError(w, e)
			return
		}
		invalidateConferenceCache(app)
		wizardJSON(w, 200, map[string]any{"saved": true, "revision": revision})
		return
	}
	data, e := eventWizardData(app, conf, canEdit)
	if e != nil {
		app.Err.Printf("event setup load: %s", e)
		http.Error(w, "Unable to load event setup", 500)
		return
	}
	raw, _ := json.Marshal(data)
	renderEventWizard(w, r, app, eventWizardPage{Title: conf.Desc + " · Event planning", Conf: conf, Data: template.JS(raw)})
}
func eventWizardData(app *config.AppContext, conf *types.Conf, edit bool) (map[string]any, error) {
	saved, e := getters.LoadEventPlanning(app, conf.Ref)
	if e != nil {
		return nil, e
	}
	team, e := getters.ListEventTeam(app, conf.Ref)
	if e != nil {
		return nil, e
	}
	local := func(t *time.Time) string {
		if t == nil || t.IsZero() {
			return ""
		}
		return t.In(conf.Loc()).Format("2006-01-02T15:04")
	}
	tickets := []eventplan.Ticket{}
	for _, t := range conf.Tickets {
		var student *int64
		if t.StudentPrice != nil {
			v := int64(*t.StudentPrice)
			student = &v
		}
		tickets = append(tickets, eventplan.Ticket{ID: t.ID, Tier: t.Tier, Price: int64(t.StandardPrice()), LocalPrice: int64(t.Local), StudentPrice: student, Capacity: int64(t.Max), Currency: t.Currency, TaxCode: t.StripeTaxCode, SalesEnd: local(&t.SalesEndAt), CardSurchargeBPS: int64(t.CardSurchargeBPS)})
	}
	products, e := getters.ListMerchProducts(app, false)
	if e != nil {
		return nil, e
	}
	choices := []map[string]string{}
	for _, p := range products {
		choices = append(choices, map[string]string{"id": p.ID, "name": p.Name})
	}
	upsells, e := getters.ListConferenceMerchUpsells(app, conf.Ref)
	if e != nil {
		return nil, e
	}
	productIDs := []string{}
	for _, p := range upsells {
		productIDs = append(productIDs, p.ID)
	}
	orgs, e := getters.ListOrgs(app)
	if e != nil {
		return nil, e
	}
	organizations := []map[string]string{}
	for _, o := range orgs {
		organizations = append(organizations, map[string]string{"id": o.Ref, "name": o.Name})
	}
	return map[string]any{"new": false, "can_edit": edit, "revision": saved.Revision, "tag": conf.Tag, "published": conf.IsPublished(), "basics": eventplan.Basics{Tag: conf.Tag, Name: conf.Desc, Edition: conf.EditionType, Location: conf.Location, Country: saved.Plan.Country, Timezone: conf.Timezone, Start: local(&conf.StartDate), End: local(&conf.EndDate), Description: conf.AboutBody}, "artwork": map[string]string{"leading": conf.LeadingImageURL, "black": conf.LogoBlackURL, "white": conf.LogoWhiteURL}, "plan": saved.Plan, "team": team, "tickets": tickets, "applications": eventplan.Applications{TalksOpen: local(conf.SpeakerApplicationsOpen), TalksClose: local(conf.SpeakerApplicationsClose), VolunteersOpen: local(conf.VolunteerApplicationsOpen), VolunteersClose: local(conf.VolunteerApplicationsClose), Notes: saved.Plan.VolunteerNotes, ShiftHours: saved.Plan.ShiftHours, MerchDesk: saved.Plan.MerchDesk}, "products": choices, "product_ids": productIDs, "organizations": organizations}, nil
}
func EventWizardArtwork(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	id, conf, _ := wizardIdentity(w, r, app)
	if id == nil {
		return
	}
	limitRequestBody(w, r, 12<<20)
	if !wizardCSRF(w, r, app) {
		return
	}
	if e := r.ParseMultipartForm(12 << 20); e != nil {
		wizardError(w, fmt.Errorf("upload an image smaller than 10 MB"))
		return
	}
	defer r.MultipartForm.RemoveAll()
	field := r.FormValue("asset")
	column := map[string]string{"leading": "leading_image_url", "black": "logo_black_url", "white": "logo_white_url"}[field]
	if column == "" {
		wizardError(w, fmt.Errorf("unknown artwork type"))
		return
	}
	file, _, e := r.FormFile("file")
	if e != nil {
		wizardError(w, fmt.Errorf("choose an image"))
		return
	}
	defer file.Close()
	raw, e := io.ReadAll(io.LimitReader(file, (10<<20)+1))
	if e != nil || len(raw) > 10<<20 {
		wizardError(w, fmt.Errorf("image must be at most 10 MB"))
		return
	}
	mime := http.DetectContentType(raw)
	ext := map[string]string{"image/png": "png", "image/jpeg": "jpg"}[mime]
	if ext == "" {
		wizardError(w, fmt.Errorf("use a PNG or JPEG image; transparent PNG is best for logos"))
		return
	}
	dimensions, _, err := image.DecodeConfig(bytes.NewReader(raw))
	if err != nil || dimensions.Width < 1 || dimensions.Height < 1 || int64(dimensions.Width)*int64(dimensions.Height) > 40000000 {
		wizardError(w, fmt.Errorf("use a valid image with at most 40 million pixels"))
		return
	}
	if !spaces.IsConfigured() {
		wizardError(w, fmt.Errorf("image storage is not configured"))
		return
	}
	hash := sha256.Sum256(raw)
	digest := hex.EncodeToString(hash[:])
	key := "events/" + conf.Ref + "/" + field + "/" + digest[:20] + "." + ext
	imageURL, e := spaces.UploadImmutable(key, raw, mime, digest)
	if e != nil {
		wizardError(w, fmt.Errorf("could not upload artwork"))
		return
	}
	_, e = app.DB.Exec(r.Context(), `UPDATE conferences SET `+column+`=$2 WHERE id=$1`, conf.Ref, imageURL)
	if e != nil {
		wizardError(w, e)
		return
	}
	invalidateConferenceCache(app)
	wizardJSON(w, 200, map[string]string{"url": imageURL})
}
func EventWizardInvite(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	id, e := auth.Resolve(r, app)
	if e != nil || id == nil {
		http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), 303)
		return
	}
	token := mux.Vars(r)["token"]
	if len(token) != 64 {
		http.NotFound(w, r)
		return
	}
	if r.Method == "POST" {
		limitRequestBody(w, r, 4096)
		if !wizardCSRF(w, r, app) {
			return
		}
		tag, e := getters.AcceptEventTeamInvite(app, token, id.PersonID)
		if e != nil {
			renderEventWizard(w, r, app, eventWizardPage{Title: "Event team invitation", Invite: true, Token: token, Error: e.Error()})
			return
		}
		http.Redirect(w, r, "/"+tag+"/admin/setup", 303)
		return
	}
	renderEventWizard(w, r, app, eventWizardPage{Title: "Join the event team", Invite: true, Token: token})
}
func eventWizardStaffing(w http.ResponseWriter, r *http.Request, app *config.AppContext, conf *types.Conf, revision int64, apply bool) {
	changes, e := getters.ReconcileEventStaffing(app, conf, revision, apply)
	if e != nil {
		wizardError(w, e)
		return
	}
	wizardJSON(w, 200, map[string]any{"changes": changes, "applied": apply})
}
