package handlers

import (
	"fmt"
	"html/template"
	"net/http"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/missives"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

type ConferenceMissivesPage struct {
	Conf        *types.Conf
	Campaigns   []*types.ConferenceEmailCampaign
	Occurrences []*types.ConferenceEmailOccurrence
	Missives    []*mtypes.Letter
	Flash       string
	Error       string
	Year        uint
}

type ConferenceMissiveDraftPage struct {
	Conf       *types.Conf
	Occurrence *types.ConferenceEmailOccurrence
	Title      string
	Markdown   string
	Preview    template.HTML
	Error      string
	Year       uint
}

func ConferenceMissivesAdmin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if err := getters.EnsureConferenceEmailCampaigns(ctx, conf, time.Now()); err != nil {
		ctx.Err.Printf("/%s/admin/missives reconcile: %s", conf.Tag, err)
		http.Error(w, "Unable to prepare event emails", http.StatusInternalServerError)
		return
	}
	campaigns, err := getters.ListConferenceEmailCampaigns(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/admin/missives campaigns: %s", conf.Tag, err)
		http.Error(w, "Unable to load event emails", http.StatusInternalServerError)
		return
	}
	occurrences, err := getters.ListConferenceEmailOccurrences(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/admin/missives occurrences: %s", conf.Tag, err)
		http.Error(w, "Unable to load event email schedule", http.StatusInternalServerError)
		return
	}
	for _, occurrence := range occurrences {
		occurrence.BuildLabel = occurrence.BuildAt.In(conf.Loc()).Format("Mon, Jan 2 at 3:04 PM")
		occurrence.SendLabel = occurrence.SendAt.In(conf.Loc()).Format("Mon, Jan 2 at 3:04 PM")
	}
	standalone, err := getters.ListConferenceStandaloneMissives(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/admin/missives standalone: %s", conf.Tag, err)
		http.Error(w, "Unable to load event missives", http.StatusInternalServerError)
		return
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/conference_missives.tmpl", &ConferenceMissivesPage{
		Conf: conf, Campaigns: campaigns, Occurrences: occurrences, Missives: standalone,
		Flash: r.URL.Query().Get("flash"), Error: r.URL.Query().Get("error"), Year: helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("/%s/admin/missives render: %s", conf.Tag, err)
		http.Error(w, "Unable to render event emails", http.StatusInternalServerError)
	}
}

func ConferenceMissiveCampaignUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
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
	campaignID := strings.TrimSpace(mux.Vars(r)["campaignID"])
	if err := getters.UpdateConferenceEmailCampaign(ctx, conf.Ref, campaignID,
		r.FormValue("title"), r.FormValue("markdown"), r.FormValue("enabled") == "1"); err != nil {
		ctx.Err.Printf("/%s/admin/missives/%s update: %s", conf.Tag, campaignID, err)
		redirectConferenceMissives(w, r, conf.Tag, "", "Could not update that campaign")
		return
	}
	redirectConferenceMissives(w, r, conf.Tag, "Campaign saved", "")
}

func ConferenceMissiveDraftEdit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	occurrenceID := strings.TrimSpace(mux.Vars(r)["occurrenceID"])
	occurrence, err := getters.GetConferenceEmailOccurrence(ctx, conf.Ref, occurrenceID)
	if err != nil || occurrence.MissiveUID == 0 {
		http.Error(w, "Draft not found", http.StatusNotFound)
		return
	}
	occurrence.SendLabel = occurrence.SendAt.In(conf.Loc()).Format("Mon, Jan 2 at 3:04 PM MST")
	letter, err := getters.GetLetter(ctx, occurrence.MissiveUID)
	if err != nil {
		http.Error(w, "Draft not found", http.StatusNotFound)
		return
	}
	previewData := conferenceCampaignRecipientData(ctx, conf, &types.ConferenceEmailRecipient{
		Email: occurrence.TargetEmail, Name: "Subscriber", SpeakerConfID: occurrence.TargetKey,
	})
	if previewData.Email == "" {
		previewData.Email = "preview@btcpp.dev"
	}
	preview, err := emails.RenderConferenceCampaignPreview(ctx, letter, previewData)
	if err != nil {
		ctx.Err.Printf("/%s/admin/missives/occurrences/%s preview: %s", conf.Tag, occurrenceID, err)
		http.Error(w, "Unable to render draft preview", http.StatusInternalServerError)
		return
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/conference_missive_draft.tmpl", &ConferenceMissiveDraftPage{
		Conf: conf, Occurrence: occurrence, Title: letter.Title, Markdown: letter.Markdown,
		Preview: template.HTML(preview), Error: r.URL.Query().Get("error"), Year: helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("/%s/admin/missives/occurrences/%s render: %s", conf.Tag, occurrenceID, err)
		http.Error(w, "Unable to render draft", http.StatusInternalServerError)
	}
}

func ConferenceMissiveDraftUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
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
	occurrenceID := strings.TrimSpace(mux.Vars(r)["occurrenceID"])
	if err := getters.UpdateConferenceOccurrenceDraft(ctx, conf.Ref, occurrenceID, r.FormValue("title"), r.FormValue("markdown")); err != nil {
		ctx.Err.Printf("/%s/admin/missives/occurrences/%s update: %s", conf.Tag, occurrenceID, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/missives/occurrences/%s?error=%s", conf.Tag,
			url.PathEscape(occurrenceID), url.QueryEscape("Could not save draft")), http.StatusSeeOther)
		return
	}
	redirectConferenceMissives(w, r, conf.Tag, "Draft saved", "")
}

func ConferenceMissiveOccurrenceCancel(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	occurrenceID := strings.TrimSpace(mux.Vars(r)["occurrenceID"])
	occurrence, err := getters.GetConferenceEmailOccurrence(ctx, conf.Ref, occurrenceID)
	if err != nil {
		redirectConferenceMissives(w, r, conf.Tag, "", "Occurrence not found")
		return
	}
	if occurrence.MissiveUID != 0 && occurrence.Status == "sending" {
		if _, err := missives.CancelMissiveByUID(ctx, occurrence.MissiveUID); err != nil {
			ctx.Err.Printf("/%s event email %s mailer cancel: %s", conf.Tag, occurrenceID, err)
			redirectConferenceMissives(w, r, conf.Tag, "", "Could not cancel queued mailer jobs")
			return
		}
	}
	if err := getters.CancelConferenceEmailOccurrence(ctx, conf.Ref, occurrenceID); err != nil {
		redirectConferenceMissives(w, r, conf.Tag, "", err.Error())
		return
	}
	redirectConferenceMissives(w, r, conf.Tag, "Event email cancelled", "")
}

func ConferenceMissiveOccurrenceRebuild(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	occurrenceID := strings.TrimSpace(mux.Vars(r)["occurrenceID"])
	if err := getters.ResetConferenceOccurrenceDraft(ctx, conf.Ref, occurrenceID); err != nil {
		redirectConferenceMissives(w, r, conf.Tag, "", "Could not rebuild that draft")
		return
	}
	occurrence, err := getters.GetConferenceEmailOccurrence(ctx, conf.Ref, occurrenceID)
	if err != nil {
		redirectConferenceMissives(w, r, conf.Tag, "", "Could not reload rebuilt draft")
		return
	}
	if err := buildConferenceEmailDraft(ctx, occurrence); err != nil {
		_ = getters.FailConferenceEmailOccurrence(ctx, occurrence.ID, "failed", err)
		redirectConferenceMissives(w, r, conf.Tag, "", "Could not populate rebuilt draft")
		return
	}
	redirectConferenceMissives(w, r, conf.Tag, "Draft rebuilt from current event data", "")
}

func redirectConferenceMissives(w http.ResponseWriter, r *http.Request, tag, flash, errMsg string) {
	query := url.Values{}
	if flash != "" {
		query.Set("flash", flash)
	}
	if errMsg != "" {
		query.Set("error", errMsg)
	}
	destination := fmt.Sprintf("/%s/admin/missives", tag)
	if encoded := query.Encode(); encoded != "" {
		destination += "?" + encoded
	}
	http.Redirect(w, r, destination, http.StatusSeeOther)
}
