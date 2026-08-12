package handlers

import (
	"fmt"
	"net/http"
	netmail "net/mail"
	"net/url"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/missives"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

type ConferenceMissivesPage struct {
	Conf             *types.Conf
	Campaigns        []*types.ConferenceEmailCampaign
	Occurrences      []*types.ConferenceEmailOccurrence
	Missives         []*mtypes.Letter
	View             string
	ScheduleURL      string
	OnSubURL         string
	TemplatesURL     string
	DevEmailOverride string
	CanGenerateDev   bool
	CanSendDevDrafts bool
	DraftCount       int
	Counts           ConferenceMissiveTabCounts
	Flash            string
	Error            string
	Year             uint
}

type ConferenceMissiveTabCounts struct {
	Schedule  int
	OnSub     int
	Templates int
}

const (
	conferenceMissiveViewSchedule  = "schedule"
	conferenceMissiveViewOnSub     = "onsub"
	conferenceMissiveViewTemplates = "templates"
)

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
	generatedOccurrences := conferenceGeneratedMissiveOccurrences(occurrences)
	draftCount := conferenceEditableDraftCount(generatedOccurrences)
	standalone, err := getters.ListConferenceStandaloneMissives(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/admin/missives standalone: %s", conf.Tag, err)
		http.Error(w, "Unable to load event missives", http.StatusInternalServerError)
		return
	}
	view := normalizeConferenceMissiveView(r.URL.Query().Get("view"))
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/conference_missives.tmpl", &ConferenceMissivesPage{
		Conf: conf, Campaigns: campaigns, Occurrences: generatedOccurrences, Missives: standalone,
		View:             view,
		ScheduleURL:      conferenceMissiveIndexURL(conf.Tag, conferenceMissiveViewSchedule),
		OnSubURL:         conferenceMissiveIndexURL(conf.Tag, conferenceMissiveViewOnSub),
		TemplatesURL:     conferenceMissiveIndexURL(conf.Tag, conferenceMissiveViewTemplates),
		DevEmailOverride: strings.TrimSpace(ctx.Env.DevEmailOverride),
		CanGenerateDev:   !ctx.Env.Prod && conf.UsesConferenceEmailCampaigns(),
		CanSendDevDrafts: !ctx.Env.Prod && conf.UsesConferenceEmailCampaigns() && !ctx.Env.MailOff && strings.TrimSpace(ctx.Env.DevEmailOverride) != "" && draftCount > 0,
		DraftCount:       draftCount,
		Counts:           ConferenceMissiveTabCounts{Schedule: len(generatedOccurrences), OnSub: len(standalone), Templates: len(campaigns)},
		Flash:            r.URL.Query().Get("flash"), Error: r.URL.Query().Get("error"), Year: helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("/%s/admin/missives render: %s", conf.Tag, err)
		http.Error(w, "Unable to render event emails", http.StatusInternalServerError)
	}
}

func conferenceEditableDraftCount(occurrences []*types.ConferenceEmailOccurrence) int {
	count := 0
	for _, occurrence := range occurrences {
		if occurrence != nil && (occurrence.Status == "draft" || occurrence.Status == "failed") {
			count++
		}
	}
	return count
}

func conferenceGeneratedMissiveOccurrences(occurrences []*types.ConferenceEmailOccurrence) []*types.ConferenceEmailOccurrence {
	generated := make([]*types.ConferenceEmailOccurrence, 0, len(occurrences))
	for _, occurrence := range occurrences {
		if occurrence != nil && strings.TrimSpace(occurrence.MissiveID) != "" {
			generated = append(generated, occurrence)
		}
	}
	return generated
}

func normalizeConferenceMissiveView(view string) string {
	switch strings.ToLower(strings.TrimSpace(view)) {
	case conferenceMissiveViewOnSub:
		return conferenceMissiveViewOnSub
	case conferenceMissiveViewTemplates:
		return conferenceMissiveViewTemplates
	default:
		return conferenceMissiveViewSchedule
	}
}

func conferenceMissiveIndexURL(tag, view string) string {
	return fmt.Sprintf("/%s/admin/missives?view=%s", url.PathEscape(tag), url.QueryEscape(normalizeConferenceMissiveView(view)))
}

func ConferenceMissiveCampaignEdit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if err := getters.EnsureConferenceEmailCampaigns(ctx, conf, time.Now()); err != nil {
		ctx.Err.Printf("/%s/admin/missives/campaigns ensure: %s", conf.Tag, err)
		http.Error(w, "Unable to refresh event email template", http.StatusInternalServerError)
		return
	}
	campaignID := strings.TrimSpace(mux.Vars(r)["campaignID"])
	campaigns, err := getters.ListConferenceEmailCampaigns(ctx, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load event email template", http.StatusInternalServerError)
		return
	}
	var campaign *types.ConferenceEmailCampaign
	for _, candidate := range campaigns {
		if candidate.ID == campaignID {
			campaign = candidate
			break
		}
	}
	if campaign == nil {
		http.Error(w, "Event email template not found", http.StatusNotFound)
		return
	}
	data, err := conferenceCampaignPreviewData(ctx, conf, campaign)
	if err != nil {
		ctx.Err.Printf("/%s/admin/missives/campaigns/%s data: %s", conf.Tag, campaignID, err)
		http.Error(w, "Unable to prepare event email template", http.StatusInternalServerError)
		return
	}
	form := formFromTemplatedLetter(&mtypes.Letter{Title: campaign.Title, Markdown: campaign.Markdown})
	page := conferenceCampaignBuilderPage(ctx, conf, campaign, form, data, r.URL.Query().Get("flash"), r.URL.Query().Get("error"))
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/templated_missives.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/admin/missives/campaigns/%s render: %s", conf.Tag, campaignID, err)
		http.Error(w, "Unable to render event email template", http.StatusInternalServerError)
	}
}

func conferenceCampaignBuilderPage(ctx *config.AppContext, conf *types.Conf, campaign *types.ConferenceEmailCampaign, form TemplatedMissiveForm, data *emails.ConferenceCampaignData, flash, errMsg string) *TemplatedMissivesPage {
	if heroURL, err := conferenceEmailHeroURL(ctx, conf); err == nil {
		form.Hero = heroURL
	}
	campaignURL := fmt.Sprintf("/%s/admin/missives/campaigns/%s", url.PathEscape(conf.Tag), url.PathEscape(campaign.ID))
	return &TemplatedMissivesPage{
		Form: form, IsCampaign: true, Conf: conf, Campaign: campaign, CampaignEnabled: campaign.Enabled,
		EditorTitle:       "Edit " + campaign.Kind + " · " + conf.Desc,
		EditorHeading:     "Edit " + campaign.Kind,
		EditorDescription: "Build and preview this event-specific campaign with the newsletter editor.",
		BackURL:           fmt.Sprintf("/%s/admin/missives?view=templates", url.PathEscape(conf.Tag)), BackLabel: "Event missives",
		FormAction: campaignURL, TestSendAction: campaignURL + "/test-send",
		UploadImageURL: fmt.Sprintf("/%s/admin/missives/upload-image", url.PathEscape(conf.Tag)), SaveLabel: "Save campaign template",
		FieldGroups:   onlyForTemplateFields(types.ConferenceCampaignTemplateOnlyFor(campaign.Kind)),
		PreviewValues: conferenceCampaignTemplatePreviewValues(data),
		FlashMessage:  flash, ErrorMessage: errMsg, SpacesReady: spaces.IsConfigured(), Year: helpers.CurrentYear(),
	}
}

func conferenceCampaignTemplatePreviewValues(data *emails.ConferenceCampaignData) []TemplatePreviewValue {
	if data == nil || data.Conf == nil {
		return nil
	}
	values := map[string]string{
		".Email": data.Email, ".Name": data.Name, ".CampaignTitle": data.CampaignTitle, ".URI": data.URI,
		".DashboardLink": data.DashboardLink, ".AffiliateDashboardLink": data.AffiliateDashboardLink,
		".DoorsOpen": data.DoorsOpen, ".BreakfastStart": data.BreakfastStart, ".SpeakerDinnerTime": data.SpeakerDinnerTime,
		".SpeakerDinnerLocation": data.SpeakerDinnerLocation, ".GeneratedUpdates": data.GeneratedUpdates,
		".SponsorAcknowledgement": data.SponsorAcknowledgement,
		".TalkDetails":            data.TalkDetails, ".Conf.Tag": data.Conf.Tag, ".Conf.Desc": data.Conf.Desc,
		".Conf.Location": data.Conf.Location, ".Conf.Venue": data.Conf.Venue, ".Conf.Emoji": data.Conf.Emoji,
	}
	out := make([]TemplatePreviewValue, 0, len(values)*2)
	for token, value := range values {
		out = append(out, TemplatePreviewValue{Token: "{{ " + token + " }}", Value: value})
		out = append(out, TemplatePreviewValue{Token: token, Value: value})
	}
	return out
}

func conferenceCampaignPreviewData(ctx *config.AppContext, conf *types.Conf, campaign *types.ConferenceEmailCampaign) (*emails.ConferenceCampaignData, error) {
	occurrence := &types.ConferenceEmailOccurrence{
		CampaignKind: campaign.Kind, Audience: campaign.Audience, ConferenceID: conf.Ref,
		ConferenceTag: conf.Tag, SendAt: time.Now().In(conf.Loc()),
	}
	if occurrences, err := getters.ListConferenceEmailOccurrences(ctx, conf.Ref); err == nil {
		for _, candidate := range occurrences {
			if candidate.CampaignID == campaign.ID {
				occurrence = candidate
				occurrence.ConferenceTag = conf.Tag
				break
			}
		}
	}
	updates, err := conferenceEmailGeneratedUpdates(ctx, conf, occurrence)
	if err != nil {
		return nil, err
	}
	recipient := &types.ConferenceEmailRecipient{Email: "preview@btcpp.dev", Name: "Subscriber", SpeakerConfID: occurrence.TargetKey}
	if recipients, recipientErr := getters.ConferenceEmailRecipients(ctx, occurrence); recipientErr == nil && len(recipients) > 0 {
		recipient = recipients[0]
	}
	data := conferenceCampaignRecipientData(ctx, conf, recipient)
	if _, headline, ok := strings.Cut(campaign.Title, ": "); ok {
		data.CampaignTitle = strings.TrimSpace(headline)
	} else {
		data.CampaignTitle = strings.TrimSpace(campaign.Title)
	}
	data.GeneratedUpdates = updates
	if conferenceCampaignHasSponsorFooter(campaign.Kind) {
		data.SponsorAcknowledgement, err = conferenceSponsorsMarkdown(ctx, conf)
		if err != nil {
			return nil, err
		}
	}
	data.SendAt = occurrence.SendAt
	return data, nil
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
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadFileBytes)
	if err := r.ParseMultipartForm(maxUploadFileBytes); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	campaignID := strings.TrimSpace(mux.Vars(r)["campaignID"])
	form := templatedMissiveFormFromRequest(r)
	campaign, err := conferenceCampaignByID(ctx, conf.Ref, campaignID)
	if err != nil {
		http.Error(w, "Event email template not found", http.StatusNotFound)
		return
	}
	if strings.TrimSpace(form.Title) == "" {
		renderConferenceCampaignFormError(w, r, ctx, conf, campaign, form, "Title is required")
		return
	}
	if err := getters.UpdateConferenceEmailCampaign(ctx, conf.Ref, campaignID,
		types.ConferenceCampaignSubject(form.Title), buildTemplatedMissiveMarkdown(form), r.FormValue("enabled") == "1"); err != nil {
		ctx.Err.Printf("/%s/admin/missives/%s update: %s", conf.Tag, campaignID, err)
		renderConferenceCampaignFormError(w, r, ctx, conf, campaign, form, "Could not update that campaign: "+err.Error())
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/%s/admin/missives/campaigns/%s?flash=%s", url.PathEscape(conf.Tag), url.PathEscape(campaignID), url.QueryEscape("Campaign template saved")), http.StatusSeeOther)
}

func ConferenceMissiveCampaignTestSend(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadFileBytes)
	if err := r.ParseMultipartForm(maxUploadFileBytes); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	campaignID := strings.TrimSpace(mux.Vars(r)["campaignID"])
	campaign, err := conferenceCampaignByID(ctx, conf.Ref, campaignID)
	if err != nil {
		http.Error(w, "Event email template not found", http.StatusNotFound)
		return
	}
	form := templatedMissiveFormFromRequest(r)
	addr, err := netmail.ParseAddress(strings.TrimSpace(form.TestEmail))
	if err != nil || addr.Address == "" {
		renderConferenceCampaignFormError(w, r, ctx, conf, campaign, form, "Enter a valid test recipient email")
		return
	}
	data, err := conferenceCampaignPreviewData(ctx, conf, campaign)
	if err != nil {
		renderConferenceCampaignFormError(w, r, ctx, conf, campaign, form, "Unable to prepare test: "+err.Error())
		return
	}
	data.Email = addr.Address
	data.Name = "Test Recipient"
	data.DashboardLink = helpers.EmailLink(ctx, addr.Address, "/dashboard")
	data.AffiliateDashboardLink = helpers.EmailLink(ctx, addr.Address, "/dashboard/affiliate")
	markdown := materializeConferenceDraftMarkdown(buildTemplatedMissiveMarkdown(form), data.GeneratedUpdates, data.SponsorAcknowledgement)
	heroURL, err := conferenceEmailHeroURL(ctx, conf)
	if err != nil {
		renderConferenceCampaignFormError(w, r, ctx, conf, campaign, form, "Unable to choose event image: "+err.Error())
		return
	}
	markdown = conferenceEmailMarkdownWithHero(markdown, heroURL)
	letter := &mtypes.Letter{
		UID: campaign.TemplateMissiveUID, Title: "[TEST] " + types.ConferenceCampaignSubject(form.Title),
		OnlyFor: mtypes.OnlyForTemplated, Markdown: markdown,
	}
	jobKey := fmt.Sprintf("conference-campaign-test-%s-%d", campaign.ID, time.Now().UnixNano())
	if err := emails.SendConferenceCampaign(ctx, letter, data, jobKey, nil); err != nil {
		renderConferenceCampaignFormError(w, r, ctx, conf, campaign, form, "Test send failed: "+err.Error())
		return
	}
	page := conferenceCampaignBuilderPage(ctx, conf, campaign, form, data, "Test sent to "+addr.Address, "")
	renderTemplatedMissivesEditor(w, r, ctx, page)
}

func ConferenceMissivesDevGenerateAll(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	if ctx == nil || ctx.Env == nil || ctx.InProduction || ctx.Env.Prod {
		http.NotFound(w, r)
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if err := getters.EnsureConferenceEmailCampaigns(ctx, conf, time.Now()); err != nil {
		redirectConferenceMissives(w, r, conf.Tag, "", "Could not prepare event templates: "+err.Error())
		return
	}
	builds, err := getters.ClaimAllConferenceEmailBuildsForConference(ctx, time.Now(), 1000, conf.Ref)
	if err != nil {
		redirectConferenceMissives(w, r, conf.Tag, "", "Could not claim event drafts: "+err.Error())
		return
	}
	if len(builds) == 0 {
		redirectConferenceMissives(w, r, conf.Tag, "No unbuilt event drafts remain", "")
		return
	}
	built := 0
	var failures []string
	for _, occurrence := range builds {
		if buildErr := buildConferenceEmailDraftWithReview(ctx, occurrence, false); buildErr != nil {
			_ = getters.FailConferenceEmailOccurrence(ctx, occurrence.ID, "failed", buildErr)
			failures = append(failures, occurrence.CampaignKind+": "+buildErr.Error())
			continue
		}
		built++
	}
	if len(failures) > 0 {
		redirectConferenceMissives(w, r, conf.Tag, "", fmt.Sprintf("Generated %d of %d drafts. Failed: %s", built, len(builds), strings.Join(failures, "; ")))
		return
	}
	redirectConferenceMissives(w, r, conf.Tag, fmt.Sprintf("Generated %d editable drafts from current event data", built), "")
}

func ConferenceMissivesDevSendAll(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	if ctx == nil || ctx.Env == nil || ctx.InProduction || ctx.Env.Prod {
		http.NotFound(w, r)
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if ctx.Env.MailOff {
		redirectConferenceMissives(w, r, conf.Tag, "", "Email delivery is disabled; turn off MAILER_OFF before sending drafts")
		return
	}
	address, err := netmail.ParseAddress(strings.TrimSpace(ctx.Env.DevEmailOverride))
	if err != nil || strings.TrimSpace(address.Address) == "" {
		redirectConferenceMissives(w, r, conf.Tag, "", "Set DEV_EMAIL_OVERRIDE to a valid email before sending drafts")
		return
	}
	sends, err := getters.ClaimAllConferenceEmailDraftsForConference(ctx, time.Now(), 1000, conf.Ref)
	if err != nil {
		redirectConferenceMissives(w, r, conf.Tag, "", "Could not claim generated drafts: "+err.Error())
		return
	}
	if len(sends) == 0 {
		redirectConferenceMissives(w, r, conf.Tag, "", "Generate at least one draft before sending")
		return
	}
	sent := 0
	var failures []string
	for _, occurrence := range sends {
		if sendErr := sendConferenceEmailOccurrence(ctx, occurrence); sendErr != nil {
			_ = getters.FailConferenceEmailOccurrence(ctx, occurrence.ID, "failed", sendErr)
			failures = append(failures, occurrence.CampaignKind+": "+sendErr.Error())
			continue
		}
		sent++
	}
	if len(failures) > 0 {
		redirectConferenceMissives(w, r, conf.Tag, "", fmt.Sprintf("Sent %d of %d drafts through %s. Failed: %s", sent, len(sends), address.Address, strings.Join(failures, "; ")))
		return
	}
	redirectConferenceMissives(w, r, conf.Tag, fmt.Sprintf("Sent all personalized deliveries for %d drafts through %s", sent, address.Address), "")
}

func ConferenceMissiveCampaignUploadImage(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	if _, err := helpers.FindConf(r, ctx); err != nil {
		handle404(w, r, ctx)
		return
	}
	uploadTemplatedMissiveImage(w, r, ctx)
}

func conferenceCampaignByID(ctx *config.AppContext, confID, campaignID string) (*types.ConferenceEmailCampaign, error) {
	campaigns, err := getters.ListConferenceEmailCampaigns(ctx, confID)
	if err != nil {
		return nil, err
	}
	for _, campaign := range campaigns {
		if campaign.ID == campaignID {
			return campaign, nil
		}
	}
	return nil, fmt.Errorf("conference campaign not found")
}

func renderConferenceCampaignFormError(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, conf *types.Conf, campaign *types.ConferenceEmailCampaign, form TemplatedMissiveForm, message string) {
	data, _ := conferenceCampaignPreviewData(ctx, conf, campaign)
	page := conferenceCampaignBuilderPage(ctx, conf, campaign, form, data, "", message)
	renderTemplatedMissivesEditor(w, r, ctx, page)
}

func redirectConferenceMissiveTemplates(w http.ResponseWriter, r *http.Request, tag, flash, errMsg string) {
	query := url.Values{"view": []string{conferenceMissiveViewTemplates}}
	if flash != "" {
		query.Set("flash", flash)
	}
	if errMsg != "" {
		query.Set("error", errMsg)
	}
	destination := fmt.Sprintf("/%s/admin/missives?%s", url.PathEscape(tag), query.Encode())
	http.Redirect(w, r, destination, http.StatusSeeOther)
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
	previewData, err := conferenceOccurrencePreviewData(ctx, conf, occurrence, letter)
	if err != nil {
		ctx.Err.Printf("/%s/admin/missives/occurrences/%s data: %s", conf.Tag, occurrenceID, err)
		http.Error(w, "Unable to prepare draft preview", http.StatusInternalServerError)
		return
	}
	page := conferenceOccurrenceBuilderPage(ctx, conf, occurrence, letter, formFromTemplatedLetter(letter), previewData, r.URL.Query().Get("flash"), r.URL.Query().Get("error"))
	renderTemplatedMissivesEditor(w, r, ctx, page)
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
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadFileBytes)
	if err := r.ParseMultipartForm(maxUploadFileBytes); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
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
	form := templatedMissiveFormFromRequest(r)
	previewData, previewErr := conferenceOccurrencePreviewData(ctx, conf, occurrence, letter)
	if strings.TrimSpace(form.Title) == "" {
		page := conferenceOccurrenceBuilderPage(ctx, conf, occurrence, letter, form, previewData, "", "Title is required")
		renderTemplatedMissivesEditor(w, r, ctx, page)
		return
	}
	if previewErr != nil {
		ctx.Err.Printf("/%s/admin/missives/occurrences/%s data: %s", conf.Tag, occurrenceID, previewErr)
	}
	if err := getters.UpdateConferenceOccurrenceDraft(ctx, conf.Ref, occurrenceID, types.ConferenceCampaignSubject(form.Title), buildTemplatedMissiveMarkdown(form)); err != nil {
		ctx.Err.Printf("/%s/admin/missives/occurrences/%s update: %s", conf.Tag, occurrenceID, err)
		page := conferenceOccurrenceBuilderPage(ctx, conf, occurrence, letter, form, previewData, "", "Could not save draft: "+err.Error())
		renderTemplatedMissivesEditor(w, r, ctx, page)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/%s/admin/missives/occurrences/%s?flash=%s", url.PathEscape(conf.Tag), url.PathEscape(occurrenceID), url.QueryEscape("Generated draft saved")), http.StatusSeeOther)
}

func conferenceOccurrencePreviewData(ctx *config.AppContext, conf *types.Conf, occurrence *types.ConferenceEmailOccurrence, letter *mtypes.Letter) (*emails.ConferenceCampaignData, error) {
	previewRecipient := &types.ConferenceEmailRecipient{Email: occurrence.TargetEmail, Name: "Subscriber", SpeakerConfID: occurrence.TargetKey}
	if recipients, err := getters.ConferenceEmailRecipients(ctx, occurrence); err == nil && len(recipients) > 0 {
		previewRecipient = recipients[0]
	}
	data := conferenceCampaignRecipientData(ctx, conf, previewRecipient)
	data.SendAt = occurrence.SendAt
	if data.Email == "" {
		data.Email = "preview@btcpp.dev"
	}
	if _, headline, ok := strings.Cut(letter.Title, ": "); ok {
		data.CampaignTitle = strings.TrimSpace(headline)
	} else {
		data.CampaignTitle = strings.TrimSpace(letter.Title)
	}
	return data, nil
}

func conferenceOccurrenceBuilderPage(ctx *config.AppContext, conf *types.Conf, occurrence *types.ConferenceEmailOccurrence, letter *mtypes.Letter, form TemplatedMissiveForm, data *emails.ConferenceCampaignData, flash, errMsg string) *TemplatedMissivesPage {
	baseURL := fmt.Sprintf("/%s/admin/missives/occurrences/%s", url.PathEscape(conf.Tag), url.PathEscape(occurrence.ID))
	return &TemplatedMissivesPage{
		Current: letter, Form: form, IsOccurrence: true, Conf: conf, Occurrence: occurrence,
		EditorTitle:       "Edit generated draft · " + conf.Desc,
		EditorHeading:     "Edit generated draft",
		EditorDescription: "This copy was populated from event data at build time. Saving changes only this occurrence.",
		BackURL:           fmt.Sprintf("/%s/admin/missives", url.PathEscape(conf.Tag)), BackLabel: "Event missives",
		FormAction: baseURL, TestSendAction: baseURL + "/test-send", UploadImageURL: fmt.Sprintf("/%s/admin/missives/upload-image", url.PathEscape(conf.Tag)), SaveLabel: "Save generated draft",
		FieldGroups: onlyForTemplateFields(types.ConferenceCampaignTemplateOnlyFor(occurrence.CampaignKind)), PreviewValues: conferenceCampaignTemplatePreviewValues(data),
		RebuildAction: baseURL + "/rebuild", CancelAction: baseURL + "/cancel",
		FlashMessage: flash, ErrorMessage: errMsg, SpacesReady: spaces.IsConfigured(), Year: helpers.CurrentYear(),
	}
}

func ConferenceMissiveDraftTestSend(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxUploadFileBytes)
	if err := r.ParseMultipartForm(maxUploadFileBytes); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	occurrenceID := strings.TrimSpace(mux.Vars(r)["occurrenceID"])
	occurrence, err := getters.GetConferenceEmailOccurrence(ctx, conf.Ref, occurrenceID)
	if err != nil || occurrence.MissiveUID == 0 {
		http.Error(w, "Draft not found", http.StatusNotFound)
		return
	}
	occurrence.SendLabel = occurrence.SendAt.In(conf.Loc()).Format("Mon, Jan 2 at 3:04 PM MST")
	storedLetter, err := getters.GetLetter(ctx, occurrence.MissiveUID)
	if err != nil {
		http.Error(w, "Draft not found", http.StatusNotFound)
		return
	}
	form := templatedMissiveFormFromRequest(r)
	addr, addressErr := netmail.ParseAddress(strings.TrimSpace(form.TestEmail))
	data, dataErr := conferenceOccurrencePreviewData(ctx, conf, occurrence, storedLetter)
	if addressErr != nil || addr.Address == "" {
		page := conferenceOccurrenceBuilderPage(ctx, conf, occurrence, storedLetter, form, data, "", "Enter a valid test recipient email")
		renderTemplatedMissivesEditor(w, r, ctx, page)
		return
	}
	if dataErr != nil {
		page := conferenceOccurrenceBuilderPage(ctx, conf, occurrence, storedLetter, form, data, "", "Unable to prepare test: "+dataErr.Error())
		renderTemplatedMissivesEditor(w, r, ctx, page)
		return
	}
	data.Email = addr.Address
	data.Name = "Test Recipient"
	data.DashboardLink = helpers.EmailLink(ctx, addr.Address, "/dashboard")
	data.AffiliateDashboardLink = helpers.EmailLink(ctx, addr.Address, "/dashboard/affiliate")
	letter := &mtypes.Letter{
		UID: storedLetter.UID, Title: "[TEST] " + types.ConferenceCampaignSubject(form.Title),
		OnlyFor: mtypes.OnlyForTemplated, Markdown: buildTemplatedMissiveMarkdown(form),
	}
	jobKey := fmt.Sprintf("conference-occurrence-test-%s-%d", occurrence.ID, time.Now().UnixNano())
	if err := emails.SendConferenceCampaign(ctx, letter, data, jobKey, nil); err != nil {
		page := conferenceOccurrenceBuilderPage(ctx, conf, occurrence, storedLetter, form, data, "", "Test send failed: "+err.Error())
		renderTemplatedMissivesEditor(w, r, ctx, page)
		return
	}
	page := conferenceOccurrenceBuilderPage(ctx, conf, occurrence, storedLetter, form, data, "Test sent to "+addr.Address, "")
	renderTemplatedMissivesEditor(w, r, ctx, page)
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
