package handlers

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

const conferenceEmailAutomationInterval = 5 * time.Minute
const conferenceEmailStartupDelay = 5 * time.Second

func StartConferenceEmailAutomation(ctx *config.AppContext) {
	if ctx == nil || ctx.Env == nil || !ctx.InProduction || ctx.Env.MailOff {
		if ctx != nil && ctx.Infos != nil {
			ctx.Infos.Println("conference email automation disabled outside mail-enabled production")
		}
		return
	}
	go func() {
		// Final-attendee sends render ticket PDFs through the local HTTP
		// server. Give ListenAndServe a moment to bind before startup catch-up.
		time.Sleep(conferenceEmailStartupDelay)
		runConferenceEmailAutomation(ctx, time.Now())
		ticker := time.NewTicker(conferenceEmailAutomationInterval)
		defer ticker.Stop()
		for now := range ticker.C {
			runConferenceEmailAutomation(ctx, now)
		}
	}()
}

func runConferenceEmailAutomation(ctx *config.AppContext, now time.Time) {
	if err := getters.ReconcileConferenceEmailCampaigns(ctx, now); err != nil {
		ctx.Err.Printf("conference email reconciliation failed: %s", err)
		return
	}
	builds, err := getters.ClaimConferenceEmailBuilds(ctx, now, 20)
	if err != nil {
		ctx.Err.Printf("conference email build claim failed: %s", err)
		return
	}
	for _, occurrence := range builds {
		if err := buildConferenceEmailDraft(ctx, occurrence); err != nil {
			ctx.Err.Printf("conference email draft %s failed: %s", occurrence.ID, err)
			_ = getters.FailConferenceEmailOccurrence(ctx, occurrence.ID, "failed", err)
		}
	}
	runConferenceEmailSendAutomation(ctx, now)
}

func runConferenceEmailSendAutomation(ctx *config.AppContext, now time.Time) {
	sends, err := getters.ClaimConferenceEmailSends(ctx, now, 10)
	if err != nil {
		ctx.Err.Printf("conference email send claim failed: %s", err)
		return
	}
	for _, occurrence := range sends {
		if err := sendConferenceEmailOccurrence(ctx, occurrence); err != nil {
			ctx.Err.Printf("conference email send %s failed: %s", occurrence.ID, err)
			_ = getters.FailConferenceEmailOccurrence(ctx, occurrence.ID, "failed", err)
		}
	}
}

func runConferenceEmailDraftAutomationForConference(ctx *config.AppContext, conf *types.Conf, now time.Time) {
	if err := getters.EnsureConferenceEmailCampaigns(ctx, conf, now); err != nil {
		ctx.Err.Printf("conference email reconciliation for %s failed: %s", conf.Tag, err)
		return
	}
	builds, err := getters.ClaimConferenceEmailBuildsForConference(ctx, now, 20, conf.Ref)
	if err != nil {
		ctx.Err.Printf("conference email build claim for %s failed: %s", conf.Tag, err)
		return
	}
	for _, occurrence := range builds {
		if err := buildConferenceEmailDraft(ctx, occurrence); err != nil {
			ctx.Err.Printf("conference email draft %s failed: %s", occurrence.ID, err)
			_ = getters.FailConferenceEmailOccurrence(ctx, occurrence.ID, "failed", err)
		}
	}
}

func buildConferenceEmailDraft(ctx *config.AppContext, occurrence *types.ConferenceEmailOccurrence) error {
	conf, err := getters.GetConfByRef(ctx, occurrence.ConferenceID)
	if err != nil || conf == nil {
		return fmt.Errorf("load conference for draft: %w", err)
	}
	campaigns, err := getters.ListConferenceEmailCampaigns(ctx, conf.Ref)
	if err != nil {
		return err
	}
	var campaign *types.ConferenceEmailCampaign
	for _, candidate := range campaigns {
		if candidate.ID == occurrence.CampaignID {
			campaign = candidate
			break
		}
	}
	if campaign == nil {
		return fmt.Errorf("campaign %s not found", occurrence.CampaignID)
	}
	updates, err := conferenceEmailGeneratedUpdates(ctx, conf, occurrence)
	if err != nil {
		return err
	}
	markdown := strings.ReplaceAll(campaign.Markdown, "{{ .GeneratedUpdates }}", updates)
	expiry := conf.EndDate
	var expiryPtr *time.Time
	if !expiry.IsZero() {
		expiryPtr = &expiry
	}
	letter, err := getters.CreateConferenceOccurrenceDraft(ctx, occurrence, campaign.Title, markdown, expiryPtr)
	if err != nil {
		return err
	}
	ctx.Infos.Printf("conference email draft MISS-%d built for %s/%s", letter.UID, conf.Tag, occurrence.CampaignKind)
	if err := emails.SendConferenceCampaignDraftReview(ctx, conf, occurrence, letter); err != nil {
		ctx.Err.Printf("conference email draft MISS-%d review notification failed: %s", letter.UID, err)
	}
	return nil
}

func conferenceEmailGeneratedUpdates(ctx *config.AppContext, conf *types.Conf, occurrence *types.ConferenceEmailOccurrence) (string, error) {
	var sections []string
	switch occurrence.CampaignKind {
	case types.ConferenceCampaignAttendeeReminder70,
		types.ConferenceCampaignAttendeeReminder49,
		types.ConferenceCampaignAttendeeReminder28,
		types.ConferenceCampaignAttendeeFinal:
		speakers, err := getters.ConferenceEmailRecipients(ctx, &types.ConferenceEmailOccurrence{Audience: "speakers", ConferenceID: conf.Ref, ConferenceTag: conf.Tag})
		if err != nil {
			return "", err
		}
		if len(speakers) > 0 {
			names := make([]string, 0, len(speakers))
			for _, speaker := range speakers {
				names = append(names, speaker.Name)
			}
			sort.Strings(names)
			sections = append(sections, "### Who's speaking\n\n- "+strings.Join(names, "\n- "))
		}
		sections = append(sections, conferenceVenueMarkdown(conf))
		sections = append(sections, conferenceHotelsMarkdown(ctx, conf))
	case types.ConferenceCampaignSpeakerReminder:
		sections = append(sections, conferenceHotelsMarkdown(ctx, conf))
		if conferenceHasOpenVolunteerShifts(ctx, conf) {
			sections = append(sections, "Volunteer shifts are still available. [Sign up to help]("+ctx.Env.GetURI()+"/"+conf.Tag+"#volunteer).")
		}
	case types.ConferenceCampaignSpeakerOnboarding:
		sections = append(sections, conferenceHotelsMarkdown(ctx, conf))
		sections = append(sections, conferenceVenueMarkdown(conf))
	case types.ConferenceCampaignVolunteerOrient:
		volInfo, err := getters.GetVolInfo(ctx, conf.Ref)
		if err != nil {
			return "", err
		}
		if volInfo.OrientTimes != nil {
			when := volInfo.OrientTimes.Start.In(conf.Loc()).Format("Monday, January 2 at 3:04 PM MST")
			sections = append(sections, "- **When:** "+when)
		}
		if volInfo.OrientLink != "" {
			sections = append(sections, "- **Where:** [Orientation location]("+volInfo.OrientLink+")")
		}
		if volInfo.Notes != "" {
			sections = append(sections, volInfo.Notes)
		}
	}
	clean := sections[:0]
	for _, section := range sections {
		if strings.TrimSpace(section) != "" {
			clean = append(clean, section)
		}
	}
	return strings.Join(clean, "\n\n"), nil
}

func conferenceVenueMarkdown(conf *types.Conf) string {
	if conf == nil {
		return ""
	}
	venue := strings.TrimSpace(strings.Join([]string{conf.Venue, conf.Location}, ", "))
	venue = strings.Trim(venue, ", ")
	if conf.VenueMap != "" {
		return "### Where to go\n\n[" + venue + "](" + conf.VenueMap + ")"
	}
	return "### Where to go\n\n" + venue
}

func conferenceHotelsMarkdown(ctx *config.AppContext, conf *types.Conf) string {
	hotels, err := getters.ListHotelsForConf(ctx, conf.Ref)
	if err != nil || len(hotels) == 0 {
		return ""
	}
	lines := []string{"### Where to stay"}
	for _, hotel := range hotels {
		if hotel.URL != "" {
			lines = append(lines, "- ["+hotel.Name+"]("+hotel.URL+")")
		} else {
			lines = append(lines, "- "+hotel.Name)
		}
	}
	return strings.Join(lines, "\n")
}

func conferenceHasOpenVolunteerShifts(ctx *config.AppContext, conf *types.Conf) bool {
	shifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
	if err != nil {
		return false
	}
	for _, shift := range shifts {
		if shift != nil && int(shift.MaxVols) > len(shift.AssigneesRef) {
			return true
		}
	}
	return false
}

func sendConferenceEmailOccurrence(ctx *config.AppContext, occurrence *types.ConferenceEmailOccurrence) error {
	conf, err := getters.GetConfByRef(ctx, occurrence.ConferenceID)
	if err != nil || conf == nil {
		return fmt.Errorf("load conference for delivery: %w", err)
	}
	letter, err := getters.GetLetter(ctx, occurrence.MissiveUID)
	if err != nil {
		return fmt.Errorf("load occurrence missive: %w", err)
	}
	recipients, err := getters.ConferenceEmailRecipients(ctx, occurrence)
	if err != nil {
		return err
	}
	var failures []string
	for _, recipient := range recipients {
		jobKey := fmt.Sprintf("conference-email-%s-%s", occurrence.ID, helpers.MakeJobHash(recipient.Email, letter.UID, recipient.Key))
		delivery, alreadyQueued, err := getters.BeginConferenceEmailDelivery(ctx, occurrence.ID, recipient.Key, recipient.Email, jobKey)
		if err != nil {
			failures = append(failures, err.Error())
			continue
		}
		if alreadyQueued {
			continue
		}
		data := conferenceCampaignRecipientData(ctx, conf, recipient)
		var files []*emails.EmailFile
		if occurrence.CampaignKind == types.ConferenceCampaignAttendeeFinal {
			for _, registration := range recipient.Registrations {
				pdf, pdfErr := emails.MakeTicketPDF(ctx, registration)
				if pdfErr != nil {
					err = fmt.Errorf("build ticket %s: %w", registration.RefID, pdfErr)
					break
				}
				short := registration.RefID
				if len(short) > 8 {
					short = short[:8]
				}
				files = append(files, &emails.EmailFile{PDF: pdf, Name: fmt.Sprintf("btcpp_%s_ticket_%s.pdf", conf.Tag, short)})
			}
		}
		if err == nil {
			err = emails.SendConferenceCampaign(ctx, letter, data, jobKey, files)
			if err != nil && strings.Contains(err.Error(), "scheduled.idem_key") {
				err = nil
			}
		}
		_ = getters.FinishConferenceEmailDelivery(ctx, delivery.ID, err)
		if err != nil {
			failures = append(failures, recipient.Email+": "+err.Error())
		}
	}
	if len(failures) > 0 {
		return fmt.Errorf("%d conference email deliveries failed: %s", len(failures), strings.Join(failures, "; "))
	}
	return getters.CompleteConferenceEmailOccurrence(ctx, occurrence.ID, time.Now())
}

func conferenceCampaignRecipientData(ctx *config.AppContext, conf *types.Conf, recipient *types.ConferenceEmailRecipient) *emails.ConferenceCampaignData {
	dinnerStart := conf.SpeakerDinnerStart
	if dinnerStart == nil {
		day := conf.StartDate.In(conf.Loc()).AddDate(0, 0, -1)
		fallback := time.Date(day.Year(), day.Month(), day.Day(), 18, 30, 0, 0, conf.Loc())
		dinnerStart = &fallback
	}
	dinnerLocation := strings.TrimSpace(conf.SpeakerDinnerLocation)
	if dinnerLocation == "" {
		dinnerLocation = "Location TBD"
	}
	data := &emails.ConferenceCampaignData{
		Conf: conf, Email: recipient.Email, Name: recipient.Name, URI: ctx.Env.GetURI(),
		DashboardLink:          helpers.EmailLink(ctx, recipient.Email, "/dashboard"),
		AffiliateDashboardLink: helpers.EmailLink(ctx, recipient.Email, "/dashboard/affiliate"),
		DoorsOpen:              emails.DoorsOpenDesc(ctx, conf), SpeakerDinnerLocation: dinnerLocation,
	}
	if dinnerStart != nil {
		data.SpeakerDinnerTime = dinnerStart.In(conf.Loc()).Format("Monday, January 2 at 3:04 PM MST")
	}
	if recipient.SpeakerConfID != "" {
		if speakerConf, err := getters.GetSpeakerConfByID(ctx, recipient.SpeakerConfID); err == nil && speakerConf != nil {
			var talks []string
			for _, proposal := range speakerConf.Proposals {
				if proposal != nil {
					talks = append(talks, "- "+proposal.Title)
				}
			}
			if len(talks) > 0 {
				data.TalkDetails = "### Your talk\n\n" + strings.Join(talks, "\n")
			}
		}
	}
	return data
}

func ConferenceMissivesTestAutomation(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	if ctx.InProduction {
		http.NotFound(w, r)
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if !ctx.Env.MailOff && strings.TrimSpace(ctx.Env.DevEmailOverride) == "" {
		redirectConferenceMissives(w, r, conf.Tag, "", "Set DEV_EMAIL_OVERRIDE before testing event-email delivery")
		return
	}
	simulatedAt := time.Now()
	if raw := strings.TrimSpace(r.FormValue("simulated_at")); raw != "" {
		parsed, parseErr := time.ParseInLocation("2006-01-02T15:04", raw, conf.Loc())
		if parseErr != nil {
			redirectConferenceMissives(w, r, conf.Tag, "", "Invalid simulated date and time")
			return
		}
		simulatedAt = parsed
	}
	runConferenceEmailDraftAutomationForConference(ctx, conf, simulatedAt)
	redirectConferenceMissives(w, r, conf.Tag, "Event email automation checked at "+simulatedAt.In(conf.Loc()).Format("Mon, Jan 2 at 3:04 PM MST"), "")
}
