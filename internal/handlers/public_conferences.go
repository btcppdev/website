package handlers

import (
	"bytes"
	"errors"
	"fmt"
	"html/template"
	"io"
	"io/ioutil"
	"mime"
	"net/http"
	"net/url"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/imgproc"
	"btcpp-web/internal/missives"
	"btcpp-web/internal/types"
)

func RenderTalks(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	allTalks, err := getters.GetTalksFor(ctx, conf.Tag)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("Unable to fetch talks: %s", err.Error())
		return
	}

	// Page renders every approved talk — Accepted (admin draft) and
	// Scheduled (cal invite sent). Declined/Rejected variants drop
	// off so retracted talks don't linger on the public list.
	var talks types.TalkTime
	for _, t := range allTalks {
		if t == nil {
			continue
		}
		if t.Status == StatusAccepted || t.Status == StatusScheduled {
			talks = append(talks, t)
		}
	}

	var evSpeakers types.Speakers
	evSpeakers = acceptedSpeakersForConf(ctx, conf, talks)

	sort.Sort(talks)
	sort.Sort(evSpeakers)

	confCopy := *conf
	confCopy.HasAgenda = anyScheduledTalk(&confCopy, allTalks)
	conf = &confCopy

	err = ctx.TemplateCache.ExecuteTemplate(w, "sched.tmpl", &ConfPage{
		Talks:         talks,
		EventSpeakers: evSpeakers,
		Conf:          conf,
		Year:          helpers.CurrentYear(),
	})
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/talks ExecuteTemplate failed ! %s", conf.Tag, err.Error())
		return
	}
}

func RenderConfSuccess(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	// Clear the stashed discount + silent-affiliate codes now that
	// the visitor has completed checkout — otherwise a subsequent
	// ticket purchase from the same browser session would silently
	// re-apply the code, even if the original link's owner only
	// intended one use. Per-conf, so other confs' stashed codes
	// stay put.
	ctx.Session.Remove(r.Context(), discountSessionKey(conf.Tag))
	ctx.Session.Remove(r.Context(), affiliateSessionKey(conf.Tag))
	var ticket *types.Registration
	sponsored := false
	attachQR := func(reg *types.Registration) *types.Registration {
		if reg == nil || reg.Revoked || reg.ConfRef != conf.Ref {
			return nil
		}
		if types.IsSponsoredTicketType(reg.Type) {
			sponsored = true
			return nil
		}
		if reg.QRCodeURI == "" {
			qr, err := ticketQRCodeURI(ctx, reg.RefID)
			if err != nil {
				ctx.Err.Printf("/%s/success ticket qr %s: %s", conf.Tag, reg.RefID, err)
			} else {
				reg.QRCodeURI = qr
			}
		}
		return reg
	}

	checkoutID := strings.TrimSpace(r.URL.Query().Get("session_id"))
	if checkoutID != "" {
		regs, err := getters.ListRegistrationsByCheckoutID(ctx, checkoutID)
		if err != nil {
			ctx.Err.Printf("/%s/success checkout lookup %s: %s", conf.Tag, checkoutID, err)
		}
		for _, reg := range regs {
			if ticket = attachQR(reg); ticket != nil {
				break
			}
		}
	}

	email := ctx.Session.GetString(r.Context(), checkoutEmailSessionKey(conf.Tag))
	if ticket == nil && email != "" {
		regs, err := getters.ListRegistrationsByEmail(ctx, email)
		if err != nil {
			ctx.Err.Printf("/%s/success ticket lookup %s: %s", conf.Tag, email, err)
		}
		for _, reg := range regs {
			if ticket = attachQR(reg); ticket != nil {
				break
			}
		}
	}
	if ticket != nil {
		ctx.Session.Remove(r.Context(), checkoutEmailSessionKey(conf.Tag))
	}

	err = ctx.TemplateCache.ExecuteTemplate(w, "success.tmpl", &SuccessPage{
		Conf:      conf,
		Ticket:    ticket,
		Sponsored: sponsored,
		Year:      helpers.CurrentYear(),
	})
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/success ExecuteTemplate failed ! %s", conf.Tag, err.Error())
		return
	}
}

func RenderSpeakers(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	confs := listConfs(w, ctx)
	err := ctx.TemplateCache.ExecuteTemplate(w, "embeds/speaker_select.tmpl", &VolunteerPage{
		Confs: confs,
		Year:  helpers.CurrentYear(),
	})

	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/speakers ExecuteTemplate failed ! %s", err.Error())
		return
	}
}

func contentTypeFromFilename(filename string) string {
	ext := filepath.Ext(filename) // e.g., ".png"
	mimeType := mime.TypeByExtension(ext)
	if mimeType == "" {
		return "application/octet-stream" // fallback
	}
	return mimeType
}

func processFileUpload(ctx *config.AppContext, r *http.Request, field string) (string, error) {
	file, handler, err := r.FormFile(field)
	if err != nil {
		return "", err
	}
	defer file.Close()

	// Read the file data
	fileData, err := ioutil.ReadAll(file)
	if err != nil {
		return "", err
	}

	filename := handler.Filename
	contentType := contentTypeFromFilename(filename)

	return getters.UploadFile(ctx, contentType, filename, fileData)
}

// readMultipartFile reads a single named file from a multipart form and
// returns its bytes + content type + lowercase file extension. It does not
// upload anywhere — caller decides what to do with the bytes. Returns
// http.ErrMissingFile when the field is absent (typical for optional
// uploads).
func readMultipartFile(r *http.Request, field string) (raw []byte, contentType string, ext string, err error) {
	return readMultipartImageFile(r, field, false)
}

func readMultipartPresentationFile(r *http.Request, field string) (raw []byte, contentType string, ext string, err error) {
	file, handler, err := r.FormFile(field)
	if err != nil {
		return nil, "", "", err
	}
	defer file.Close()
	raw, err = ioutil.ReadAll(io.LimitReader(file, maxPresentationBytes+1))
	if err != nil {
		return nil, "", "", err
	}
	if int64(len(raw)) > maxPresentationBytes {
		return nil, "", "", errUploadTooLarge
	}
	if len(raw) == 0 {
		return nil, "", "", errors.New("empty upload")
	}
	ext = strings.ToLower(filepath.Ext(handler.Filename))
	switch ext {
	case ".pdf":
		contentType = "application/pdf"
	case ".ppt":
		contentType = "application/vnd.ms-powerpoint"
	case ".pptx":
		contentType = "application/vnd.openxmlformats-officedocument.presentationml.presentation"
	case ".key":
		contentType = "application/vnd.apple.keynote"
	case ".odp":
		contentType = "application/vnd.oasis.opendocument.presentation"
	default:
		return nil, "", "", errors.New("unsupported presentation type")
	}
	return raw, contentType, ext, nil
}

func readMultipartLogoFile(r *http.Request, field string) (raw []byte, contentType string, ext string, err error) {
	return readMultipartImageFile(r, field, true)
}

func readMultipartImageFile(r *http.Request, field string, allowSVG bool) (raw []byte, contentType string, ext string, err error) {
	file, handler, err := r.FormFile(field)
	if err != nil {
		return nil, "", "", err
	}
	defer file.Close()
	raw, err = ioutil.ReadAll(io.LimitReader(file, maxUploadFileBytes+1))
	if err != nil {
		return nil, "", "", err
	}
	if int64(len(raw)) > maxUploadFileBytes {
		return nil, "", "", errUploadTooLarge
	}
	if len(raw) == 0 {
		return nil, "", "", errors.New("empty upload")
	}
	filename := handler.Filename
	contentType = detectedImageContentType(raw, filename, allowSVG)
	if contentType == "" {
		return nil, "", "", errors.New("unsupported image type")
	}
	ext = strings.ToLower(filepath.Ext(filename))
	if ext == "" || contentTypeFromFilename(filename) != contentType {
		ext = extForImageContentType(contentType)
	}
	return raw, contentType, ext, nil
}

func detectedImageContentType(raw []byte, filename string, allowSVG bool) string {
	detected := http.DetectContentType(raw)
	if allowedUploadImageType(detected) {
		return detected
	}
	if strings.EqualFold(filepath.Ext(filename), ".avif") && isAVIF(raw) {
		return "image/avif"
	}
	if allowSVG && strings.EqualFold(filepath.Ext(filename), ".svg") && isSVG(raw) {
		return "image/svg+xml"
	}
	return ""
}

func isAVIF(raw []byte) bool {
	if len(raw) < 12 || string(raw[4:8]) != "ftyp" {
		return false
	}
	for i := 8; i+4 <= len(raw); i += 4 {
		brand := string(raw[i : i+4])
		if brand == "avif" || brand == "avis" {
			return true
		}
	}
	return false
}

func allowedUploadImageType(contentType string) bool {
	switch strings.ToLower(strings.TrimSpace(contentType)) {
	case "image/png", "image/jpeg", "image/gif", "image/webp", "image/avif", "image/svg+xml":
		return true
	default:
		return false
	}
}

func instagramURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}

	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return raw
	}

	raw = strings.TrimPrefix(raw, "@")
	raw = strings.TrimSpace(raw)
	raw = strings.TrimPrefix(raw, "/")
	if raw == "" {
		return ""
	}

	for _, prefix := range []string{"www.instagram.com/", "instagram.com/"} {
		if strings.HasPrefix(strings.ToLower(raw), prefix) {
			raw = raw[len(prefix):]
			break
		}
	}
	raw = strings.TrimPrefix(raw, "@")
	raw = strings.TrimPrefix(raw, "/")
	if raw == "" {
		return ""
	}

	return "https://www.instagram.com/" + raw
}

func profileURL(raw string, hostPath string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return raw
	}
	raw = strings.TrimPrefix(raw, "@")
	raw = strings.Trim(raw, " /")
	if raw == "" {
		return ""
	}
	hostPath = strings.Trim(hostPath, "/")
	prefixes := []string{hostPath + "/", "www." + hostPath + "/"}
	if slash := strings.Index(hostPath, "/"); slash > 0 {
		host := hostPath[:slash]
		prefixes = append(prefixes, host+"/", "www."+host+"/")
	}
	for _, prefix := range prefixes {
		if strings.HasPrefix(strings.ToLower(raw), prefix) {
			raw = strings.Trim(raw[len(prefix):], " /")
			break
		}
	}
	if raw == "" {
		return ""
	}
	return "https://" + hostPath + "/" + raw
}

func websiteURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return raw
	}
	return "https://" + strings.TrimLeft(raw, "/")
}

func isSVG(raw []byte) bool {
	s := bytes.TrimSpace(raw)
	s = bytes.TrimPrefix(s, []byte{0xef, 0xbb, 0xbf})
	lower := bytes.ToLower(s)
	if bytes.Contains(lower, []byte("<script")) ||
		bytes.Contains(lower, []byte("javascript:")) ||
		bytes.Contains(lower, []byte(" onload=")) {
		return false
	}
	return bytes.HasPrefix(lower, []byte("<svg")) ||
		bytes.HasPrefix(lower, []byte("<?xml")) && bytes.Contains(lower[:min(len(lower), 512)], []byte("<svg"))
}

func extForImageContentType(contentType string) string {
	switch strings.ToLower(contentType) {
	case "image/png":
		return ".png"
	case "image/gif":
		return ".gif"
	case "image/webp":
		return ".webp"
	case "image/avif":
		return ".avif"
	case "image/svg+xml":
		return ".svg"
	default:
		return ".jpg"
	}
}

// uploadSpeakerPic uploads PicFile (returning the stored file ID) and also
// returns the raw bytes + content type + extension so the caller can mirror
// the original to Spaces and generate AVIF derivatives.
func uploadSpeakerPic(ctx *config.AppContext, r *http.Request) (fileID string, raw []byte, contentType string, ext string, err error) {
	file, handler, err := r.FormFile("PicFile")
	if err != nil {
		return "", nil, "", "", err
	}
	defer file.Close()

	raw, err = ioutil.ReadAll(file)
	if err != nil {
		return "", nil, "", "", err
	}

	filename := handler.Filename
	contentType = contentTypeFromFilename(filename)
	ext = strings.ToLower(filepath.Ext(filename))
	if ext == "" {
		ext = ".jpg"
	}

	fileID, err = getters.UploadFile(ctx, contentType, filename, raw)
	return fileID, raw, contentType, ext, err
}

func RenderSpeakerConf(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	if !conf.Active {
		handle404(w, r, ctx)
		return
	}

	confs := listConfs(w, ctx)

	switch r.Method {
	case http.MethodGet:

		// Optional magic-link auth — when present and valid, pre-fill the
		// form with the speaker's existing data so they don't re-type
		// contact info / pfp / shirt size / etc.
		var knownSpeaker *types.Speaker
		var encodedHMAC, encodedEmail string
		var subscribed, returning bool
		if email, h, err := validateVolEmail(r, ctx); err == nil {
			encodedHMAC = h
			encodedEmail = r.URL.Query().Get("em")
			speakers, lerr := getters.GetSpeakersByEmail(ctx, email)
			if lerr == nil && len(speakers) == 1 {
				knownSpeaker = speakers[0]
			}
			// Best-effort lookups: failures just leave the
			// checkbox visible. The form still works.
			if s, err := getters.IsSubscribedTo(ctx, email, "newsletter"); err == nil {
				subscribed = s
			}
			if reg, err := getters.EmailHasRegistration(ctx, email); err == nil {
				returning = reg
			}
		}

		daylist := conf.DaysList("days-", true)
		err = ctx.TemplateCache.ExecuteTemplate(w, "embeds/talk.tmpl", &SpeakerPage{
			Conf:                   conf,
			Confs:                  confs,
			ConfItems:              helpers.GetOtherConfs(confs, *conf),
			DueDate:                conf.DateBeforeStart(conf.TalksDueDays()),
			DaysList:               daylist[1:],
			RSVPFor:                daylist[0].ItemDesc,
			PresentationType:       helpers.GetPresentationTypes(),
			RecordingOptions:       helpers.GetRecordingOptions(),
			KnownSpeaker:           knownSpeaker,
			HMAC:                   encodedHMAC,
			Email:                  encodedEmail,
			IsNewsletterSubscriber: subscribed,
			IsReturningAttendee:    returning,
			Year:                   helpers.CurrentYear(),
		})

		if err != nil {
			http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
			ctx.Err.Printf("/volunteer/%s ExecuteTemplate failed ! %s", conf.Tag, err.Error())
			return
		}
		return
	case http.MethodPost:
		limitRequestBody(w, r, maxMultipartBodyBytes)
		err = r.ParseMultipartForm(maxUploadFileBytes)
		if err != nil {
			ctx.Err.Printf("/talk/{conf} unable to parse multipart form %s", err)
			w.Write([]byte(helpers.ErrSpeakerApp("Error parsing form.")))
			return
		}

		dec := newFormDecoder()
		var talkapp types.TalkApp
		err = dec.Decode(&talkapp, r.PostForm)
		if err != nil {
			ctx.Err.Printf("/speaker/{conf} unable to decode form %s", err)
			w.Write([]byte(helpers.ErrSpeakerApp("Unable to register you: form parsing error")))
			return
		}
		trimTalkApp(&talkapp)

		/* ten divided by two is five */
		if talkapp.Captcha != 5 {
			w.Write([]byte(helpers.ErrSpeakerApp("Incorrect captcha. The answer is 5.")))
			return
		}

		talkapp.ParseAvailability("days-", r.PostForm)
		dinneropt := r.PostForm.Get("DinnerOpt")
		talkapp.DinnerRSVP = dinneropt == "Yes"
		talkapp.OtherEvents = helpers.ParseFormConfs("conf-", r.PostForm, confs)

		/* Read PicFile bytes (no Notion upload — cropped JPEG goes
		   only to Spaces). Optional for returning speakers — when
		   the form was rendered with KnownSpeaker, the upload field
		   is hidden and Submit will keep the existing Photo. */
		picRaw, picContentType, picExt, err := readMultipartFile(r, "PicFile")
		hasNewPic := err == nil && len(picRaw) > 0
		if err != nil && err != http.ErrMissingFile {
			ctx.Err.Printf("/talk/{conf} unable to read speaker profile pic %s", err)
			w.Write([]byte(helpers.ErrSpeakerApp("Error uploading pfp.")))
			return
		}
		if hasNewPic {
			picShortID := imgproc.ShortID(picRaw)
			talkapp.NormPhoto = picShortID + picExt
		}

		/* Read OrgLogoFile if present (optional). */
		logoRaw, logoContentType, logoExt, logoErr := readMultipartLogoFile(r, "OrgLogoFile")
		hasLogo := logoErr == nil && len(logoRaw) > 0
		if logoErr != nil && logoErr != http.ErrMissingFile {
			ctx.Err.Printf("/talk/{conf} unable to read org logo %s", logoErr)
			w.Write([]byte(helpers.ErrSpeakerApp("Error uploading org logo.")))
			return
		}
		if hasLogo {
			logoShortID := imgproc.ShortID(logoRaw)
			talkapp.OrgLogo = logoShortID + logoExt
		}

		if talkapp.ScheduleFor == nil {
			talkapp.ScheduleFor = conf
		}

		ctx.Infos.Printf("parsed talkapp: %v", talkapp)

		submitResult, err := newSubmitPipeline(ctx).Submit(&talkapp)
		if err != nil {
			ctx.Err.Printf("/talk/{conf} submit pipeline failed %s", err)
			if errors.Is(err, ErrDuplicateSpeakerEmail) {
				w.Write([]byte(helpers.ErrSpeakerApp("That email already has multiple speaker records — please contact us to resolve.")))
			} else {
				w.Write([]byte(helpers.ErrSpeakerApp("Unable to register you.")))
			}
			return
		}

		/* Mirror photo to Spaces — fire-and-forget so we don't block
		   the user behind ffmpeg encodes. Skip when no new pic. */
		if hasNewPic {
			go newPhotoPipeline(ctx).mirrorPicToSpaces(picRaw, picContentType, picExt)
		}
		if hasLogo {
			go newPhotoPipeline(ctx).mirrorOrgLogoToSpaces(logoRaw, logoContentType, logoExt)
		}

		/* Subscribe the applicant to the talkapp + per-conf
		   talkapp lists (and the general newsletter when they
		   opted in). We bypass NewSubs here so the
		   subscription is recorded without firing the legacy
		   list-welcome missives — the OnlyFor "talkapp"
		   letter below is what they actually get. */
		newslist := missives.MakeApplicationSublist(conf.Tag, "talkapp", talkapp.Subscribe)
		if _, err := getters.SubscribeEmailList(ctx, talkapp.Email, newslist); err != nil {
			ctx.Err.Printf("!!! Unable to subscribe to newsletter %s: %v", err, talkapp)
		}

		/* Send the application-received ack via the OnlyFor
		   "talkapp" letter. */
		sendTalkAppLetter(ctx, conf, submitResult, talkapp.Email)

		/* When the form was submitted from a magic-link-authed
		   context (the dashboard's "Propose another talk" link
		   sets ?hr= & ?em= on the form action), bounce the user
		   back to the dashboard rather than dropping them on a
		   standalone success page. HTMX consumes HX-Redirect to
		   navigate the whole page. */
		if encHMAC := r.URL.Query().Get("hr"); encHMAC != "" {
			encEmail := r.URL.Query().Get("em")
			flash := url.QueryEscape("Thanks — your talk proposal is in.")
			w.Header().Set("HX-Redirect",
				fmt.Sprintf("/dashboard?hr=%s&em=%s&flash=%s", encHMAC, encEmail, flash))
			w.WriteHeader(http.StatusOK)
			return
		}

		w.Write([]byte(helpers.SuccessApp("Your speaker application has been submitted! We'll be in touch.")))
		return
	}

}

func RenderVolunteers(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	confs := listVolunteerConfs(w, ctx)
	err := ctx.TemplateCache.ExecuteTemplate(w, "embeds/volunteer_select.tmpl", &VolunteerPage{
		Confs: confs,
		Year:  helpers.CurrentYear(),
	})

	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/volunteers ExecuteTemplate failed ! %s", err.Error())
		return
	}
}

func RenderVolunteerConf(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	if !conf.Active {
		handle404(w, r, ctx)
		return
	}
	if !conf.VolunteerOpen() {
		handle404(w, r, ctx)
		return
	}

	jobs := listJobs(w, ctx)
	confs := listVolunteerConfs(w, ctx)

	switch r.Method {
	case http.MethodGet:
		// Pre-fill from the user's Speakers row when the
		// form is opened from a /dashboard "Sign up to
		// volunteer" link (hr+em query params verified).
		// Silent fallback when params are absent / wrong /
		// the user has no Speakers row — public visitors
		// get the blank form.
		//
		// Hometown rides along from the SpeakerConf for
		// THIS conf when one exists (the speaker has
		// already volunteered Hometown there); falls back
		// to blank otherwise.
		var prefill *types.Speaker
		var prefillHome string
		if email, _, vErr := validateVolEmail(r, ctx); vErr == nil {
			sps, scs, sErr := getters.GetSpeakerConfsByEmail(ctx, email)
			if sErr == nil && len(sps) > 0 {
				prefill = sps[0]
			}
			if sErr == nil {
				for _, sc := range scs {
					if sc == nil || sc.ComingFrom == "" {
						continue
					}
					for _, p := range sc.Proposals {
						if p != nil && p.ScheduleFor != nil && p.ScheduleFor.Ref == conf.Ref {
							prefillHome = sc.ComingFrom
							break
						}
					}
					if prefillHome != "" {
						break
					}
				}
			}
		}
		err = ctx.TemplateCache.ExecuteTemplate(w, "embeds/volunteer.tmpl", &VolunteerPage{
			Conf:            conf,
			Confs:           confs,
			YesJobs:         helpers.BuildJobs("yjob-", jobs, true),
			NoJobs:          helpers.BuildJobs("njob-", jobs, false),
			ConfItems:       helpers.GetOtherConfs(confs, *conf),
			DaysList:        conf.DaysList("days-", true),
			Prefill:         prefill,
			PrefillHometown: prefillHome,
			Year:            helpers.CurrentYear(),
		})

		if err != nil {
			http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
			ctx.Err.Printf("/volunteer/%s ExecuteTemplate failed ! %s", conf.Tag, err.Error())
			return
		}
		return
	case http.MethodPost:
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		dec := newFormDecoder()
		var vol types.Volunteer
		err = dec.Decode(&vol, r.PostForm)
		if err != nil {
			ctx.Err.Printf("/volunteer/{conf} unable to decode form %s", err)
			w.Write([]byte(helpers.ErrVolApp("Unable to register you.")))
			return
		}
		trimVolunteer(&vol)
		vol.Shirt = validShirtCode(vol.Shirt)
		if vol.Shirt == "" {
			w.Write([]byte(helpers.ErrVolApp("Please select a valid shirt size.")))
			return
		}

		/* ten divided by two is five */
		if vol.Captcha != 5 {
			w.Write([]byte(helpers.ErrVolApp("Incorrect captcha. The answer is 5.")))
			return
		}

		vol.ParseAvailability("days-", r.PostForm)
		vol.OtherEvents = helpers.ParseFormConfs("conf-", r.PostForm, confs)
		vol.WorkYes = helpers.ParseFormJobs("yjob-", r.PostForm, jobs)
		vol.WorkNo = helpers.ParseFormJobs("njob-", r.PostForm, jobs)

		if len(vol.ScheduleFor) == 0 {
			vol.ScheduleFor = append(vol.ScheduleFor, conf)
		}

		err = getters.RegisterVolunteer(ctx, &vol)
		if err != nil {
			ctx.Err.Printf("/volunteer/{conf} unable to register volunteer %s", err)
			w.Write([]byte(helpers.ErrVolApp("Unable to register you.")))
			return
		}

		/* Send application acknowledgment email */
		volinfo, err := getters.GetVolInfo(ctx, conf.Ref)
		if err != nil {
			ctx.Err.Printf("/volunteer/{conf} unable to fetch volinfos %s", err)
			w.Write([]byte(helpers.ErrVolApp("Unable to register you.")))
			return
		}

		_, err = emails.OnlyForVolApp(ctx, &vol, conf, volinfo)
		if err != nil {
			ctx.Err.Printf("/volunteer/{conf} unable to send ack email: %s", err)
		}

		/* Register to mailing lists :) */
		/* Note: this also sends pre-saved missives for the vol app list! */
		newslist := missives.MakeApplicationSublist(conf.Tag, "volapp", vol.Subscribe)
		err = missives.NewSubs(ctx, vol.Email, newslist)

		if err != nil {
			ctx.Err.Printf("!!! Unable to subscribe to newsletter %s: %v", err, vol)
		}

		w.Write([]byte(helpers.SuccessApp("Your volunteer application has been submitted! We'll be in touch.")))
		return
	}

}

func RenderConf(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if !conf.IsPublished() {
		handle404(w, r, ctx)
		return
	}

	// Stash a ?code= query in the session so the checkout page
	// can apply it without the visitor copy-pasting. The slot
	// depends on whether the code is buyer-facing (any non-zero
	// percent / fixed-amount discount) or a silent affiliate
	// referral (a `%0` code, which doesn't reduce the buyer's
	// price but still credits the affiliate at checkout):
	//
	//   - Buyer-facing → disc:{tag}     — pre-fills the visible
	//     Discount input + drives the price preview.
	//   - Silent affiliate → aff:{tag}  — invisible in the UI;
	//     a hidden form field carries it through checkout so
	//     the affiliate gets credited.
	//
	// Multi-conf stashing applies to both slots: a code valid for
	// vienna+nairobi auto-applies on either's checkout.
	//
	// Lookup is best-effort. An unknown / expired code falls back
	// to disc:{tag} for the landing conf (the checkout error path
	// surfaces the rejection there).
	if code := strings.TrimSpace(r.URL.Query().Get("code")); code != "" {
		disc, _ := getters.FindDiscount(ctx, code)
		stashKey := discountSessionKey
		if disc != nil && disc.DiscType == '%' && disc.Amount == 0 {
			stashKey = affiliateSessionKey
		}
		ctx.Session.Put(r.Context(), stashKey(conf.Tag), code)
		if disc != nil {
			allConfs, _ := getters.ListConfs(ctx)
			if len(disc.ConfRef) > 0 {
				// Code is pinned to specific confs — stash
				// for each one in the list.
				tagByRef := make(map[string]string, len(allConfs))
				for _, c := range allConfs {
					if c != nil {
						tagByRef[c.Ref] = c.Tag
					}
				}
				for _, ref := range disc.ConfRef {
					tag := tagByRef[ref]
					if tag == "" || tag == conf.Tag {
						continue
					}
					ctx.Session.Put(r.Context(), stashKey(tag), code)
				}
			} else {
				// Universal code (no ConfRef) — stash for
				// every active event so a visitor browsing
				// multiple confs in one session still gets
				// auto-apply on each one's checkout.
				for _, c := range allConfs {
					if c == nil || !c.Active || c.Tag == conf.Tag {
						continue
					}
					ctx.Session.Put(r.Context(), stashKey(c.Tag), code)
				}
			}
		}
	}

	talks, err := getters.GetTalksFor(ctx, conf.Tag)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("Unable to fetch talks: %s", err.Error())
		return
	}

	var evSpeakers types.Speakers
	evSpeakers = acceptedSpeakersForConf(ctx, conf, talks)
	sort.Sort(evSpeakers)
	featuredSpeakers, communitySpeakers := splitFeaturedSpeakersForConf(ctx, conf, evSpeakers)

	soldCount, err := getters.SoldTix(ctx, conf)
	if err != nil {
		ctx.Err.Printf("Unable to fetch sold ticket count for '%s': %s", conf.Tag, err.Error())
	}

	buckets, err := bucketTalks(ctx, conf, talks)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("Unable to bucket '%s' talks: %s", conf.Tag, err.Error())
		return
	}

	days, err := talkDaysFromBuckets(buckets)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("Unable to make days '%s' from talks: %s", conf.Tag, err.Error())
		return
	}

	// Per-day schedule strip data (doors / lunch / coffee). Best-effort
	// — a fetch failure leaves AgendaDays without time strips, which the
	// template handles by collapsing to chrono-only.
	var infosByDay map[int]*types.ConfInfo
	var confInfos []*types.ConfInfo
	if cis, err := getters.ListConfInfos(ctx, conf.Tag); err != nil {
		ctx.Err.Printf("/%s ListConfInfos failed (continuing): %s", conf.Tag, err)
	} else {
		confInfos = cis
		infosByDay = confInfosByDay(cis)
	}
	agendaDays := buildAgendaDays(ctx, conf, talks, infosByDay)

	// Flatten AgendaDays into a single chrono-ordered slice for the
	// JSON-LD subEvent[] emission. Each day's .All is already
	// status-filtered + sorted by buildAgendaDays.
	var scheduledSessions []*types.Session
	for _, d := range agendaDays {
		if d == nil {
			continue
		}
		scheduledSessions = append(scheduledSessions, d.All...)
	}

	// Populate countdown bounds + HasAgenda for the conf_nav widget
	// + agenda section without mutating the loaded Conf.
	confCopy := *conf
	confCopy.CountdownStart, confCopy.CountdownEnd = computeCountdownBounds(&confCopy, infosByDay)
	confCopy.HasAgenda = anyScheduledTalk(&confCopy, talks)

	confHotels := helpers.HotelsForConf(ctx, conf)
	satelliteEvents, err := getters.ListSatelliteEvents(ctx, conf.Ref, false)
	if err != nil {
		ctx.Err.Printf("/%s satellite events load failed (continuing): %s", conf.Tag, err)
	}

	viewer := auth.RequireOptional(r, ctx)
	hackathonCanAdmin := false
	hackathon, err := getters.GetCompetitionByConferenceID(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s hackathon load failed (continuing): %s", conf.Tag, err)
	}
	var hackathonScheduleEvents []HackathonScheduleEvent
	var hackathonJudges []*types.CompetitionJudge
	var hackathonPlaceRows []*HackathonPlaceRow
	var hackathonOrgs map[string]*types.Org
	if hackathon != nil {
		hackathonViewer := hackathonViewerFromIdentity(viewer, conf)
		hackathonCanAdmin = hackathonViewer.Admin || hackathonViewer.Manager
		if hackathon.Visibility != getters.CompetitionVisibilityPublic && !hackathonCanAdmin {
			hackathon = nil
			hackathonCanAdmin = false
		}
	}
	if hackathon != nil {
		hackathonScheduleEvents, err = loadLocalizedHackathonScheduleEvents(ctx, hackathon, conf)
		if err != nil {
			ctx.Err.Printf("/%s hackathon schedule events %s failed (continuing): %s", conf.Tag, hackathon.ID, err)
		}
		hackathonJudges, err = getters.ListCompetitionJudges(ctx, hackathon.ID)
		if err != nil {
			ctx.Err.Printf("/%s hackathon judges %s failed (continuing): %s", conf.Tag, hackathon.ID, err)
		}
		hackathonOrgs, err = loadHackathonOrgMap(ctx)
		if err != nil {
			ctx.Err.Printf("/%s hackathon orgs %s failed (continuing): %s", conf.Tag, hackathon.ID, err)
		}
		hackathonPlaceRows, err = loadConfHackathonPlaceRows(ctx, hackathon.ID, hackathon.ResultsFinalizedAt != nil, hackathonOrgs)
		if err != nil {
			ctx.Err.Printf("/%s hackathon place rows %s failed (continuing): %s", conf.Tag, hackathon.ID, err)
		}
	}
	confCopy.ShowHackathon = hackathon != nil && hackathon.Visibility == getters.CompetitionVisibilityPublic
	conf = &confCopy

	currTix := findCurrTix(conf, soldCount)
	maxTix := findMaxTix(conf)

	var tixLeft uint
	if currTix == nil {
		tixLeft = 0
	} else {
		tixLeft = currTix.Max - soldCount
	}
	tmplTag := "conf/generic.tmpl"
	err = ctx.TemplateCache.ExecuteTemplate(w, tmplTag, &ConfPage{
		Conf:                    conf,
		Hotels:                  confHotels,
		Tix:                     currTix,
		MaxTix:                  maxTix,
		Sold:                    soldCount,
		TixLeft:                 tixLeft,
		Talks:                   talks,
		EventSpeakers:           evSpeakers,
		FeaturedSpeakers:        featuredSpeakers,
		CommunitySpeakers:       communitySpeakers,
		Buckets:                 buckets,
		Days:                    days,
		AgendaDays:              agendaDays,
		ConfInfos:               confInfos,
		ScheduledSessions:       scheduledSessions,
		SatelliteEvents:         satelliteEvents,
		Hackathon:               hackathon,
		HackathonScheduleEvents: hackathonScheduleEvents,
		HackathonJudges:         hackathonJudges,
		HackathonPlaceRows:      hackathonPlaceRows,
		HackathonCanAdmin:       hackathonCanAdmin,
		Year:                    helpers.CurrentYear(),
	})
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/%s ExecuteTemplate failed ! %s", conf.Tag, err.Error())
		return
	}
}

func RenderConfAgenda(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if !conf.IsPublished() {
		handle404(w, r, ctx)
		return
	}

	talks, err := getters.GetTalksFor(ctx, conf.Tag)
	if err != nil {
		http.Error(w, "Unable to load agenda, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/agenda unable to fetch talks: %s", conf.Tag, err.Error())
		return
	}

	var infosByDay map[int]*types.ConfInfo
	var confInfos []*types.ConfInfo
	if cis, err := getters.ListConfInfos(ctx, conf.Tag); err != nil {
		ctx.Err.Printf("/%s/agenda ListConfInfos failed (continuing): %s", conf.Tag, err)
	} else {
		confInfos = cis
		infosByDay = confInfosByDay(cis)
	}
	agendaDays := buildAgendaDays(ctx, conf, talks, infosByDay)

	confCopy := *conf
	confCopy.CountdownStart, confCopy.CountdownEnd = computeCountdownBounds(&confCopy, infosByDay)
	conf = publicHackathonNavConference(ctx, &confCopy, talks)

	if err := ctx.TemplateCache.ExecuteTemplate(w, "conf/agenda.tmpl", &ConfPage{
		Conf:       conf,
		Talks:      talks,
		AgendaDays: agendaDays,
		ConfInfos:  confInfos,
		Year:       helpers.CurrentYear(),
	}); err != nil {
		http.Error(w, "Unable to load agenda, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/agenda ExecuteTemplate failed: %s", conf.Tag, err.Error())
		return
	}
}

func RenderConfSpeakers(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	if !conf.IsPublished() {
		handle404(w, r, ctx)
		return
	}

	talks, err := getters.GetTalksFor(ctx, conf.Tag)
	if err != nil {
		http.Error(w, "Unable to load speakers, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/speakers unable to fetch talks: %s", conf.Tag, err.Error())
		return
	}

	evSpeakers := acceptedSpeakersForConf(ctx, conf, talks)
	sort.Sort(evSpeakers)

	var infosByDay map[int]*types.ConfInfo
	if cis, err := getters.ListConfInfos(ctx, conf.Tag); err != nil {
		ctx.Err.Printf("/%s/speakers ListConfInfos failed (continuing): %s", conf.Tag, err)
	} else {
		infosByDay = confInfosByDay(cis)
	}

	confCopy := *conf
	confCopy.CountdownStart, confCopy.CountdownEnd = computeCountdownBounds(&confCopy, infosByDay)
	conf = publicHackathonNavConference(ctx, &confCopy, talks)

	if err := ctx.TemplateCache.ExecuteTemplate(w, "conf/speakers.tmpl", &ConfPage{
		Conf:          conf,
		Talks:         talks,
		EventSpeakers: evSpeakers,
		Year:          helpers.CurrentYear(),
	}); err != nil {
		http.Error(w, "Unable to load speakers, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/speakers ExecuteTemplate failed: %s", conf.Tag, err.Error())
		return
	}
}

func ContactPage(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	switch r.Method {
	case http.MethodGet:
		err := ctx.TemplateCache.ExecuteTemplate(w, "embeds/contact.tmpl", &struct{ Year uint }{
			Year: helpers.CurrentYear(),
		})
		if err != nil {
			http.Error(w, "Unable to load page", http.StatusInternalServerError)
			ctx.Err.Printf("/contact ExecuteTemplate failed: %s", err.Error())
		}
		return
	case http.MethodPost:
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}

		name := r.FormValue("Name")
		phone := r.FormValue("Phone")
		email := r.FormValue("Email")
		contactAt := r.FormValue("ContactAt")
		message := r.FormValue("Message")
		captcha := r.FormValue("Captcha")

		if captcha != "5" {
			w.Write([]byte(helpers.ErrApp("Incorrect captcha. The answer is 5.", "hello")))
			return
		}

		if email == "" || !strings.Contains(email, "@") {
			w.Write([]byte(helpers.ErrApp("Please provide a valid email address.", "hello")))
			return
		}

		if name == "" || message == "" {
			w.Write([]byte(helpers.ErrApp("Name and message are required.", "hello")))
			return
		}

		htmlBody := fmt.Sprintf(
			"<h3>Contact Form Submission</h3>"+
				"<p><strong>Name:</strong> %s</p>"+
				"<p><strong>Email:</strong> %s</p>"+
				"<p><strong>Phone:</strong> %s</p>"+
				"<p><strong>Best way to contact:</strong> %s</p>"+
				"<hr/>"+
				"<p>%s</p>",
			template.HTMLEscapeString(name), template.HTMLEscapeString(email), template.HTMLEscapeString(phone), template.HTMLEscapeString(contactAt), template.HTMLEscapeString(message))

		textBody := fmt.Sprintf(
			"Contact Form Submission\n\nName: %s\nEmail: %s\nPhone: %s\nBest way to contact: %s\n\n%s",
			name, email, phone, contactAt, message)

		mail := &emails.Mail{
			JobKey:   fmt.Sprintf("contact-%s-%d", email, time.Now().Unix()),
			Email:    "hello@btcpp.dev",
			ReplyTo:  email,
			Title:    fmt.Sprintf("Contact Form: %s", name),
			SendAt:   time.Now(),
			HTMLBody: []byte(htmlBody),
			TextBody: []byte(textBody),
		}

		err := emails.ComposeAndSendMail(ctx, mail)
		if err != nil {
			ctx.Err.Printf("/contact failed to send email: %s", err.Error())
			w.Write([]byte(helpers.ErrApp("Unable to send your message. Please try again.", "hello")))
			return
		}

		ctx.Infos.Printf("Contact form submitted by %s (%s)", name, email)
		w.Write([]byte(helpers.SuccessApp("Your message has been sent! We'll get back to you soon.")))
		return
	}
}

func RenderPage(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, page string) {

	confList := listConfs(w, ctx)
	if confList == nil {
		return
	}

	// Single Notion call (empty tag = all rows) so the homepage's
	// countdown widget on each conf card has the same per-day-strip
	// bounds the per-conf page uses. Bucket by tag → day for cheap
	// per-conf lookup.
	infosByTag := map[string]map[int]*types.ConfInfo{}
	if cis, err := getters.ListConfInfos(ctx, ""); err != nil {
		ctx.Err.Printf("/%s ListConfInfos for index countdown (continuing): %s", page, err)
	} else {
		for _, ci := range cis {
			if ci == nil || ci.Day < 1 || ci.ConfTag == "" {
				continue
			}
			m, ok := infosByTag[ci.ConfTag]
			if !ok {
				m = map[int]*types.ConfInfo{}
				infosByTag[ci.ConfTag] = m
			}
			m[ci.Day] = ci
		}
	}

	hackathonConfs := publicHackathonConfs(ctx, page)

	// Shallow-copy each conf before populating the runtime-only
	// CountdownStart/End and hackathon link fields.
	enriched := make([]*types.Conf, 0, len(confList))
	for _, c := range confList {
		if c == nil {
			continue
		}
		copy := *c
		copy.CountdownStart, copy.CountdownEnd = computeCountdownBounds(&copy, infosByTag[copy.Tag])
		if hackathonConfs[copy.Ref] {
			copy.ShowHackathon = true
			copy.HackathonURL = "/" + url.PathEscape(copy.Tag) + "#hackathon"
		}
		enriched = append(enriched, &copy)
	}

	data := HomePageData{
		Confs:            enriched,
		Upcoming:         homeUpcomingConfs(enriched),
		Past:             homePastConfs(enriched),
		Years:            homeTimelineYears(enriched),
		Sponsors:         homeSponsors(ctx, enriched, time.Now()),
		FeaturedSpeakers: homeFeaturedSpeakers(ctx),
		MapMarkers:       homeMapMarkers(enriched),
		Year:             helpers.CurrentYear(),
	}

	template := fmt.Sprintf("embeds/%s.tmpl", page)
	err := ctx.TemplateCache.ExecuteTemplate(w, template, &data)

	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/%s ExecuteTemplate failed ! %s", page, err.Error())
	}
}

func homeFeaturedSpeakers(ctx *config.AppContext) []*types.Speaker {
	speakers, err := getters.ListHomepageFeaturedSpeakers(ctx)
	if err != nil {
		ctx.Err.Printf("/ homepage featured speakers (continuing): %s", err)
		return nil
	}
	return speakers
}

func homeUpcomingConfs(confs []*types.Conf) []*types.Conf {
	return homeUpcomingConfsAt(confs, time.Now())
}

func homeUpcomingConfsAt(confs []*types.Conf, now time.Time) []*types.Conf {
	out := make([]*types.Conf, 0, len(confs))
	for _, c := range confs {
		if c != nil && c.IsInActiveEventListAt(now) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].StartDate.Before(out[j].StartDate)
	})
	return out
}

func homePastConfs(confs []*types.Conf) []*types.Conf {
	return homePastConfsAt(confs, time.Now())
}

func homePastConfsAt(confs []*types.Conf, now time.Time) []*types.Conf {
	out := make([]*types.Conf, 0, len(confs))
	for _, c := range confs {
		if c != nil && c.IsPublished() && !c.IsInActiveEventListAt(now) {
			out = append(out, c)
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		return out[i].StartDate.After(out[j].StartDate)
	})
	return out
}

func homeTimelineYears(confs []*types.Conf) []*HomeTimelineYear {
	byYear := map[int][]*types.Conf{}
	for _, c := range confs {
		if c == nil || !c.IsPublished() || c.StartDate.IsZero() {
			continue
		}
		year := c.StartDate.In(c.Loc()).Year()
		byYear[year] = append(byYear[year], c)
	}
	years := make([]int, 0, len(byYear))
	for y := range byYear {
		years = append(years, y)
	}
	sort.Sort(sort.Reverse(sort.IntSlice(years)))
	out := make([]*HomeTimelineYear, 0, len(years))
	for _, y := range years {
		items := byYear[y]
		sort.SliceStable(items, func(i, j int) bool {
			return items[i].StartDate.After(items[j].StartDate)
		})
		out = append(out, &HomeTimelineYear{Year: y, Confs: items})
	}
	return out
}

func homeMapMarkers(confs []*types.Conf) []*HomeMapMarker {
	type markerGroup struct {
		x      float64
		y      float64
		marker *HomeMapMarker
	}
	groups := map[string]*markerGroup{}
	for _, conf := range confs {
		if conf == nil || !conf.IsPublished() {
			continue
		}
		x, y, ok := homeMapPosition(conf)
		if !ok {
			continue
		}
		label := strings.TrimSpace(conf.MapLabel)
		if label == "" {
			label = conf.Desc
		}
		side := normalizeMapLabelSide(conf.MapLabelSide)
		key := fmt.Sprintf("%.2f|%.2f|%s", x, y, strings.ToLower(label))
		group := groups[key]
		if group == nil {
			group = &markerGroup{
				x: x,
				y: y,
				marker: &HomeMapMarker{
					Conf:      conf,
					Label:     label,
					Style:     fmt.Sprintf("left: %.2f%%; top: %.2f%%;", x, y),
					LabelSide: side,
				},
			}
			groups[key] = group
		}
		if conf.IsInActiveEventList() {
			group.marker.Upcoming = true
		}
		group.marker.Editions = append(group.marker.Editions, &HomeMapEdition{
			Conf:        conf,
			Label:       conf.Desc,
			Date:        conf.DateDesc,
			EditionType: conf.EditionType,
			Upcoming:    conf.IsInActiveEventList(),
		})
		if group.marker.Conf == nil || conf.StartDate.Before(group.marker.Conf.StartDate) {
			group.marker.Conf = conf
		}
	}
	out := make([]*HomeMapMarker, 0, len(groups))
	for _, group := range groups {
		sort.SliceStable(group.marker.Editions, func(i, j int) bool {
			return group.marker.Editions[i].Conf.StartDate.After(group.marker.Editions[j].Conf.StartDate)
		})
		out = append(out, group.marker)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].Upcoming != out[j].Upcoming {
			return !out[i].Upcoming
		}
		return out[i].Conf.StartDate.Before(out[j].Conf.StartDate)
	})
	return out
}

func homeMapPosition(conf *types.Conf) (float64, float64, bool) {
	if conf.MapXPercent > 0 && conf.MapYPercent > 0 {
		return clampPercent(conf.MapXPercent), clampPercent(conf.MapYPercent), true
	}
	if conf.MapLatitude == 0 && conf.MapLongitude == 0 {
		return 0, 0, false
	}
	x := (conf.MapLongitude + 180) / 360 * 100
	y := (90 - conf.MapLatitude) / 180 * 100
	return clampPercent(x), clampPercent(y), true
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func homeSponsors(ctx *config.AppContext, confs []*types.Conf, now time.Time) []*HomeSponsor {
	if now.IsZero() {
		now = time.Now()
	}
	currentYear := now.Year()
	keepLevels := map[string]bool{
		"Headline": true,
		"Workshop": true,
	}
	seen := map[string]bool{}
	var out []*HomeSponsor
	for _, conf := range confs {
		if conf == nil || conf.Ref == "" || conf.StartDate.IsZero() {
			continue
		}
		year := conf.StartDate.In(conf.Loc()).Year()
		if year < currentYear-1 || year > currentYear {
			continue
		}
		for _, tier := range SponsorTiersForConf(ctx, conf.Ref) {
			if tier == nil || !keepLevels[tier.Level] {
				continue
			}
			for _, sp := range tier.Sponsors {
				if sp == nil || sp.Org == nil {
					continue
				}
				key := strings.ToLower(strings.TrimSpace(sp.Org.Ref + "|" + tier.Level))
				if key == "|"+strings.ToLower(tier.Level) {
					key = strings.ToLower(strings.TrimSpace(sp.Org.Name + "|" + tier.Level))
				}
				if seen[key] {
					continue
				}
				seen[key] = true
				out = append(out, &HomeSponsor{
					Name:      sp.Org.Name,
					Level:     tier.Level,
					LogoDark:  strings.TrimSpace(sp.Org.LogoDark),
					LogoLight: strings.TrimSpace(sp.Org.LogoLight),
					URL:       strings.TrimSpace(sp.Org.Website),
				})
			}
		}
	}
	sort.SliceStable(out, func(i, j int) bool {
		ri := homeSponsorRank(out[i].Level)
		rj := homeSponsorRank(out[j].Level)
		if ri != rj {
			return ri < rj
		}
		return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name)
	})
	return out
}

func homeSponsorRank(level string) int {
	switch normalizeLevel(level) {
	case "Headline":
		return 0
	case "Workshop":
		return 1
	default:
		return 2
	}
}

func publicHackathonConfs(ctx *config.AppContext, page string) map[string]bool {
	competitions, err := getters.ListCompetitions(ctx)
	if err != nil {
		ctx.Err.Printf("/%s ListCompetitions for index hackathon links (continuing): %s", page, err)
		return nil
	}
	confs := make(map[string]bool, len(competitions))
	for _, competition := range competitions {
		if competition == nil || competition.Visibility != getters.CompetitionVisibilityPublic || competition.ConferenceID == "" {
			continue
		}
		confs[competition.ConferenceID] = true
	}
	return confs
}

type TicketTmpl struct {
	QRCodeURI string
	Domain    string
	CSS       string
	Type      string
	Conf      *types.Conf
}
