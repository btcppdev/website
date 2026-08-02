package handlers

import (
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/ics"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func AdminGifts(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfStaff(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil || conf == nil {
		handle404(w, r, ctx)
		return
	}
	filePath := r.URL.Query().Get("filepath")

	talks, err := getters.GetTalksFor(ctx, conf.Tag)
	if err != nil {
		http.Error(w, "Unable to load talks", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/admin/gifts get talks: %s", conf.Tag, err)
		return
	}

	// Pick the smallest-panel talk per speaker. Key on Speaker.ID
	// when available, fall back to lower-cased name (older rows
	// may lack stable IDs).
	type pick struct {
		name    string
		clipart string
		panelN  int
	}
	best := map[string]*pick{}
	for _, talk := range talks {
		if talk == nil {
			continue
		}
		n := len(talk.Speakers)
		for _, sp := range talk.Speakers {
			if sp == nil {
				continue
			}
			key := sp.ID
			if key == "" {
				key = "name:" + strings.ToLower(strings.TrimSpace(sp.Name))
			}
			prev, ok := best[key]
			if !ok {
				best[key] = &pick{name: sp.Name, clipart: talk.Clipart, panelN: n}
				continue
			}
			// Fewer co-speakers wins. On tie, prefer the
			// non-empty clipart so a panel-with-art doesn't
			// lose to a same-size panel-without-art.
			if n < prev.panelN || (n == prev.panelN && prev.clipart == "" && talk.Clipart != "") {
				prev.clipart = talk.Clipart
				prev.panelN = n
				prev.name = sp.Name
			}
		}
	}

	rows := make([]*GiftRow, 0, len(best))
	for _, p := range best {
		rows = append(rows, &GiftRow{Clipart: p.clipart, SpeakerName: p.name})
	}

	// {conf}-staff Speakers row too — leading.png as their
	// clipart, skipped if they're already on a talk.
	for _, sp := range staffSpeakersForConf(ctx, conf.Tag) {
		if sp == nil {
			continue
		}
		key := sp.ID
		if key == "" {
			key = "name:" + strings.ToLower(strings.TrimSpace(sp.Name))
		}
		if _, ok := best[key]; ok {
			continue
		}
		best[key] = &pick{} // mark to dedupe across staff list itself
		rows = append(rows, &GiftRow{
			Clipart:     "leading.png",
			SpeakerName: sp.Name,
		})
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return strings.ToLower(rows[i].SpeakerName) < strings.ToLower(rows[j].SpeakerName)
	})

	if err := ctx.TemplateCache.ExecuteTemplate(w, "talks/gifts.tmpl", &TalksGiftsPage{
		Conf:     conf,
		Rows:     rows,
		FilePath: filePath,
		Year:     helpers.CurrentYear(),
	}); err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/admin/gifts template: %s", conf.Tag, err)
	}
}

func speakerAdminRows(ctx *config.AppContext, conf *types.Conf) ([]*SpeakerRow, error) {
	// Iterate Proposals (filtered to this conf) so each row carries
	// the proposal IDs directly. Each SpeakerConf on a proposal
	// contributes one (speaker, talk) pair; first-seen also pulls
	// per-conf overrides from the SpeakerConf.
	proposals := loadConfProposals(ctx, conf)
	rowByID := make(map[string]*SpeakerRow)
	for _, p := range proposals {
		if p == nil {
			continue
		}
		for _, sc := range resolveProposalSpeakers(p, ctx) {
			if sc == nil || sc.Speaker == nil {
				continue
			}
			sp := sc.Speaker
			row, ok := rowByID[sp.ID]
			if !ok {
				row = &SpeakerRow{
					ID:            sp.ID,
					Name:          sp.Name,
					Email:         sp.Email,
					Signal:        sp.Signal,
					Photo:         sp.Photo,
					Company:       sp.Company,
					OrgLogo:       sp.OrgLogo,
					SpeakerConfID: sc.ID,
					ComingFrom:    sc.ComingFrom,
					FeaturedRank:  sc.FeaturedRank,
				}
				if sc.Company != "" {
					row.Company = sc.Company
				}
				if sc.OrgPhoto != "" {
					row.OrgLogo = sc.OrgPhoto
				}
				rowByID[sp.ID] = row
			}
			row.Talks = append(row.Talks, &SpeakerRowTalk{
				ProposalID: p.ID,
				Title:      p.Title,
				Status:     p.Status,
			})
			if p.Status == StatusAccepted || p.Status == StatusScheduled {
				row.FeaturedEligible = true
				if row.FeaturedTalkTitle == "" {
					row.FeaturedTalkTitle = p.Title
				}
			}
			if mostAdvancedProposalStatus(p.Status, row.MostAdvancedStatus) == p.Status {
				row.MostAdvancedStatus = p.Status
			}
			if p.Status == StatusScheduled {
				row.Scheduled = true
			}
			if row.CardURL == "" {
				ct, err := getters.GetConfTalkByProposal(ctx, p.ID)
				if err != nil {
					return nil, fmt.Errorf("conftalk %s: %w", p.ID, err)
				}
				if ct != nil {
					row.Scheduled = true
					row.CardURL = SpeakerCardURL(ctx, conf.Tag, "insta", sp.ID, ct.ID)
				}
			}
		}
	}
	rows := make([]*SpeakerRow, 0, len(rowByID))
	for _, r := range rowByID {
		// Mark speakers whose only attachments are soft statuses
		// so the page-level filter can collapse them.
		if len(r.Talks) > 0 {
			allSoft := true
			for _, t := range r.Talks {
				if t.Status != "Waitlisted" && t.Status != "Invited" {
					allSoft = false
					break
				}
			}
			r.OnlySoftStatuses = allSoft
		}
		rows = append(rows, r)
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return rows[i].Name < rows[j].Name
	})
	return rows, nil
}

func featuredSpeakerSlots(rows []*SpeakerRow) []*FeaturedSpeakerSlot {
	slots := make([]*FeaturedSpeakerSlot, 6)
	for i := range slots {
		slots[i] = &FeaturedSpeakerSlot{Slot: i + 1}
	}
	for _, row := range rows {
		if row == nil || row.FeaturedRank < 1 || row.FeaturedRank > 6 {
			continue
		}
		slots[row.FeaturedRank-1].SelectedSpeakerConfID = row.SpeakerConfID
	}
	return slots
}

func SpeakerAdmin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireConfStaff(w, r, ctx)
	if id == nil {
		return
	}

	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	rows, err := speakerAdminRows(ctx, conf)
	if err != nil {
		ctx.Err.Printf("/%s/speakers load rows: %s", conf.Tag, err)
		http.Error(w, "Unable to load speakers", http.StatusInternalServerError)
		return
	}

	err = ctx.TemplateCache.ExecuteTemplate(w, "talks/speakers.tmpl", &SpeakerAdminPage{
		Conf:          conf,
		Rows:          rows,
		FeaturedSlots: featuredSpeakerSlots(rows),
		FlashMessage:  r.URL.Query().Get("flash"),
		IsConfAdmin:   id.HasRoleForConf(conf.Tag, auth.RoleAdmin),
		Year:          helpers.CurrentYear(),
		EmailCompose: &EmailComposeData{
			Title:            "Email Selected Speakers",
			Description:      "Write a one-off email to speakers. Uses Go template syntax.",
			TitlePlaceholder: "Subject line",
			BodyPlaceholder:  "Hi {{ .Speaker.Name }},\n\nLooking forward to your talk at {{ .Conf.Desc }}...",
			Fields: []EmailFieldGroup{
				fieldGroup(".Speaker", types.Speaker{}, false),
				fieldGroup(".Conf", types.Conf{}, false),
				fieldGroup(".Talks", types.Talk{}, true),
			},
		},
	})
	if err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("/talks/speakers template failed: %s", err.Error())
	}
}

func SpeakerAdminFeatured(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=%s", conf.Tag, url.QueryEscape("Unable to read featured speaker form.")), http.StatusSeeOther)
		return
	}

	rows, err := speakerAdminRows(ctx, conf)
	if err != nil {
		ctx.Err.Printf("/%s/admin/speakers/featured load rows: %s", conf.Tag, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=%s", conf.Tag, url.QueryEscape("Unable to load speakers.")), http.StatusSeeOther)
		return
	}

	allSpeakerConfs := make(map[string]bool, len(rows))
	eligibleSpeakerConfs := make(map[string]bool, len(rows))
	for _, row := range rows {
		if row == nil || row.SpeakerConfID == "" {
			continue
		}
		allSpeakerConfs[row.SpeakerConfID] = true
		if row.FeaturedEligible {
			eligibleSpeakerConfs[row.SpeakerConfID] = true
		}
	}

	selectedBySlot := make(map[int]string, 6)
	seen := map[string]int{}
	for slot := 1; slot <= 6; slot++ {
		field := fmt.Sprintf("featured_slot_%d", slot)
		speakerConfID := strings.TrimSpace(r.PostForm.Get(field))
		if speakerConfID == "" {
			continue
		}
		if !eligibleSpeakerConfs[speakerConfID] {
			http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=%s", conf.Tag, url.QueryEscape("Featured speakers must have an accepted or scheduled proposal for this event.")), http.StatusSeeOther)
			return
		}
		if prevSlot, ok := seen[speakerConfID]; ok {
			http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=%s", conf.Tag, url.QueryEscape(fmt.Sprintf("The same speaker is selected for slots %d and %d.", prevSlot, slot))), http.StatusSeeOther)
			return
		}
		seen[speakerConfID] = slot
		selectedBySlot[slot] = speakerConfID
	}

	for speakerConfID := range allSpeakerConfs {
		if err := getters.UpdateSpeakerConfFeaturedRank(ctx, speakerConfID, 0); err != nil {
			ctx.Err.Printf("/%s/admin/speakers/featured clear %s: %s", conf.Tag, speakerConfID, err)
			http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=%s", conf.Tag, url.QueryEscape("Unable to clear existing featured speakers.")), http.StatusSeeOther)
			return
		}
	}
	for slot, speakerConfID := range selectedBySlot {
		if err := getters.UpdateSpeakerConfFeaturedRank(ctx, speakerConfID, slot); err != nil {
			ctx.Err.Printf("/%s/admin/speakers/featured set %s -> %d: %s", conf.Tag, speakerConfID, slot, err)
			http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=%s", conf.Tag, url.QueryEscape("Unable to save featured speaker slots.")), http.StatusSeeOther)
			return
		}
	}

	http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=%s", conf.Tag, url.QueryEscape("Featured speakers updated.")), http.StatusSeeOther)
}

func mostAdvancedProposalStatus(a, b string) string {
	if proposalStatusRank(a) >= proposalStatusRank(b) {
		return a
	}
	return b
}

func proposalStatusRank(status string) int {
	switch status {
	case StatusScheduled:
		return 70
	case StatusAccepted:
		return 60
	case "Invited":
		return 50
	case "Waitlisted", "Waitlist":
		return 40
	case "InReview":
		return 30
	case "Applied":
		return 20
	case "TheyDecline", "WeDecline", "Rejected", "Declined":
		return 10
	case "":
		return 0
	default:
		return 1
	}
}

func SpeakerAdminBulkEmail(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
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
	speakerRefs := r.Form["speaker_refs"]

	title := r.FormValue("title")
	body := r.FormValue("body")
	if title == "" || body == "" {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=Title+and+body+required", conf.Tag), http.StatusSeeOther)
		return
	}

	// For test mode, don't require speaker selection
	testEmail := r.FormValue("test_email")
	isTest := r.FormValue("send_test") == "1" && testEmail != ""

	if len(speakerRefs) == 0 && !isTest {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=No+speakers+selected", conf.Tag), http.StatusSeeOther)
		return
	}

	talks, err := getters.GetTalksFor(ctx, conf.Tag)
	if err != nil {
		http.Error(w, "Unable to load talks", http.StatusInternalServerError)
		return
	}

	// Build speaker ID -> speaker and speaker ID -> talks maps
	speakerMap := make(map[string]*types.Speaker)
	speakerTalks := make(map[string][]*types.Talk)
	for _, talk := range talks {
		for _, s := range talk.Speakers {
			speakerMap[s.ID] = s
			speakerTalks[s.ID] = append(speakerTalks[s.ID], talk)
		}
	}

	refSet := make(map[string]bool, len(speakerRefs))
	for _, ref := range speakerRefs {
		refSet[ref] = true
	}

	if isTest {
		// Use first selected speaker, or first available if none selected
		var testSpeaker *types.Speaker
		var testTalks []*types.Talk
		for id, speaker := range speakerMap {
			if len(refSet) == 0 || refSet[id] {
				testSpeaker = speaker
				testTalks = speakerTalks[id]
				break
			}
		}
		if testSpeaker == nil {
			http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=No+speakers+available+for+test", conf.Tag), http.StatusSeeOther)
			return
		}
		ts := *testSpeaker
		ts.Email = testEmail
		_, err := emails.SendCustomToSpeaker(ctx, &ts, conf, testTalks, title, body)
		if err != nil {
			ctx.Err.Printf("/%s/admin/speakers/email test -> %s failed: %s", conf.Tag, testEmail, err)
			http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=Test+email+failed", conf.Tag), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=Test+sent+to+%s", conf.Tag, testEmail), http.StatusSeeOther)
		return
	}

	sent := 0
	for id, speaker := range speakerMap {
		if !refSet[id] {
			continue
		}
		if speaker.Email == "" {
			continue
		}
		_, err := emails.SendCustomToSpeaker(ctx, speaker, conf, speakerTalks[id], title, body)
		if err != nil {
			ctx.Err.Printf("/%s/admin/speakers/email -> %s failed: %s", conf.Tag, speaker.Email, err)
			continue
		}
		sent++
	}

	flash := fmt.Sprintf("Sent+to+%d+of+%d+speakers", sent, len(speakerRefs))
	http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=%s", conf.Tag, flash), http.StatusSeeOther)
}

func AdminSpeakerRefreshCards(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	if !ctx.InProduction {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=%s", mux.Vars(r)["conf"], url.QueryEscape("Social card updates are disabled outside production.")), http.StatusSeeOther)
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	speakerID := strings.TrimSpace(mux.Vars(r)["speakerID"])
	if speakerID == "" {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=Missing+speaker", conf.Tag), http.StatusSeeOther)
		return
	}
	talks, err := talksForSpeakerMediaRefresh(ctx, conf, speakerID)
	if err != nil {
		ctx.Err.Printf("/%s/admin/speakers/%s/refresh-cards: %s", conf.Tag, speakerID, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=%s", conf.Tag, url.QueryEscape("Refresh failed: "+err.Error())), http.StatusSeeOther)
		return
	}
	if len(talks) == 0 {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=No+social+cards+for+speaker", conf.Tag), http.StatusSeeOther)
		return
	}
	go RefreshTalkCardsForceOpt(ctx.Detached(), talks, true)
	http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers?flash=%s", conf.Tag, url.QueryEscape(fmt.Sprintf("Queued card refresh for %d talk(s).", len(talks)))), http.StatusSeeOther)
}

func SpeakerAdminNew(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	backURL := fmt.Sprintf("/%s/admin/speakers", conf.Tag)
	formAction := fmt.Sprintf("/%s/admin/speakers/new", conf.Tag)
	if r.Method == http.MethodPost {
		adminCreateSpeakerPOST(w, r, ctx, conf, backURL)
		return
	}
	page := &EditSpeakerPage{
		Mode:       "create",
		IsAdmin:    true,
		BackURL:    backURL,
		FormAction: formAction,
		Year:       helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_edit_speaker.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/admin/speakers/new render: %s", conf.Tag, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func adminCreateSpeakerPOST(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, conf *types.Conf, backURL string) {
	limitRequestBody(w, r, maxMultipartBodyBytes)
	if err := r.ParseMultipartForm(maxUploadFileBytes); err != nil {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
	}
	name := strings.TrimSpace(r.FormValue("Name"))
	email := strings.TrimSpace(r.FormValue("Email"))
	if name == "" || email == "" {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers/new?flash=%s", conf.Tag, url.QueryEscape("Name and email are required.")), http.StatusSeeOther)
		return
	}
	existing, err := getters.GetSpeakersByEmail(ctx, email)
	if err != nil {
		ctx.Err.Printf("/%s/admin/speakers/new lookup %s: %s", conf.Tag, email, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers/new?flash=%s", conf.Tag, url.QueryEscape("Speaker lookup failed: "+err.Error())), http.StatusSeeOther)
		return
	}
	if len(existing) > 0 && existing[0] != nil {
		sp := existing[0]
		flash := "Speaker already exists for " + email + ". Edit the existing profile, then attach them to a proposal."
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers/%s/edit?flash=%s", conf.Tag, sp.ID, url.QueryEscape(flash)), http.StatusSeeOther)
		return
	}
	picRaw, picContentType, picExt, picErr := readMultipartFile(r, "PicFile")
	hasNewPic := picErr == nil && len(picRaw) > 0
	if picErr != nil && picErr != http.ErrMissingFile {
		ctx.Err.Printf("/%s/admin/speakers/new read pic: %s", conf.Tag, picErr)
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers/new?flash=%s", conf.Tag, url.QueryEscape("Photo upload failed.")), http.StatusSeeOther)
		return
	}
	in := getters.SpeakerInput{
		Name:      name,
		Email:     email,
		Phone:     strings.TrimSpace(r.FormValue("Phone")),
		Signal:    strings.TrimSpace(r.FormValue("Signal")),
		Telegram:  strings.TrimSpace(r.FormValue("Telegram")),
		Twitter:   strings.TrimSpace(r.FormValue("Twitter")),
		Nostr:     strings.TrimSpace(r.FormValue("Nostr")),
		Github:    strings.TrimSpace(r.FormValue("Github")),
		Instagram: strings.TrimSpace(r.FormValue("Instagram")),
		LinkedIn:  strings.TrimSpace(r.FormValue("LinkedIn")),
		LeetCode:  strings.TrimSpace(r.FormValue("LeetCode")),
		Website:   strings.TrimSpace(r.FormValue("Website")),
		Bio:       strings.TrimSpace(r.FormValue("Bio")),
		TShirt:    validShirtCode(strings.TrimSpace(r.FormValue("TShirt"))),
	}
	if hasNewPic {
		in.Photo = imgproc.ShortID(picRaw) + picExt
	}
	speakerID, err := getters.CreateSpeaker(ctx, in)
	if err != nil {
		ctx.Err.Printf("/%s/admin/speakers/new create %s: %s", conf.Tag, email, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/speakers/new?flash=%s", conf.Tag, url.QueryEscape("Create failed: "+err.Error())), http.StatusSeeOther)
		return
	}
	if hasNewPic {
		go newPhotoPipeline(ctx).mirrorPicToSpaces(picRaw, picContentType, picExt)
	}
	http.Redirect(w, r, backURL+"?flash="+url.QueryEscape("Speaker created. Attach them to a proposal from the proposal editor. ID: "+speakerID), http.StatusSeeOther)
}

func SpeakerAdminEdit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	speakerID := mux.Vars(r)["speakerID"]
	if !speakerIsOnConf(ctx, conf, speakerID) {
		http.Error(w, "speaker is not attached to this event", http.StatusForbidden)
		return
	}
	sp, err := getters.FetchSpeakerByID(ctx, speakerID)
	if err != nil {
		ctx.Err.Printf("/%s/admin/speakers/%s/edit load: %s", conf.Tag, speakerID, err)
		http.Error(w, "speaker lookup failed", http.StatusInternalServerError)
		return
	}
	if sp == nil {
		http.NotFound(w, r)
		return
	}

	backURL := fmt.Sprintf("/%s/admin/speakers", conf.Tag)
	formAction := fmt.Sprintf("/%s/admin/speakers/%s/edit", conf.Tag, speakerID)
	if r.Method == http.MethodPost {
		adminUpdateSpeakerPOST(w, r, ctx, conf, sp, backURL)
		return
	}
	page := &EditSpeakerPage{
		Speaker:      sp,
		Mode:         "edit",
		FlashMessage: r.URL.Query().Get("flash"),
		IsAdmin:      true,
		BackURL:      backURL,
		FormAction:   formAction,
		Year:         helpers.CurrentYear(),
	}
	if hasPublicWhoIsProfile(ctx, sp) {
		page.PublicURL = "/whois/" + publicSpeakerSlug(sp)
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_edit_speaker.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/admin/speakers/%s/edit render: %s", conf.Tag, speakerID, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func adminUpdateSpeakerPOST(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, conf *types.Conf, sp *types.Speaker, backURL string) {
	limitRequestBody(w, r, maxMultipartBodyBytes)
	if err := r.ParseMultipartForm(maxUploadFileBytes); err != nil {
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
	}
	picRaw, picContentType, picExt, picErr := readMultipartFile(r, "PicFile")
	hasNewPic := picErr == nil && len(picRaw) > 0
	if picErr != nil && picErr != http.ErrMissingFile {
		ctx.Err.Printf("/%s/admin/speakers/%s/edit read pic: %s", conf.Tag, sp.ID, picErr)
		http.Redirect(w, r, backURL+"?flash="+url.QueryEscape("Photo upload failed."), http.StatusSeeOther)
		return
	}
	name := strings.TrimSpace(r.FormValue("Name"))
	if name == "" {
		http.Redirect(w, r, backURL+"?flash="+url.QueryEscape("Name is required."), http.StatusSeeOther)
		return
	}
	email := strings.TrimSpace(r.FormValue("Email"))
	if email == "" {
		http.Redirect(w, r, backURL+"?flash="+url.QueryEscape("Email is required."), http.StatusSeeOther)
		return
	}
	if !strings.EqualFold(email, sp.Email) {
		existing, err := getters.GetSpeakersByEmail(ctx, email)
		if err != nil {
			ctx.Err.Printf("/%s/admin/speakers/%s/edit email lookup %s: %s", conf.Tag, sp.ID, email, err)
			http.Redirect(w, r, backURL+"?flash="+url.QueryEscape("Email lookup failed: "+err.Error()), http.StatusSeeOther)
			return
		}
		for _, other := range existing {
			if other != nil && other.ID != sp.ID {
				http.Redirect(w, r, backURL+"?flash="+url.QueryEscape("Email already belongs to another speaker."), http.StatusSeeOther)
				return
			}
		}
	}
	up := getters.SpeakerUpdate{
		Name:      name,
		Email:     email,
		Phone:     strings.TrimSpace(r.FormValue("Phone")),
		Signal:    strings.TrimSpace(r.FormValue("Signal")),
		Telegram:  strings.TrimSpace(r.FormValue("Telegram")),
		Twitter:   strings.TrimSpace(r.FormValue("Twitter")),
		Nostr:     strings.TrimSpace(r.FormValue("Nostr")),
		Github:    strings.TrimSpace(r.FormValue("Github")),
		Instagram: strings.TrimSpace(r.FormValue("Instagram")),
		LinkedIn:  strings.TrimSpace(r.FormValue("LinkedIn")),
		LeetCode:  strings.TrimSpace(r.FormValue("LeetCode")),
		Website:   strings.TrimSpace(r.FormValue("Website")),
		Bio:       strings.TrimSpace(r.FormValue("Bio")),
		BioSet:    true,
		TShirt:    validShirtCode(strings.TrimSpace(r.FormValue("TShirt"))),
	}
	if hasNewPic {
		up.Photo = imgproc.ShortID(picRaw) + picExt
	}
	if err := getters.UpdateSpeaker(ctx, sp.ID, up); err != nil {
		ctx.Err.Printf("/%s/admin/speakers/%s/edit update: %s", conf.Tag, sp.ID, err)
		http.Redirect(w, r, backURL+"?flash="+url.QueryEscape("Update failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	invalidateWhoIsDirectoryCache()
	if hasNewPic {
		go newPhotoPipeline(ctx).mirrorPicToSpaces(picRaw, picContentType, picExt)
	}
	http.Redirect(w, r, backURL+"?flash="+url.QueryEscape("Speaker info updated."), http.StatusSeeOther)
}

func SpeakerConfAdminEdit(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	speakerConfID := mux.Vars(r)["speakerConfID"]
	sc, err := getters.FetchSpeakerConfWithSpeaker(ctx, speakerConfID)
	if err != nil {
		ctx.Err.Printf("/%s/admin/speakerconfs/%s/edit load: %s", conf.Tag, speakerConfID, err)
		http.Error(w, "speaker conf lookup failed", http.StatusInternalServerError)
		return
	}
	if sc == nil {
		http.NotFound(w, r)
		return
	}
	if scConf := speakerConfConf(sc); scConf == nil || scConf.Tag != conf.Tag {
		http.Error(w, "speaker conf is not attached to this event", http.StatusForbidden)
		return
	}

	backURL := fmt.Sprintf("/%s/admin/speakers", conf.Tag)
	formAction := fmt.Sprintf("/%s/admin/speakerconfs/%s/edit", conf.Tag, speakerConfID)
	if r.Method == http.MethodPost {
		adminUpdateSpeakerConfPOST(w, r, ctx, conf, sc, backURL)
		return
	}

	var returning bool
	if sc.Speaker != nil && sc.Speaker.Email != "" {
		if reg, err := getters.EmailHasRegistration(ctx, sc.Speaker.Email); err == nil {
			returning = reg
		}
	}
	rsvpDayList := conf.DaysList("", true)
	rsvpFor := ""
	if len(rsvpDayList) > 0 {
		rsvpFor = rsvpDayList[0].ItemDesc
	}
	page := &EditSpeakerConfPage{
		SpeakerConf:         sc,
		Conf:                conf,
		Locked:              false,
		DaysList:            conf.DaysList("", false),
		RecordingOptions:    helpers.GetRecordingOptions(),
		IsReturningAttendee: returning,
		RSVPFor:             rsvpFor,
		IsAdmin:             true,
		BackURL:             backURL,
		FormAction:          formAction,
		Year:                helpers.CurrentYear(),
	}
	if err := ctx.TemplateCache.ExecuteTemplate(w, "dashboard_edit_speakerconf.tmpl", page); err != nil {
		ctx.Err.Printf("/%s/admin/speakerconfs/%s/edit render: %s", conf.Tag, speakerConfID, err)
		http.Error(w, "render failed", http.StatusInternalServerError)
	}
}

func adminUpdateSpeakerConfPOST(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, conf *types.Conf, sc *types.SpeakerConf, backURL string) {
	limitRequestBody(w, r, maxMultipartBodyBytes)
	if err := r.ParseMultipartForm(maxUploadFileBytes); err != nil {
		ctx.Err.Printf("/%s/admin/speakerconfs/%s/edit parseform: %s", conf.Tag, sc.ID, err)
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	fields := getters.SpeakerConfFields{
		Company:      strings.TrimSpace(r.PostForm.Get("Company")),
		OrgID:        strings.TrimSpace(r.PostForm.Get("OrgID")),
		ComingFrom:   strings.TrimSpace(r.PostForm.Get("ComingFrom")),
		Availability: r.PostForm["Availability"],
		RecordOK:     strings.TrimSpace(r.PostForm.Get("RecordOK")),
		Visa:         strings.TrimSpace(r.PostForm.Get("Visa")),
		FirstEvent:   r.PostForm.Get("FirstEvent") == "on",
		DinnerRSVP:   r.PostForm.Get("DinnerRSVP") == "on",
		Sponsor:      r.PostForm.Get("Sponsor") == "on",
	}
	featuredRankRaw := strings.TrimSpace(r.PostForm.Get("FeaturedRank"))
	if featuredRankRaw == "" {
		clearRank := 0
		fields.FeaturedRank = &clearRank
	} else {
		featuredRank, err := strconv.Atoi(featuredRankRaw)
		if err != nil || featuredRank < 1 || featuredRank > 6 {
			http.Redirect(w, r, backURL+"?flash="+url.QueryEscape("Featured speaker slot must be blank or a number from 1 to 6."), http.StatusSeeOther)
			return
		}
		fields.FeaturedRank = &featuredRank
	}
	logoRaw, logoContentType, logoExt, logoErr := readMultipartLogoFile(r, "OrgLogoFile")
	hasLogo := logoErr == nil && len(logoRaw) > 0
	if logoErr != nil && logoErr != http.ErrMissingFile {
		ctx.Err.Printf("/%s/admin/speakerconfs/%s/edit read logo: %s", conf.Tag, sc.ID, logoErr)
		http.Redirect(w, r, backURL+"?flash="+url.QueryEscape("Logo upload failed."), http.StatusSeeOther)
		return
	}
	if hasLogo {
		fields.OrgPhoto = imgproc.ShortID(logoRaw) + logoExt
	}
	if err := getters.UpdateSpeakerConf(ctx, sc.ID, fields); err != nil {
		ctx.Err.Printf("/%s/admin/speakerconfs/%s/edit update: %s", conf.Tag, sc.ID, err)
		http.Redirect(w, r, backURL+"?flash="+url.QueryEscape("Update failed: "+err.Error()), http.StatusSeeOther)
		return
	}
	if hasLogo {
		go newPhotoPipeline(ctx).mirrorOrgLogoToSpaces(logoRaw, logoContentType, logoExt)
	}
	http.Redirect(w, r, backURL+"?flash="+url.QueryEscape("Speaker conf updated."), http.StatusSeeOther)
}

func speakerIsOnConf(ctx *config.AppContext, conf *types.Conf, speakerID string) bool {
	for _, p := range loadConfProposals(ctx, conf) {
		for _, sc := range resolveProposalSpeakers(p, ctx) {
			if sc != nil && sc.Speaker != nil && sc.Speaker.ID == speakerID {
				return true
			}
		}
	}
	return false
}

func RegistrationsAdmin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireConfStaff(w, r, ctx)
	if id == nil {
		return
	}

	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	regs, err := getters.FetchRegistrations(ctx, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load registrations", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/admin/registrations failed: %s", conf.Tag, err.Error())
		return
	}

	// Deduplicate by email
	seen := make(map[string]bool)
	var unique []*RegistrationAdminRow
	loc := conf.Loc()
	for _, r := range regs {
		if r.Email != "" && !seen[r.Email] {
			seen[r.Email] = true
			row := &RegistrationAdminRow{Registration: r}
			if r.CheckedInAt != nil && !r.CheckedInAt.IsZero() {
				row.CheckedInLabel = r.CheckedInAt.In(loc).Format("Jan 2, 3:04 PM")
			}
			unique = append(unique, row)
		}
	}

	regTypeOrder := map[string]int{
		"speaker":   0,
		"sponsor":   1,
		"volunteer": 2,
		"local":     3,
		"genpop":    4,
	}
	sort.SliceStable(unique, func(i, j int) bool {
		oi, oj := regTypeOrder[unique[i].Type], regTypeOrder[unique[j].Type]
		if oi != oj {
			return oi < oj
		}
		return unique[i].Email < unique[j].Email
	})

	merchPickups, err := getters.ListShopPickupsForConference(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/admin/registrations merch pickups: %s", conf.Tag, err)
	}

	err = ctx.TemplateCache.ExecuteTemplate(w, "admin/registrations.tmpl", &RegistrationsAdminPage{
		Conf:          conf,
		Registrations: unique,
		MerchPickups:  merchPickups,
		FlashMessage:  r.URL.Query().Get("flash"),
		IsConfAdmin:   id.HasRoleForConf(conf.Tag, auth.RoleAdmin),
		Year:          helpers.CurrentYear(),
		EmailCompose: &EmailComposeData{
			Title:            "Email Attendees",
			Description:      "Write a one-off email to registered attendees. Uses Go template syntax.",
			TitlePlaceholder: "Subject line",
			BodyPlaceholder:  "Hi there!\n\nExciting news about {{ .Conf.Desc }}...",
			Fields: []EmailFieldGroup{
				fieldGroup(".Conf", types.Conf{}, false),
			},
		},
	})
	if err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/admin/registrations template failed: %s", conf.Tag, err.Error())
	}
}

func RegistrationsAdminMerchPickup(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	id := requireConfStaff(w, r, ctx)
	if id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	itemID := strings.TrimSpace(mux.Vars(r)["itemID"])
	if err := getters.MarkShopOrderItemPickedUp(ctx, itemID, id.Email, "registration desk pickup"); err != nil {
		ctx.Err.Printf("/%s/admin/registrations/merch/%s/pickup: %s", conf.Tag, itemID, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/registrations?flash=%s", conf.Tag, url.QueryEscape("Could not mark merch pickup.")), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/%s/admin/registrations?flash=%s", conf.Tag, url.QueryEscape("Merch pickup marked complete.")), http.StatusSeeOther)
}

func RegistrationsAdminBulkEmail(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
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
	selectedEmails := r.Form["reg_emails"]

	title := r.FormValue("title")
	body := r.FormValue("body")
	if title == "" || body == "" {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/registrations?flash=Title+and+body+required", conf.Tag), http.StatusSeeOther)
		return
	}

	testEmail := r.FormValue("test_email")
	isTest := r.FormValue("send_test") == "1" && testEmail != ""

	if len(selectedEmails) == 0 && !isTest {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/registrations?flash=No+attendees+selected", conf.Tag), http.StatusSeeOther)
		return
	}

	// Load registrations
	regs, err := getters.FetchRegistrations(ctx, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load registrations", http.StatusInternalServerError)
		return
	}

	if isTest {
		// Use first available registration as sample data
		var testReg *types.Registration
		if len(selectedEmails) > 0 {
			for _, reg := range regs {
				if reg.Email == selectedEmails[0] {
					testReg = reg
					break
				}
			}
		}
		if testReg == nil && len(regs) > 0 {
			testReg = regs[0]
		}
		if testReg == nil {
			http.Redirect(w, r, fmt.Sprintf("/%s/admin/registrations?flash=No+registrations+available", conf.Tag), http.StatusSeeOther)
			return
		}
		tr := *testReg
		tr.Email = testEmail
		_, err := emails.SendCustomToAttendee(ctx, &tr, conf, title, body)
		if err != nil {
			ctx.Err.Printf("/%s/admin/registrations/email test failed: %s", conf.Tag, err)
			http.Redirect(w, r, fmt.Sprintf("/%s/admin/registrations?flash=Test+email+failed", conf.Tag), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/registrations?flash=Test+sent+to+%s", conf.Tag, testEmail), http.StatusSeeOther)
		return
	}

	emailSet := make(map[string]bool, len(selectedEmails))
	for _, e := range selectedEmails {
		emailSet[e] = true
	}

	// Deduplicate registrations by email for sending
	sent := 0
	sentEmails := make(map[string]bool)
	for _, reg := range regs {
		if !emailSet[reg.Email] || sentEmails[reg.Email] {
			continue
		}
		sentEmails[reg.Email] = true
		_, err := emails.SendCustomToAttendee(ctx, reg, conf, title, body)
		if err != nil {
			ctx.Err.Printf("/%s/admin/registrations/email -> %s failed: %s", conf.Tag, reg.Email, err)
			continue
		}
		sent++
	}

	flash := fmt.Sprintf("Sent+to+%d+of+%d+attendees", sent, len(selectedEmails))
	http.Redirect(w, r, fmt.Sprintf("/%s/admin/registrations?flash=%s", conf.Tag, flash), http.StatusSeeOther)
}

func RegistrationsAdminBulkCheckIn(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
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
	selectedEmails := r.Form["reg_emails"]
	if len(selectedEmails) == 0 {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/registrations?flash=No+attendees+selected", conf.Tag), http.StatusSeeOther)
		return
	}

	count, err := getters.BulkCheckInRegistrations(ctx, conf.Ref, selectedEmails)
	if err != nil {
		ctx.Err.Printf("/%s/admin/registrations/check-in failed: %s", conf.Tag, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/registrations?flash=Check-in+failed", conf.Tag), http.StatusSeeOther)
		return
	}
	flash := url.QueryEscape(fmt.Sprintf("Marked %d attendee(s) checked in", count))
	http.Redirect(w, r, fmt.Sprintf("/%s/admin/registrations?flash=%s", conf.Tag, flash), http.StatusSeeOther)
}

func ProposalAdmin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}

	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	rows, err := loadProposalRowsForConf(ctx, conf)
	if err != nil {
		http.Error(w, "Unable to load applicants", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/admin/applicants failed: %s", conf.Tag, err.Error())
		return
	}

	sort.SliceStable(rows, func(i, j int) bool {
		return speakerName(rows[i].Speaker) < speakerName(rows[j].Speaker)
	})

	err = ctx.TemplateCache.ExecuteTemplate(w, "talks/applicants.tmpl", &ProposalAdminPage{
		Conf:         conf,
		Rows:         rows,
		FlashMessage: r.URL.Query().Get("flash"),
		Year:         helpers.CurrentYear(),
		EmailCompose: &EmailComposeData{
			Title:            "Email Selected Applicants",
			Description:      "Write a one-off email to speaker applicants. Uses Go template syntax.",
			TitlePlaceholder: "Subject line",
			BodyPlaceholder:  "Hi {{ .Speaker.Name }},\n\nThank you for applying to speak at {{ .Conf.Desc }}...",
			Fields: []EmailFieldGroup{
				fieldGroup(".Speaker", types.Speaker{}, false),
				fieldGroup(".Proposal", types.Proposal{}, false),
				fieldGroup(".Conf", types.Conf{}, false),
			},
		},
	})
	if err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/admin/applicants template failed: %s", conf.Tag, err.Error())
	}
}

// loadProposalRowsForConf returns one ProposalAdminRow per Proposal
// whose ScheduleFor matches conf, joined to the Speakers that filed
// it via the SpeakerProposal link table and to the ConfTalk (when
// scheduled). Rows whose proposal has no SpeakerProposal still
// appear with Speakers == nil.
//
// Each row carries pre-computed display labels (StartLabel,
// VenueLabel, …) and a CalState flag derived from the stored
// CalNotif vs the freshly-computed content hash. CalState drives
// the per-card "Send / Resend / Update cal invite" button.
func loadProposalRowsForConf(ctx *config.AppContext, conf *types.Conf) ([]*ProposalAdminRow, error) {
	proposals, err := getters.ListProposalsForConf(ctx, conf.Ref)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}

	proposalMap := make(map[string]*types.Proposal, len(proposals))
	for _, p := range proposals {
		if p != nil {
			proposalMap[p.ID] = p
		}
	}
	if len(proposalMap) == 0 {
		return nil, nil
	}

	speakers, err := getters.ListSpeakers(ctx)
	if err != nil {
		return nil, fmt.Errorf("list speakers: %w", err)
	}
	speakerMap := make(map[string]*types.Speaker, len(speakers))
	for _, sp := range speakers {
		speakerMap[sp.ID] = sp
	}

	sps, err := getters.ListSpeakerConfs(ctx, speakerMap, proposalMap)
	if err != nil {
		return nil, fmt.Errorf("list speaker confs: %w", err)
	}

	// Each SpeakerConf carries one or more Proposals (multi-relation `talk`).
	// Build proposalID → []*Speaker so each card can list all collaborators.
	speakersByProposal := make(map[string][]*types.Speaker, len(proposalMap))
	for _, sp := range sps {
		if sp.Speaker == nil {
			continue
		}
		for _, p := range sp.Proposals {
			if p == nil {
				continue
			}
			speakersByProposal[p.ID] = append(speakersByProposal[p.ID], sp.Speaker)
		}
	}

	loc := conf.Loc()
	confTalks, err := getters.ListConfTalksForConf(ctx, conf.Ref, proposalMap)
	if err != nil {
		return nil, fmt.Errorf("list conference talks: %w", err)
	}
	confTalkByProposal := make(map[string]*types.ConfTalk, len(confTalks))
	for _, ct := range confTalks {
		if ct == nil || ct.Conf == nil || ct.Conf.Ref != conf.Ref || ct.Proposal == nil {
			continue
		}
		confTalkByProposal[ct.Proposal.ID] = ct
	}
	rows := make([]*ProposalAdminRow, 0, len(proposalMap))
	for _, p := range proposalMap {
		spList := speakersByProposal[p.ID]
		// Stable order so the "first speaker" picked for the
		// email-compose .Speaker field is deterministic.
		sort.SliceStable(spList, func(i, j int) bool {
			return strings.ToLower(spList[i].Name) < strings.ToLower(spList[j].Name)
		})
		row := &ProposalAdminRow{
			Proposal:           p,
			Speakers:           spList,
			DurationDesiredMin: p.DesiredDuration,
		}
		if len(spList) > 0 {
			row.Speaker = spList[0]
		}

		// Pull the ConfTalk if this proposal has been scheduled.
		// Nil means the proposal isn't in the schedule yet.
		ct := confTalkByProposal[p.ID]
		if ct != nil {
			row.ConfTalk = ct
			row.TalkCardURL = TalkCardURL(ctx, conf.Tag, "1080p", ct.ID)
			if ct.Sched != nil {
				row.StartLabel = ct.Sched.Start.In(loc).Format("Mon Jan 2 · 3:04 PM")
				if ct.Sched.End != nil {
					row.EndLabel = ct.Sched.End.In(loc).Format("3:04 PM")
					row.DurationActualMin = int(ct.Sched.End.Sub(ct.Sched.Start).Minutes())
				}
			}
			row.VenueLabel = ics.MapVenue(ct.Venue)
			row.CalState = computeCalState(ct, p, conf)
		}
		rows = append(rows, row)
	}
	return rows, nil
}

// computeCalState classifies a ConfTalk's CalNotif against its
// current content for the per-card cal-invite button. Returns one of
// "none" (no CalNotif yet), "fresh" (CalNotif matches current data —
// idempotent re-send), or "stale" (data changed since last send,
// re-send will bump SEQUENCE).
func computeCalState(ct *types.ConfTalk, p *types.Proposal, conf *types.Conf) string {
	if ct == nil || ct.Sched == nil || ct.Sched.End == nil {
		return ""
	}
	prev, ok := ics.ParseCalNotif(ct.CalNotif)
	if !ok || prev.HashHex == "" {
		return "none"
	}
	cur := ics.ContentHash(ct.Sched.Start, *ct.Sched.End, conf.Tag, p.Title)
	if cur == prev.HashHex {
		return "fresh"
	}
	return "stale"
}

func AdminProposalRefreshCard(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	if !ctx.InProduction {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/applicants?flash=%s", mux.Vars(r)["conf"], url.QueryEscape("Social card updates are disabled outside production.")), http.StatusSeeOther)
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	proposalID := strings.TrimSpace(mux.Vars(r)["proposalID"])
	talk, err := talkForProposalMediaRefresh(ctx, conf, proposalID)
	if err != nil {
		ctx.Err.Printf("/%s/admin/applicants/%s/refresh-card: %s", conf.Tag, proposalID, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/applicants?flash=%s", conf.Tag, url.QueryEscape("Refresh failed: "+err.Error())), http.StatusSeeOther)
		return
	}
	go RefreshTalkCardsForceOpt(ctx.Detached(), []*types.Talk{talk}, true)
	http.Redirect(w, r, fmt.Sprintf("/%s/admin/applicants?flash=%s", conf.Tag, url.QueryEscape("Queued card refresh for "+talk.Name)), http.StatusSeeOther)
}

func talkForProposalMediaRefresh(ctx *config.AppContext, conf *types.Conf, proposalID string) (*types.Talk, error) {
	if proposalID == "" {
		return nil, fmt.Errorf("missing proposal")
	}
	proposals, err := getters.ListProposals(ctx)
	if err != nil {
		return nil, fmt.Errorf("list proposals: %w", err)
	}
	proposalMap := make(map[string]*types.Proposal, len(proposals))
	for _, p := range proposals {
		if p != nil {
			proposalMap[p.ID] = p
		}
	}
	proposal := proposalMap[proposalID]
	if proposal == nil {
		return nil, fmt.Errorf("proposal not found")
	}
	if proposal.ScheduleFor == nil || proposal.ScheduleFor.Ref != conf.Ref {
		return nil, fmt.Errorf("proposal is not attached to %s", conf.Tag)
	}
	confTalks, err := getters.ListConfTalks(ctx, proposalMap)
	if err != nil {
		return nil, fmt.Errorf("list conf talks: %w", err)
	}
	var target *types.ConfTalk
	for _, ct := range confTalks {
		if ct != nil && ct.Proposal != nil && ct.Proposal.ID == proposalID {
			target = ct
			break
		}
	}
	if target == nil {
		return nil, fmt.Errorf("proposal is not scheduled yet")
	}
	talks, err := getters.LoadTalksFromConfTalks(ctx, conf.Tag)
	if err != nil {
		return nil, fmt.Errorf("load talks: %w", err)
	}
	for _, talk := range talks {
		if talk != nil && talk.ID == target.ID {
			return talk, nil
		}
	}
	return nil, fmt.Errorf("scheduled talk card source not found")
}

func talksForSpeakerMediaRefresh(ctx *config.AppContext, conf *types.Conf, speakerID string) ([]*types.Talk, error) {
	var out []*types.Talk
	for _, p := range loadConfProposals(ctx, conf) {
		if p == nil {
			continue
		}
		var matched bool
		for _, sc := range resolveProposalSpeakers(p, ctx) {
			if sc != nil && sc.Speaker != nil && sc.Speaker.ID == speakerID {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		ct, err := getters.GetConfTalkByProposal(ctx, p.ID)
		if err != nil {
			return nil, fmt.Errorf("lookup conftalk for proposal %s: %w", p.ID, err)
		}
		if ct == nil {
			continue
		}
		out = append(out, talkForAdminMediaRefresh(ctx, conf, p, ct))
	}
	return out, nil
}

func talkForAdminMediaRefresh(ctx *config.AppContext, conf *types.Conf, proposal *types.Proposal, ct *types.ConfTalk) *types.Talk {
	talk := &types.Talk{
		ID:          ct.ID,
		Name:        proposal.Title,
		Description: proposal.Description,
		Type:        proposal.TalkType,
		Status:      proposal.Status,
		Event:       conf.Tag,
		Clipart:     ct.Clipart,
		Sched:       ct.Sched,
		Venue:       ct.Venue,
		Section:     ct.Section,
		CalNotif:    ct.CalNotif,
		TalkCardURL: ct.SocialCard,
	}
	if talk.Sched != nil {
		talk.TimeDesc = talk.Sched.Desc()
	}
	for _, sc := range resolveProposalSpeakers(proposal, ctx) {
		if sc == nil || sc.Speaker == nil {
			continue
		}
		view := *sc.Speaker
		if sc.Company != "" {
			view.Company = sc.Company
		}
		if sc.OrgPhoto != "" {
			view.OrgLogo = sc.OrgPhoto
		}
		talk.Speakers = append(talk.Speakers, &view)
	}
	return talk
}

func speakerName(sp *types.Speaker) string {
	if sp == nil {
		return ""
	}
	return sp.Name
}

// AdminProposalSendCalAll fires cal invites for every scheduled
// proposal on the page where the data hasn't drifted since the
// last send (CalState in {none, fresh}; "stale" is skipped). The
// "none" rows produce first-send emails (seq=0). "fresh" rows hit
// the hash-unchanged short-circuit inside dispatch and don't email
// anyone. "stale" is deliberately excluded so an admin reviews
// pending changes individually via the per-card "Update cal
// invite" button.
//
// Path: POST /{conf}/admin/applicants/sendcal-all
func AdminProposalSendCalAll(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	rows, err := loadProposalRowsForConf(ctx, conf)
	if err != nil {
		ctx.Err.Printf("/%s/admin/applicants/sendcal-all load rows: %s", conf.Tag, err)
		http.Redirect(w, r,
			fmt.Sprintf("/%s/admin/applicants?flash=%s",
				conf.Tag, url.QueryEscape("Bulk sendcal failed: "+err.Error())),
			http.StatusSeeOther)
		return
	}

	var attempted, sent, skippedStale, failed int
	for _, row := range rows {
		if row == nil || row.Proposal == nil {
			continue
		}
		// Skip unscheduled proposals — no ConfTalk → no
		// CalState → no cal-invite to send.
		if row.CalState == "" {
			continue
		}
		// Skip stale: data has drifted since the last send,
		// the admin should review and click "Update cal
		// invite" individually rather than push a silent
		// SEQUENCE bump out via the bulk button.
		if row.CalState == "stale" {
			skippedStale++
			continue
		}
		attempted++
		// force=false: "fresh" rows hit the hash-unchanged
		// no-op inside dispatch and don't email; "none" rows
		// fire seq=0 first-sends.
		if err := DispatchTalkICSForProposal(ctx, row.Proposal, conf, row.Speakers, false); err != nil {
			ctx.Err.Printf("sendcal-all %q: %s", row.Proposal.Title, err)
			failed++
			continue
		}
		// "fresh" returns nil from dispatch but didn't actually
		// email; only count "none" as a real send. We can tell
		// them apart by the entering CalState.
		if row.CalState == "none" {
			sent++
			// First-send locks the schedule in: flip
			// Accepted → Scheduled.
			if row.Proposal.Status == StatusAccepted {
				if err := getters.UpdateProposalStatus(ctx, row.Proposal.ID, StatusScheduled); err != nil {
					ctx.Err.Printf("sendcal-all %q status flip: %s", row.Proposal.Title, err)
				}
			}
		}
	}

	flash := fmt.Sprintf("Bulk cal invites: %d sent · %d already current · %d pending updates skipped",
		sent, attempted-sent-failed, skippedStale)
	if failed > 0 {
		flash = fmt.Sprintf("%s · %d failed (see logs)", flash, failed)
	}
	http.Redirect(w, r,
		fmt.Sprintf("/%s/admin/applicants?flash=%s",
			conf.Tag, url.QueryEscape(flash)),
		http.StatusSeeOther)
}

// AdminProposalSendCal handles the per-card "Send / Resend /
// Update cal invite" button on /{conf}/admin/applicants. Looks up
// the proposal's ConfTalk + speakers and fires
// DispatchTalkICSForProposal. force=true is implied — clicking the
// button means the admin wants a send regardless of whether the
// content hash changed (the button label tells the admin which
// state they're in; the backend always honors the click).
//
// Path: POST /{conf}/admin/proposals/{proposalID}/sendcal
func AdminProposalSendCal(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	proposalID := mux.Vars(r)["proposalID"]
	if proposalID == "" {
		http.Redirect(w, r,
			fmt.Sprintf("/%s/admin/applicants?flash=Missing+proposal", conf.Tag),
			http.StatusSeeOther)
		return
	}

	proposal, err := getters.GetProposal(ctx, proposalID)
	if err != nil || proposal == nil {
		http.Redirect(w, r,
			fmt.Sprintf("/%s/admin/applicants?flash=Proposal+not+found", conf.Tag),
			http.StatusSeeOther)
		return
	}

	speakers, err := proposalSpeakers(ctx, proposal)
	if err != nil {
		ctx.Err.Printf("/%s/admin/proposals/%s/sendcal speakers: %s", conf.Tag, proposalID, err)
		http.Redirect(w, r,
			fmt.Sprintf("/%s/admin/applicants?flash=%s",
				conf.Tag, url.QueryEscape("Cal invite failed: "+err.Error())),
			http.StatusSeeOther)
		return
	}
	if err := DispatchTalkICSForProposal(ctx, proposal, conf, speakers, true); err != nil {
		ctx.Err.Printf("/%s/admin/proposals/%s/sendcal: %s", conf.Tag, proposalID, err)
		http.Redirect(w, r,
			fmt.Sprintf("/%s/admin/applicants?flash=%s",
				conf.Tag, url.QueryEscape("Cal invite failed: "+err.Error())),
			http.StatusSeeOther)
		return
	}

	// Sending the cal invite is what locks the talk in: flip
	// Accepted (draft) → Scheduled. Re-sends and updates leave
	// the status alone (already Scheduled). Best-effort — the
	// dispatch already succeeded, so log the status-write
	// failure but still tell the admin the invite went out.
	if proposal.Status == StatusAccepted {
		if err := getters.UpdateProposalStatus(ctx, proposalID, StatusScheduled); err != nil {
			ctx.Err.Printf("/%s/admin/proposals/%s/sendcal status flip: %s", conf.Tag, proposalID, err)
		}
	}

	http.Redirect(w, r,
		fmt.Sprintf("/%s/admin/applicants?flash=%s",
			conf.Tag,
			url.QueryEscape(fmt.Sprintf("Cal invite sent for %q to %d speaker(s).", proposal.Title, len(speakers)))),
		http.StatusSeeOther)
}

// proposalSpeakers walks proposal.SpeakerConfRefs and returns the *Speaker for
// each. Used by per-proposal cal-invite dispatch where the page-level speakers
// map isn't already in scope. Distinct from resolveProposalSpeakers
// (admin_review.go), which returns SpeakerConf wrappers.
func proposalSpeakers(ctx *config.AppContext, proposal *types.Proposal) ([]*types.Speaker, error) {
	if proposal == nil {
		return nil, nil
	}
	out := make([]*types.Speaker, 0, len(proposal.SpeakerConfRefs))
	for _, ref := range proposal.SpeakerConfRefs {
		sc, err := getters.GetSpeakerConfByID(ctx, ref)
		if err != nil {
			return nil, fmt.Errorf("speaker conf %s: %w", ref, err)
		}
		if sc == nil || sc.Speaker == nil {
			continue
		}
		out = append(out, sc.Speaker)
	}
	return out, nil
}

func ProposalAdminBulkEmail(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
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
	proposalRefs := r.Form["proposal_refs"]
	if len(proposalRefs) == 0 {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/applicants?flash=No+applicants+selected", conf.Tag), http.StatusSeeOther)
		return
	}

	title := r.FormValue("title")
	body := r.FormValue("body")
	if title == "" || body == "" {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/applicants?flash=Title+and+body+required", conf.Tag), http.StatusSeeOther)
		return
	}

	rows, err := loadProposalRowsForConf(ctx, conf)
	if err != nil {
		http.Error(w, "Unable to load applicants", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/admin/applicants/email failed: %s", conf.Tag, err.Error())
		return
	}

	rowByID := make(map[string]*ProposalAdminRow, len(rows))
	for _, row := range rows {
		rowByID[row.Proposal.ID] = row
	}

	refSet := make(map[string]bool, len(proposalRefs))
	for _, ref := range proposalRefs {
		refSet[ref] = true
	}

	testEmail := r.FormValue("test_email")
	isTest := r.FormValue("send_test") == "1" && testEmail != ""

	if isTest {
		for _, ref := range proposalRefs {
			row := rowByID[ref]
			if row == nil || row.Speaker == nil {
				continue
			}
			testSpeaker := *row.Speaker
			testSpeaker.Email = testEmail
			_, err := emails.SendCustomToProposalSpeaker(ctx, row.Proposal, &testSpeaker, conf, title, body)
			if err != nil {
				http.Redirect(w, r, fmt.Sprintf("/%s/admin/applicants?flash=Test+email+failed:+%s", conf.Tag, err.Error()), http.StatusSeeOther)
				return
			}
			http.Redirect(w, r, fmt.Sprintf("/%s/admin/applicants?flash=Test+sent+to+%s", conf.Tag, testEmail), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/applicants?flash=No+applicant+selected+for+test", conf.Tag), http.StatusSeeOther)
		return
	}

	sent := 0
	for _, ref := range proposalRefs {
		row := rowByID[ref]
		if row == nil || row.Speaker == nil || row.Speaker.Email == "" {
			continue
		}
		_, err := emails.SendCustomToProposalSpeaker(ctx, row.Proposal, row.Speaker, conf, title, body)
		if err != nil {
			ctx.Err.Printf("/%s/admin/applicants/email -> %s failed: %s", conf.Tag, row.Speaker.Email, err)
			continue
		}
		sent++
	}

	flash := fmt.Sprintf("Sent+to+%d+of+%d+applicants", sent, len(proposalRefs))
	http.Redirect(w, r, fmt.Sprintf("/%s/admin/applicants?flash=%s", conf.Tag, flash), http.StatusSeeOther)
}

// ProposalAdminAccept flips a Proposal to Accepted and creates a ConfTalk row.
// Always redirects back to the applicants page with a flash describing the
// outcome.
func ProposalAdminAccept(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
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
	proposalID := r.FormValue("proposal_ref")
	if proposalID == "" {
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/applicants?flash=No+proposal+selected", conf.Tag), http.StatusSeeOther)
		return
	}

	result, err := newAcceptPipeline(ctx).AcceptProposal(proposalID)
	if err != nil {
		ctx.Err.Printf("/%s/admin/applicants/accept (%s) failed: %s", conf.Tag, proposalID, err)
		flash := url.QueryEscape(fmt.Sprintf("Accept failed: %s", err.Error()))
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/applicants?flash=%s", conf.Tag, flash), http.StatusSeeOther)
		return
	}

	var msg string
	if result.AlreadyAccepted {
		msg = "Already accepted"
	} else {
		msg = "Accepted: created conf talk"
	}
	http.Redirect(w, r, fmt.Sprintf("/%s/admin/applicants?flash=%s", conf.Tag, url.QueryEscape(msg)), http.StatusSeeOther)
}

// SendVolCals fans the self-hosted ICS pipeline across every
// SendVolOrientation broadcasts the volunteer-orientation invite
// to every Scheduled volunteer for a conf. Used by the
// "Resend orientation invite" button on /{conf}/volcoord when the
// orientation time has changed and the admin wants the new time
// to land in every volunteer's calendar in one click.
//
// Unlike the per-vol DispatchOrientICS (fires from scheduledFlow,
// hash-gated), this is an explicit force-send: SEQUENCE bumps
// once and the same seq lands on every recipient. Conf.OrientCalNotif
// stamps the new state.
//
// Path: POST /{conf}/volcoord/send-orientation
func SendVolOrientation(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	volinfo, err := getters.GetVolInfo(ctx, conf.Ref)
	if err != nil || volinfo == nil || volinfo.OrientTimes == nil || volinfo.OrientTimes.End == nil {
		http.Redirect(w, r,
			fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag,
				url.QueryEscape("No orientation time set — add it on the conf's VolInfo row first.")),
			http.StatusSeeOther)
		return
	}

	vols, err := getters.ListVolunteersForConf(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/send-orientation list vols: %s", conf.Tag, err)
		http.Error(w, "Unable to load volunteers", http.StatusInternalServerError)
		return
	}

	recipients := make([]ics.Attendee, 0, len(vols))
	for _, v := range vols {
		if v == nil || v.Email == "" {
			continue
		}
		// Only Scheduled vols got an orientation invite the
		// first time around (DispatchOrientICS fires inside
		// scheduledFlow, post-status flip). Mirror that here
		// so the broadcast doesn't blast unscheduled
		// applicants who never received the original.
		if v.Status != "Scheduled" {
			continue
		}
		recipients = append(recipients, ics.Attendee{Email: v.Email, Name: v.Name})
	}

	recipients = append(recipients, orientationStaffRecipients(ctx, conf.Tag)...)
	recipients = dedupeAttendees(recipients)

	if len(recipients) == 0 {
		http.Redirect(w, r,
			fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag,
				url.QueryEscape("No Scheduled volunteers or staff/admin recipients to notify.")),
			http.StatusSeeOther)
		return
	}

	sent, err := BroadcastOrientICS(ctx, conf, volinfo.OrientTimes.Start, *volinfo.OrientTimes.End, volinfo.OrientLink, recipients)
	if err != nil && sent == 0 {
		ctx.Err.Printf("/%s/volcoord/send-orientation: %s", conf.Tag, err)
		http.Redirect(w, r,
			fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag,
				url.QueryEscape("Orientation broadcast failed: "+err.Error())),
			http.StatusSeeOther)
		return
	}

	http.Redirect(w, r,
		fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag,
			url.QueryEscape(fmt.Sprintf("Orientation invite re-sent to %d recipient(s).", sent))),
		http.StatusSeeOther)
}

func VolAdminScheduleOrientation(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if err := r.ParseForm(); err != nil {
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("Bad orientation form.")), http.StatusSeeOther)
		return
	}
	start, err := parseOrientationTime(r.FormValue("start"), conf)
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("Orientation start time is required.")), http.StatusSeeOther)
		return
	}
	end, err := parseOrientationTime(r.FormValue("end"), conf)
	if err != nil || !end.After(start) {
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("Orientation end time must be after the start time.")), http.StatusSeeOther)
		return
	}
	orientLink := strings.TrimSpace(r.FormValue("orient_link"))
	if orientLink == "" {
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("Orientation link is required.")), http.StatusSeeOther)
		return
	}
	volinfo, err := getters.GetVolInfo(ctx, conf.Ref)
	if err != nil || volinfo == nil {
		ctx.Err.Printf("/%s/volcoord/orientation volinfo: %s", conf.Tag, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("No VolInfo row found for this conference.")), http.StatusSeeOther)
		return
	}
	if err := getters.UpdateVolInfoOrientation(ctx, volinfo.Ref, start, end, orientLink); err != nil {
		ctx.Err.Printf("/%s/volcoord/orientation update: %s", conf.Tag, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("Orientation update failed: "+err.Error())), http.StatusSeeOther)
		return
	}
	vols, err := getters.ListVolunteersForConf(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/orientation list vols: %s", conf.Tag, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("Orientation saved, but volunteers could not be loaded.")), http.StatusSeeOther)
		return
	}
	recipients := dedupeAttendees(append(scheduledVolunteerAttendees(vols), orientationStaffRecipients(ctx, conf.Tag)...))
	if len(recipients) == 0 {
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("Orientation saved. No Scheduled volunteers or staff/admin recipients to notify.")), http.StatusSeeOther)
		return
	}
	sent, err := BroadcastOrientICS(ctx, conf, start, end, orientLink, recipients)
	if err != nil && sent == 0 {
		ctx.Err.Printf("/%s/volcoord/orientation broadcast: %s", conf.Tag, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("Orientation saved, but invite send failed: "+err.Error())), http.StatusSeeOther)
		return
	}
	http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape(fmt.Sprintf("Orientation scheduled and invite sent to %d recipient(s).", sent))), http.StatusSeeOther)
}

func orientationInputValues(volinfo *types.VolInfo, conf *types.Conf) (string, string) {
	if volinfo == nil || volinfo.OrientTimes == nil {
		return "", ""
	}
	loc := time.Local
	if conf != nil {
		loc = conf.Loc()
	}
	start := volinfo.OrientTimes.Start.In(loc).Format("2006-01-02T15:04")
	end := ""
	if volinfo.OrientTimes.End != nil {
		end = volinfo.OrientTimes.End.In(loc).Format("2006-01-02T15:04")
	}
	return start, end
}

func parseOrientationTime(raw string, conf *types.Conf) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("empty orientation time")
	}
	loc := time.Local
	if conf != nil {
		loc = conf.Loc()
	}
	return time.ParseInLocation("2006-01-02T15:04", raw, loc)
}

func scheduledVolunteerAttendees(vols []*types.Volunteer) []ics.Attendee {
	out := make([]ics.Attendee, 0, len(vols))
	for _, v := range vols {
		if v == nil || v.Email == "" || v.Status != "Scheduled" {
			continue
		}
		out = append(out, ics.Attendee{Email: v.Email, Name: v.Name})
	}
	return out
}

func orientationStaffRecipients(ctx *config.AppContext, confTag string) []ics.Attendee {
	speakers, err := getters.ListSpeakers(ctx)
	if err != nil || len(speakers) == 0 {
		return nil
	}
	out := make([]ics.Attendee, 0)
	for _, sp := range speakers {
		if sp == nil || sp.Email == "" || !speakerGetsOrientationStaffInvite(sp, confTag) {
			continue
		}
		out = append(out, ics.Attendee{Email: sp.Email, Name: sp.Name})
	}
	return out
}

func speakerGetsOrientationStaffInvite(sp *types.Speaker, confTag string) bool {
	for _, role := range auth.ParseRoles(sp.Roles) {
		if role.Scope != auth.GlobalScope && role.Scope != confTag {
			continue
		}
		switch role.Name {
		case auth.RoleAdmin, auth.RoleVolcoord, auth.RoleStaff:
			return true
		}
	}
	return false
}

func dedupeAttendees(in []ics.Attendee) []ics.Attendee {
	seen := map[string]bool{}
	out := make([]ics.Attendee, 0, len(in))
	for _, a := range in {
		email := strings.ToLower(strings.TrimSpace(a.Email))
		if email == "" || seen[email] {
			continue
		}
		seen[email] = true
		a.Email = strings.TrimSpace(a.Email)
		out = append(out, a)
	}
	return out
}

// scheduled volunteer shift for a conf. Mirrors SendCals on the
// volunteer side; per-shift CalNotif now stores the "UID:Sequence:
// Hashbytes" triple. Idempotent re-clicks skip emails when nothing
// changed.
func SendVolCals(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	shifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/sendcal failed to get shifts: %s", conf.Tag, err.Error())
		http.Error(w, "Unable to load shifts", http.StatusInternalServerError)
		return
	}

	vols, err := getters.ListVolunteersForConf(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/sendcal failed to get volunteers: %s", conf.Tag, err.Error())
		http.Error(w, "Unable to load volunteers", http.StatusInternalServerError)
		return
	}

	// Build a map of volunteer ref -> attendee (email + name) so
	// the rendered ICS carries CN= alongside mailto:.
	volByRef := make(map[string]ics.Attendee)
	for _, vol := range vols {
		if vol == nil || vol.Email == "" {
			continue
		}
		volByRef[vol.Ref] = ics.Attendee{Email: vol.Email, Name: vol.Name}
	}

	for _, shift := range shifts {
		if len(shift.AssigneesRef) == 0 {
			continue
		}
		if shift.ShiftTime == nil || shift.ShiftTime.End == nil {
			ctx.Err.Printf("Skipping shift %s: no end time", shift.Name)
			continue
		}

		recipients := make([]ics.Attendee, 0, len(shift.AssigneesRef))
		for _, ref := range shift.AssigneesRef {
			if a, ok := volByRef[ref]; ok {
				recipients = append(recipients, a)
			}
		}
		if len(recipients) == 0 {
			continue
		}

		if err := DispatchShiftICS(ctx, shift, conf, recipients, kindRequest, false); err != nil {
			ctx.Err.Printf("vol sendcal %q: %s", shift.Name, err)
		}
	}
}
