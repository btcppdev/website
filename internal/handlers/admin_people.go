package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

type AdminPeoplePage struct {
	ConflictGroups []*AdminPersonConflictGroup
	MergeEvents    []*getters.PersonMergeEvent
	Flash          string
	Error          string
	Year           uint
}

type AdminPersonConflictGroup struct {
	Email  string
	People []*types.PersonEmailConflict
	Links  []AdminPersonConflictLink
}

type AdminPersonConflictLink struct {
	CanonicalName string
	SourceName    string
	URL           string
}

type AdminPersonMergePage struct {
	Preview *getters.PersonMergePreview
	Fields  []AdminPersonMergeField
	Error   string
	Year    uint
}

type AdminPersonMergeField struct {
	Key              string
	Label            string
	Kind             string
	CanonicalDisplay string
	SourceDisplay    string
	DefaultChoice    string
}

type AdminPersonMergeAuditPage struct {
	Undo      *getters.PersonMergeUndoPreview
	Decisions []AdminPersonMergeDecision
	CanUndo   bool
	Flash     string
	Error     string
	Year      uint
}

type AdminPersonMergeDecision struct {
	Label  string
	Choice string
	Value  string
}

func AdminPeople(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireGlobalAdmin(w, r, ctx) == nil {
		return
	}
	conflicts, err := getters.ListPersonEmailConflicts(ctx)
	if err != nil {
		ctx.Err.Printf("/admin/people conflicts: %s", err)
		http.Error(w, "Unable to load people", http.StatusInternalServerError)
		return
	}
	events, err := getters.ListPersonMergeEvents(ctx, 50)
	if err != nil {
		ctx.Err.Printf("/admin/people merges: %s", err)
		http.Error(w, "Unable to load people", http.StatusInternalServerError)
		return
	}
	page := &AdminPeoplePage{
		ConflictGroups: groupPersonEmailConflicts(conflicts),
		MergeEvents:    events,
		Flash:          r.URL.Query().Get("flash"),
		Error:          r.URL.Query().Get("error"),
		Year:           helpers.CurrentYear(),
	}
	renderAdminPeople(w, ctx, page)
}

func AdminPersonMerge(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireGlobalAdmin(w, r, ctx) == nil {
		return
	}
	canonicalID := r.URL.Query().Get("canonical")
	sourceID := r.URL.Query().Get("source")
	preview, err := getters.PreviewPersonMerge(ctx, canonicalID, sourceID)
	if err != nil {
		http.Redirect(w, r, "/admin/people?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	page := adminPersonMergePage(preview, "")
	renderAdminPersonMerge(w, ctx, page)
}

func AdminPersonMergeSave(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireGlobalAdmin(w, r, ctx)
	if id == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	canonicalID := strings.TrimSpace(r.FormValue("CanonicalPersonID"))
	sourceID := strings.TrimSpace(r.FormValue("SourcePersonID"))
	preview, err := getters.PreviewPersonMerge(ctx, canonicalID, sourceID)
	if err != nil {
		page := adminPersonMergePage(preview, err.Error())
		renderAdminPersonMerge(w, ctx, page)
		return
	}
	if len(preview.Conflicts) > 0 {
		page := adminPersonMergePage(preview, "Resolve every relationship conflict before merging.")
		renderAdminPersonMerge(w, ctx, page)
		return
	}
	decisions, err := parsePersonMergeDecisions(r, preview)
	if err != nil {
		page := adminPersonMergePage(preview, err.Error())
		renderAdminPersonMerge(w, ctx, page)
		return
	}
	eventID, err := getters.MergePeople(ctx, getters.PersonMergeInput{
		CanonicalPersonID: canonicalID,
		SourcePersonID:    sourceID,
		MergedByPersonID:  id.PersonID,
		Decisions:         decisions,
	})
	if err != nil {
		ctx.Err.Printf("/admin/people/merge: %s", err)
		page := adminPersonMergePage(preview, err.Error())
		renderAdminPersonMerge(w, ctx, page)
		return
	}
	http.Redirect(w, r, "/admin/people/merges/"+eventID+"?flash="+url.QueryEscape("People merged. Undo is available for seven days."), http.StatusSeeOther)
}

func AdminPersonMergeAudit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if requireGlobalAdmin(w, r, ctx) == nil {
		return
	}
	preview, err := getters.GetPersonMergeUndoPreview(ctx, mux.Vars(r)["mergeID"])
	if err != nil {
		ctx.Err.Printf("/admin/people/merges audit: %s", err)
		http.Error(w, "Unable to load merge audit", http.StatusInternalServerError)
		return
	}
	page := adminPersonMergeAuditPage(preview)
	page.Flash = r.URL.Query().Get("flash")
	page.Error = r.URL.Query().Get("error")
	renderAdminPersonMergeAudit(w, ctx, page)
}

func AdminPersonMergeUndo(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireGlobalAdmin(w, r, ctx)
	if id == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	eventID := mux.Vars(r)["mergeID"]
	preview, err := getters.GetPersonMergeUndoPreview(ctx, eventID)
	if err != nil {
		http.Redirect(w, r, "/admin/people?error="+url.QueryEscape(err.Error()), http.StatusSeeOther)
		return
	}
	if strings.TrimSpace(r.FormValue("confirmation")) != "RESTORE" || r.FormValue("confirm_restore") != "yes" {
		page := adminPersonMergeAuditPage(preview)
		page.Error = "Type RESTORE and check the confirmation box before restoring."
		renderAdminPersonMergeAudit(w, ctx, page)
		return
	}
	if err := getters.UndoPersonMerge(ctx, eventID, id.PersonID, preview); err != nil {
		ctx.Err.Printf("/admin/people/merges/%s/undo: %s", eventID, err)
		page := adminPersonMergeAuditPage(preview)
		page.Error = err.Error()
		renderAdminPersonMergeAudit(w, ctx, page)
		return
	}
	http.Redirect(w, r, "/admin/people/merges/"+eventID+"?flash="+url.QueryEscape("Merge restored. The audit record was retained."), http.StatusSeeOther)
}

func groupPersonEmailConflicts(conflicts []*types.PersonEmailConflict) []*AdminPersonConflictGroup {
	byEmail := make(map[string]*AdminPersonConflictGroup)
	var groups []*AdminPersonConflictGroup
	for _, conflict := range conflicts {
		group := byEmail[conflict.Email]
		if group == nil {
			group = &AdminPersonConflictGroup{Email: conflict.Email}
			byEmail[conflict.Email] = group
			groups = append(groups, group)
		}
		group.People = append(group.People, conflict)
	}
	for _, group := range groups {
		if len(group.People) < 2 {
			continue
		}
		canonical := group.People[0]
		for _, source := range group.People[1:] {
			group.Links = append(group.Links, AdminPersonConflictLink{
				CanonicalName: canonical.PersonName,
				SourceName:    source.PersonName,
				URL: "/admin/people/merge?canonical=" + url.QueryEscape(canonical.PersonID) +
					"&source=" + url.QueryEscape(source.PersonID),
			})
		}
	}
	return groups
}

func adminPersonMergePage(preview *getters.PersonMergePreview, message string) *AdminPersonMergePage {
	page := &AdminPersonMergePage{Preview: preview, Error: message, Year: helpers.CurrentYear()}
	if preview == nil {
		return page
	}
	for _, field := range preview.Fields {
		choice := "canonical"
		if mergeValueEmpty(field.Canonical) && !mergeValueEmpty(field.Source) {
			choice = "source"
		}
		page.Fields = append(page.Fields, AdminPersonMergeField{
			Key:              field.Spec.Key,
			Label:            field.Spec.Label,
			Kind:             field.Spec.Kind,
			CanonicalDisplay: displayPersonMergeValue(field.Canonical),
			SourceDisplay:    displayPersonMergeValue(field.Source),
			DefaultChoice:    choice,
		})
	}
	return page
}

func parsePersonMergeDecisions(r *http.Request, preview *getters.PersonMergePreview) (map[string]getters.PersonMergeDecision, error) {
	decisions := make(map[string]getters.PersonMergeDecision, len(preview.Fields))
	for _, field := range preview.Fields {
		choice := strings.TrimSpace(r.FormValue("choice_" + field.Spec.Key))
		var value any
		switch choice {
		case "canonical":
			value = field.Canonical
		case "source":
			value = field.Source
		case "custom":
			custom := strings.TrimSpace(r.FormValue("custom_" + field.Spec.Key))
			switch field.Spec.Kind {
			case "bool":
				parsed, err := strconv.ParseBool(custom)
				if err != nil {
					return nil, fmt.Errorf("choose a custom value for %s", field.Spec.Label)
				}
				value = parsed
			case "value":
				if custom == "" {
					value = nil
				} else {
					parsed, err := time.ParseInLocation("2006-01-02T15:04", custom, time.Local)
					if err != nil {
						return nil, fmt.Errorf("enter a valid date and time for %s", field.Spec.Label)
					}
					value = parsed
				}
			default:
				value = custom
			}
		default:
			return nil, fmt.Errorf("choose which value to keep for %s", field.Spec.Label)
		}
		decisions[field.Spec.Key] = getters.PersonMergeDecision{Choice: choice, Value: value}
	}
	return decisions, nil
}

func adminPersonMergeAuditPage(preview *getters.PersonMergeUndoPreview) *AdminPersonMergeAuditPage {
	page := &AdminPersonMergeAuditPage{Undo: preview, Year: helpers.CurrentYear()}
	if preview == nil || preview.Event == nil {
		return page
	}
	page.CanUndo = preview.Event.Status == "merged" && time.Now().Before(preview.Event.UndoExpiresAt)
	for _, spec := range getters.PersonMergeFieldSpecs {
		decision, ok := preview.Event.Decisions[spec.Key]
		if !ok {
			continue
		}
		choice := map[string]string{"canonical": "Kept destination", "source": "Used source", "custom": "Custom"}[decision.Choice]
		page.Decisions = append(page.Decisions, AdminPersonMergeDecision{
			Label: spec.Label, Choice: choice, Value: displayPersonMergeValue(decision.Value),
		})
	}
	return page
}

func mergeValueEmpty(value any) bool {
	return value == nil || strings.TrimSpace(fmt.Sprint(value)) == ""
}

func displayPersonMergeValue(value any) string {
	if value == nil || strings.TrimSpace(fmt.Sprint(value)) == "" {
		return "(empty)"
	}
	if typed, ok := value.(bool); ok {
		if typed {
			return "Yes"
		}
		return "No"
	}
	return fmt.Sprint(value)
}

func renderAdminPeople(w http.ResponseWriter, ctx *config.AppContext, page *AdminPeoplePage) {
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/people.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/people template: %s", err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func renderAdminPersonMerge(w http.ResponseWriter, ctx *config.AppContext, page *AdminPersonMergePage) {
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/person_merge.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/people/merge template: %s", err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func renderAdminPersonMergeAudit(w http.ResponseWriter, ctx *config.AppContext, page *AdminPersonMergeAuditPage) {
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/person_merge_audit.tmpl", page); err != nil {
		ctx.Err.Printf("/admin/people/merges template: %s", err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}
