package handlers

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/http/httptest"
	"net/url"
	"regexp"
	"strings"
	"testing"
	"time"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/eventplan"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
)

func TestTicketAttendeeDetailsLifecycle(t *testing.T) {
	app := wizardTestApp(t)
	c := context.Background()
	suffix := fmt.Sprintf("attendee-%d", time.Now().UnixNano())
	confID, err := getters.CreatePlannedEvent(app, eventplan.Basics{Tag: suffix, Name: "Attendee test", Edition: "global", Timezone: "UTC"}, "")
	if err != nil {
		t.Fatal(err)
	}
	conf, _ := getters.GetConfByRef(app, confID)
	owner, err := getters.CreateSpeaker(app, getters.SpeakerInput{Name: "Buyer", Email: suffix + "@example.test", AttendeePreferences: &types.AttendeePreferences{TShirt: "MM", Diet: "other", DietOther: "Kosher", Allergies: "Shellfish"}})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		app.DB.Exec(c, `DELETE FROM conferences WHERE id=$1`, confID)
		app.DB.Exec(c, `DELETE FROM people WHERE id=$1`, owner)
	})
	refs := []string{suffix + "-1", suffix + "-2", suffix + "-3"}
	for i, ref := range refs {
		kind := "supporter"
		if i == 2 {
			kind = "genpop"
		}
		if _, err = app.DB.Exec(c, `INSERT INTO registrations(ref_id,checkout_id,conference_id,type,email,person_id) VALUES($1,$2,$3,$6,$4,$5)`, ref, suffix, confID, suffix+"@example.test", owner, kind); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = app.DB.Exec(c, `UPDATE people SET norm_photo_path='booklet-test.png',twitter_handle='builder' WHERE id=$1`, owner); err != nil {
		t.Fatal(err)
	}
	anchor, _ := getters.GetTicketAttendeeDetails(app, refs[0])
	link := helpers.TicketDetailsURL(app, anchor)
	var cookie *http.Cookie
	request := func(path, method string, values url.Values, login bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(values.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		if cookie != nil {
			req.AddCookie(cookie)
		}
		ref := strings.Split(strings.TrimPrefix(req.URL.Path, "/ticket/"), "/")[0]
		req = mux.SetURLVars(req, map[string]string{"ticket": ref})
		w := httptest.NewRecorder()
		app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if login {
				if e := auth.LoginPerson(app, r, owner, auth.MethodEmailLink); e != nil {
					t.Fatal(e)
				}
			}
			TicketAttendeeDetails(w, r, app)
		})).ServeHTTP(w, req)
		for _, ck := range w.Result().Cookies() {
			if ck.Name == app.Session.Cookie.Name {
				cookie = ck
			}
		}
		return w
	}
	if w := request("/ticket/"+refs[0]+"/details?key=wrong", "GET", nil, false); w.Code != 404 {
		t.Fatal("forged key", w.Code)
	}
	page := request(link, "GET", nil, false)
	if page.Code != 200 || !strings.Contains(page.Body.String(), "Ticket 3") || strings.Contains(page.Body.String(), `value="MM" selected`) {
		t.Fatal("group prefill/render", page.Code, page.Body.String())
	}
	if page.Header().Get("Cache-Control") != "private, no-store" || page.Header().Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("private response headers missing")
	}
	if strings.Count(page.Body.String(), "data-booklet-ticket=") != 2 || strings.Count(page.Body.String(), `value="named" selected`) != 2 || strings.Contains(page.Body.String(), "Decide later") {
		t.Fatal("booklet form must be supporter-only with named/private choices", page.Body.String())
	}
	if anchor.BookletChoice != "" || anchor.BookletUseAvatar {
		t.Fatal("displaying the form must not publish an acknowledgement or opt in an avatar")
	}
	csrfMatch := regexp.MustCompile(`name="csrf" value="([^"]+)"`).FindStringSubmatch(page.Body.String())
	if len(csrfMatch) != 2 {
		t.Fatal("csrf missing")
	}
	csrf := html.UnescapeString(csrfMatch[1])
	values := func() url.Values {
		v := url.Values{"csrf": {csrf}, "ticket": refs}
		for _, ref := range refs {
			d, e := getters.GetTicketAttendeeDetails(app, ref)
			if e != nil {
				t.Fatal(e)
			}
			v.Set("version_"+ref, fmt.Sprint(d.Version))
		}
		return v
	}
	for _, ref := range refs {
		d, e := getters.GetRegistrationCheckIn(app, ref)
		if e != nil || d.TShirtSize != "" {
			t.Fatal("buyer size copied to group", d, e)
		}
	}
	signedIn := request(link, "GET", nil, true)
	if signedIn.Code != 200 || strings.Count(signedIn.Body.String(), `value="MM" selected`) != 1 || strings.Count(signedIn.Body.String(), `value="other" selected`) != 1 || !strings.Contains(signedIn.Body.String(), `value="Kosher"`) || !strings.Contains(signedIn.Body.String(), `value="Shellfish" data-attendee-allergies>`) {
		t.Fatal("signed-in defaults must populate exactly one ticket", signedIn.Code, signedIn.Body.String())
	}
	v := values()
	v.Set("shirt_"+refs[0], "ML")
	v.Set("name_"+refs[0], "Buyer")
	v.Set("mine_"+refs[0], "1")
	v.Set("diet_"+refs[0], "vegan")
	v.Set("shirt_"+refs[1], "LM")
	v.Set("name_"+refs[1], "<Guest>")
	v.Set("diet_"+refs[1], "vegetarian")
	v.Set("allergies_"+refs[1], "Peanuts")
	v.Set("shirt_"+refs[2], "MXL")
	v.Set("diet_"+refs[2], "other")
	v.Set("other_"+refs[2], "Pescatarian")
	v.Set("booklet_"+refs[0], "named")
	v.Set("booklet_name_"+refs[0], " @builder ")
	v.Set("booklet_avatar_"+refs[0], "1")
	v.Set("booklet_"+refs[1], "named")
	v.Set("booklet_name_"+refs[1], "<Guest supporter>")
	v.Set("booklet_avatar_"+refs[1], "1") // A guest cannot borrow the buyer's photo.
	v.Set("booklet_"+refs[2], "named")    // A standard ticket cannot gain acknowledgement fields.
	v.Set("booklet_name_"+refs[2], "Forged supporter")
	v.Set("booklet_avatar_"+refs[2], "1")
	bad := url.Values{}
	for k, vs := range v {
		bad[k] = append([]string{}, vs...)
	}
	bad.Set("csrf", "forged")
	if w := request(link, "POST", bad, false); w.Code != 403 {
		t.Fatal("csrf accepted", w.Code)
	}
	bad.Set("csrf", csrf)
	bad.Set("shirt_"+refs[2], "invalid")
	if w := request(link, "POST", bad, false); w.Code != 422 {
		t.Fatal("invalid shirt accepted", w.Code)
	}
	unchanged, _ := getters.GetTicketAttendeeDetails(app, refs[0])
	if unchanged.Version != 0 || unchanged.TShirt != "" {
		t.Fatal("invalid bulk save was not atomic")
	}
	bad.Set("shirt_"+refs[2], "MXL")
	for _, invalidName := range []string{"", strings.Repeat("a", 101), "two\nlines"} {
		bad.Set("booklet_name_"+refs[1], invalidName)
		if w := request(link, "POST", bad, false); w.Code != 422 {
			t.Fatal("invalid booklet name accepted", w.Code)
		}
		unchanged, _ = getters.GetTicketAttendeeDetails(app, refs[0])
		if unchanged.Version != 0 || unchanged.BookletChoice != "" {
			t.Fatal("invalid acknowledgement partially saved")
		}
	}
	bad.Set("booklet_name_"+refs[1], "Guest")
	bad.Set("booklet_"+refs[1], "invented")
	if w := request(link, "POST", bad, false); w.Code != 422 {
		t.Fatal("invalid booklet choice accepted", w.Code)
	}
	if w := request(link, "POST", v, false); w.Code != 303 {
		t.Fatal("save", w.Code, w.Body.String())
	}
	if w := request(link, "POST", v, false); w.Code != 409 {
		t.Fatal("stale save accepted", w.Code)
	}
	loaded := request(link, "GET", nil, false)
	if !strings.Contains(loaded.Body.String(), "&lt;Guest&gt;") {
		t.Fatal("guest name not escaped")
	}
	if !strings.Contains(loaded.Body.String(), "&lt;Guest supporter&gt;") || !strings.Contains(loaded.Body.String(), `href="/dashboard/profile" target="_blank"`) {
		t.Fatal("booklet text escaping or profile link missing")
	}
	booklet, _ := getters.GetTicketAttendeeDetails(app, refs[0])
	guestBooklet, _ := getters.GetTicketAttendeeDetails(app, refs[1])
	standardBooklet, _ := getters.GetTicketAttendeeDetails(app, refs[2])
	if booklet.BookletName != "@builder" || booklet.BookletChoice != "named" || booklet.BookletAvatar() != "booklet-test.png" || booklet.ProfileName != "Buyer" {
		t.Fatal("own supporter details or profile isolation", booklet)
	}
	if guestBooklet.BookletUseAvatar || guestBooklet.BookletAvatar() != "" || standardBooklet.BookletChoice != "" || standardBooklet.BookletName != "" || standardBooklet.BookletUseAvatar {
		t.Fatal("guest or standard ticket inherited supporter identity", guestBooklet, standardBooklet)
	}
	check, e := getters.GetRegistrationCheckIn(app, refs[1])
	if e != nil || check.TShirtSize != "LM" || check.AttendeeName != "<Guest>" {
		t.Fatal("check-in details", check, e)
	}
	checkHTML := httptest.NewRecorder()
	if err = app.TemplateCache.ExecuteTemplate(checkHTML, "checkin.tmpl", &CheckInPage{TicketRef: refs[1], TicketType: "supporter", AttendeeName: check.AttendeeName, TShirtSize: check.TShirtSize}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(checkHTML.Body.String(), "<dd>M</dd>") || strings.Contains(checkHTML.Body.String(), "<dt>Food</dt>") || strings.Contains(checkHTML.Body.String(), "Allergies / food needs") {
		t.Fatal("check-in must show shirt size without food details", checkHTML.Body.String())
	}
	dashboardDetails, err := getters.PersonTicketAttendeeDetails(app, owner)
	if err != nil || dashboardDetails[refs[1]].Diet != "vegetarian" || dashboardDetails[refs[1]].Allergies != "Peanuts" {
		t.Fatal("private dashboard preferences", dashboardDetails, err)
	}
	dashboardHTML := httptest.NewRecorder()
	regs, err := getters.ListRegistrationsForPerson(app, owner)
	if err != nil {
		t.Fatal(err)
	}
	if err = app.TemplateCache.ExecuteTemplate(dashboardHTML, "dashboard.tmpl", &DashboardPage{Stats: &DashboardStats{}, TicketDetails: dashboardDetails, ActiveBlocks: []*EventBlock{{Conf: conf, Tickets: regs}}}); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(dashboardHTML.Body.String(), "<dd>M</dd>") || !strings.Contains(dashboardHTML.Body.String(), "Vegetarian") || !strings.Contains(dashboardHTML.Body.String(), "Peanuts") || !strings.Contains(dashboardHTML.Body.String(), "/ticket/"+refs[1]+"/details") {
		t.Fatal("dashboard must expose each ticket's preferences and editing link", dashboardHTML.Body.String())
	}
	app.DB.Exec(c, `UPDATE registrations SET checked_in_at=now() WHERE ref_id=$1`, refs[1])
	if e = getters.MarkTicketPickups(app, refs[1], nil, true, "staff@example.test"); e != nil {
		t.Fatal("per-ticket shirt pickup", e)
	}
	var profileShirt string
	app.DB.QueryRow(c, `SELECT tshirt FROM people WHERE id=$1`, owner).Scan(&profileShirt)
	if profileShirt != "ML" {
		t.Fatal("own profile not updated", profileShirt)
	}
	profile, err := getters.GetPersonAttendeePreferences(app, owner)
	if err != nil || profile.Diet != "vegan" || profile.DietOther != "" || profile.Allergies != "" {
		t.Fatal("guest preferences must not replace personal defaults", profile, err)
	}
	all, _ := getters.ConferenceAttendeeDetails(app, confID)
	summary := summarizeAttendees(all)
	if len(summary.Shirts) != 3 || len(summary.Meals) != 3 || summary.MissingShirts != 0 {
		t.Fatal("totals", summary)
	}
	for _, choice := range []string{"private", "", "named"} {
		group, e := getters.TicketAttendeeGroup(app, anchor)
		if e != nil {
			t.Fatal(e)
		}
		group[0].BookletChoice, group[0].BookletName, group[0].BookletUseAvatar = choice, "@builder", true
		if e = getters.SaveTicketAttendeeDetails(app, anchor, group); e != nil {
			t.Fatal(e)
		}
		got, _ := getters.GetTicketAttendeeDetails(app, refs[0])
		if choice != "named" && (got.BookletName != "" || got.BookletUseAvatar || got.BookletAvatar() != "") {
			t.Fatal("private/pending choice retained publication data", got)
		}
	}
	// A typed checkout email grants no edit link, even when paid tickets exist.
	app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app.Session.Put(r.Context(), checkoutEmailSessionKey(conf.Tag), suffix+"@example.test")
		if completedTicketDetails(app, r, conf) != nil {
			t.Fatal("email authorized edit")
		}
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/"+suffix+"/success", nil))
	var ticketID, reservationID string
	if err = app.DB.QueryRow(c, `INSERT INTO conference_tickets(conference_id,ticket_key,tier) VALUES($1,'test','Standard') RETURNING id::text`, confID).Scan(&ticketID); err != nil {
		t.Fatal(err)
	}
	if err = app.DB.QueryRow(c, `INSERT INTO ticket_reservations(conference_id,ticket_id,quantity,email,provider,provider_id) VALUES($1,$2,3,$3,'stripe',$4) RETURNING id::text`, confID, ticketID, suffix+"@example.test", suffix).Scan(&reservationID); err != nil {
		t.Fatal(err)
	}
	for _, fulfilled := range []bool{false, true} {
		if fulfilled {
			app.DB.Exec(c, `UPDATE ticket_reservations SET status='fulfilled' WHERE id=$1`, reservationID)
		}
		app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			app.Session.Put(r.Context(), ticketDetailsReservationKey(conf.Tag), reservationID)
			got := completedTicketDetails(app, r, conf)
			if (got != nil) != fulfilled {
				t.Fatal("payment return authorization", fulfilled, got)
			}
		})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/"+suffix+"/success", nil))
		result := httptest.NewRecorder()
		app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			app.Session.Put(r.Context(), ticketDetailsReservationKey(conf.Tag), reservationID)
			RenderConfSuccess(w, mux.SetURLVars(r, map[string]string{"conf": conf.Tag}), app)
		})).ServeHTTP(result, httptest.NewRequest("GET", "/"+suffix+"/success", nil))
		if fulfilled {
			if result.Code != 200 || !strings.Contains(result.Body.String(), `class="checkout-shell checkout-shell--success"`) || !strings.Contains(result.Body.String(), `action="/`+suffix+`/success" data-attendee-form`) || !strings.Contains(result.Body.String(), `data-current="confirmed"`) {
				t.Fatal("confirmed return did not embed the attendee form", result.Code, result.Body.String())
			}
		} else if result.Code != 200 || !strings.Contains(result.Body.String(), "data-ticket-confirmation") || strings.Contains(result.Body.String(), "Payment received") {
			t.Fatal("pending confirmation", result.Code, result.Body.String())
		}
	}
	// The final checkout step must save in place with the same authorization,
	// CSRF and revision protection as the private ticket-details entry point.
	finish := func(path, method string, form url.Values, authorized bool) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(method, path, strings.NewReader(form.Encode()))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		req.AddCookie(cookie)
		w := httptest.NewRecorder()
		app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			app.Session.Remove(r.Context(), ticketDetailsReservationKey(conf.Tag))
			if authorized {
				app.Session.Put(r.Context(), ticketDetailsReservationKey(conf.Tag), reservationID)
			}
			RenderConfSuccess(w, mux.SetURLVars(r, map[string]string{"conf": conf.Tag}), app)
		})).ServeHTTP(w, req)
		return w
	}
	successPath := "/" + suffix + "/success"
	if result := finish(successPath+"?check_details=1&lang=en", "GET", nil, true); result.Code != 200 || !strings.Contains(result.Body.String(), `"details_url":"`+successPath+`?lang=en"`) {
		t.Fatal("confirmation polling must return to the last checkout step", result.Code, result.Body.String())
	}
	if result := finish(successPath+"?session_id=unrelated", "GET", nil, true); strings.Contains(result.Body.String(), "data-attendee-form") {
		t.Fatal("unrelated checkout exposed attendee form")
	}
	for key, value := range values() {
		v[key] = value
	}
	if result := finish(successPath, "POST", v, false); result.Code != 403 {
		t.Fatal("account/email alone authorized a checkout form save", result.Code)
	}
	v.Set("csrf", "forged")
	if result := finish(successPath, "POST", v, true); result.Code != 403 {
		t.Fatal("checkout accepted forged CSRF", result.Code)
	}
	v.Set("csrf", csrf)
	if result := finish(successPath, "POST", v, true); result.Code != 303 || result.Header().Get("Location") != successPath+"?saved=1" {
		t.Fatal("checkout form did not save in place", result.Code, result.Body.String())
	}
	if result := finish(successPath+"?saved=1", "GET", nil, true); result.Code != 200 || !strings.Contains(result.Body.String(), "Details saved.") || !strings.Contains(result.Body.String(), `value="&lt;Guest&gt;"`) {
		t.Fatal("checkout form did not retain saved answers", result.Code, result.Body.String())
	}
	if result := finish(successPath, "POST", v, true); result.Code != 409 {
		t.Fatal("checkout form accepted stale revision", result.Code)
	}
	// An unrelated recipient's answers must never remain visible on transfer.
	oldGuest, _ := getters.GetTicketAttendeeDetails(app, refs[1])
	oldGuestLink := helpers.TicketDetailsURL(app, oldGuest)
	staleGroup := values()
	if _, err = app.DB.Exec(c, `UPDATE registrations SET email='recipient@example.test' WHERE ref_id=$1`, refs[1]); err != nil {
		t.Fatal(err)
	}
	if w := request(oldGuestLink, "GET", nil, false); w.Code != 404 {
		t.Fatal("transferred edit link still works", w.Code)
	}
	transferred, _ := getters.GetTicketAttendeeDetails(app, refs[1])
	if transferred.Name != "" || transferred.TShirt != "" || transferred.Diet != "" || transferred.Allergies != "" || transferred.AccessKey == oldGuest.AccessKey || transferred.BookletChoice != "" || transferred.BookletName != "" || transferred.BookletUseAvatar {
		t.Fatal("transfer retained preferences", transferred)
	}
	if w := request(link, "POST", staleGroup, false); w.Code != 409 {
		t.Fatal("stale group saved over transfer", w.Code)
	}
	group, _ := getters.TicketAttendeeGroup(app, anchor)
	if len(group) != 2 {
		t.Fatal("transferred ticket in buyer group", len(group))
	}
	dashboardDetails, err = getters.PersonTicketAttendeeDetails(app, owner)
	if err != nil || dashboardDetails[refs[1]] != nil {
		t.Fatal("transferred preferences remained on old dashboard", err)
	}
	if w := request("/ticket/"+refs[0]+"/details", "GET", nil, true); w.Code != 200 {
		t.Fatal("dashboard owner access", w.Code)
	}
	if w := request("/ticket/"+refs[1]+"/details", "GET", nil, true); w.Code != 404 {
		t.Fatal("old account retained transferred access", w.Code)
	}
	if _, err = app.DB.Exec(c, `UPDATE registrations SET revoked=true WHERE ref_id=$1`, refs[2]); err != nil {
		t.Fatal(err)
	}
	all, _ = getters.ConferenceAttendeeDetails(app, confID)
	summary = summarizeAttendees(all)
	if len(all) != 2 || summary.MissingMeals != 1 || len(summary.Meals) != 1 {
		t.Fatal("revoked/transfer totals", summary)
	}
	// The original checkout browser cannot mint a new link for a transferred ticket.
	app.DB.Exec(c, `UPDATE registrations SET email='recipient@example.test',person_id=NULL WHERE ref_id=$1`, refs[0])
	transferredOwner, _ := getters.GetTicketAttendeeDetails(app, refs[0])
	if transferredOwner.BookletChoice != "" || transferredOwner.BookletName != "" || transferredOwner.BookletUseAvatar || transferredOwner.BookletAvatar() != "" {
		t.Fatal("transferred supporter retained acknowledgement or avatar", transferredOwner)
	}
	app.Session.LoadAndSave(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		app.Session.Put(r.Context(), ticketDetailsReservationKey(conf.Tag), reservationID)
		if completedTicketDetails(app, r, conf) != nil {
			t.Fatal("old checkout browser regained transferred tickets")
		}
	})).ServeHTTP(httptest.NewRecorder(), httptest.NewRequest("GET", "/"+suffix+"/success", nil))
	// Direct ownership/admin changes must obey the same publication rules.
	for _, ref := range refs[:2] {
		if _, err = app.DB.Exec(c, `UPDATE registrations SET booklet_choice='named',booklet_name='Reissued supporter' WHERE ref_id=$1`, ref); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = app.DB.Exec(c, `UPDATE registrations SET revoked=true WHERE ref_id=$1`, refs[0]); err != nil {
		t.Fatal(err)
	}
	var choice, name string
	var avatar bool
	if err = app.DB.QueryRow(c, `SELECT booklet_choice,booklet_name,booklet_use_avatar FROM registrations WHERE ref_id=$1`, refs[0]).Scan(&choice, &name, &avatar); err != nil || choice != "" || name != "" || avatar {
		t.Fatal("revocation retained supporter acknowledgement", choice, name, avatar, err)
	}
	if _, err = app.DB.Exec(c, `UPDATE registrations SET type='genpop' WHERE ref_id=$1`, refs[1]); err != nil {
		t.Fatal(err)
	}
	standard, err := getters.GetTicketAttendeeDetails(app, refs[1])
	if err != nil || standard.BookletChoice != "" || standard.BookletName != "" || standard.BookletUseAvatar {
		t.Fatal("downgraded ticket retained acknowledgement", standard, err)
	}
	withoutSupporter := request(helpers.TicketDetailsURL(app, standard), "GET", nil, false)
	if withoutSupporter.Code != 200 || strings.Contains(withoutSupporter.Body.String(), "data-booklet-ticket=") {
		t.Fatal("non-supporter order rendered booklet form", withoutSupporter.Code)
	}
}
