package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	netmail "net/mail"
	"net/url"
	"sort"
	"strconv"
	"strings"
	texttemplate "text/template"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/external/spaces"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/missives"
	"btcpp-web/internal/mtypes"
	"btcpp-web/internal/types"

	"github.com/gorilla/mux"
)

type TemplatedMissivesPage struct {
	Letters           []*mtypes.Letter
	Current           *mtypes.Letter
	Form              TemplatedMissiveForm
	IsNew             bool
	FlashMessage      string
	ErrorMessage      string
	SpacesReady       bool
	Year              uint
	NextWeeklyLabel   string
	NewsletterFilter  string
	NewsletterOptions []string
	MissiveView       string
	MissiveTabCounts  MissiveTabCounts
	OneShotsTabURL    string
	UnsentTabURL      string
	SentTabURL        string
	ClearFilterURL    string
	OneShotLabels     map[string]string
	ScheduledMissives map[uint64]bool
	IsDevelopment     bool
	DevReviewEmail    string
	CanDelete         bool
	CanCancel         bool
}

type MissiveTabCounts struct {
	OneShots      int
	Unsent        int
	SentScheduled int
}

const (
	missiveViewOneShots = "oneshots"
	missiveViewUnsent   = "unsent"
	missiveViewSent     = "sent"
)

type InlineMissivePage struct {
	Current      *mtypes.Letter
	Fields       []EmailFieldGroup
	FieldNotice  string
	FlashMessage string
	ErrorMessage string
	Year         uint
}

type TemplatedMissiveForm struct {
	UID             uint64
	PageID          string
	Title           string
	SendAt          string
	Expiry          string
	Newsletters     string
	Template        string
	Palette         string
	Issue           string
	Hero            string
	Ticker          string
	LeadEyebrow     string
	LeadTitle       string
	LeadDeck        string
	NewsItems       string
	Stats           string
	Pullquote       string
	PullquoteBy     string
	CTAEyebrow      string
	CTATitle        string
	CTASubtitle     string
	CTALabel        string
	CTAURL          string
	ContentMarkdown string
	TestEmail       string
}

func TemplatedMissivesAdmin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}

	letters, err := getters.ListAdminEditableLetters(ctx)
	if err != nil {
		http.Error(w, "Unable to load missives", http.StatusInternalServerError)
		ctx.Err.Printf("/admin/missives list failed: %s", err)
		return
	}
	if uid := strings.TrimSpace(r.URL.Query().Get("uid")); uid != "" {
		http.Redirect(w, r, "/admin/missives/"+url.PathEscape(uid), http.StatusMovedPermanently)
		return
	}

	newsletterFilter := strings.TrimSpace(r.URL.Query().Get("newsletter"))
	missiveView := normalizeMissiveView(r.URL.Query().Get("view"))
	letters, options, counts := prepareTemplatedMissiveIndex(letters, newsletterFilter, missiveView)
	scheduledMissives := make(map[uint64]bool)
	for _, letter := range letters {
		scheduledMissives[letter.UID] = missiveIsScheduled(letter)
	}

	page := &TemplatedMissivesPage{
		Letters:           letters,
		FlashMessage:      r.URL.Query().Get("flash"),
		ErrorMessage:      r.URL.Query().Get("error"),
		Year:              helpers.CurrentYear(),
		NextWeeklyLabel:   nextWeeklyNewsletterSendAt(time.Now()).Format("Mon, Jan 2 at 3:04 PM MST"),
		NewsletterFilter:  newsletterFilter,
		NewsletterOptions: options,
		MissiveView:       missiveView,
		MissiveTabCounts:  counts,
		OneShotsTabURL:    missiveIndexURL(missiveViewOneShots, newsletterFilter),
		UnsentTabURL:      missiveIndexURL(missiveViewUnsent, newsletterFilter),
		SentTabURL:        missiveIndexURL(missiveViewSent, newsletterFilter),
		ClearFilterURL:    missiveIndexURL(missiveView, ""),
		OneShotLabels:     oneShotMissiveLabels(),
		ScheduledMissives: scheduledMissives,
		IsDevelopment:     !ctx.InProduction,
		DevReviewEmail:    strings.TrimSpace(ctx.Env.DevEmailOverride),
	}
	renderTemplatedMissivesIndex(w, r, ctx, page)
}

func prepareTemplatedMissiveIndex(letters []*mtypes.Letter, newsletterFilter, missiveView string) ([]*mtypes.Letter, []string, MissiveTabCounts) {
	optionSet := make(map[string]struct{})
	for _, letter := range letters {
		for _, newsletter := range letter.Newsletters {
			newsletter = strings.TrimSpace(newsletter)
			if newsletter != "" {
				optionSet[newsletter] = struct{}{}
			}
		}
	}
	options := make([]string, 0, len(optionSet))
	for newsletter := range optionSet {
		options = append(options, newsletter)
	}
	sort.Strings(options)
	visible := append([]*mtypes.Letter(nil), letters...)
	if newsletterFilter != "" {
		filtered := make([]*mtypes.Letter, 0, len(visible))
		for _, letter := range visible {
			if containsString(letter.Newsletters, newsletterFilter) {
				filtered = append(filtered, letter)
			}
		}
		visible = filtered
	}

	var counts MissiveTabCounts
	for _, letter := range visible {
		switch missiveViewForLetter(letter) {
		case missiveViewOneShots:
			counts.OneShots++
		case missiveViewSent:
			counts.SentScheduled++
		default:
			counts.Unsent++
		}
	}
	missiveView = normalizeMissiveView(missiveView)
	filtered := make([]*mtypes.Letter, 0, len(visible))
	for _, letter := range visible {
		if missiveViewForLetter(letter) == missiveView {
			filtered = append(filtered, letter)
		}
	}
	visible = filtered

	sort.SliceStable(visible, func(i, j int) bool {
		if visible[i].SentAt == nil && visible[j].SentAt != nil {
			return true
		}
		if visible[i].SentAt != nil && visible[j].SentAt == nil {
			return false
		}
		if visible[i].SentAt == nil {
			return visible[i].UID > visible[j].UID
		}
		if !visible[i].SentAt.Equal(*visible[j].SentAt) {
			return visible[i].SentAt.After(*visible[j].SentAt)
		}
		return visible[i].UID > visible[j].UID
	})
	return visible, options, counts
}

func normalizeMissiveView(view string) string {
	switch strings.ToLower(strings.TrimSpace(view)) {
	case missiveViewOneShots:
		return missiveViewOneShots
	case missiveViewSent:
		return missiveViewSent
	default:
		return missiveViewUnsent
	}
}

func missiveViewForLetter(letter *mtypes.Letter) string {
	if letter.OnlyFor != mtypes.OnlyForTemplated {
		return missiveViewOneShots
	}
	if letter.SentAt != nil || missiveIsScheduled(letter) {
		return missiveViewSent
	}
	return missiveViewUnsent
}

func missiveIsScheduled(letter *mtypes.Letter) bool {
	if letter == nil || letter.SentAt != nil {
		return false
	}
	switch strings.ToLower(strings.TrimSpace(letter.SendAt)) {
	case "", "now", "onsub":
		return false
	default:
		return true
	}
}

func missiveIndexURL(view, newsletter string) string {
	query := url.Values{}
	query.Set("view", normalizeMissiveView(view))
	if newsletter = strings.TrimSpace(newsletter); newsletter != "" {
		query.Set("newsletter", newsletter)
	}
	return "/admin/missives?" + query.Encode()
}

func oneShotMissiveLabels() map[string]string {
	return map[string]string{
		"vollogin":           "Sign-in link",
		"volapp":             "Volunteer application received",
		"volsignup":          "Volunteer shift signup",
		"volwaitlist":        "Volunteer waitlist",
		"volshifts":          "Volunteer shifts confirmed",
		"volcancel":          "Volunteer cancellation",
		"ticket":             "Ticket receipt",
		"talkapp":            "Talk application received",
		"talkinvited":        "Talk invitation",
		"talkinvited-direct": "Direct talk invitation",
		"talkconfirmed":      "Talk confirmed",
		"talkdeclined":       "Talk declined",
		"talkwaitlisted":     "Talk waitlisted",
		"talkrejected":       "Talk rejected",
		"talkselfdecline":    "Speaker declined talk",
	}
}

func TemplatedMissivesNew(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	renderTemplatedMissivesEditor(w, r, ctx, &TemplatedMissivesPage{
		IsNew:        true,
		Form:         defaultTemplatedMissiveForm(),
		FlashMessage: r.URL.Query().Get("flash"),
		ErrorMessage: r.URL.Query().Get("error"),
		SpacesReady:  spaces.IsConfigured(),
		Year:         helpers.CurrentYear(),
	})
}

func TemplatedMissivesEdit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	uid, err := strconv.ParseUint(strings.TrimSpace(mux.Vars(r)["uid"]), 10, 64)
	if err != nil || uid == 0 {
		http.Error(w, "Bad missive UID", http.StatusBadRequest)
		return
	}
	letter, err := getters.GetLetter(ctx, uid)
	if err != nil || strings.TrimSpace(letter.OnlyFor) == "" {
		http.Error(w, "Missive not found", http.StatusNotFound)
		return
	}
	if letter.OnlyFor != mtypes.OnlyForTemplated {
		active, activeErr := getters.GetLetterFor(ctx, letter.OnlyFor)
		if activeErr != nil || active.PageID != letter.PageID {
			http.Error(w, "Reusable missive not found", http.StatusNotFound)
			return
		}
		renderInlineMissiveEditor(w, r, ctx, &InlineMissivePage{
			Current:      letter,
			Fields:       onlyForTemplateFields(letter.OnlyFor),
			FieldNotice:  onlyForTemplateFieldNotice(letter.OnlyFor),
			FlashMessage: r.URL.Query().Get("flash"),
			ErrorMessage: r.URL.Query().Get("error"),
			Year:         helpers.CurrentYear(),
		})
		return
	}
	renderTemplatedMissivesEditor(w, r, ctx, &TemplatedMissivesPage{
		Current:      letter,
		Form:         formFromTemplatedLetter(letter),
		IsNew:        false,
		CanDelete:    letter.SentAt == nil,
		CanCancel:    letter.SentAt == nil,
		FlashMessage: r.URL.Query().Get("flash"),
		ErrorMessage: r.URL.Query().Get("error"),
		SpacesReady:  spaces.IsConfigured(),
		Year:         helpers.CurrentYear(),
	})
}

func InlineMissiveSave(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	uid, err := strconv.ParseUint(strings.TrimSpace(mux.Vars(r)["uid"]), 10, 64)
	if err != nil || uid == 0 {
		http.Error(w, "Bad missive UID", http.StatusBadRequest)
		return
	}
	letter, err := getters.GetLetter(ctx, uid)
	if err != nil || strings.TrimSpace(letter.OnlyFor) == "" || letter.OnlyFor == mtypes.OnlyForTemplated {
		http.Error(w, "Reusable missive not found", http.StatusNotFound)
		return
	}
	active, err := getters.GetLetterFor(ctx, letter.OnlyFor)
	if err != nil || active.PageID != letter.PageID {
		http.Error(w, "Reusable missive not found", http.StatusNotFound)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "Bad form", http.StatusBadRequest)
		return
	}
	title := strings.TrimSpace(r.FormValue("Title"))
	markdown := r.FormValue("Markdown")
	if title == "" {
		renderInlineMissiveSaveError(w, r, ctx, letter, title, markdown, "Subject is required")
		return
	}
	if _, err := texttemplate.New("subject").Parse(title); err != nil {
		renderInlineMissiveSaveError(w, r, ctx, letter, title, markdown, "Subject template is invalid: "+err.Error())
		return
	}
	if _, err := texttemplate.New("body").Parse(markdown); err != nil {
		renderInlineMissiveSaveError(w, r, ctx, letter, title, markdown, "Body template is invalid: "+err.Error())
		return
	}
	if err := getters.UpdateOnlyForMissive(ctx, letter.PageID, title, markdown); err != nil {
		renderInlineMissiveSaveError(w, r, ctx, letter, title, markdown, "Save failed: "+err.Error())
		return
	}
	http.Redirect(w, r, templatedMissiveEditorURL(uid, "flash", letter.OnlyFor+" template updated"), http.StatusSeeOther)
}

func renderInlineMissiveSaveError(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, letter *mtypes.Letter, title, markdown, message string) {
	copy := *letter
	copy.Title = title
	copy.Markdown = markdown
	renderInlineMissiveEditor(w, r, ctx, &InlineMissivePage{
		Current:      &copy,
		Fields:       onlyForTemplateFields(letter.OnlyFor),
		FieldNotice:  onlyForTemplateFieldNotice(letter.OnlyFor),
		ErrorMessage: message,
		Year:         helpers.CurrentYear(),
	})
}

func onlyForTemplateFields(onlyFor string) []EmailFieldGroup {
	direct := func(items ...string) EmailFieldGroup {
		return EmailFieldGroup{Name: "Direct fields", Items: items}
	}
	volunteer := func(items ...string) []EmailFieldGroup {
		groups := []EmailFieldGroup{direct(items...), fieldGroup(".Volunteer", types.Volunteer{}, false), fieldGroup(".Conf", types.Conf{}, false)}
		return groups
	}
	switch strings.ToLower(strings.TrimSpace(onlyFor)) {
	case "vollogin":
		return []EmailFieldGroup{direct(".Email", ".VolShiftLink", ".URI")}
	case "volapp":
		return append(volunteer(".Name", ".Email", ".VolShiftLink", ".URI"), fieldGroup(".VolInfo", types.VolInfo{}, false))
	case "volsignup", "volwaitlist":
		return volunteer(".Email", ".VolShiftLink", ".URI")
	case "volshifts":
		return append(volunteer(".Email", ".VolShiftLink", ".URI"), fieldGroup(".VolInfo", types.VolInfo{}, false))
	case "volcancel":
		return volunteer(".VolShiftLink", ".URI")
	case "ticket":
		return []EmailFieldGroup{
			direct(".Email", ".URI", ".DayCount", ".DashboardLink"),
			fieldGroup(".Conf", types.Conf{}, false),
		}
	case "talkapp", "talkinvited", "talkconfirmed", "talkdeclined", "talkwaitlisted", "talkrejected", "talkinvited-direct", "talkselfdecline":
		return []EmailFieldGroup{
			direct(".Email", ".TalkConfirmLink", ".DashboardLink", ".MagicLink", ".Note", ".URI"),
			fieldGroup(".Proposal", types.Proposal{}, false),
			fieldGroup(".Speaker", types.Speaker{}, false),
			fieldGroup(".Conf", types.Conf{}, false),
			fieldGroup(".Speakers", types.Speaker{}, true),
			fieldGroup(".SpeakerConfs", types.SpeakerConf{}, true),
		}
	default:
		return []EmailFieldGroup{direct(".Email", ".URI")}
	}
}

func onlyForTemplateFieldNotice(onlyFor string) string {
	switch strings.ToLower(strings.TrimSpace(onlyFor)) {
	case "talkinvited-direct":
		return ".MagicLink and .Note are populated for direct invitations. Other proposal emails may receive them as empty strings."
	case "ticket":
		return "The ticket PDF is attached by the sending code and is not referenced from the body template."
	default:
		return "Fields may be empty when the underlying record does not contain a value."
	}
}

func TemplatedMissivesCreateWeekly(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}

	result, err := createWeeklyNewsletterDraft(ctx, time.Now())
	if err != nil {
		redirectTemplatedMissivesErr(w, r, "Unable to create weekly issue: "+err.Error())
		return
	}
	if result.Existing {
		http.Redirect(w, r, templatedMissiveEditorURL(result.Letter.UID, "flash", "This weekly issue already exists"), http.StatusSeeOther)
		return
	}
	flash := "Weekly issue draft created — review and send a test before scheduling"
	if result.InsiderErr != nil {
		flash += ". Insider RSS was unavailable, so that section was omitted"
	}
	http.Redirect(w, r, templatedMissiveEditorURL(result.Letter.UID, "flash", flash), http.StatusSeeOther)
}

type weeklyNewsletterDraftResult struct {
	Letter     *mtypes.Letter
	Existing   bool
	InsiderErr error
}

func createWeeklyNewsletterDraft(ctx *config.AppContext, builtAt time.Time) (*weeklyNewsletterDraftResult, error) {
	sendAt := nextWeeklyNewsletterSendAt(builtAt)
	dedupeKey := weeklyNewsletterDedupeKey(sendAt)
	if existing, err := getters.GetTemplatedLetterByDedupeKey(ctx, dedupeKey); err != nil {
		return nil, fmt.Errorf("check for an existing weekly issue: %w", err)
	} else if existing != nil {
		return &weeklyNewsletterDraftResult{Letter: existing, Existing: true}, nil
	}

	confs, err := getters.ListConfs(ctx)
	if err != nil {
		return nil, fmt.Errorf("load upcoming conferences: %w", err)
	}
	updates, err := getters.WeeklyNewsletterUpdates(ctx, sendAt)
	if err != nil {
		return nil, fmt.Errorf("load weekly updates: %w", err)
	}
	insiderIssue, insiderErr := getters.LatestInsiderWeeklyIssue(ctx.DatabaseContext(), sendAt)
	if insiderErr != nil {
		ctx.Err.Printf("weekly newsletter Insider RSS failed: %s", insiderErr)
	}
	form := weeklyNewsletterForm(sendAt, nextNewsletterConference(confs, sendAt), updates, insiderIssue)
	if spaces.IsConfigured() {
		if heroKey, heroErr := spaces.LatestKey("talks/", ".png"); heroErr != nil {
			ctx.Err.Printf("weekly newsletter latest clipart failed: %s", heroErr)
		} else if heroKey != "" {
			form.Hero = spaces.PublicURL(heroKey)
		}
	}
	featuredTalkID := ""
	if updates.TalkOfWeek != nil {
		featuredTalkID = updates.TalkOfWeek.TalkID
	}
	letter, err := getters.CreateWeeklyNewsletterMissive(ctx, getters.MissiveInput{
		Title:       form.Title,
		Markdown:    buildTemplatedMissiveMarkdown(form),
		SendAt:      form.SendAt,
		Newsletters: []string{"newsletter"},
		OnlyFor:     mtypes.OnlyForTemplated,
		DedupeKey:   dedupeKey,
	}, featuredTalkID)
	if err != nil {
		// The unique dedupe key also protects two near-simultaneous builders.
		if existing, lookupErr := getters.GetTemplatedLetterByDedupeKey(ctx, dedupeKey); lookupErr == nil && existing != nil {
			return &weeklyNewsletterDraftResult{Letter: existing, Existing: true, InsiderErr: insiderErr}, nil
		}
		return nil, fmt.Errorf("save weekly issue: %w", err)
	}
	return &weeklyNewsletterDraftResult{Letter: letter, InsiderErr: insiderErr}, nil
}

func weeklyNewsletterDedupeKey(sendAt time.Time) string {
	return "weekly-newsletter:" + sendAt.Format("2006-01-02")
}

func nextWeeklyNewsletterSendAt(now time.Time) time.Time {
	loc := weeklyNewsletterCentralLocation()
	localNow := now.In(loc)
	days := (int(time.Tuesday) - int(localNow.Weekday()) + 7) % 7
	candidate := time.Date(localNow.Year(), localNow.Month(), localNow.Day()+days, 10, 0, 0, 0, loc)
	if !candidate.After(localNow) {
		candidate = candidate.AddDate(0, 0, 7)
	}
	return candidate
}

func nextNewsletterConference(confs []*types.Conf, after time.Time) *types.Conf {
	var next *types.Conf
	for _, conf := range confs {
		if conf == nil || !conf.Active || !conf.IsPublished() || conf.StartDate.Before(after) {
			continue
		}
		if next == nil || conf.StartDate.Before(next.StartDate) {
			next = conf
		}
	}
	return next
}

func weeklyNewsletterForm(sendAt time.Time, conf *types.Conf, updates *getters.WeeklyNewsletterUpdateBundle, insiderIssue *getters.InsiderWeeklyIssue) TemplatedMissiveForm {
	date := sendAt.Format("January 2, 2006")
	sponsorThanks := weeklyNewsletterSponsorThanksMarkdown(updates)
	if sponsorThanks != "" {
		sponsorThanks += "\n\n"
	}
	happening := weeklyNewsletterUpdatesMarkdown(updates)
	if happening != "" {
		happening = "### § What's Happening at bitcoin++\n\n*Nothing ever happens, except at the frontier of bitcoin. Here's what's new in the bitcoin++ universe.*\n\n" + happening + "\n\n"
	}
	broadcasts := weeklyNewsletterBroadcastsMarkdown(updates)
	if broadcasts != "" {
		broadcasts += "\n\n"
	}
	insider := weeklyNewsletterInsiderMarkdown(insiderIssue)
	if insider != "" {
		insider += "\n\n"
	}
	talkOfWeek := "[Choose a past bitcoin++ talk related to this week's news. Add a short description and link.]"
	if updates != nil && updates.TalkOfWeek != nil {
		talkOfWeek = weeklyNewsletterTalkOfWeekMarkdown(updates.TalkOfWeek)
	}
	form := TemplatedMissiveForm{
		Title:       "bitcoin++ weekly · " + date,
		SendAt:      sendAt.Format(time.RFC3339),
		Newsletters: "newsletter",
		Template:    "roundup",
		Palette:     "ember",
		Issue:       "WEEKLY · " + sendAt.Format("2006-01-02"),
		Ticker:      "BITCOIN++ WEEKLY\nTALKS RELEASED\nSPEAKERS ADDED\nEVENTS\nMERCHANDISE",
		LeadEyebrow: "",
		LeadTitle:   "what's new in the bitcoin++ universe",
		LeadDeck:    "A weekly briefing from bitcoin++ · " + date,
		ContentMarkdown: sponsorThanks + happening + broadcasts + insider + "### § Talk of the week\n\n" + talkOfWeek + `

Want more cutting-edge technical Bitcoin content?

Our weekly email summaries tell you what happened in Bitcoin, but our conferences are where you learn what happens next directly from top Bitcoin developers—and where you get to build the future of Bitcoin.

See you there,

~nifty`,
	}
	if conf != nil {
		confPath := "/" + url.PathEscape(conf.Tag)
		confURL := weeklyNewsletterSiteURL(confPath)
		ticketsURL := weeklyNewsletterSiteURL(confPath + "?code=SUBSCRIBER20#tickets")
		eventTitle := firstNonEmpty(conf.Desc, "bitcoin++ "+conf.Location)
		eventSubtitle := strings.Trim(strings.Join([]string{conf.DateDesc, conf.Location}, " · "), " ·")
		upcomingEvent := "Come to our upcoming event, [" + eventTitle + "](" + confURL + ")"
		if eventSubtitle != "" {
			upcomingEvent += ", " + eventSubtitle
		}
		upcomingEvent += "."
		form.ContentMarkdown = strings.Replace(form.ContentMarkdown,
			"Want more cutting-edge technical Bitcoin content?",
			"Want more cutting-edge technical Bitcoin content?\n\n"+upcomingEvent, 1)
		form.CTAEyebrow = "subscriber offer"
		form.CTATitle = "Join us in " + firstNonEmpty(strings.TrimSpace(conf.Location), eventTitle)
		form.CTASubtitle = "Since you're a bitcoin++ subscriber, use this 20% discount code: **SUBSCRIBER20**"
		form.CTALabel = "Get your ticket"
		form.CTAURL = ticketsURL
	}
	return form
}

func weeklyNewsletterTalkOfWeekMarkdown(talk *getters.WeeklyNewsletterTalk) string {
	if talk == nil {
		return ""
	}
	title := markdownNewsletterText(firstNonEmpty(talk.Title, "bitcoin++ talk"))
	line := "[" + title + "](" + strings.TrimSpace(talk.YouTubeURL) + ")"
	if speakers := strings.TrimSpace(talk.SpeakerNames); speakers != "" {
		line += " by " + markdownNewsletterText(speakers)
	}
	if event := firstNonEmpty(talk.ConfTitle, talk.ConfTag); event != "" {
		line += " · " + markdownNewsletterText(event)
	}
	line += "."
	if summary := weeklyNewsletterTalkSummary(talk.Description, 320); summary != "" {
		line += "\n\n" + summary
	}
	return line
}

func weeklyNewsletterTalkSummary(description string, limit int) string {
	description = strings.Join(strings.Fields(description), " ")
	if description == "" || limit < 1 {
		return ""
	}
	runes := []rune(description)
	if len(runes) <= limit {
		return description
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func weeklyNewsletterSiteURL(path string) string {
	return "{{ .URI }}" + path
}

func weeklyNewsletterInsiderMarkdown(issue *getters.InsiderWeeklyIssue) string {
	if issue == nil || strings.TrimSpace(issue.Link) == "" || len(issue.Bullets) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("### § Last week in Bitcoin\n\n")
	b.WriteString("*Here's what we're reporting on over at [Insider Edition](")
	b.WriteString(issue.Link)
	b.WriteString("):*\n\n")
	limit := min(len(issue.Bullets), 3)
	for _, bullet := range issue.Bullets[:limit] {
		b.WriteString("- ")
		text := markdownNewsletterText(bullet.Text)
		b.WriteString(text)
		if strings.TrimSpace(bullet.Link) != "" {
			b.WriteString(" [Link](")
			b.WriteString(strings.TrimSpace(bullet.Link))
			b.WriteString(")")
		}
		b.WriteByte('\n')
	}
	b.WriteString("\n")
	b.WriteString("→ [Read the full weekly summary](")
	b.WriteString(issue.Link)
	b.WriteString(")")
	return b.String()
}

func weeklyNewsletterUpdatesMarkdown(updates *getters.WeeklyNewsletterUpdateBundle) string {
	if updates == nil {
		return ""
	}
	var bullets []string
	for _, group := range updates.SpeakerGroups {
		eventURL := weeklyNewsletterSiteURL("/" + url.PathEscape(group.ConfTag) + "/talks")
		eventTitle := firstNonEmpty(group.ConfTitle, group.Location, group.ConfTag)
		var speakerLines []string
		for _, speaker := range group.Speakers[:min(len(group.Speakers), 5)] {
			speakerLines = append(speakerLines, weeklyNewsletterSpeakerLine(speaker))
		}
		if len(speakerLines) > 0 {
			bullets = append(bullets, "- New speakers confirmed for ["+markdownNewsletterText(eventTitle)+"]("+eventURL+"):\n"+strings.Join(speakerLines, "\n"))
		}
	}
	for _, talk := range updates.Talks {
		bullets = append(bullets, weeklyNewsletterTalkBullet(talk, "New talk: "))
	}
	for _, merch := range updates.MerchUpdates {
		shopURL := weeklyNewsletterSiteURL("/shop/" + url.PathEscape(merch.Slug))
		name := "[" + markdownNewsletterText(firstNonEmpty(merch.Name, "bitcoin++ merch")) + "](" + shopURL + ")"
		switch merch.Kind {
		case "added":
			bullets = append(bullets, "- New merch: "+name+" is now available in the bitcoin++ shop.")
		case "restocked":
			bullets = append(bullets, "- Back in stock: "+name+" is available again in the bitcoin++ shop.")
		}
	}
	for _, group := range updates.NewSponsorGroups {
		eventURL := weeklyNewsletterSiteURL("/" + url.PathEscape(group.ConfTag))
		eventTitle := firstNonEmpty(group.ConfTitle, group.ConfTag)
		var sponsorLines []string
		for _, sponsor := range group.Sponsors {
			name := weeklyNewsletterSponsorLink(sponsor)
			level := weeklyNewsletterSponsorLevel(sponsor.Level)
			if level != "" {
				name += " — " + markdownNewsletterText(level) + " sponsor"
			}
			sponsorLines = append(sponsorLines, "    - "+name)
		}
		if len(sponsorLines) > 0 {
			bullets = append(bullets, "- New sponsors for ["+markdownNewsletterText(eventTitle)+"]("+eventURL+"):\n"+strings.Join(sponsorLines, "\n"))
		}
	}
	for _, change := range updates.TicketChanges {
		if change.Conf == nil || change.Current == nil || change.Next == nil {
			continue
		}
		eventURL := weeklyNewsletterSiteURL("/" + url.PathEscape(change.Conf.Tag))
		eventTitle := firstNonEmpty(change.Conf.Desc, change.Conf.Location, change.Conf.Tag)
		bullets = append(bullets, "- Ticket prices for ["+markdownNewsletterText(eventTitle)+"]("+eventURL+") rise from "+weeklyNewsletterTicketPrice(change.Current)+" to "+weeklyNewsletterTicketPrice(change.Next)+" on "+change.Current.SalesEndAt.Format("Monday, January 2")+". Get them before they're gone.")
	}
	for _, winner := range updates.HackathonWinners[:min(len(updates.HackathonWinners), 3)] {
		projectURL := weeklyNewsletterSiteURL("/" + url.PathEscape(winner.ConfTag) + "/hackathon/projects/" + url.PathEscape(winner.ProjectID))
		bullets = append(bullets, "- Hackathon winner: ["+markdownNewsletterText(winner.ProjectTitle)+"]("+projectURL+") won "+markdownNewsletterText(winner.Awards)+" at "+markdownNewsletterText(winner.Competition)+".")
	}
	return strings.Join(bullets, "\n")
}

func weeklyNewsletterBroadcastsMarkdown(updates *getters.WeeklyNewsletterUpdateBundle) string {
	if updates == nil || len(updates.Broadcasts) == 0 {
		return ""
	}
	broadcasts := updates.Broadcasts
	if len(broadcasts) > 3 {
		broadcasts = broadcasts[len(broadcasts)-3:]
	}
	bullets := make([]string, 0, len(broadcasts))
	for _, talk := range broadcasts {
		line := weeklyNewsletterTalkBullet(talk, "")
		if !talk.PublishAt.IsZero() {
			line = strings.TrimSuffix(line, ".") + ". Published " + talk.PublishAt.In(weeklyNewsletterCentralLocation()).Format("Monday, January 2") + "."
		}
		bullets = append(bullets, line)
	}
	return "### § bitcoin++ broadcasts\n\n*New talks were posted. Check out the latest on our [YouTube](https://www.youtube.com/@btcplusplus/videos).*\n\n" + strings.Join(bullets, "\n")
}

func weeklyNewsletterSpeakerLine(speaker getters.WeeklyNewsletterSpeaker) string {
	line := markdownNewsletterText(firstNonEmpty(speaker.Name, "Unnamed speaker"))
	if company := strings.TrimSpace(speaker.Company); company != "" {
		line += " of " + markdownNewsletterText(company)
	}
	line += "."

	xURL := strings.TrimSpace(speaker.XURL)
	nostrURL := strings.TrimSpace(speaker.NostrURL)
	websiteURL := strings.TrimSpace(speaker.WebsiteURL)
	if xURL == "" && nostrURL == "" && websiteURL == "" {
		profileURL := strings.TrimSpace(speaker.ProfileURL)
		switch {
		case strings.Contains(profileURL, "x.com/"), strings.Contains(profileURL, "twitter.com/"):
			xURL = profileURL
		case strings.Contains(profileURL, "njump.me/"):
			nostrURL = profileURL
		default:
			websiteURL = profileURL
		}
	}
	var links []string
	if xURL != "" {
		links = append(links, "[x.com]("+xURL+")")
	}
	if nostrURL != "" {
		links = append(links, "[nostr]("+nostrURL+")")
	}
	if websiteURL != "" {
		links = append(links, "[website]("+websiteURL+")")
	}
	if len(links) > 0 {
		line += " " + strings.Join(links, " ")
	}
	return "    - " + line
}

func weeklyNewsletterTalkBullet(talk getters.WeeklyNewsletterTalk, prefix string) string {
	talkURL := strings.TrimSpace(talk.YouTubeURL)
	if talkURL == "" {
		talkURL = weeklyNewsletterSiteURL("/" + url.PathEscape(talk.ConfTag) + "/agenda")
	}
	credit := ""
	if strings.TrimSpace(talk.SpeakerNames) != "" {
		credit = " by " + markdownNewsletterText(talk.SpeakerNames)
	}
	return "- " + prefix + "[" + markdownNewsletterText(talk.Title) + "](" + talkURL + ")" + credit + "."
}

func weeklyNewsletterSponsorThanksMarkdown(updates *getters.WeeklyNewsletterUpdateBundle) string {
	if updates == nil || len(updates.SupportingSponsors) == 0 {
		return ""
	}
	names := make([]string, 0, len(updates.SupportingSponsors))
	for _, sponsor := range updates.SupportingSponsors {
		names = append(names, weeklyNewsletterSponsorLink(sponsor))
	}
	return "We'd like to thank " + newsletterEnglishList(names) + " for their support in making bitcoin++ possible."
}

func weeklyNewsletterSponsorLink(sponsor getters.WeeklyNewsletterSponsor) string {
	name := markdownNewsletterText(sponsor.Name)
	if sponsorURL := strings.TrimSpace(sponsor.URL); sponsorURL != "" {
		return "[" + name + "](" + sponsorURL + ")"
	}
	return name
}

func weeklyNewsletterSponsorLevel(level string) string {
	level = strings.TrimSpace(level)
	lower := strings.ToLower(level)
	for _, suffix := range []string{" sponsors", " sponsor", " level"} {
		if strings.HasSuffix(lower, suffix) {
			return strings.TrimSpace(level[:len(level)-len(suffix)])
		}
	}
	return level
}

func newsletterEnglishList(items []string) string {
	switch len(items) {
	case 0:
		return ""
	case 1:
		return items[0]
	case 2:
		return items[0] + " and " + items[1]
	default:
		return strings.Join(items[:len(items)-1], ", ") + ", and " + items[len(items)-1]
	}
}

func weeklyNewsletterTicketPrice(ticket *types.ConfTicket) string {
	if ticket == nil {
		return ""
	}
	price := strconv.FormatUint(uint64(ticket.StandardPrice()), 10)
	if ticket.Symbol != "" || ticket.PostSymbol != "" {
		return ticket.Symbol + price + ticket.PostSymbol
	}
	if ticket.Currency != "" {
		return price + " " + strings.ToUpper(ticket.Currency)
	}
	return price
}

func markdownNewsletterText(value string) string {
	value = strings.ReplaceAll(value, "[", "\\[")
	return strings.ReplaceAll(value, "]", "\\]")
}

func TemplatedMissivesSave(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadFileBytes)
	if err := r.ParseMultipartForm(maxUploadFileBytes); err != nil {
		redirectTemplatedMissivesErr(w, r, "Bad form: "+err.Error())
		return
	}

	form := templatedMissiveFormFromRequest(r)
	if strings.TrimSpace(form.Title) == "" {
		renderTemplatedMissivesAdminWithForm(w, r, ctx, form, "Title is required")
		return
	}

	expiry, err := parseOptionalDate(form.Expiry)
	if err != nil {
		renderTemplatedMissivesAdminWithForm(w, r, ctx, form, "Expiry must be YYYY-MM-DD")
		return
	}

	input := getters.MissiveInput{
		Title:       strings.TrimSpace(form.Title),
		Markdown:    buildTemplatedMissiveMarkdown(form),
		SendAt:      strings.TrimSpace(form.SendAt),
		Newsletters: splitCommaList(form.Newsletters),
		OnlyFor:     mtypes.OnlyForTemplated,
		Expiry:      expiry,
	}
	if len(input.Newsletters) == 0 {
		input.Newsletters = []string{"newsletter"}
	}

	if form.UID == 0 {
		letter, err := getters.CreateTemplatedMissive(ctx, input)
		if err != nil {
			renderTemplatedMissivesAdminWithForm(w, r, ctx, form, "Create failed: "+err.Error())
			return
		}
		http.Redirect(w, r, templatedMissiveEditorURL(letter.UID, "flash", "Templated missive created"), http.StatusSeeOther)
		return
	}

	letter, err := getters.GetLetter(ctx, form.UID)
	if err != nil {
		renderTemplatedMissivesAdminWithForm(w, r, ctx, form, "Missive not found: "+err.Error())
		return
	}
	if letter.OnlyFor != mtypes.OnlyForTemplated {
		renderTemplatedMissivesAdminWithForm(w, r, ctx, form, "Refusing to edit a non-templated missive")
		return
	}
	if err := getters.UpdateTemplatedMissive(ctx, letter.PageID, input); err != nil {
		renderTemplatedMissivesAdminWithForm(w, r, ctx, form, "Update failed: "+err.Error())
		return
	}
	http.Redirect(w, r, templatedMissiveEditorURL(form.UID, "flash", "Templated missive updated"), http.StatusSeeOther)
}

func TemplatedMissivesUploadImage(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	if !spaces.IsConfigured() {
		http.Error(w, "spaces not configured", http.StatusInternalServerError)
		return
	}
	limitRequestBody(w, r, maxMultipartBodyBytes)
	raw, contentType, ext, err := readMultipartFile(r, "file")
	if err != nil {
		http.Error(w, "missing or unreadable image", http.StatusBadRequest)
		return
	}
	shortID := imgproc.ShortID(raw)
	key := "newsletter/" + shortID + ext
	if !spaces.Exists(key) {
		if _, err := spaces.Upload(key, raw, contentType, ""); err != nil {
			ctx.Err.Printf("/admin/missives/upload-image: %s", err)
			http.Error(w, "upload failed", http.StatusInternalServerError)
			return
		}
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"url": spaces.PublicURL(key)})
}

func TemplatedMissivesTestSend(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadFileBytes)
	if err := r.ParseMultipartForm(maxUploadFileBytes); err != nil {
		redirectTemplatedMissivesErr(w, r, "Bad form: "+err.Error())
		return
	}

	form := templatedMissiveFormFromRequest(r)
	form.TestEmail = strings.TrimSpace(r.FormValue("TestEmail"))
	if strings.TrimSpace(form.Title) == "" {
		renderTemplatedMissivesAdminWithForm(w, r, ctx, form, "Title is required before sending a test")
		return
	}
	addr, err := netmail.ParseAddress(form.TestEmail)
	if err != nil || addr.Address == "" {
		renderTemplatedMissivesAdminWithForm(w, r, ctx, form, "Enter a valid test recipient email")
		return
	}

	letter := templatedMissiveTestLetter(form)
	sub := subscriberForTemplatedMissiveTest(addr.Address, letter)
	if _, err := emails.SendNewsletterMissive(ctx, sub, letter, time.Now(), true); err != nil {
		renderTemplatedMissivesAdminWithForm(w, r, ctx, form, "Test send failed: "+err.Error())
		return
	}
	renderTemplatedMissivesAdminWithMessages(w, r, ctx, form, "Test missive sent to "+addr.Address, "")
}

func TemplatedMissivesSchedule(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, maxUploadFileBytes)
	if err := r.ParseMultipartForm(maxUploadFileBytes); err != nil {
		redirectTemplatedMissivesErr(w, r, "Bad form: "+err.Error())
		return
	}
	uidValue := strings.TrimSpace(r.FormValue("UID"))
	if uidValue == "" {
		uidValue = strings.TrimSpace(r.URL.Query().Get("uid"))
	}
	uid, err := strconv.ParseUint(uidValue, 10, 64)
	if err != nil || uid == 0 {
		redirectTemplatedMissivesErr(w, r, "Save the missive before scheduling it")
		return
	}
	letter, started, err := missives.QueueMissiveByUID(ctx, uid)
	if err != nil {
		http.Redirect(w, r, templatedMissiveEditorURL(uid, "error", "Schedule failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	message := "Scheduling " + letter.Missive() + " in the background"
	if !started {
		message = letter.Missive() + " is already being scheduled"
	}
	http.Redirect(w, r, templatedMissiveEditorURL(uid, "flash", message), http.StatusSeeOther)
}

func TemplatedMissivesDelete(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	uid, err := strconv.ParseUint(strings.TrimSpace(mux.Vars(r)["uid"]), 10, 64)
	if err != nil || uid == 0 {
		http.Error(w, "Bad missive UID", http.StatusBadRequest)
		return
	}
	deleted, err := getters.DeleteTemplatedDraft(ctx, uid)
	if err != nil {
		http.Redirect(w, r, templatedMissiveEditorURL(uid, "error", "Delete failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	if !deleted {
		http.Redirect(w, r, templatedMissiveEditorURL(uid, "error", "Only unsent templated missives can be deleted"), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, "/admin/missives?flash="+url.QueryEscape("Draft MISS-"+strconv.FormatUint(uid, 10)+" deleted"), http.StatusSeeOther)
}

func TemplatedMissivesCancel(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	uid, err := strconv.ParseUint(strings.TrimSpace(mux.Vars(r)["uid"]), 10, 64)
	if err != nil || uid == 0 {
		http.Error(w, "Bad missive UID", http.StatusBadRequest)
		return
	}
	if _, err := missives.CancelMissiveByUID(ctx, uid); err != nil {
		http.Redirect(w, r, templatedMissiveEditorURL(uid, "error", "Cancellation failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, templatedMissiveEditorURL(uid, "flash", "Queued deliveries canceled for MISS-"+strconv.FormatUint(uid, 10)), http.StatusSeeOther)
}

func renderTemplatedMissivesAdminWithForm(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, form TemplatedMissiveForm, msg string) {
	renderTemplatedMissivesAdminWithMessages(w, r, ctx, form, "", msg)
}

func renderTemplatedMissivesAdminWithMessages(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, form TemplatedMissiveForm, flash, errMsg string) {
	canDelete := false
	canCancel := false
	if form.UID != 0 {
		if letter, err := getters.GetLetter(ctx, form.UID); err == nil {
			canDelete = letter.OnlyFor == mtypes.OnlyForTemplated && letter.SentAt == nil
			canCancel = canDelete
		}
	}
	renderTemplatedMissivesEditor(w, r, ctx, &TemplatedMissivesPage{
		Form:         form,
		IsNew:        form.UID == 0,
		CanDelete:    canDelete,
		CanCancel:    canCancel,
		FlashMessage: flash,
		ErrorMessage: errMsg,
		SpacesReady:  spaces.IsConfigured(),
		Year:         helpers.CurrentYear(),
	})
}

func renderTemplatedMissivesIndex(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, page *TemplatedMissivesPage) {
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/templated_missives_index.tmpl", page); err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("/admin/missives index template failed: %s", err)
	}
}

func renderTemplatedMissivesEditor(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, page *TemplatedMissivesPage) {
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/templated_missives.tmpl", page); err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("/admin/missives editor template failed: %s", err)
	}
}

func renderInlineMissiveEditor(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, page *InlineMissivePage) {
	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/inline_missive.tmpl", page); err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("/admin/missives inline editor failed: %s", err)
	}
}

func templatedMissiveEditorURL(uid uint64, key, message string) string {
	dest := "/admin/missives/" + strconv.FormatUint(uid, 10)
	if key != "" && message != "" {
		dest += "?" + url.Values{key: []string{message}}.Encode()
	}
	return dest
}

func defaultTemplatedMissiveForm() TemplatedMissiveForm {
	return TemplatedMissiveForm{
		SendAt:      "now",
		Newsletters: "newsletter",
		Template:    "roundup",
		Palette:     "ember",
		LeadEyebrow: "§ FEATURE",
		CTALabel:    "READ MORE",
	}
}

func formFromTemplatedLetter(letter *mtypes.Letter) TemplatedMissiveForm {
	form := defaultTemplatedMissiveForm()
	form.UID = letter.UID
	form.PageID = letter.PageID
	form.Title = letter.Title
	form.SendAt = letter.SendAt
	form.Newsletters = strings.Join(letter.Newsletters, ", ")
	form.ContentMarkdown = letter.Markdown
	if letter.Expiry != nil {
		form.Expiry = letter.Expiry.Format("2006-01-02")
	}

	cfg, body := parseTemplatedMissiveFrontmatter(letter.Markdown)
	form.ContentMarkdown = body
	if cfg["template"] != "" {
		form.Template = cfg["template"]
	}
	if cfg["palette"] != "" {
		form.Palette = cfg["palette"]
	}
	if cfg["issue"] != "" {
		form.Issue = cfg["issue"]
	}
	if cfg["hero"] != "" {
		form.Hero = cfg["hero"]
	}
	if cfg["ticker"] != "" {
		form.Ticker = cfg["ticker"]
	}
	form.ContentMarkdown = hydrateTemplatedShortcodes(&form, form.ContentMarkdown)
	return form
}

func hydrateTemplatedShortcodes(form *TemplatedMissiveForm, body string) string {
	body = strings.ReplaceAll(body, "\r\n", "\n")
	var remaining []string
	for _, line := range strings.Split(body, "\n") {
		name, args, ok := parseTemplatedShortcodeLine(line)
		if !ok {
			remaining = append(remaining, line)
			continue
		}
		switch name {
		case "lead":
			if len(args) >= 3 {
				form.LeadEyebrow = args[0]
				form.LeadTitle = args[1]
				form.LeadDeck = args[2]
				continue
			}
		case "newsList":
			if len(args) > 0 {
				form.NewsItems = strings.Join(args, "\n")
				continue
			}
		case "stats":
			if len(args) > 0 {
				form.Stats = strings.Join(args, "\n")
				continue
			}
		case "pullquote":
			if len(args) >= 1 {
				form.Pullquote = args[0]
				if len(args) >= 2 {
					form.PullquoteBy = args[1]
				}
				continue
			}
		case "cta":
			if len(args) >= 5 {
				form.CTAEyebrow = args[0]
				form.CTATitle = args[1]
				form.CTASubtitle = args[2]
				form.CTALabel = args[3]
				form.CTAURL = args[4]
				continue
			}
		}
		remaining = append(remaining, line)
	}
	return strings.TrimSpace(strings.Join(remaining, "\n"))
}

func parseTemplatedShortcodeLine(line string) (string, []string, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "{{") || !strings.HasSuffix(trimmed, "}}") {
		return "", nil, false
	}
	inner := strings.TrimSpace(strings.TrimSuffix(strings.TrimPrefix(trimmed, "{{"), "}}"))
	if inner == "" {
		return "", nil, false
	}
	nameEnd := strings.IndexAny(inner, " \t")
	if nameEnd == -1 {
		return inner, nil, true
	}
	name := inner[:nameEnd]
	args, ok := parseTemplatedQuotedArgs(strings.TrimSpace(inner[nameEnd:]))
	if !ok {
		return "", nil, false
	}
	return name, args, true
}

func parseTemplatedQuotedArgs(input string) ([]string, bool) {
	var args []string
	for {
		input = strings.TrimSpace(input)
		if input == "" {
			return args, true
		}
		const uriPrintPrefix = `(print .URI `
		if strings.HasPrefix(input, uriPrintPrefix) {
			value, rest, ok := parseTemplatedQuotedArg(strings.TrimSpace(strings.TrimPrefix(input, uriPrintPrefix)))
			if !ok {
				return nil, false
			}
			rest = strings.TrimSpace(rest)
			if !strings.HasPrefix(rest, ")") {
				return nil, false
			}
			args = append(args, weeklyNewsletterSiteURL(value))
			input = rest[1:]
			continue
		}
		if !strings.HasPrefix(input, `"`) {
			return nil, false
		}
		value, rest, ok := parseTemplatedQuotedArg(input)
		if !ok {
			return nil, false
		}
		args = append(args, value)
		input = rest
	}
}

func parseTemplatedQuotedArg(input string) (string, string, bool) {
	if !strings.HasPrefix(input, `"`) {
		return "", input, false
	}
	end := -1
	for i := 1; i < len(input); i++ {
		if input[i] == '\\' {
			i++
			continue
		}
		if input[i] == '"' {
			end = i
			break
		}
	}
	if end == -1 {
		return "", input, false
	}
	value, err := strconv.Unquote(input[:end+1])
	if err != nil {
		return "", input, false
	}
	return value, input[end+1:], true
}

func templatedMissiveFormFromRequest(r *http.Request) TemplatedMissiveForm {
	uid, _ := strconv.ParseUint(strings.TrimSpace(r.FormValue("UID")), 10, 64)
	return TemplatedMissiveForm{
		UID:             uid,
		Title:           strings.TrimSpace(r.FormValue("Title")),
		SendAt:          strings.TrimSpace(r.FormValue("SendAt")),
		Expiry:          strings.TrimSpace(r.FormValue("Expiry")),
		Newsletters:     strings.TrimSpace(r.FormValue("Newsletters")),
		Template:        strings.TrimSpace(r.FormValue("Template")),
		Palette:         strings.TrimSpace(r.FormValue("Palette")),
		Issue:           strings.TrimSpace(r.FormValue("Issue")),
		Hero:            strings.TrimSpace(r.FormValue("Hero")),
		Ticker:          strings.TrimSpace(r.FormValue("Ticker")),
		LeadEyebrow:     strings.TrimSpace(r.FormValue("LeadEyebrow")),
		LeadTitle:       strings.TrimSpace(r.FormValue("LeadTitle")),
		LeadDeck:        strings.TrimSpace(r.FormValue("LeadDeck")),
		NewsItems:       strings.TrimSpace(r.FormValue("NewsItems")),
		Stats:           strings.TrimSpace(r.FormValue("Stats")),
		Pullquote:       strings.TrimSpace(r.FormValue("Pullquote")),
		PullquoteBy:     strings.TrimSpace(r.FormValue("PullquoteBy")),
		CTAEyebrow:      strings.TrimSpace(r.FormValue("CTAEyebrow")),
		CTATitle:        strings.TrimSpace(r.FormValue("CTATitle")),
		CTASubtitle:     strings.TrimSpace(r.FormValue("CTASubtitle")),
		CTALabel:        strings.TrimSpace(r.FormValue("CTALabel")),
		CTAURL:          strings.TrimSpace(r.FormValue("CTAURL")),
		ContentMarkdown: strings.TrimSpace(r.FormValue("ContentMarkdown")),
		TestEmail:       strings.TrimSpace(r.FormValue("TestEmail")),
	}
}

func templatedMissiveTestLetter(form TemplatedMissiveForm) *mtypes.Letter {
	uid := form.UID
	if uid == 0 {
		uid = uint64(time.Now().UTC().UnixNano())
	}
	newsletters := splitCommaList(form.Newsletters)
	if len(newsletters) == 0 {
		newsletters = []string{"newsletter"}
	}
	testForm := form
	testForm.SendAt = "now"
	return &mtypes.Letter{
		UID:         uid,
		Title:       "[TEST] " + strings.TrimSpace(form.Title),
		Newsletters: newsletters,
		OnlyFor:     mtypes.OnlyForTemplated,
		Markdown:    buildTemplatedMissiveMarkdown(testForm),
		SendAt:      "now",
	}
}

func subscriberForTemplatedMissiveTest(email string, letter *mtypes.Letter) *mtypes.Subscriber {
	names := letter.InNewsletters()
	if len(names) == 0 {
		names = []string{"newsletter"}
	}
	subs := make([]*mtypes.Subscription, 0, len(names))
	seen := make(map[string]bool, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" || seen[name] {
			continue
		}
		seen[name] = true
		subs = append(subs, &mtypes.Subscription{Name: name})
	}
	return &mtypes.Subscriber{Email: email, Subs: subs}
}

func buildTemplatedMissiveMarkdown(form TemplatedMissiveForm) string {
	var b strings.Builder
	b.WriteString("---\n")
	writeFrontmatter(&b, "template", firstNonEmpty(form.Template, "roundup"))
	writeFrontmatter(&b, "palette", firstNonEmpty(form.Palette, "ember"))
	writeFrontmatter(&b, "issue", form.Issue)
	writeFrontmatter(&b, "hero", form.Hero)
	if form.Ticker != "" {
		b.WriteString("ticker:\n")
		for _, item := range splitLines(form.Ticker) {
			b.WriteString("  - ")
			b.WriteString(item)
			b.WriteByte('\n')
		}
	}
	b.WriteString("---\n\n")

	if form.LeadTitle != "" || form.LeadDeck != "" {
		b.WriteString(fmt.Sprintf("{{ lead %q %q %q }}\n\n", form.LeadEyebrow, form.LeadTitle, form.LeadDeck))
	}
	if form.NewsItems != "" {
		b.WriteString("{{ newsList")
		for _, item := range splitLines(form.NewsItems) {
			b.WriteString(fmt.Sprintf(" %q", item))
		}
		b.WriteString(" }}\n\n")
	}
	if form.Stats != "" {
		b.WriteString("{{ stats")
		for _, item := range splitLines(form.Stats) {
			b.WriteString(fmt.Sprintf(" %q", item))
		}
		b.WriteString(" }}\n\n")
	}
	if form.Pullquote != "" {
		b.WriteString(fmt.Sprintf("{{ pullquote %q %q }}\n\n", form.Pullquote, form.PullquoteBy))
	}
	if form.ContentMarkdown != "" {
		b.WriteString(form.ContentMarkdown)
		b.WriteString("\n\n")
	}
	if form.CTATitle != "" || form.CTAURL != "" {
		b.WriteString(fmt.Sprintf("{{ cta %q %q %q %q %s }}\n", form.CTAEyebrow, form.CTATitle, form.CTASubtitle, firstNonEmpty(form.CTALabel, "READ MORE"), templatedMissiveURLArg(form.CTAURL)))
	}
	return strings.TrimSpace(b.String()) + "\n"
}

func templatedMissiveURLArg(value string) string {
	const uriSelector = "{{ .URI }}"
	if strings.HasPrefix(value, uriSelector) {
		return fmt.Sprintf("(print .URI %q)", strings.TrimPrefix(value, uriSelector))
	}
	return strconv.Quote(value)
}

func writeFrontmatter(b *strings.Builder, key, value string) {
	value = strings.TrimSpace(value)
	if value == "" {
		return
	}
	b.WriteString(key)
	b.WriteString(": ")
	b.WriteString(strconv.Quote(value))
	b.WriteByte('\n')
}

func parseTemplatedMissiveFrontmatter(markdown string) (map[string]string, string) {
	out := map[string]string{}
	markdown = strings.ReplaceAll(markdown, "\r\n", "\n")
	if !strings.HasPrefix(markdown, "---\n") {
		return out, markdown
	}
	end := strings.Index(markdown[4:], "\n---")
	if end == -1 {
		return out, markdown
	}
	raw := markdown[4 : 4+end]
	body := strings.TrimLeft(markdown[4+end+len("\n---"):], "\n")
	var listKey string
	var listItems []string
	flushList := func() {
		if listKey != "" {
			out[listKey] = strings.Join(listItems, "\n")
		}
		listKey = ""
		listItems = nil
	}
	for _, line := range strings.Split(raw, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "- ") && listKey != "" {
			listItems = append(listItems, strings.Trim(strings.TrimSpace(strings.TrimPrefix(trimmed, "- ")), `"`))
			continue
		}
		flushList()
		parts := strings.SplitN(trimmed, ":", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(parts[0]))
		value := strings.Trim(strings.TrimSpace(parts[1]), `"`)
		if value == "" && key == "ticker" {
			listKey = key
			continue
		}
		out[key] = value
	}
	flushList()
	return out, body
}

func parseOptionalDate(value string) (*time.Time, error) {
	value = strings.TrimSpace(value)
	if value == "" {
		return nil, nil
	}
	t, err := time.Parse("2006-01-02", value)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func splitCommaList(value string) []string {
	parts := strings.Split(value, ",")
	var out []string
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

func splitLines(value string) []string {
	lines := strings.Split(value, "\n")
	var out []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line != "" {
			out = append(out, line)
		}
	}
	return out
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

func redirectTemplatedMissivesErr(w http.ResponseWriter, r *http.Request, msg string) {
	http.Redirect(w, r, "/admin/missives?error="+url.QueryEscape(msg), http.StatusSeeOther)
}
