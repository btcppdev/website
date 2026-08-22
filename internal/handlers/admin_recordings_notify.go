package handlers

import (
	"crypto/sha256"
	"fmt"
	"net/http"
	"net/mail"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"btcpp-web/external/buffer"
	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/types"
)

type RecordingSpeakerCampaignTalk struct {
	Row             *RecordingRow
	TalkTitle       string
	VenueLabel      string
	AgendaLabel     string
	YouTubeURL      string
	TalkURL         string
	CardKey         string
	CardURL         string
	PublishAt       time.Time
	PublishLabel    string
	PublishUTCLabel string
	BufferAt        time.Time
	BufferLabel     string
	SpeakerNames    string
	SpeakerCredit   string
}

type RecordingSpeakerNotification struct {
	Talk          *RecordingSpeakerCampaignTalk
	Row           *RecordingRow
	Speaker       *types.Speaker
	Email         string
	TalkTitle     string
	YouTubeURL    string
	PublishAt     time.Time
	PublishLabel  string
	ReminderAt    time.Time
	ReminderLabel string
	JobKeySuffix  string
}

type RecordingSpeakerDigestRecipient struct {
	Speaker      *types.Speaker
	Email        string
	JobKeySuffix string
}

type RecordingSpeakerNotificationSkip struct {
	Row     *RecordingRow
	Speaker *types.Speaker
	Reason  string
}

type recordingSpeakerCampaign struct {
	Talks      []*RecordingSpeakerCampaignTalk
	Recipients []*RecordingSpeakerDigestRecipient
	Reminders  []*RecordingSpeakerNotification
	Skipped    []*RecordingSpeakerNotificationSkip
}

type RecordingSpeakerNotificationsPage struct {
	Conf             *types.Conf
	Talks            []*RecordingSpeakerCampaignTalk
	Recipients       []*RecordingSpeakerDigestRecipient
	Items            []*RecordingSpeakerNotification
	Skipped          []*RecordingSpeakerNotificationSkip
	MailOff          bool
	DevEmailOverride string
	CanSendEmailTest bool
	BufferOK         bool
	FlashMessage     string
	FlashError       string
	Year             uint
}

func RecordingsAdminNotifySpeakersPreview(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, ok := requireRecordingsConfAdmin(w, r, ctx)
	if !ok {
		return
	}
	campaign := buildRecordingSpeakerCampaign(recordingRowsForConf(ctx, conf.Tag), conf, time.Now(), ctx.Env.GetURI())
	staffErr := addRecordingDigestRoleRecipients(ctx, campaign, conf.Tag)
	devEmailOverride := ""
	if !ctx.Env.Prod {
		devEmailOverride = strings.TrimSpace(ctx.Env.DevEmailOverride)
	}
	page := &RecordingSpeakerNotificationsPage{
		Conf:             conf,
		Talks:            campaign.Talks,
		Recipients:       campaign.Recipients,
		Items:            campaign.Reminders,
		Skipped:          campaign.Skipped,
		MailOff:          ctx.Env.MailOff,
		DevEmailOverride: devEmailOverride,
		CanSendEmailTest: !ctx.Env.MailOff && devEmailOverride != "",
		BufferOK:         buffer.IsConfigured(),
		FlashMessage:     r.URL.Query().Get("flash"),
		FlashError:       r.URL.Query().Get("err"),
		Year:             uint(time.Now().Year()),
	}
	if staffErr != nil && page.FlashError == "" {
		page.FlashError = "Could not load staff recipients: " + staffErr.Error()
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/recordings_notify_speakers.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/admin/recordings/notify-speakers render: %s", conf.Tag, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func RecordingsAdminNotifySpeakersTest(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, ok := requireRecordingsConfAdmin(w, r, ctx)
	if !ok {
		return
	}
	if ctx.Env.Prod {
		redirectRecordingSpeakerTestErr(w, r, conf.Tag, "Development email tests are disabled in production")
		return
	}
	override, err := canonicalRecordingNotificationEmail(ctx.Env.DevEmailOverride)
	if err != nil {
		redirectRecordingSpeakerTestErr(w, r, conf.Tag, "Set DEV_EMAIL_OVERRIDE to a valid email address before sending a test")
		return
	}
	if ctx.Env.MailOff {
		redirectRecordingSpeakerTestErr(w, r, conf.Tag, "Set MAILER_OFF=false before sending a development email test")
		return
	}

	now := time.Now()
	campaign := buildRecordingSpeakerCampaign(recordingRowsForConf(ctx, conf.Tag), conf, now, ctx.Env.GetURI())
	if err := addRecordingDigestRoleRecipients(ctx, campaign, conf.Tag); err != nil {
		redirectRecordingSpeakerTestErr(w, r, conf.Tag, "Could not load staff recipients: "+err.Error())
		return
	}
	if len(campaign.Talks) == 0 || len(campaign.Recipients) == 0 || len(campaign.Reminders) == 0 {
		redirectRecordingSpeakerTestErr(w, r, conf.Tag, "No complete upcoming speaker campaign is available to test")
		return
	}

	attachments, err := loadRecordingCampaignAttachments(campaign.Reminders[:1])
	if err != nil {
		redirectRecordingSpeakerTestErr(w, r, conf.Tag, "Email test was not sent: "+err.Error())
		return
	}
	suffix := fmt.Sprintf("-devtest-%d", now.UnixNano())
	recipient := *campaign.Recipients[0]
	recipient.JobKeySuffix = suffix
	if err := sendRecordingSpeakerDigest(ctx, conf, campaign.Talks, &recipient); err != nil {
		redirectRecordingSpeakerTestErr(w, r, conf.Tag, "Test digest failed: "+err.Error())
		return
	}
	reminder := *campaign.Reminders[0]
	reminder.ReminderAt = now
	reminder.JobKeySuffix = suffix
	attachment := attachments[reminder.Row.Recording.ID]
	if err := sendRecordingSpeakerReminder(ctx, conf, &reminder, attachment); err != nil {
		redirectRecordingSpeakerTestErr(w, r, conf.Tag, "Test digest was queued, but the reminder failed: "+err.Error())
		return
	}
	msg := fmt.Sprintf("Sent a development digest and reminder to %s; Buffer was not touched", override)
	http.Redirect(w, r, recordingsAdminPath(conf.Tag, "/notify-speakers?flash="+url.QueryEscape(msg)), http.StatusSeeOther)
}

func redirectRecordingSpeakerTestErr(w http.ResponseWriter, r *http.Request, confTag, message string) {
	http.Redirect(w, r, recordingsAdminPath(confTag, "/notify-speakers?err="+url.QueryEscape(message)), http.StatusSeeOther)
}

func RecordingsAdminNotifySpeakersApply(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, ok := requireRecordingsConfAdmin(w, r, ctx)
	if !ok {
		return
	}
	campaign := buildRecordingSpeakerCampaign(recordingRowsForConf(ctx, conf.Tag), conf, time.Now(), ctx.Env.GetURI())
	if err := addRecordingDigestRoleRecipients(ctx, campaign, conf.Tag); err != nil {
		redirectRecordingsListErr(w, r, conf.Tag, "Speaker campaign was not queued: could not load staff recipients: "+err.Error())
		return
	}
	if len(campaign.Talks) == 0 || len(campaign.Recipients) == 0 {
		http.Redirect(w, r, recordingsAdminPath(conf.Tag, "?flash="+url.QueryEscape("No speakers have an upcoming scheduled YouTube campaign to queue")), http.StatusSeeOther)
		return
	}

	attachments := map[string]*emails.EmailFile{}
	if !ctx.Env.MailOff {
		var err error
		attachments, err = loadRecordingCampaignAttachments(campaign.Reminders)
		if err != nil {
			redirectRecordingsListErr(w, r, conf.Tag, "Speaker campaign was not queued: "+err.Error())
			return
		}
	}

	digests := 0
	for _, recipient := range campaign.Recipients {
		if err := sendRecordingSpeakerDigest(ctx, conf, campaign.Talks, recipient); err != nil {
			redirectRecordingCampaignPartialError(w, r, conf.Tag, digests, 0, 0, err)
			return
		}
		digests++
	}

	reminders := 0
	for _, item := range campaign.Reminders {
		attachment := attachments[item.Row.Recording.ID]
		if err := sendRecordingSpeakerReminder(ctx, conf, item, attachment); err != nil {
			redirectRecordingCampaignPartialError(w, r, conf.Tag, digests, reminders, 0, err)
			return
		}
		reminders++
	}

	bufferQueued, bufferSkipped, err := scheduleRecordingBufferPosts(ctx, campaign.Talks)
	if err != nil {
		redirectRecordingCampaignPartialError(w, r, conf.Tag, digests, reminders, bufferQueued, err)
		return
	}

	msg := fmt.Sprintf("Queued %d immediate speaker digest(s), %d 24-hour reminder(s), and %d Buffer/X post(s)", digests, reminders, bufferQueued)
	if ctx.Env.MailOff {
		msg = fmt.Sprintf("Mailer is off; previewed %d digest(s) and %d reminder(s); queued %d Buffer/X post(s)", digests, reminders, bufferQueued)
	}
	if !buffer.IsConfigured() {
		msg += "; Buffer is not configured, so X posts were not queued"
	} else if bufferSkipped > 0 {
		msg += fmt.Sprintf("; %d Buffer/X post(s) were already scheduled", bufferSkipped)
	}
	http.Redirect(w, r, recordingsAdminPath(conf.Tag, "?flash="+url.QueryEscape(msg)), http.StatusSeeOther)
}

func redirectRecordingCampaignPartialError(w http.ResponseWriter, r *http.Request, confTag string, digests, reminders, bufferPosts int, err error) {
	msg := fmt.Sprintf("Campaign stopped after queuing %d digest(s), %d reminder(s), and %d Buffer/X post(s): %s. Retry is safe; stable job keys prevent duplicate emails.", digests, reminders, bufferPosts, err)
	redirectRecordingsListErr(w, r, confTag, msg)
}

func buildRecordingSpeakerCampaign(rows []*RecordingRow, conf *types.Conf, now time.Time, baseURI string) *recordingSpeakerCampaign {
	campaign := &recordingSpeakerCampaign{}
	ordered := append([]*RecordingRow(nil), rows...)
	sort.SliceStable(ordered, func(i, j int) bool {
		return recordingAutoscheduleSortKey(ordered[i]) < recordingAutoscheduleSortKey(ordered[j])
	})
	recipients := map[string]*RecordingSpeakerDigestRecipient{}

	for _, row := range ordered {
		if row == nil || row.Recording == nil {
			continue
		}
		rec := row.Recording
		youtubeURL := recordingNotificationYouTubeURL(row.YTURL)
		if youtubeURL == "" {
			campaign.Skipped = append(campaign.Skipped, &RecordingSpeakerNotificationSkip{Row: row, Reason: "missing a valid YouTube URL"})
			continue
		}
		if rec.PublishAt == nil || rec.PublishAt.IsZero() {
			campaign.Skipped = append(campaign.Skipped, &RecordingSpeakerNotificationSkip{Row: row, Reason: "YouTube publication is not scheduled"})
			continue
		}
		if !rec.PublishAt.After(now) {
			campaign.Skipped = append(campaign.Skipped, &RecordingSpeakerNotificationSkip{Row: row, Reason: "publication time has already passed"})
			continue
		}

		talk := &RecordingSpeakerCampaignTalk{
			Row:             row,
			TalkTitle:       recordingNotificationTalkTitle(row),
			VenueLabel:      recordingNotificationVenueLabel(row),
			AgendaLabel:     recordingNotificationAgendaLabel(row, conf),
			YouTubeURL:      youtubeURL,
			TalkURL:         recordingNotificationTalkURL(baseURI, conf, row),
			CardKey:         recordingNotificationCardKey(row, conf),
			PublishAt:       *rec.PublishAt,
			PublishLabel:    recordingNotificationPublishLabel(*rec.PublishAt, conf),
			PublishUTCLabel: recordingNotificationUTCPublishLabel(*rec.PublishAt),
			BufferAt:        rec.PublishAt.Add(5 * time.Minute),
			BufferLabel:     recordingNotificationPublishLabel(rec.PublishAt.Add(5*time.Minute), conf),
			SpeakerNames:    joinSpeakerNames(row.Speakers),
			SpeakerCredit:   joinSpeakerXCredits(row.Speakers),
		}
		if talk.CardKey != "" {
			if spaces.IsConfigured() {
				talk.CardURL = spaces.PublicURL(talk.CardKey)
			} else if row.ConfTalk != nil {
				talk.CardURL = fmt.Sprintf("%s/media/png/%s/talk/1080p/%s", strings.TrimRight(baseURI, "/"), url.PathEscape(conf.Tag), url.PathEscape(row.ConfTalk.ID))
			}
		}
		campaign.Talks = append(campaign.Talks, talk)

		if len(row.Speakers) == 0 {
			campaign.Skipped = append(campaign.Skipped, &RecordingSpeakerNotificationSkip{Row: row, Reason: "no speakers are attached"})
			continue
		}
		seenForTalk := map[string]bool{}
		for _, speaker := range row.Speakers {
			if speaker == nil {
				continue
			}
			email, err := canonicalRecordingNotificationEmail(speaker.Email)
			if err != nil {
				campaign.Skipped = append(campaign.Skipped, &RecordingSpeakerNotificationSkip{Row: row, Speaker: speaker, Reason: "speaker is missing a valid email"})
				continue
			}
			if recipients[email] == nil {
				recipients[email] = &RecordingSpeakerDigestRecipient{Speaker: speaker, Email: email}
			}
			if seenForTalk[email] {
				continue
			}
			seenForTalk[email] = true
			reminderAt := rec.PublishAt.Add(-24 * time.Hour)
			if reminderAt.Before(now) {
				reminderAt = now
			}
			campaign.Reminders = append(campaign.Reminders, &RecordingSpeakerNotification{
				Talk:          talk,
				Row:           row,
				Speaker:       speaker,
				Email:         email,
				TalkTitle:     talk.TalkTitle,
				YouTubeURL:    youtubeURL,
				PublishAt:     talk.PublishAt,
				PublishLabel:  talk.PublishLabel,
				ReminderAt:    reminderAt,
				ReminderLabel: recordingNotificationPublishLabel(reminderAt, conf),
			})
		}
	}

	for _, recipient := range recipients {
		campaign.Recipients = append(campaign.Recipients, recipient)
	}
	sort.SliceStable(campaign.Recipients, func(i, j int) bool {
		left := strings.ToLower(campaign.Recipients[i].Speaker.Name + campaign.Recipients[i].Email)
		right := strings.ToLower(campaign.Recipients[j].Speaker.Name + campaign.Recipients[j].Email)
		return left < right
	})
	return campaign
}

func addRecordingDigestRoleRecipients(ctx *config.AppContext, campaign *recordingSpeakerCampaign, confTag string) error {
	roles := make([]string, 0, 6)
	for _, scope := range []string{strings.TrimSpace(confTag), "global"} {
		if scope == "" {
			continue
		}
		for _, role := range []string{"admin", "volcoord", "staff"} {
			roles = append(roles, scope+"-"+role)
		}
	}
	people, err := getters.ListSpeakersWithAnyRole(ctx, roles)
	if err != nil {
		return err
	}
	mergeRecordingDigestRecipients(campaign, people)
	return nil
}

func mergeRecordingDigestRecipients(campaign *recordingSpeakerCampaign, people []*types.Speaker) {
	if campaign == nil {
		return
	}
	seen := make(map[string]bool, len(campaign.Recipients)+len(people))
	for _, recipient := range campaign.Recipients {
		if recipient != nil {
			seen[strings.ToLower(strings.TrimSpace(recipient.Email))] = true
		}
	}
	for _, person := range people {
		if person == nil {
			continue
		}
		email, err := canonicalRecordingNotificationEmail(person.Email)
		if err != nil || seen[email] {
			continue
		}
		seen[email] = true
		campaign.Recipients = append(campaign.Recipients, &RecordingSpeakerDigestRecipient{Speaker: person, Email: email})
	}
	sort.SliceStable(campaign.Recipients, func(i, j int) bool {
		left := strings.ToLower(campaign.Recipients[i].Speaker.Name + campaign.Recipients[i].Email)
		right := strings.ToLower(campaign.Recipients[j].Speaker.Name + campaign.Recipients[j].Email)
		return left < right
	})
}

func recordingSpeakerNotifications(rows []*RecordingRow, conf *types.Conf, now time.Time) ([]*RecordingSpeakerNotification, []*RecordingSpeakerNotificationSkip) {
	campaign := buildRecordingSpeakerCampaign(rows, conf, now, "https://btcpp.dev")
	return campaign.Reminders, campaign.Skipped
}

func canonicalRecordingNotificationEmail(raw string) (string, error) {
	address, err := mail.ParseAddress(strings.TrimSpace(raw))
	if err != nil || !strings.Contains(address.Address, "@") {
		return "", fmt.Errorf("invalid email")
	}
	return strings.ToLower(address.Address), nil
}

func recordingNotificationTalkTitle(row *RecordingRow) string {
	if row != nil && row.ConfTalk != nil && row.ConfTalk.Proposal != nil && strings.TrimSpace(row.ConfTalk.Proposal.Title) != "" {
		return strings.TrimSpace(row.ConfTalk.Proposal.Title)
	}
	if row != nil && row.Recording != nil {
		return strings.TrimSpace(row.Recording.TalkName)
	}
	return "Your bitcoin++ talk"
}

func recordingNotificationYouTubeURL(raw string) string {
	videoID := youtubeVideoID(raw)
	if videoID == "" {
		return ""
	}
	if strings.HasPrefix(strings.TrimSpace(raw), "http://") || strings.HasPrefix(strings.TrimSpace(raw), "https://") {
		return strings.TrimSpace(raw)
	}
	return "https://www.youtube.com/watch?v=" + url.QueryEscape(videoID)
}

func recordingNotificationPublishLabel(publishAt time.Time, conf *types.Conf) string {
	loc := time.UTC
	zone := "UTC"
	if conf != nil {
		loc = conf.Loc()
		zone = recordingPublishTimezone(conf)
	}
	return publishAt.In(loc).Format("Monday, January 2, 2006 at 3:04 PM") + " " + zone
}

func recordingNotificationUTCPublishLabel(publishAt time.Time) string {
	return publishAt.UTC().Format("Monday, January 2, 2006 at 3:04 PM UTC")
}

func recordingNotificationAgendaLabel(row *RecordingRow, conf *types.Conf) string {
	if row == nil || row.ConfTalk == nil || row.ConfTalk.Sched == nil {
		return "Agenda time unavailable"
	}
	loc := time.UTC
	if conf != nil {
		loc = conf.Loc()
	}
	return row.ConfTalk.Sched.Start.In(loc).Format("Monday, January 2 at 3:04 PM")
}

func recordingNotificationVenueLabel(row *RecordingRow) string {
	if row == nil || row.ConfTalk == nil {
		return "Other"
	}
	venue := strings.TrimSpace(row.ConfTalk.Venue)
	switch recordingAutoscheduleStageRank(row) {
	case 0:
		return "Main Stage"
	case 1:
		return "Talks Stage"
	case 2:
		return "Workshops"
	}
	if venue != "" {
		return venue
	}
	return "Other"
}

func recordingNotificationTalkURL(baseURI string, conf *types.Conf, row *RecordingRow) string {
	if conf == nil {
		return strings.TrimRight(baseURI, "/")
	}
	target := strings.TrimRight(baseURI, "/") + "/" + url.PathEscape(conf.Tag) + "/agenda"
	if row != nil && row.ConfTalk != nil && row.ConfTalk.AnchorTag() != "" {
		target += "#" + url.PathEscape(row.ConfTalk.AnchorTag())
	}
	return target
}

func recordingNotificationCardKey(row *RecordingRow, conf *types.Conf) string {
	if row == nil || row.ConfTalk == nil {
		return ""
	}
	if card := strings.TrimSpace(row.ConfTalk.SocialCard); card != "" {
		return recordingSourceObjectKey(card)
	}
	if conf == nil {
		return ""
	}
	return fmt.Sprintf("%s/talks/%s-1080p.png", conf.Tag, row.ConfTalk.ID)
}

func loadRecordingCampaignAttachments(reminders []*RecordingSpeakerNotification) (map[string]*emails.EmailFile, error) {
	if !spaces.IsConfigured() {
		return nil, fmt.Errorf("Spaces is not configured, so talk-card attachments cannot be loaded")
	}
	out := make(map[string]*emails.EmailFile, len(reminders))
	for _, reminder := range reminders {
		if reminder == nil || reminder.Talk == nil {
			continue
		}
		talk := reminder.Talk
		if talk == nil || talk.Row == nil || talk.Row.Recording == nil {
			continue
		}
		if out[talk.Row.Recording.ID] != nil {
			continue
		}
		if talk.CardKey == "" {
			return nil, fmt.Errorf("%q does not have a talk-card image", talk.TalkTitle)
		}
		data, err := spaces.Get(talk.CardKey)
		if err != nil {
			return nil, fmt.Errorf("load talk-card image for %q (%s): %w", talk.TalkTitle, talk.CardKey, err)
		}
		out[talk.Row.Recording.ID] = &emails.EmailFile{
			Bytes:       data,
			ContentType: "image/png",
			Name:        filepath.Base(talk.CardKey),
		}
	}
	return out, nil
}

func recordingSpeakerDigestJobKey(recipient *RecordingSpeakerDigestRecipient, talks []*RecordingSpeakerCampaignTalk) string {
	var schedule strings.Builder
	for _, talk := range talks {
		if talk == nil || talk.Row == nil || talk.Row.Recording == nil {
			continue
		}
		fmt.Fprintf(&schedule, "%s:%d:%s|", talk.Row.Recording.ID, talk.PublishAt.UTC().Unix(), talk.YouTubeURL)
	}
	scheduleHash := sha256.Sum256([]byte(schedule.String()))
	emailHash := sha256.Sum256([]byte(strings.ToLower(recipient.Email)))
	return fmt.Sprintf("recording-speaker-digest-%x-%x%s", scheduleHash[:8], emailHash[:8], recipient.JobKeySuffix)
}

func recordingSpeakerNotificationJobKey(item *RecordingSpeakerNotification) string {
	sum := sha256.Sum256([]byte(strings.ToLower(item.Email)))
	return fmt.Sprintf("recording-speaker-reminder-%s-%x%s", item.Row.Recording.ID, sum[:8], item.JobKeySuffix)
}

func sendRecordingSpeakerDigest(ctx *config.AppContext, conf *types.Conf, talks []*RecordingSpeakerCampaignTalk, recipient *RecordingSpeakerDigestRecipient) error {
	if recipient == nil || recipient.Speaker == nil || recipient.Email == "" {
		return fmt.Errorf("digest recipient is incomplete")
	}
	name := strings.TrimSpace(recipient.Speaker.Name)
	if name == "" {
		name = "there"
	}
	var markdown strings.Builder
	var textBody strings.Builder
	fmt.Fprintf(&markdown, "# The %s YouTube schedule\n\nHi %s,\n\nHere is the release schedule for all **%s** conference talks, ordered by room and presentation order.\n\n", markdownEmailText(conf.Desc), markdownEmailText(name), markdownEmailText(conf.Desc))
	fmt.Fprintf(&textBody, "The %s YouTube schedule\n\nHi %s,\n\nHere is the release schedule for all %s conference talks, ordered by room and presentation order.\n\n", conf.Desc, name, conf.Desc)
	fmt.Fprintf(&markdown, "**Timezone:** %s. UTC is included with every publication time.\n\n", markdownEmailText(recordingPublishTimezone(conf)))
	fmt.Fprintf(&textBody, "Timezone: %s. UTC is included with every publication time.\n\n", recordingPublishTimezone(conf))
	for _, talk := range talks {
		fmt.Fprintf(&markdown, "- **%s** — [%s](%s) — **Speakers:** %s — [YouTube](%s) will be published **%s** (**%s**)\n", markdownEmailText(talk.VenueLabel), markdownEmailText(talk.TalkTitle), talk.TalkURL, markdownEmailText(talk.SpeakerNames), talk.YouTubeURL, markdownEmailText(talk.PublishLabel), markdownEmailText(talk.PublishUTCLabel))
		fmt.Fprintf(&textBody, "- %s — %s\n  Speakers: %s\n  Talk: %s\n  YouTube: %s\n  Will be published: %s\n  UTC: %s\n", talk.VenueLabel, talk.TalkTitle, talk.SpeakerNames, talk.TalkURL, talk.YouTubeURL, talk.PublishLabel, talk.PublishUTCLabel)
	}
	markdown.WriteString("\nFeel free to review your talk and flag any issues ASAP. We ask that you kindly wait until the publication date to share.\n\nWe’ll send a second note 24 hours before each of your talks is published with a promotional image you can share.\n\n— bitcoin++")
	textBody.WriteString("\nFeel free to review your talk and flag any issues ASAP. We ask that you kindly wait until the publication date to share.\n\nWe'll send a second note 24 hours before each of your talks is published with a promotional image you can share.\n\n— bitcoin++")
	htmlBody, err := emails.BuildHTMLEmail(ctx, []byte(markdown.String()))
	if err != nil {
		return fmt.Errorf("render speaker digest: %w", err)
	}
	return emails.ComposeAndSendMail(ctx, &emails.Mail{
		JobKey:   recordingSpeakerDigestJobKey(recipient, talks),
		Email:    recipient.Email,
		Title:    fmt.Sprintf("[%s] YouTube release schedule", conf.Desc),
		SendAt:   time.Now(),
		HTMLBody: htmlBody,
		TextBody: []byte(textBody.String()),
	})
}

func sendRecordingSpeakerReminder(ctx *config.AppContext, conf *types.Conf, item *RecordingSpeakerNotification, attachment *emails.EmailFile) error {
	if item == nil || item.Talk == nil || item.Row == nil || item.Row.Recording == nil || item.Speaker == nil {
		return fmt.Errorf("reminder is incomplete")
	}
	if !ctx.Env.MailOff && attachment == nil {
		return fmt.Errorf("promotional image is missing for %q", item.TalkTitle)
	}
	name := strings.TrimSpace(item.Speaker.Name)
	if name == "" {
		name = "there"
	}
	lead := "Your talk will be published on YouTube tomorrow!"
	subjectLead := "Your talk will be published on YouTube tomorrow"
	markdown := fmt.Sprintf("# %s\n\nHi %s,\n\n**%s** will become public on **%s**.\n\n[View the talk page](%s) · [Open the YouTube video](%s)\n\nWe attached your talk image so you can use it to promote the release.\n\n— bitcoin++", markdownEmailText(lead), markdownEmailText(name), markdownEmailText(item.TalkTitle), markdownEmailText(item.PublishLabel), item.Talk.TalkURL, item.YouTubeURL)
	textBody := fmt.Sprintf("%s\n\nHi %s,\n\n%s will become public on %s.\n\nTalk page: %s\nYouTube: %s\n\nWe attached your talk image so you can use it to promote the release.\n\n— bitcoin++", lead, name, item.TalkTitle, item.PublishLabel, item.Talk.TalkURL, item.YouTubeURL)
	htmlBody, err := emails.BuildHTMLEmail(ctx, []byte(markdown))
	if err != nil {
		return fmt.Errorf("render speaker reminder: %w", err)
	}
	var files []*emails.EmailFile
	if attachment != nil {
		files = []*emails.EmailFile{attachment}
	}
	return emails.ComposeAndSendMail(ctx, &emails.Mail{
		JobKey:   recordingSpeakerNotificationJobKey(item),
		Email:    item.Email,
		Title:    fmt.Sprintf("[%s] %s", conf.Desc, subjectLead),
		SendAt:   item.ReminderAt,
		HTMLBody: htmlBody,
		TextBody: []byte(textBody),
		Files:    files,
	})
}

func scheduleRecordingBufferPosts(ctx *config.AppContext, talks []*RecordingSpeakerCampaignTalk) (int, int, error) {
	if !buffer.IsConfigured() {
		return 0, len(talks), nil
	}
	channels, err := buffer.FetchChannels()
	if err != nil {
		return 0, 0, fmt.Errorf("fetch Buffer channels: %w", err)
	}
	var targets []buffer.Channel
	for _, channel := range channels {
		if channel.Service == "twitter" && strings.Contains(strings.ToLower(channel.Name), "btcplusplus") {
			targets = append(targets, channel)
		}
	}
	if len(targets) == 0 {
		return 0, 0, fmt.Errorf("no btcplusplus X/Twitter channel was found in Buffer")
	}

	channel := targets[0]
	queued, skipped := 0, 0
	for _, talk := range talks {
		if talk.Row.XURL != "" || socialPostBlocksBufferSchedule(talk.Row.XSocialPost) {
			skipped++
			continue
		}
		ref := recordingSocialPostRef(talk.Row.Recording, recordingPlatformBufferX)
		existing, err := getters.FindSocialPostByRef(ctx, ref)
		if err != nil {
			return queued, skipped, fmt.Errorf("load existing Buffer post for %q: %w", talk.TalkTitle, err)
		}
		if existing != nil && strings.EqualFold(existing.Status, recordingStatusScheduled) && existing.ScheduledAt != nil && existing.ScheduledAt.Equal(talk.BufferAt) && existing.Text == recordingBufferXText(talk) {
			skipped++
			continue
		}
		text := recordingBufferXText(talk)
		status := recordingStatusScheduling
		if _, err := getters.UpsertSocialPost(ctx, getters.SocialPostUpdate{
			Ref:         ref,
			Text:        &text,
			PostedTo:    "buffer-twitter",
			Kind:        getters.SocialPostKindRecording,
			RecordingID: talk.Row.Recording.ID,
			ConfTalkID:  talk.Row.Recording.ConfTalkID,
			Status:      &status,
			ScheduledAt: &talk.BufferAt,
		}); err != nil {
			return queued, skipped, fmt.Errorf("save pending Buffer post for %q: %w", talk.TalkTitle, err)
		}
		var images []string
		if talk.CardURL != "" {
			images = []string{talk.CardURL}
		}
		var result *buffer.PostResult
		if existing != nil && strings.EqualFold(existing.Status, recordingStatusScheduled) {
			if strings.TrimSpace(existing.URL) == "" {
				return queued, skipped, fmt.Errorf("Buffer/X post for %q needs updating, but its Buffer post ID is missing", talk.TalkTitle)
			}
			result, err = buffer.EditScheduledPost(existing.URL, text, images, channel.Service, talk.BufferAt)
		} else {
			result, err = buffer.CreateScheduledPost(channel.ID, text, images, channel.Service, talk.BufferAt)
		}
		if err != nil {
			failed := recordingStatusFailed
			msg := err.Error()
			_, _ = getters.UpsertSocialPost(ctx, getters.SocialPostUpdate{Ref: ref, Status: &failed, Error: &msg})
			return queued, skipped, fmt.Errorf("schedule Buffer/X post for %q: %w", talk.TalkTitle, err)
		}
		if result == nil || strings.TrimSpace(result.ID) == "" {
			failed := recordingStatusFailed
			msg := "Buffer returned no post ID"
			_, _ = getters.UpsertSocialPost(ctx, getters.SocialPostUpdate{Ref: ref, Status: &failed, Error: &msg})
			return queued, skipped, fmt.Errorf("schedule Buffer/X post for %q: %s", talk.TalkTitle, msg)
		}
		status = recordingStatusScheduled
		bufferID := result.ID
		clear := ""
		if _, err := getters.UpsertSocialPost(ctx, getters.SocialPostUpdate{Ref: ref, Status: &status, URL: &bufferID, Error: &clear, ScheduledAt: &talk.BufferAt}); err != nil {
			return queued, skipped, fmt.Errorf("save scheduled Buffer post for %q: %w", talk.TalkTitle, err)
		}
		queued++
	}
	return queued, skipped, nil
}

func socialPostBlocksBufferSchedule(post *types.SocialPost) bool {
	if post == nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(post.Status)) {
	case recordingStatusScheduling, recordingStatusScheduled, recordingStatusPosting, recordingStatusPosted, recordingStatusUploaded:
		return true
	default:
		return strings.TrimSpace(post.URL) != ""
	}
}

func recordingBufferXText(talk *RecordingSpeakerCampaignTalk) string {
	if talk == nil {
		return ""
	}
	prefix := "NOW LIVE 🎥: " + talk.TalkTitle
	if talk.SpeakerCredit != "" {
		prefix += "\n\nFeaturing: " + talk.SpeakerCredit
	}
	available := 275 - utf8.RuneCountInString(talk.YouTubeURL)
	if available < 20 {
		available = 20
	}
	prefix = truncateRunes(prefix, available)
	return strings.TrimSpace(prefix) + "\n\n" + talk.YouTubeURL
}

func truncateRunes(value string, max int) string {
	if max <= 0 || utf8.RuneCountInString(value) <= max {
		return value
	}
	runes := []rune(value)
	if max == 1 {
		return "…"
	}
	return strings.TrimSpace(string(runes[:max-1])) + "…"
}

func markdownEmailText(value string) string {
	return strings.NewReplacer(
		"\\", "\\\\",
		"*", "\\*",
		"_", "\\_",
		"[", "\\[",
		"]", "\\]",
		"`", "\\`",
	).Replace(value)
}
