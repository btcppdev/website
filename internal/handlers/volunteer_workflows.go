package handlers

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/config"
	"btcpp-web/internal/emails"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/ics"
	"btcpp-web/internal/missives"
	"btcpp-web/internal/types"
	"btcpp-web/internal/volunteers"
	"github.com/gorilla/mux"
)

func RenderFindShift(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	switch r.Method {
	case http.MethodGet:
		err := ctx.TemplateCache.ExecuteTemplate(w, "volunteers/findshift.tmpl", &VolShiftPage{
			Year: helpers.CurrentYear(),
		})

		if err != nil {
			http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
			ctx.Err.Printf("/volunteers/findshift ExecuteTemplate failed ! %s", err.Error())
			return
		}
	case http.MethodPost:
		limitRequestBody(w, r, maxFormBodyBytes)
		if err := r.ParseForm(); err != nil {
			http.Error(w, "bad form", http.StatusBadRequest)
			return
		}
		dec := newFormDecoder()
		var form EmailForm
		err := dec.Decode(&form, r.PostForm)
		if err != nil {
			ctx.Err.Printf("/vols/shift unable to decode email form %s", err)
			w.Write([]byte(helpers.ErrVolApp("Unable to send you email link.")))
			return
		}

		_, err = emails.OnlyForLogin(ctx, form.Email)
		if err != nil {
			http.Error(w, "Unable to send login link via email", http.StatusInternalServerError)
			ctx.Err.Printf("/volunteers/findshift onlyforvollogin failed ! %s", err.Error())
			return
		}

		/* We redirect to home on success */
		http.Redirect(w, r, "/", http.StatusSeeOther)
	}
}

func calcStats(apps []*types.Volunteer) *ApplicationStats {

	pending, accepted, totalShifts := 0, 0, 0
	for _, app := range apps {
		switch app.Status {
		case "Applied":
		case "PendingShifts":
		case "Waitlist":
			pending += 1
		case "Scheduled":
			accepted += 1
		}
		totalShifts += len(app.WorkShifts)
	}

	return &ApplicationStats{
		Applied:     len(apps),
		Pending:     pending,
		Accepted:    accepted,
		TotalShifts: totalShifts,
	}
}

func validateVolEmail(r *http.Request, ctx *config.AppContext) (string, string, error) {
	encodedHMAC := r.URL.Query().Get("hr")
	encodedEmail := r.URL.Query().Get("em")

	if encodedHMAC == "" || encodedEmail == "" {
		return "", "", fmt.Errorf("missing credentials")
	}

	emailval, err := base64.RawURLEncoding.DecodeString(encodedEmail)
	if err != nil {
		return "", "", err
	}

	hashResult, err := base64.RawURLEncoding.DecodeString(encodedHMAC)
	if err != nil {
		return "", "", err
	}
	email := string(emailval)
	hmacVal := string(hashResult)

	if !helpers.VerifyEmailHMAC(ctx, hmacVal, email) {
		return "", "", fmt.Errorf("invalid HMAC")
	}

	return email, encodedHMAC, nil
}

func VolunteerShift(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	/* We put a hash + email in the link */
	email, encodedHMAC, err := validateVolEmail(r, ctx)
	if err != nil {
		ctx.Infos.Printf("/vols/shift HMAC validation failed: %s", err.Error())
		RenderFindShift(w, r, ctx)
		return
	}
	ctx.Infos.Printf("/vols/shift validated email: %s", email)

	/* Find volunteer signups */
	volapps, err := getters.ListVolunteerApps(ctx, email)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/vol/shift listvolunteerapps failed ! %s", err.Error())
		return
	}

	// fixme: add "sign up to volunteer" state :)
	if len(volapps) == 0 {
		handle404(w, r, ctx)
		return
	}

	// Populate WorkShifts and per-conf VolInfo for each volunteer application
	volInfosByConf, err := getters.GetVolInfoMap(ctx)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/vol/shift getvolinfomap failed ! %s", err.Error())
		return
	}

	for _, vol := range volapps {
		conf := vol.ScheduleFor[0]
		confShifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
		if err != nil {
			ctx.Err.Printf("/vol/shift failed to get shifts for conf %s: %s", conf.Tag, err.Error())
			continue
		}
		vol.WorkShifts = getSelectedShifts(vol, confShifts)
	}

	encodedEmail := r.URL.Query().Get("em")
	confs := listConfs(w, ctx)
	err = ctx.TemplateCache.ExecuteTemplate(w, "volunteers/shift.tmpl", &VolShiftPage{
		Name:     volapps[0].Name,
		Hometown: volapps[0].Hometown,
		Email:    encodedEmail,
		HMAC:     encodedHMAC,
		Stats:    calcStats(volapps),
		VolApps:  volapps,
		Confs:    confs,
		VolInfos: volInfosByConf,
		Year:     helpers.CurrentYear(),
	})

	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/vol/shift ExecuteTemplate failed ! %s", err.Error())
		return
	}
}

func buildShiftDisplays(vol *types.Volunteer, shifts []*types.WorkShift, selectedShifts []*types.WorkShift) map[string][]*ShiftDisplay {
	grouped := make(map[string][]*ShiftDisplay)

	for _, shift := range shifts {
		if shift.ShiftTime == nil {
			continue
		}

		dayKey := shift.DayOf()
		display := &ShiftDisplay{
			Shift:       shift,
			IsAvailable: vol.AvailableOn(shift),
			IsEligible:  shift.Type == nil || !vol.WillNotWork(shift.Type),
			IsFull:      shift.IsFull(),
			IsSelected:  shift.IsAssigned(vol.Ref),
			Conflicts:   shift.Intersects(selectedShifts),
		}

		// Compute CanSelect and Reason
		if display.IsSelected {
			display.CanSelect = false
			display.Reason = "Already selected"
		} else if !display.IsAvailable {
			display.CanSelect = false
			display.Reason = "Not available this day"
		} else if !display.IsEligible {
			display.CanSelect = false
			display.Reason = "Job type not preferred"
		} else if display.IsFull {
			display.CanSelect = false
			display.Reason = "Shift is full"
		} else if display.Conflicts {
			display.CanSelect = false
			display.Reason = "Conflicts with selected shift"
		} else {
			display.CanSelect = true
		}

		grouped[dayKey] = append(grouped[dayKey], display)
	}

	// Sort each day's shifts by start time
	for _, dayShifts := range grouped {
		sort.Slice(dayShifts, func(i, j int) bool {
			return dayShifts[i].Shift.ShiftTime.Start.Before(dayShifts[j].Shift.ShiftTime.Start)
		})
	}

	return grouped
}

func getSelectedShifts(vol *types.Volunteer, shifts []*types.WorkShift) []*types.WorkShift {
	var selected []*types.WorkShift
	for _, shift := range shifts {
		if shift.IsAssigned(vol.Ref) {
			selected = append(selected, shift)
		}
	}

	// Sort by day and start time
	sort.Slice(selected, func(i, j int) bool {
		if selected[i].ShiftTime == nil {
			return true
		}
		if selected[j].ShiftTime == nil {
			return false
		}
		return selected[i].ShiftTime.Start.Before(selected[j].ShiftTime.Start)
	})

	return selected
}

func findVolForConf(volapps []*types.Volunteer, confTag string) *types.Volunteer {
	for _, vol := range volapps {
		for _, conf := range vol.ScheduleFor {
			if conf.Tag == confTag {
				return vol
			}
		}
	}
	return nil
}

func VolunteerShiftSignup(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, _, err := validateVolEmail(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	params := mux.Vars(r)
	confTag := params["conf"]

	// Get volunteer applications
	volapps, err := getters.ListVolunteerApps(ctx, email)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/vols/shift/%s listvolunteerapps failed ! %s", confTag, err.Error())
		return
	}

	// Find the volunteer application for this conference
	vol := findVolForConf(volapps, confTag)
	if vol == nil {
		ctx.Err.Printf("/vols/shift/%s no volunteer app for conf", confTag)
		handle404(w, r, ctx)
		return
	}

	// Check if volunteer is in Pending Shifts status
	if vol.Status != "PendingShifts" && vol.Status != "Scheduled" {
		ctx.Err.Printf("/vols/shift/%s volunteer not in Pending Shifts status: %s", confTag, vol.Status)
		handle404(w, r, ctx)
		return
	}

	// Get shifts for this conference
	confShifts, err := getters.GetShiftsForConf(ctx, confTag)
	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/vols/shift/%s getshiftsforconf failed ! %s", confTag, err.Error())
		return
	}

	// Get currently selected shifts
	selectedShifts := getSelectedShifts(vol, confShifts)

	// Build display data
	shiftDisplays := buildShiftDisplays(vol, confShifts, selectedShifts)

	// Get conference info
	var conf *types.Conf
	for _, c := range vol.ScheduleFor {
		if c.Tag == confTag {
			conf = c
			break
		}
	}

	minShifts := 3
	canSubmit := len(selectedShifts) >= minShifts

	encodedHMAC := r.URL.Query().Get("hr")
	encodedEmail := r.URL.Query().Get("em")

	// Build form helpers (availability + work prefs editor) so the volunteer
	// can update them inline without going back to the application form.
	jobs := listJobs(w, ctx)
	yesJobs := helpers.BuildJobs("yjob-", jobs, false)
	noJobs := helpers.BuildJobs("njob-", jobs, false)

	yesSet := make(map[string]bool)
	for _, j := range vol.WorkYes {
		yesSet[j.Tag] = true
	}
	noSet := make(map[string]bool)
	for _, j := range vol.WorkNo {
		noSet[j.Tag] = true
	}
	for i := range yesJobs {
		yesJobs[i].Checked = yesSet[yesJobs[i].ItemID[len("yjob-"):]]
	}
	for i := range noJobs {
		noJobs[i].Checked = noSet[noJobs[i].ItemID[len("njob-"):]]
	}

	daysList := conf.DaysList("days-", true)
	availSet := make(map[string]bool)
	for _, d := range vol.Availability {
		availSet[d] = true
	}
	for i := range daysList {
		daysList[i].Checked = availSet[daysList[i].ItemID[len("days-"):]]
	}

	err = ctx.TemplateCache.ExecuteTemplate(w, "volunteers/shift_signup.tmpl", &ShiftSignupPage{
		Vol:            vol,
		Conf:           conf,
		AvailShifts:    shiftDisplays,
		SelectedShifts: selectedShifts,
		MinShifts:      minShifts,
		ShiftProgress:  len(selectedShifts),
		CanSubmit:      canSubmit,
		ConfRef:        confTag,
		Email:          encodedEmail,
		HMAC:           encodedHMAC,
		DaysList:       daysList,
		YesJobs:        yesJobs,
		NoJobs:         noJobs,
		Year:           helpers.CurrentYear(),
	})

	if err != nil {
		http.Error(w, "Unable to load page, please try again later", http.StatusInternalServerError)
		ctx.Err.Printf("/vols/shift/%s ExecuteTemplate failed ! %s", confTag, err.Error())
		return
	}
}

func VolunteerSelectShift(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, _, err := validateVolEmail(r, ctx)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	params := mux.Vars(r)
	confTag := params["conf"]

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	shiftRef := r.Form.Get("shiftRef")

	if shiftRef == "" {
		http.Error(w, "Missing shift reference", http.StatusBadRequest)
		return
	}

	// Get volunteer
	volapps, err := getters.ListVolunteerApps(ctx, email)
	if err != nil {
		http.Error(w, "Unable to load volunteer", http.StatusInternalServerError)
		return
	}

	vol := findVolForConf(volapps, confTag)
	if vol == nil {
		http.Error(w, "Volunteer not found", http.StatusNotFound)
		return
	}

	// Assign volunteer to shift
	err = getters.AssignVolunteerToShift(ctx, vol.Ref, shiftRef)
	if err != nil {
		ctx.Err.Printf("/vols/shift/%s/select assign failed: %s", confTag, err.Error())
		http.Error(w, "Failed to assign shift", http.StatusInternalServerError)
		return
	}

	// Re-render the shift list
	renderShiftList(w, r, ctx, email, confTag)
}

func VolunteerRemoveShift(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, _, err := validateVolEmail(r, ctx)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	params := mux.Vars(r)
	confTag := params["conf"]

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	shiftRef := r.Form.Get("shiftRef")

	if shiftRef == "" {
		http.Error(w, "Missing shift reference", http.StatusBadRequest)
		return
	}

	// Get volunteer
	volapps, err := getters.ListVolunteerApps(ctx, email)
	if err != nil {
		http.Error(w, "Unable to load volunteer", http.StatusInternalServerError)
		return
	}

	vol := findVolForConf(volapps, confTag)
	if vol == nil {
		http.Error(w, "Volunteer not found", http.StatusNotFound)
		return
	}

	// Prevent removal within two weeks of conference start
	if len(vol.ScheduleFor) > 0 && vol.ScheduleFor[0].WithinTwoWeeks() {
		http.Error(w, "Cannot modify shifts within two weeks of the conference", http.StatusBadRequest)
		return
	}

	// Remove volunteer from shift
	err = getters.RemoveVolunteerFromShift(ctx, vol.Ref, shiftRef)
	if err != nil {
		ctx.Err.Printf("/vols/shift/%s/remove failed: %s", confTag, err.Error())
		http.Error(w, "Failed to remove shift", http.StatusInternalServerError)
		return
	}

	// CANCEL ICS for this volunteer's calendar entry.
	// Best-effort — log on error, don't fail the remove.
	cancelShiftCalForVol(ctx, vol, shiftRef, confTag)

	// Re-render the shift list
	renderShiftList(w, r, ctx, email, confTag)
}

// cancelShiftCalForVol looks up the shift + conf and fires a
// CANCEL ICS to the given volunteer, removing the dropped shift
// from their calendar. No-op when the shift has no CalNotif
// (never invited) or the lookups fail. Logged-only; never fails
// the surrounding remove.
func cancelShiftCalForVol(ctx *config.AppContext, vol *types.Volunteer, shiftRef, confTag string) {
	if vol == nil || vol.Email == "" {
		return
	}
	conf, err := getters.GetConfByTag(ctx, confTag)
	if err != nil {
		ctx.Err.Printf("cancelShiftCalForVol conf: %s", err)
		return
	}
	if conf == nil {
		return
	}
	shifts, err := getters.GetShiftsForConf(ctx, confTag)
	if err != nil {
		ctx.Err.Printf("cancelShiftCalForVol shifts %s: %s", confTag, err)
		return
	}
	for _, s := range shifts {
		if s != nil && s.Ref == shiftRef {
			if err := DispatchShiftICSCancelForVol(ctx, s, conf, vol.Email, vol.Name); err != nil {
				ctx.Err.Printf("cancelShiftCalForVol dispatch %q: %s", s.Name, err)
			}
			return
		}
	}
}

func renderShiftList(w http.ResponseWriter, r *http.Request, ctx *config.AppContext, email, confTag string) {
	// Re-fetch data for updated display
	volapps, err := getters.ListVolunteerApps(ctx, email)
	if err != nil {
		http.Error(w, "Unable to load volunteer", http.StatusInternalServerError)
		return
	}

	vol := findVolForConf(volapps, confTag)
	if vol == nil {
		http.Error(w, "Volunteer not found", http.StatusNotFound)
		return
	}

	confShifts, err := getters.GetShiftsForConf(ctx, confTag)
	if err != nil {
		http.Error(w, "Unable to load shifts", http.StatusInternalServerError)
		return
	}

	selectedShifts := getSelectedShifts(vol, confShifts)
	shiftDisplays := buildShiftDisplays(vol, confShifts, selectedShifts)

	var conf *types.Conf
	for _, c := range vol.ScheduleFor {
		if c.Tag == confTag {
			conf = c
			break
		}
	}

	minShifts := 3
	canSubmit := len(selectedShifts) >= minShifts

	encodedHMAC := r.URL.Query().Get("hr")
	encodedEmail := r.URL.Query().Get("em")

	err = ctx.TemplateCache.ExecuteTemplate(w, "volunteers/shift_list.tmpl", &ShiftSignupPage{
		Vol:            vol,
		Conf:           conf,
		AvailShifts:    shiftDisplays,
		SelectedShifts: selectedShifts,
		MinShifts:      minShifts,
		ShiftProgress:  len(selectedShifts),
		CanSubmit:      canSubmit,
		ConfRef:        confTag,
		Email:          encodedEmail,
		HMAC:           encodedHMAC,
		Year:           helpers.CurrentYear(),
	})

	if err != nil {
		ctx.Err.Printf("shift_list template failed: %s", err.Error())
		http.Error(w, "Template error", http.StatusInternalServerError)
	}
}

func VolunteerSubmitShifts(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, _, err := validateVolEmail(r, ctx)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	params := mux.Vars(r)
	confTag := params["conf"]

	// Get volunteer
	volapps, err := getters.ListVolunteerApps(ctx, email)
	if err != nil {
		http.Error(w, "Unable to load volunteer", http.StatusInternalServerError)
		return
	}

	vol := findVolForConf(volapps, confTag)
	if vol == nil {
		http.Error(w, "Volunteer not found", http.StatusNotFound)
		return
	}

	// Get shifts and verify minimum
	confShifts, err := getters.GetShiftsForConf(ctx, confTag)
	if err != nil {
		http.Error(w, "Unable to load shifts", http.StatusInternalServerError)
		return
	}

	selectedShifts := getSelectedShifts(vol, confShifts)
	minShifts := 3

	if len(selectedShifts) < minShifts {
		http.Error(w, fmt.Sprintf("Must select at least %d shifts", minShifts), http.StatusBadRequest)
		return
	}

	// Run the full scheduled flow (status update, email, ticket, calendar)
	conf := vol.ScheduleFor[0]
	vol.WorkShifts = selectedShifts
	err = runScheduledFlow(ctx, vol, conf)
	if err != nil {
		ctx.Err.Printf("/vols/shift/%s/submit scheduled flow failed: %s", confTag, err.Error())
		http.Error(w, "Failed to schedule volunteer", http.StatusInternalServerError)
		return
	}

	// Redirect back to dashboard
	encodedHMAC := r.URL.Query().Get("hr")
	encodedEmail := r.URL.Query().Get("em")
	redirectURL := fmt.Sprintf("/vols/shift?hr=%s&em=%s", encodedHMAC, encodedEmail)
	w.Header().Set("HX-Redirect", redirectURL)
}

// runScheduledFlow runs the post-status-update logic that promotes a volunteer
// to "Scheduled": updates status, sends the onboarding email, issues a
// ticket, subscribes to the volunteer newsletter, and sends calendar invites
// (if Google Calendar is connected). Caller must have already populated
// vol.WorkShifts with the assigned shifts. Failures in non-critical steps
// (email, calendar invites) are logged but don't abort the flow.
func runScheduledFlow(ctx *config.AppContext, vol *types.Volunteer, conf *types.Conf) error {
	// Update status
	err := getters.UpdateVolunteerStatus(ctx, vol.Ref, "Scheduled")
	if err != nil {
		return fmt.Errorf("status update: %w", err)
	}

	// Look up VolInfo for orientation details
	volinfo, err := getters.GetVolInfo(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("scheduled flow: failed to get volinfo for %s: %s", conf.Tag, err)
		// continue without volinfo
	}

	// Send onboarding email
	_, err = emails.OnlyForVolShift(ctx, volinfo, vol)
	if err != nil {
		ctx.Err.Printf("scheduled flow: failed to send onboarding email to %s: %s", vol.Email, err)
	}

	// Issue volunteer ticket
	tixType := "volunteer"
	entry := types.Entry{
		ID:       vol.RegisID(),
		ConfRef:  conf.Ref,
		Currency: "USD",
		Created:  time.Now(),
		Email:    vol.Email,
		Items: []types.Item{
			types.Item{
				Total: 1,
				Desc:  conf.Desc,
				Type:  tixType,
			},
		},
	}

	err = getters.AddTickets(ctx, &entry, "volreg")
	if err != nil {
		return fmt.Errorf("add ticket: %w", err)
	}

	err = missives.NewTicketSub(ctx, vol.Email, conf.Tag, tixType, true)
	if err != nil {
		ctx.Err.Printf("scheduled flow: newsletter sub failed for %s: %s", vol.Email, err)
	}

	ctx.Infos.Println("Scheduled volunteer, ticket added:", entry.ID)

	// Self-hosted ICS calendar invites: one per shift this
	// volunteer just signed up for, plus one orientation invite if
	// the conf has volinfo.OrientTimes set. No more
	// google.IsLoggedIn() gate — the pipeline runs as long as the
	// mailer is reachable.
	recipient := ics.Attendee{Email: vol.Email, Name: vol.Name}
	for _, shift := range vol.WorkShifts {
		if shift == nil || shift.ShiftTime == nil || shift.ShiftTime.End == nil {
			continue
		}
		if err := DispatchShiftICS(ctx, shift, conf, []ics.Attendee{recipient}, kindRequest, false); err != nil {
			ctx.Err.Printf("scheduled flow: cal invite failed for shift %s: %s", shift.Name, err)
		}
	}

	if volinfo != nil && volinfo.OrientTimes != nil && volinfo.OrientTimes.End != nil {
		if err := DispatchOrientICS(ctx, conf, recipient, volinfo.OrientTimes.Start, *volinfo.OrientTimes.End, volinfo.OrientLink); err != nil {
			ctx.Err.Printf("scheduled flow: orientation cal invite failed: %s", err)
		}
	}

	return nil
}

func VolAdmin(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}

	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	vols, err := getters.ListVolunteersForConf(ctx, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load volunteers", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/volcoord failed to get volunteers: %s", conf.Tag, err.Error())
		return
	}

	shifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
	if err != nil {
		http.Error(w, "Unable to load shifts", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/volcoord failed to get shifts: %s", conf.Tag, err.Error())
		return
	}

	// Populate WorkShifts for each volunteer
	for _, vol := range vols {
		vol.WorkShifts = getSelectedShifts(vol, shifts)
	}

	// Sort shifts by day and time, earliest first
	sort.SliceStable(shifts, func(i, j int) bool {
		a, b := shifts[i].ShiftTime, shifts[j].ShiftTime
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return a.Start.Before(b.Start)
	})

	// Sort volunteers by created time, most recent first
	sort.SliceStable(vols, func(i, j int) bool {
		a, b := vols[i].CreatedAt, vols[j].CreatedAt
		if a == nil {
			return false
		}
		if b == nil {
			return true
		}
		return a.Before(*b)
	})

	// Compute dashboard stats from the *unfiltered* volunteer list so
	// the headline numbers don't shift when admins click filter chips.
	stats := computeVolAdminStats(vols, shifts)
	allVols := vols

	statusFilter := r.URL.Query().Get("status")

	// Filter by status if requested
	if statusFilter != "" {
		var filtered []*types.Volunteer
		for _, vol := range vols {
			if vol.Status == statusFilter {
				filtered = append(filtered, vol)
			}
		}
		vols = filtered
	}

	missiveList, err := getters.ListOnlyForLetters(ctx)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord failed to load missives: %s", conf.Tag, err.Error())
		// continue without missives
	}

	volinfo, err := getters.GetVolInfo(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord failed to load volinfo: %s", conf.Tag, err.Error())
		// continue without volinfo
	}
	orientStartInput, orientEndInput := orientationInputValues(volinfo, conf)
	orientationRecipientCt := len(dedupeAttendees(append(scheduledVolunteerAttendees(allVols), orientationStaffRecipients(ctx, conf.Tag)...)))

	err = ctx.TemplateCache.ExecuteTemplate(w, "volunteers/admin.tmpl", &VolAdminPage{
		Conf:                   conf,
		Volunteers:             vols,
		Shifts:                 shifts,
		VolInfo:                volinfo,
		OrientationStartInput:  orientStartInput,
		OrientationEndInput:    orientEndInput,
		OrientationRecipientCt: orientationRecipientCt,
		StatusFilter:           statusFilter,
		Missives:               missiveList,
		FlashMessage:           r.URL.Query().Get("flash"),
		Year:                   helpers.CurrentYear(),
		DeclineTitle:           defaultVolDeclineTitle(conf),
		DeclineBody:            defaultVolDeclineBody(),
		Stats:                  stats,
		EmailCompose: &EmailComposeData{
			Title:            "Email Selected Volunteers",
			Description:      "Write a one-off email to volunteers. Uses Go template syntax.",
			TitlePlaceholder: "Subject line",
			BodyPlaceholder:  "Hi {{ .Volunteer.Name }},\n\nYour shifts for {{ .Conf.Desc }}...",
			Fields: []EmailFieldGroup{
				fieldGroup(".Volunteer", types.Volunteer{}, false),
				fieldGroup(".Conf", types.Conf{}, false),
				fieldGroup(".VolInfo", types.VolInfo{}, false),
			},
		},
	})
	if err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/volcoord template failed: %s", conf.Tag, err.Error())
	}
}

func VolAdminPromote(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
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
	targetStatus := r.FormValue("target_status")
	fromStatus := r.FormValue("from_status")

	if targetStatus == "" || fromStatus == "" {
		http.Error(w, "Missing status parameters", http.StatusBadRequest)
		return
	}

	vols, err := getters.ListVolunteersForConf(ctx, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load volunteers", http.StatusInternalServerError)
		return
	}

	promoted := 0
	for _, vol := range vols {
		if vol.Status != fromStatus {
			continue
		}

		err = getters.UpdateVolunteerStatus(ctx, vol.Ref, targetStatus)
		if err != nil {
			ctx.Err.Printf("/%s/volcoord/promote failed to update %s: %s", conf.Tag, vol.Name, err.Error())
			continue
		}

		// Send shift signup email when promoting to PendingShifts
		if targetStatus == "PendingShifts" {
			_, emailErr := emails.OnlyForVolSignup(ctx, vol, conf)
			if emailErr != nil {
				ctx.Err.Printf("/%s/volcoord/promote email failed for %s: %s", conf.Tag, vol.Email, emailErr)
			}
		}

		promoted++
	}

	// Redirect back to admin page
	http.Redirect(w, r, fmt.Sprintf("/%s/volcoord", conf.Tag), http.StatusSeeOther)
}

func VolAdminAutoAssign(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}

	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	vols, err := getters.ListVolunteersForConf(ctx, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load volunteers", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/volcoord/auto-assign failed to get volunteers: %s", conf.Tag, err.Error())
		return
	}

	shifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
	if err != nil {
		http.Error(w, "Unable to load shifts", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/volcoord/auto-assign failed to get shifts: %s", conf.Tag, err.Error())
		return
	}

	// Only consider PendingShifts volunteers; pre-populate their existing assignments
	var eligibleVols []*types.Volunteer
	for _, vol := range vols {
		if vol.Status != "PendingShifts" {
			continue
		}
		vol.WorkShifts = getSelectedShifts(vol, shifts)
		eligibleVols = append(eligibleVols, vol)
	}

	err = volunteers.AssignShifts(ctx, eligibleVols, shifts)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/auto-assign failed: %s", conf.Tag, err.Error())
	}

	http.Redirect(w, r, fmt.Sprintf("/%s/volcoord", conf.Tag), http.StatusSeeOther)
}

func VolunteerDecline(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, _, err := validateVolEmail(r, ctx)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	params := mux.Vars(r)
	confTag := params["conf"]

	volapps, err := getters.ListVolunteerApps(ctx, email)
	if err != nil {
		http.Error(w, "Unable to load volunteer", http.StatusInternalServerError)
		return
	}

	vol := findVolForConf(volapps, confTag)
	if vol == nil {
		http.Error(w, "Volunteer not found", http.StatusNotFound)
		return
	}

	if vol.Status == "Declined" {
		http.Error(w, "Already declined", http.StatusBadRequest)
		return
	}

	if len(vol.ScheduleFor) == 0 {
		http.Error(w, "No conference associated", http.StatusBadRequest)
		return
	}
	conf := vol.ScheduleFor[0]

	// Prevent cancellation within two weeks of conference start
	if vol.Status == "Scheduled" && conf.WithinTwoWeeks() {
		http.Error(w, "Cannot cancel shifts within two weeks of the conference. Please reach out to the organizers if you can no longer attend.", http.StatusBadRequest)
		return
	}

	// Release any shift assignments
	confShifts, err := getters.GetShiftsForConf(ctx, confTag)
	if err != nil {
		ctx.Err.Printf("/vols/shift/%s/decline failed to load shifts: %s", confTag, err.Error())
	} else {
		releaseVolunteerShifts(ctx, conf, vol, confShifts, "vols/shift/decline")
	}

	// Update status to Declined
	err = getters.UpdateVolunteerStatus(ctx, vol.Ref, "Declined")
	if err != nil {
		ctx.Err.Printf("/vols/shift/%s/decline status update failed: %s", confTag, err.Error())
	}

	// Send cancellation email
	_, err = emails.OnlyForVolCancel(ctx, vol, conf)
	if err != nil {
		ctx.Err.Printf("/vols/shift/%s/decline email failed: %s", confTag, err)
	}

	// Revoke their ticket if one was issued
	ctx.Infos.Printf("revoking ticket with id %s", vol.RegisID())
	err = getters.RevokeTicket(ctx, vol.RegisID())
	if err != nil {
		ctx.Err.Printf("/vols/shift/%s/decline ticket revoke failed: %s", confTag, err.Error())
	}

	// Redirect back to dashboard
	encodedHMAC := r.URL.Query().Get("hr")
	encodedEmail := r.URL.Query().Get("em")
	redirectURL := fmt.Sprintf("/vols/shift?hr=%s&em=%s", encodedHMAC, encodedEmail)
	http.Redirect(w, r, redirectURL, http.StatusSeeOther)
}

// volAdminLoadVol fetches a single volunteer for the conf, populates their
// volSelfRedirect returns the volunteer to their own shift signup page,
// preserving the HMAC + email query string.
func volSelfRedirect(w http.ResponseWriter, r *http.Request, confTag string) {
	encodedHMAC := r.URL.Query().Get("hr")
	encodedEmail := r.URL.Query().Get("em")
	http.Redirect(w, r, fmt.Sprintf("/vols/shift/%s?hr=%s&em=%s", confTag, encodedHMAC, encodedEmail), http.StatusSeeOther)
}

func VolunteerUpdateAvailability(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, _, err := validateVolEmail(r, ctx)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	params := mux.Vars(r)
	confTag := params["conf"]

	volapps, err := getters.ListVolunteerApps(ctx, email)
	if err != nil {
		http.Error(w, "Unable to load volunteer", http.StatusInternalServerError)
		return
	}
	vol := findVolForConf(volapps, confTag)
	if vol == nil {
		http.Error(w, "Volunteer not found", http.StatusNotFound)
		return
	}

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	var days []string
	for k := range r.PostForm {
		if strings.HasPrefix(k, "days-") {
			days = append(days, k[len("days-"):])
		}
	}

	err = getters.UpdateVolunteerAvailability(ctx, vol.Ref, days)
	if err != nil {
		ctx.Err.Printf("/vols/shift/%s/availability update failed: %s", confTag, err)
		http.Error(w, "Failed to update availability", http.StatusInternalServerError)
		return
	}

	volSelfRedirect(w, r, confTag)
}

func VolunteerUpdateWorkPrefs(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	email, _, err := validateVolEmail(r, ctx)
	if err != nil {
		http.Error(w, "Invalid credentials", http.StatusUnauthorized)
		return
	}

	params := mux.Vars(r)
	confTag := params["conf"]

	volapps, err := getters.ListVolunteerApps(ctx, email)
	if err != nil {
		http.Error(w, "Unable to load volunteer", http.StatusInternalServerError)
		return
	}
	vol := findVolForConf(volapps, confTag)
	if vol == nil {
		http.Error(w, "Volunteer not found", http.StatusNotFound)
		return
	}

	jobs := listJobs(w, ctx)
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	yesJobs := helpers.ParseFormJobs("yjob-", r.PostForm, jobs)
	noJobs := helpers.ParseFormJobs("njob-", r.PostForm, jobs)

	yesRefs := make([]string, len(yesJobs))
	for i, j := range yesJobs {
		yesRefs[i] = j.Ref
	}
	noRefs := make([]string, len(noJobs))
	for i, j := range noJobs {
		noRefs[i] = j.Ref
	}

	err = getters.UpdateVolunteerWorkPrefs(ctx, vol.Ref, yesRefs, noRefs)
	if err != nil {
		ctx.Err.Printf("/vols/shift/%s/work-prefs update failed: %s", confTag, err)
		http.Error(w, "Failed to update work preferences", http.StatusInternalServerError)
		return
	}

	volSelfRedirect(w, r, confTag)
}

// WorkShifts from the current shift assignments, and returns it. Used by
// every per-volunteer admin handler. Returns nil and writes an error response
// on failure.
func volAdminLoadVol(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) (*types.Conf, *types.Volunteer, []*types.WorkShift) {
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return nil, nil, nil
	}

	params := mux.Vars(r)
	volRef := params["volRef"]

	// Use a direct page fetch (strongly consistent) so the page reflects any
	// edits made in a preceding write within the same redirect chain. The
	// QueryDatabase index used by ListVolunteersForConf is eventually
	// consistent and can return stale results immediately after a PATCH.
	vol, err := getters.FetchVolunteer(ctx, volRef)
	if err != nil {
		http.Error(w, "Unable to load volunteer", http.StatusInternalServerError)
		ctx.Err.Printf("vol admin: failed to fetch vol %s: %s", volRef, err.Error())
		return nil, nil, nil
	}
	if vol == nil {
		handle404(w, r, ctx)
		return nil, nil, nil
	}

	shifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
	if err != nil {
		http.Error(w, "Unable to load shifts", http.StatusInternalServerError)
		ctx.Err.Printf("vol admin: failed to load shifts for %s: %s", conf.Tag, err.Error())
		return nil, nil, nil
	}
	vol.WorkShifts = getSelectedShifts(vol, shifts)

	return conf, vol, shifts
}

func VolAdminDetails(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}

	conf, vol, shifts := volAdminLoadVol(w, r, ctx)
	if vol == nil {
		return
	}

	// Build form helpers, marking current values as Checked
	jobs := listJobs(w, ctx)
	yesJobs := helpers.BuildJobs("yjob-", jobs, false)
	noJobs := helpers.BuildJobs("njob-", jobs, false)

	// Mark which jobs are currently in WorkYes/WorkNo
	yesSet := make(map[string]bool)
	for _, j := range vol.WorkYes {
		yesSet[j.Tag] = true
	}
	noSet := make(map[string]bool)
	for _, j := range vol.WorkNo {
		noSet[j.Tag] = true
	}
	for i := range yesJobs {
		yesJobs[i].Checked = yesSet[yesJobs[i].ItemID[len("yjob-"):]]
	}
	for i := range noJobs {
		noJobs[i].Checked = noSet[noJobs[i].ItemID[len("njob-"):]]
	}

	daysList := conf.DaysList("days-", true)
	availSet := make(map[string]bool)
	for _, d := range vol.Availability {
		availSet[d] = true
	}
	for i := range daysList {
		daysList[i].Checked = availSet[daysList[i].ItemID[len("days-"):]]
	}

	// Build shift selection display data (mirrors the volunteer's own selection page)
	selectedShifts := getSelectedShifts(vol, shifts)
	shiftDisplays := buildShiftDisplays(vol, shifts, selectedShifts)

	// Sorted day keys so the table renders chronologically
	dayKeys := make([]string, 0, len(shiftDisplays))
	for k := range shiftDisplays {
		dayKeys = append(dayKeys, k)
	}
	sort.Slice(dayKeys, func(i, j int) bool {
		return shiftDisplays[dayKeys[i]][0].Shift.ShiftTime.Start.Before(
			shiftDisplays[dayKeys[j]][0].Shift.ShiftTime.Start)
	})

	// Unique job types appearing in this conf's shifts (for the type filter)
	seenJobs := make(map[string]bool)
	var jobTypes []*types.JobType
	for _, s := range shifts {
		if s.Type == nil || seenJobs[s.Type.Tag] {
			continue
		}
		seenJobs[s.Type.Tag] = true
		jobTypes = append(jobTypes, s.Type)
	}
	sort.Slice(jobTypes, func(i, j int) bool {
		return jobTypes[i].Title < jobTypes[j].Title
	})

	err := ctx.TemplateCache.ExecuteTemplate(w, "volunteers/vol_details.tmpl", &VolDetailsPage{
		Conf:           conf,
		Vol:            vol,
		AllShifts:      shifts,
		ShiftDisplays:  shiftDisplays,
		SelectedShifts: selectedShifts,
		DayKeys:        dayKeys,
		JobTypes:       jobTypes,
		YesJobs:        yesJobs,
		NoJobs:         noJobs,
		DaysList:       daysList,
		Statuses:       []string{"Applied", "Waitlist", "PendingShifts", "Scheduled", "Declined"},
		Year:           helpers.CurrentYear(),
	})
	if err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("vol admin details template failed: %s", err.Error())
	}
}

// computeVolAdminStats sums shift capacity vs assignments and counts
// volunteers still in pre-scheduled states. VolsNeeded is ceil-divided
// by VolShiftQuota so a 15-spot gap with a 3-shift quota reads as 5
// vols needed (matching the user-facing wording).
func computeVolAdminStats(vols []*types.Volunteer, shifts []*types.WorkShift) *VolAdminStats {
	s := &VolAdminStats{}
	for _, sh := range shifts {
		if sh == nil {
			continue
		}
		s.ShiftsTotal += int(sh.MaxVols)
		assigned := len(sh.AssigneesRef)
		if assigned > int(sh.MaxVols) {
			assigned = int(sh.MaxVols)
		}
		s.ShiftsFilled += assigned
	}
	s.ShiftsLeft = s.ShiftsTotal - s.ShiftsFilled
	if s.ShiftsLeft < 0 {
		s.ShiftsLeft = 0
	}
	for _, v := range vols {
		if v == nil {
			continue
		}
		if v.Status == "Applied" || v.Status == "PendingShifts" {
			s.UnscheduledVols++
		}
	}
	if s.ShiftsLeft > 0 && VolShiftQuota > 0 {
		s.VolsNeeded = (s.ShiftsLeft + VolShiftQuota - 1) / VolShiftQuota
	}
	return s
}

func volAdminRedirect(w http.ResponseWriter, r *http.Request, conf *types.Conf, volRef string) {
	// Honor an optional `return` form value so callers (e.g. the admin
	// list page's quick-action buttons) can stay on their current view
	// instead of bouncing into the vol_details page. Only accept paths
	// rooted at /vols/admin/<this-conf>/ to avoid open-redirect.
	if ret := r.FormValue("return"); ret != "" {
		prefix := fmt.Sprintf("/%s/volcoord", conf.Tag)
		if strings.HasPrefix(ret, prefix+"/") || ret == prefix || strings.HasPrefix(ret, prefix+"?") {
			http.Redirect(w, r, ret, http.StatusSeeOther)
			return
		}
	}
	http.Redirect(w, r, fmt.Sprintf("/%s/volcoord/vol/%s", conf.Tag, volRef), http.StatusSeeOther)
}

func VolAdminUpdateStatus(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}

	conf, vol, shifts := volAdminLoadVol(w, r, ctx)
	if vol == nil {
		return
	}

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	status := r.FormValue("status")
	if status == "" {
		http.Error(w, "Missing status", http.StatusBadRequest)
		return
	}

	if status == "Declined" {
		releaseVolunteerShifts(ctx, conf, vol, shifts, "volcoord/status-decline")
	}

	err := getters.UpdateVolunteerStatus(ctx, vol.Ref, status)
	if err != nil {
		ctx.Err.Printf("vol admin update status failed: %s", err)
		http.Error(w, "Failed to update status", http.StatusInternalServerError)
		return
	}

	volAdminRedirect(w, r, conf, vol.Ref)
}

func VolAdminUpdateAvailability(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}

	conf, vol, _ := volAdminLoadVol(w, r, ctx)
	if vol == nil {
		return
	}

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	var days []string
	for k := range r.PostForm {
		if strings.HasPrefix(k, "days-") {
			days = append(days, k[len("days-"):])
		}
	}

	err := getters.UpdateVolunteerAvailability(ctx, vol.Ref, days)
	if err != nil {
		ctx.Err.Printf("vol admin update availability failed: %s", err)
		http.Error(w, "Failed to update availability", http.StatusInternalServerError)
		return
	}

	volAdminRedirect(w, r, conf, vol.Ref)
}

func VolAdminUpdateWorkPrefs(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}

	conf, vol, _ := volAdminLoadVol(w, r, ctx)
	if vol == nil {
		return
	}

	jobs := listJobs(w, ctx)
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	yesJobs := helpers.ParseFormJobs("yjob-", r.PostForm, jobs)
	noJobs := helpers.ParseFormJobs("njob-", r.PostForm, jobs)

	yesRefs := make([]string, len(yesJobs))
	for i, j := range yesJobs {
		yesRefs[i] = j.Ref
	}
	noRefs := make([]string, len(noJobs))
	for i, j := range noJobs {
		noRefs[i] = j.Ref
	}

	err := getters.UpdateVolunteerWorkPrefs(ctx, vol.Ref, yesRefs, noRefs)
	if err != nil {
		ctx.Err.Printf("vol admin update work prefs failed: %s", err)
		http.Error(w, "Failed to update work preferences", http.StatusInternalServerError)
		return
	}

	volAdminRedirect(w, r, conf, vol.Ref)
}

func VolAdminAddShift(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}

	conf, vol, _ := volAdminLoadVol(w, r, ctx)
	if vol == nil {
		return
	}

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	shiftRef := r.FormValue("shiftRef")
	if shiftRef == "" {
		http.Error(w, "Missing shiftRef", http.StatusBadRequest)
		return
	}

	err := getters.AssignVolunteerToShift(ctx, vol.Ref, shiftRef)
	if err != nil {
		ctx.Err.Printf("vol admin add shift failed: %s", err)
		http.Error(w, "Failed to add shift", http.StatusInternalServerError)
		return
	}

	volAdminRedirect(w, r, conf, vol.Ref)
}

func VolAdminRemoveShift(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}

	conf, vol, _ := volAdminLoadVol(w, r, ctx)
	if vol == nil {
		return
	}

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	shiftRef := r.FormValue("shiftRef")
	if shiftRef == "" {
		http.Error(w, "Missing shiftRef", http.StatusBadRequest)
		return
	}

	err := getters.RemoveVolunteerFromShift(ctx, vol.Ref, shiftRef)
	if err != nil {
		ctx.Err.Printf("vol admin remove shift failed: %s", err)
		http.Error(w, "Failed to remove shift", http.StatusInternalServerError)
		return
	}

	// CANCEL ICS for this volunteer's calendar entry — vol
	// admin removed them from the shift, so it shouldn't sit
	// on their calendar.
	cancelShiftCalForVol(ctx, vol, shiftRef, conf.Tag)

	volAdminRedirect(w, r, conf, vol.Ref)
}

func VolAdminMarkScheduled(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}

	conf, vol, _ := volAdminLoadVol(w, r, ctx)
	if vol == nil {
		return
	}

	if len(vol.WorkShifts) == 0 {
		http.Error(w, "Cannot schedule a volunteer with zero assigned shifts", http.StatusBadRequest)
		return
	}

	// Make sure ScheduleFor is set so runScheduledFlow has the conf
	if len(vol.ScheduleFor) == 0 {
		vol.ScheduleFor = []*types.Conf{conf}
	}

	err := runScheduledFlow(ctx, vol, conf)
	if err != nil {
		ctx.Err.Printf("vol admin mark scheduled failed for %s: %s", vol.Ref, err)
		http.Error(w, "Failed to schedule volunteer", http.StatusInternalServerError)
		return
	}

	volAdminRedirect(w, r, conf, vol.Ref)
}

func VolAdminBulkEmail(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
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
	volRefs := r.Form["vol_refs"]

	testEmail := r.FormValue("test_email")
	isTest := r.FormValue("send_test") == "1" && testEmail != ""

	if len(volRefs) == 0 && !isTest {
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=No+volunteers+selected", conf.Tag), http.StatusSeeOther)
		return
	}

	// Load volunteers + filter to selected
	allVols, err := getters.ListVolunteersForConf(ctx, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load volunteers", http.StatusInternalServerError)
		return
	}

	refSet := make(map[string]bool, len(volRefs))
	for _, ref := range volRefs {
		refSet[ref] = true
	}

	var targets []*types.Volunteer
	for _, v := range allVols {
		if refSet[v.Ref] {
			targets = append(targets, v)
		}
	}

	// Pre-load shifts and volinfo so each send can include shift context
	shifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/email failed to load shifts: %s", conf.Tag, err.Error())
	}
	for _, v := range targets {
		v.WorkShifts = getSelectedShifts(v, shifts)
	}

	volinfo, err := getters.GetVolInfo(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/email failed to load volinfo: %s", conf.Tag, err.Error())
	}

	sent := 0
	title := r.FormValue("title")
	body := r.FormValue("body")
	if title == "" || body == "" {
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=Title+and+body+required", conf.Tag), http.StatusSeeOther)
		return
	}

	if isTest {
		// Use first selected volunteer, or first available if none selected
		var testVol *types.Volunteer
		if len(targets) > 0 {
			testVol = targets[0]
		} else if len(allVols) > 0 {
			testVol = allVols[0]
		}
		if testVol == nil {
			http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=No+volunteers+available+for+test", conf.Tag), http.StatusSeeOther)
			return
		}
		tv := *testVol
		tv.Email = testEmail
		_, err := emails.SendCustomToVol(ctx, &tv, conf, volinfo, title, body)
		if err != nil {
			ctx.Err.Printf("/%s/volcoord/email test -> %s failed: %s", conf.Tag, testEmail, err)
			http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=Test+email+failed", conf.Tag), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=Test+sent+to+%s", conf.Tag, testEmail), http.StatusSeeOther)
		return
	}

	for _, v := range targets {
		_, err := emails.SendCustomToVol(ctx, v, conf, volinfo, title, body)
		if err != nil {
			ctx.Err.Printf("/%s/volcoord/email custom -> %s failed: %s", conf.Tag, v.Email, err)
			continue
		}
		sent++
	}

	flash := fmt.Sprintf("Sent+to+%d+of+%d+volunteers", sent, len(targets))
	http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, flash), http.StatusSeeOther)
}

func defaultVolDeclineTitle(conf *types.Conf) string {
	return fmt.Sprintf("Volunteer update for %s", conf.Desc)
}

func defaultVolDeclineBody() string {
	return "Hi {{ .Volunteer.Name }},\n\nThank you again for applying to volunteer at {{ .Conf.Desc }}. We had more volunteer interest than available shifts this time, so we are not able to add you to the volunteer roster for this event.\n\nWe would still love to have you join us as an attendee. You can use discount code `{{ .DiscountCode.CodeName }}` for a discounted ticket.\n\nThank you for being willing to help make bitcoin++ happen.\n\n- bitcoin++"
}

func VolAdminDeclineSelected(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
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
	volRefs := r.Form["vol_refs"]
	testEmail := strings.TrimSpace(r.FormValue("decline_test_email"))
	isTest := r.FormValue("decline_send_test") == "1"
	if isTest && testEmail == "" {
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("Test email is required")), http.StatusSeeOther)
		return
	}
	if len(volRefs) == 0 && !isTest {
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("No volunteers selected")), http.StatusSeeOther)
		return
	}

	title := strings.TrimSpace(r.FormValue("decline_title"))
	body := strings.TrimSpace(r.FormValue("decline_body"))
	if title == "" || body == "" {
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("Decline title and body are required")), http.StatusSeeOther)
		return
	}

	discount, err := validateVolDeclineDiscount(ctx, conf, r.FormValue("decline_discount_code"))
	if err != nil {
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape(err.Error())), http.StatusSeeOther)
		return
	}

	allVols, err := getters.ListVolunteersForConf(ctx, conf.Ref)
	if err != nil {
		http.Error(w, "Unable to load volunteers", http.StatusInternalServerError)
		return
	}

	shifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/decline-selected failed to load shifts: %s", conf.Tag, err.Error())
	}

	refSet := make(map[string]bool, len(volRefs))
	for _, ref := range volRefs {
		refSet[ref] = true
	}

	var targets []*types.Volunteer
	var firstEligible *types.Volunteer
	for _, v := range allVols {
		if !volBulkDeclineStatusAllowed(v.Status) {
			continue
		}
		v.WorkShifts = getSelectedShifts(v, shifts)
		if firstEligible == nil {
			firstEligible = v
		}
		if refSet[v.Ref] {
			targets = append(targets, v)
		}
	}

	volinfo, err := getters.GetVolInfo(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/decline-selected failed to load volinfo: %s", conf.Tag, err.Error())
	}

	if isTest {
		testVol := firstEligible
		if len(targets) > 0 {
			testVol = targets[0]
		}
		if testVol == nil {
			http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("No Applied or Pending Shifts volunteers available for test")), http.StatusSeeOther)
			return
		}
		tv := *testVol
		tv.Email = testEmail
		if _, err := emails.SendCustomToVolWithDiscount(ctx, &tv, conf, volinfo, discount, title, body); err != nil {
			ctx.Err.Printf("/%s/volcoord/decline-selected test -> %s failed: %s", conf.Tag, testEmail, err)
			http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("Test decline email failed")), http.StatusSeeOther)
			return
		}
		http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape("Test decline email sent to "+testEmail)), http.StatusSeeOther)
		return
	}

	sent := 0
	declined := 0
	for _, v := range targets {
		if _, err := emails.SendCustomToVolWithDiscount(ctx, v, conf, volinfo, discount, title, body); err != nil {
			ctx.Err.Printf("/%s/volcoord/decline-selected custom -> %s failed: %s", conf.Tag, v.Email, err)
			continue
		}
		sent++

		releaseVolunteerShifts(ctx, conf, v, shifts, "volcoord/decline-selected")
		if err := getters.UpdateVolunteerStatus(ctx, v.Ref, "Declined"); err != nil {
			ctx.Err.Printf("/%s/volcoord/decline-selected status %s failed: %s", conf.Tag, v.Email, err)
			continue
		}
		declined++
	}

	flash := fmt.Sprintf("Sent decline email to %d of %d selected volunteers. Moved %d to Declined.", sent, len(targets), declined)
	http.Redirect(w, r, fmt.Sprintf("/%s/volcoord?flash=%s", conf.Tag, url.QueryEscape(flash)), http.StatusSeeOther)
}

func volBulkDeclineStatusAllowed(status string) bool {
	return status == "Applied" || status == "PendingShifts"
}

func validateVolDeclineDiscount(ctx *config.AppContext, conf *types.Conf, code string) (*types.DiscountCode, error) {
	code = strings.TrimSpace(code)
	if code == "" {
		return nil, fmt.Errorf("Discount code is required")
	}
	discount, err := getters.FindDiscount(ctx, code)
	if err != nil {
		return nil, fmt.Errorf("Discount lookup failed: %w", err)
	}
	if discount == nil {
		return nil, fmt.Errorf("Discount code %q was not found", code)
	}
	if len(discount.ConfRef) > 0 {
		found := false
		for _, ref := range discount.ConfRef {
			if ref == conf.Ref {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("Discount code %q is not valid for %s", discount.CodeName, conf.Desc)
		}
	}
	if discount.MaxUses > 0 && discount.UsesCount >= discount.MaxUses {
		return nil, fmt.Errorf("Discount code %q has been fully redeemed", discount.CodeName)
	}
	if discount.IsDateExpired(time.Now().UTC()) {
		return nil, fmt.Errorf("Discount code %q is not active today", discount.CodeName)
	}
	return discount, nil
}

func releaseVolunteerShifts(ctx *config.AppContext, conf *types.Conf, vol *types.Volunteer, shifts []*types.WorkShift, label string) {
	selectedShifts := vol.WorkShifts
	if selectedShifts == nil {
		selectedShifts = getSelectedShifts(vol, shifts)
	}
	for _, shift := range selectedShifts {
		if shift == nil {
			continue
		}
		if err := getters.RemoveVolunteerFromShift(ctx, vol.Ref, shift.Ref); err != nil {
			ctx.Err.Printf("/%s/%s remove shift %s for %s failed: %s", conf.Tag, label, shift.Name, vol.Email, err)
			continue
		}
		if dErr := DispatchShiftICSCancelForVol(ctx, shift, conf, vol.Email, vol.Name); dErr != nil {
			ctx.Err.Printf("/%s/%s cancel-cal %q for %s: %s", conf.Tag, label, shift.Name, vol.Email, dErr)
		}
	}
}

// parseShiftFormTimes turns a date (YYYY-MM-DD or 01/02/2006) plus two HH:MM
// time strings into start/end time.Time values in the conference's timezone.
// End is rolled over to the next day if it's earlier than start (e.g. an
// overnight shift).
func parseShiftFormTimes(conf *types.Conf, dayStr, startStr, endStr string) (time.Time, time.Time, error) {
	// Accept either the legacy "01/02/2006" format or HTML date input "2006-01-02"
	loc := conf.Loc()
	var day time.Time
	var err error
	if t, e := time.ParseInLocation("2006-01-02", dayStr, loc); e == nil {
		day = t
	} else if t, e := time.ParseInLocation("01/02/2006", dayStr, loc); e == nil {
		day = t
	} else {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid date %q", dayStr)
	}

	startHM, err := time.Parse("15:04", startStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid start time %q", startStr)
	}
	endHM, err := time.Parse("15:04", endStr)
	if err != nil {
		return time.Time{}, time.Time{}, fmt.Errorf("invalid end time %q", endStr)
	}

	start := time.Date(day.Year(), day.Month(), day.Day(), startHM.Hour(), startHM.Minute(), 0, 0, loc)
	end := time.Date(day.Year(), day.Month(), day.Day(), endHM.Hour(), endHM.Minute(), 0, 0, loc)
	if !end.After(start) {
		end = end.Add(24 * time.Hour)
	}
	return start, end, nil
}

// findJobByTag locates a JobType by its Tag from a loaded job list.
func findJobByTag(jobs []*types.JobType, tag string) *types.JobType {
	for _, j := range jobs {
		if j.Tag == tag {
			return j
		}
	}
	return nil
}

func VolAdminShifts(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}

	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	shifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
	if err != nil {
		http.Error(w, "Unable to load shifts", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/volcoord/shifts failed to get shifts: %s", conf.Tag, err.Error())
		return
	}

	jobs, err := getters.ListJobTypes(ctx)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/shifts failed to fetch jobs: %s", conf.Tag, err.Error())
	}

	// Resolve all unique assignees → Volunteer for name display
	volMap := make(map[string]*types.Volunteer)
	allVols, err := getters.ListVolunteersForConf(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/shifts failed to load vols: %s", conf.Tag, err.Error())
	}
	for _, v := range allVols {
		volMap[v.Ref] = v
	}

	// Per-day ConfInfo strip — used below to widen the gantt's
	// MinHour/MaxHour bounds with doors-open / doors-close so a
	// coord can drag a shift earlier than the existing earliest
	// shift (e.g. into pre-doors setup time) without having to
	// edit the form. Best-effort: a load error degrades to bounds
	// based purely on shift times. We also compute a conf-wide
	// "fallback" doors range (widest window across all days that
	// DO have Doors set) so a day that's missing its own Doors row
	// still gets widened to the conf-wide venue-open window.
	infoByDay := map[string]*types.ConfInfo{}
	fallbackDoorsMin, fallbackDoorsMax := -1, -1
	if infos, err := getters.ListConfInfos(ctx, conf.Tag); err != nil {
		ctx.Err.Printf("/%s/volcoord/shifts list confinfos (continuing): %s", conf.Tag, err.Error())
	} else {
		for _, ci := range infos {
			if ci == nil || ci.Day < 1 {
				continue
			}
			key := dayDateFor(conf, ci.Day).Format("01/02/2006")
			infoByDay[key] = ci
			if ci.Doors == nil {
				continue
			}
			sH := ci.Doors.Start.Hour()
			if fallbackDoorsMin < 0 || sH < fallbackDoorsMin {
				fallbackDoorsMin = sH
			}
			if ci.Doors.End != nil {
				eH := ci.Doors.End.Hour()
				if ci.Doors.End.Minute() > 0 {
					eH++
				}
				if eH > fallbackDoorsMax {
					fallbackDoorsMax = eH
				}
			}
		}
	}

	// Group shifts by day
	groups := make(map[string]*ShiftDayGroup)
	for _, shift := range shifts {
		if shift.ShiftTime == nil {
			continue
		}
		day := shift.DayOf()
		g, ok := groups[day]
		if !ok {
			g = &ShiftDayGroup{
				Date:     day,
				DateDesc: shift.DayOfDesc(),
				MinHour:  24,
				MaxHour:  0,
			}
			groups[day] = g
		}
		g.Shifts = append(g.Shifts, shift)
		startH := shift.ShiftTime.Start.Hour()
		if startH < g.MinHour {
			g.MinHour = startH
		}
		if shift.ShiftTime.End != nil {
			endH := shift.ShiftTime.End.Hour()
			if shift.ShiftTime.End.Minute() > 0 {
				endH++
			}
			if endH > g.MaxHour {
				g.MaxHour = endH
			}
		}
	}

	// Sort each day's shifts and finalize hour ranges
	var dayList []*ShiftDayGroup
	for _, g := range groups {
		sort.Slice(g.Shifts, func(i, j int) bool {
			return g.Shifts[i].ShiftTime.Start.Before(g.Shifts[j].ShiftTime.Start)
		})
		// Widen bounds with doors-open / doors-close from this
		// day's ConfInfo so the gantt covers the full venue-open
		// window even when no shift currently touches the edges.
		// Per-day Doors win when set; otherwise fall back to the
		// conf-wide widest doors window so a day without its own
		// Doors row (common: only Day 1 has a ConfInfo entry)
		// still gets widened.
		dayMin, dayMax := -1, -1
		if ci := infoByDay[g.Date]; ci != nil && ci.Doors != nil {
			dayMin = ci.Doors.Start.Hour()
			if ci.Doors.End != nil {
				dayMax = ci.Doors.End.Hour()
				if ci.Doors.End.Minute() > 0 {
					dayMax++
				}
			}
		}
		if dayMin < 0 {
			dayMin = fallbackDoorsMin
		}
		if dayMax < 0 {
			dayMax = fallbackDoorsMax
		}
		if dayMin >= 0 && dayMin < g.MinHour {
			g.MinHour = dayMin
		}
		if dayMax >= 0 && dayMax > g.MaxHour {
			g.MaxHour = dayMax
		}
		// Pad ranges so the gantt has a little headroom (applied
		// after the doors merge so the result is min(shift, doors)
		// - 1 on the left and max(shift, doors) + 1 on the right).
		if g.MinHour > 0 {
			g.MinHour--
		}
		if g.MaxHour < 24 {
			g.MaxHour++
		}
		if g.MaxHour <= g.MinHour {
			g.MaxHour = g.MinHour + 1
		}
		dayList = append(dayList, g)
	}
	sort.Slice(dayList, func(i, j int) bool {
		return dayList[i].Shifts[0].ShiftTime.Start.Before(dayList[j].Shifts[0].ShiftTime.Start)
	})

	err = ctx.TemplateCache.ExecuteTemplate(w, "volunteers/admin_shifts.tmpl", &VolAdminShiftsPage{
		Conf:     conf,
		Days:     dayList,
		VolMap:   volMap,
		JobTypes: jobs,
		DaysList: conf.DaysList("days-", true),
		Year:     helpers.CurrentYear(),
	})
	if err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("/%s/volcoord/shifts template failed: %s", conf.Tag, err.Error())
	}
}

func volAdminShiftsRedirect(w http.ResponseWriter, r *http.Request, conf *types.Conf) {
	http.Redirect(w, r, fmt.Sprintf("/%s/volcoord/shifts", conf.Tag), http.StatusSeeOther)
}

func VolAdminCreateShift(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
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
	name := r.FormValue("name")
	jobTag := r.FormValue("job_type")
	day := r.FormValue("day")
	startStr := r.FormValue("start_time")
	endStr := r.FormValue("end_time")
	maxVols, _ := strconv.ParseUint(r.FormValue("max_vols"), 10, 32)
	priority, _ := strconv.ParseUint(r.FormValue("priority"), 10, 32)

	if name == "" {
		http.Error(w, "Name required", http.StatusBadRequest)
		return
	}

	start, end, err := parseShiftFormTimes(conf, day, startStr, endStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	jobType, _ := getters.GetJobByTag(ctx, jobTag)

	err = getters.CreateShift(ctx, conf, jobType, name, start, end, uint(maxVols), uint(priority))
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/shifts/new failed: %s", conf.Tag, err.Error())
		http.Error(w, "Failed to create shift: "+err.Error(), http.StatusInternalServerError)
		return
	}

	volAdminShiftsRedirect(w, r, conf)
}

func VolAdminUpdateShift(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}

	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	shiftRef := mux.Vars(r)["shiftRef"]

	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	name := r.FormValue("name")
	jobTag := r.FormValue("job_type")
	day := r.FormValue("day")
	startStr := r.FormValue("start_time")
	endStr := r.FormValue("end_time")
	maxVols, _ := strconv.ParseUint(r.FormValue("max_vols"), 10, 32)
	priority, _ := strconv.ParseUint(r.FormValue("priority"), 10, 32)

	if name == "" {
		http.Error(w, "Name required", http.StatusBadRequest)
		return
	}

	start, end, err := parseShiftFormTimes(conf, day, startStr, endStr)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}

	jobType, _ := getters.GetJobByTag(ctx, jobTag)

	err = getters.UpdateShift(ctx, shiftRef, name, jobType, start, end, uint(maxVols), uint(priority))
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/shifts/%s/update failed: %s", conf.Tag, shiftRef, err.Error())
		http.Error(w, "Failed to update shift: "+err.Error(), http.StatusInternalServerError)
		return
	}

	volAdminShiftsRedirect(w, r, conf)
}

func VolAdminDeleteShift(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}

	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	shiftRef := mux.Vars(r)["shiftRef"]
	if shiftRef == "" {
		http.Error(w, "shift required", http.StatusBadRequest)
		return
	}
	shifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
	if err != nil {
		ctx.Err.Printf("/%s/volcoord/shifts/%s/delete load shifts failed: %s", conf.Tag, shiftRef, err.Error())
		http.Error(w, "Unable to load shifts", http.StatusInternalServerError)
		return
	}
	found := false
	for _, shift := range shifts {
		if shift != nil && shift.Ref == shiftRef {
			found = true
			break
		}
	}
	if !found {
		http.Error(w, "shift not found", http.StatusNotFound)
		return
	}
	if err := getters.DeleteShift(ctx, shiftRef); err != nil {
		ctx.Err.Printf("/%s/volcoord/shifts/%s/delete failed: %s", conf.Tag, shiftRef, err.Error())
		http.Error(w, "Failed to delete shift: "+err.Error(), http.StatusInternalServerError)
		return
	}

	volAdminShiftsRedirect(w, r, conf)
}

// VolShiftReschedule handles drag/resize gestures on the gantt UI at
// /{conf}/volcoord/shifts. JSON body: {day, startMin, endMin}. Day is
// either "01/02/2006" (matches ShiftDayGroup.Date) or "2006-01-02";
// startMin/endMin are minutes from midnight in conf-local time. Only
// the ShiftTime property gets patched — Name / JobType / MaxVols /
// Priority / Assignees stay as-is so a concurrent edit-form save
// elsewhere doesn't get clobbered.
func VolShiftReschedule(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireConfVolcoord(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}
	shiftRef := mux.Vars(r)["shiftRef"]

	var req struct {
		Day      string `json:"day"`
		StartMin int    `json:"startMin"`
		EndMin   int    `json:"endMin"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "bad json", http.StatusBadRequest)
		return
	}
	if req.EndMin <= req.StartMin {
		http.Error(w, "endMin must be after startMin", http.StatusBadRequest)
		return
	}

	loc := conf.Loc()
	var day time.Time
	if t, e := time.ParseInLocation("01/02/2006", req.Day, loc); e == nil {
		day = t
	} else if t, e := time.ParseInLocation("2006-01-02", req.Day, loc); e == nil {
		day = t
	} else {
		http.Error(w, "bad day format", http.StatusBadRequest)
		return
	}
	start := day.Add(time.Duration(req.StartMin) * time.Minute)
	end := day.Add(time.Duration(req.EndMin) * time.Minute)

	if err := getters.UpdateShiftTimes(ctx, shiftRef, start, end); err != nil {
		ctx.Err.Printf("/%s/volcoord/shifts/%s/reschedule: %s", conf.Tag, shiftRef, err)
		http.Error(w, "update failed", http.StatusInternalServerError)
		return
	}

	// Auto-fire calendar update for every assignee on this
	// shift. Hash check inside dispatch suppresses email when
	// the time genuinely didn't change (same start/end after a
	// no-op drag); changed times bump SEQUENCE on the
	// assignees' calendars. Best-effort — log on error, don't
	// fail the schedule write.
	dispatchShiftCalAfterReschedule(ctx, conf, shiftRef)

	w.WriteHeader(http.StatusOK)
}

// dispatchShiftCalAfterReschedule looks up the freshly-updated
// shift, resolves its assignees to (email, name) attendees, and
// fans the cal-invite update to each via DispatchShiftICS. force=
// false so the hash check inside dispatch silently skips when
// times didn't actually move.
func dispatchShiftCalAfterReschedule(ctx *config.AppContext, conf *types.Conf, shiftRef string) {
	shifts, err := getters.GetShiftsForConf(ctx, conf.Tag)
	if err != nil {
		ctx.Err.Printf("shift cal-fire: load shifts %s: %s", conf.Tag, err)
		return
	}
	var shift *types.WorkShift
	for _, s := range shifts {
		if s != nil && s.Ref == shiftRef {
			shift = s
			break
		}
	}
	if shift == nil || shift.ShiftTime == nil || shift.ShiftTime.End == nil {
		return
	}
	if len(shift.AssigneesRef) == 0 {
		return
	}

	vols, err := getters.ListVolunteersForConf(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("shift cal-fire: load vols %s: %s", conf.Tag, err)
		return
	}
	volByRef := make(map[string]ics.Attendee, len(vols))
	for _, v := range vols {
		if v == nil || v.Email == "" {
			continue
		}
		volByRef[v.Ref] = ics.Attendee{Email: v.Email, Name: v.Name}
	}
	recipients := make([]ics.Attendee, 0, len(shift.AssigneesRef))
	for _, ref := range shift.AssigneesRef {
		if a, ok := volByRef[ref]; ok {
			recipients = append(recipients, a)
		}
	}
	if len(recipients) == 0 {
		return
	}
	if err := DispatchShiftICS(ctx, shift, conf, recipients, kindRequest, false); err != nil {
		ctx.Err.Printf("shift cal-fire %q: %s", shift.Name, err)
	}
}

// AdminGifts renders /{conf}/admin/gifts — the per-event Speaker
// Gifts list. Conf is in the URL (no dropdown), auth gated by
// requireConfStaff. Each row is one speaker (deduped — a speaker
// on multiple talks appears once, with the clipart from their
// "most interesting" talk: fewer co-speakers wins, so a solo keynote
// outranks a panel appearance). Ties break on first-encountered, with
// a non-empty clipart beating an empty one. {conf}-staff volunteers
// also appear, using the conf's leading.png as their gift clipart.
