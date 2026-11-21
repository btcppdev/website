package handlers

import (
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"btcpp-web/external/getters"
	"btcpp-web/internal/auth"
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
	"github.com/gorilla/mux"
	"github.com/jackc/pgx/v5"
)

type attendeeDetailsPage struct {
	Conf                *types.Conf
	Rows                []*types.TicketAttendeeDetails
	CSRF, Action, Error string
	Saved               bool
	HasAddOns           bool
	Year                uint
}

func (p *attendeeDetailsPage) ReadyCount() int {
	n := 0
	for _, row := range p.Rows {
		if row.Ready() {
			n++
		}
	}
	return n
}

func (p *attendeeDetailsPage) HasSupporters() bool {
	for _, row := range p.Rows {
		if row.IsSupporter() {
			return true
		}
	}
	return false
}

func TicketAttendeeDetails(w http.ResponseWriter, r *http.Request, app *config.AppContext) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	anchor, err := getters.GetTicketAttendeeDetails(app, mux.Vars(r)["ticket"])
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			http.NotFound(w, r)
		} else {
			http.Error(w, "Unable to load ticket details", 500)
		}
		return
	}
	key := r.URL.Query().Get("key")
	if key != "" {
		if !secureTokenEqual(helpers.TicketDetailsSignature(app, anchor), key) {
			http.NotFound(w, r)
			return
		}
	} else {
		identity, e := auth.Resolve(r, app)
		if e != nil {
			http.Error(w, "Unable to verify account", 503)
			return
		}
		if identity == nil {
			http.Redirect(w, r, "/login?next="+url.QueryEscape(r.URL.RequestURI()), http.StatusSeeOther)
			return
		}
		if identity.PersonID == "" || identity.PersonID != anchor.OwnerPersonID {
			http.NotFound(w, r)
			return
		}
	}
	renderTicketAttendeeDetails(w, r, app, anchor)
}

// Called only after ticket-link/account authorization or a verified completed
// checkout reservation. Both entry points use the same form and save handler.
func renderTicketAttendeeDetails(w http.ResponseWriter, r *http.Request, app *config.AppContext, anchor *types.TicketAttendeeDetails) {
	w.Header().Set("Cache-Control", "private, no-store")
	w.Header().Set("Referrer-Policy", "no-referrer")
	w.Header().Set("X-Robots-Tag", "noindex, nofollow")
	conf, err := getters.GetConfByRef(app, anchor.ConferenceID)
	if err != nil || conf == nil {
		http.NotFound(w, r)
		return
	}
	rows, err := getters.TicketAttendeeGroup(app, anchor)
	if err != nil {
		http.Error(w, "Unable to load ticket details", 500)
		return
	}
	csrf, err := ensureAuthMethodsCSRF(app, r)
	if err != nil {
		http.Error(w, "Unable to prepare form", 500)
		return
	}
	page := &attendeeDetailsPage{Conf: conf, Rows: rows, CSRF: csrf, Action: r.URL.RequestURI(), Saved: r.URL.Query().Get("saved") == "1", HasAddOns: app.Session.GetBool(r.Context(), checkoutAddOnsSessionKey(conf.Tag)), Year: helpers.CurrentYear()}
	status := http.StatusOK
	if r.Method == http.MethodPost {
		limitRequestBody(w, r, maxFormBodyBytes)
		if err = r.ParseForm(); err != nil || !secureTokenEqual(csrf, r.PostForm.Get("csrf")) {
			http.Error(w, "Invalid form; reload and try again", 403)
			return
		}
		refs := r.PostForm["ticket"]
		if len(refs) != len(rows) {
			http.Error(w, "Tickets changed; reload and try again", 409)
			return
		}
		byRef := map[string]*types.TicketAttendeeDetails{}
		for _, row := range rows {
			byRef[row.RefID] = row
		}
		var input []*types.TicketAttendeeDetails
		for _, ref := range refs {
			old, ok := byRef[ref]
			if !ok {
				http.Error(w, "Invalid ticket", 400)
				return
			}
			delete(byRef, ref)
			version, e := strconv.ParseInt(r.PostForm.Get("version_"+ref), 10, 64)
			if e != nil {
				http.Error(w, "Invalid ticket revision", 400)
				return
			}
			row := *old
			row.Version = version
			row.Name = r.PostForm.Get("name_" + ref)
			row.TShirt = r.PostForm.Get("shirt_" + ref)
			row.Diet = r.PostForm.Get("diet_" + ref)
			row.DietOther = r.PostForm.Get("other_" + ref)
			row.Allergies = r.PostForm.Get("allergies_" + ref)
			row.IsBuyer = r.PostForm.Get("mine_"+ref) == "1"
			row.BookletChoice = r.PostForm.Get("booklet_" + ref)
			row.BookletName = r.PostForm.Get("booklet_name_" + ref)
			row.BookletUseAvatar = r.PostForm.Get("booklet_avatar_"+ref) == "1"
			input = append(input, &row)
		}
		if err = getters.SaveTicketAttendeeDetails(app, anchor, input); err == nil {
			u := *r.URL
			q := u.Query()
			q.Set("saved", "1")
			u.RawQuery = q.Encode()
			http.Redirect(w, r, u.String(), http.StatusSeeOther)
			return
		}
		page.Error = err.Error()
		page.Saved = false
		page.Rows = input
		status = http.StatusUnprocessableEntity
		if errors.Is(err, getters.ErrAttendeeDetailsConflict) {
			status = http.StatusConflict
		}
	} else {
		identity, err := auth.Resolve(r, app)
		if err != nil {
			http.Error(w, "Unable to load account preferences", http.StatusServiceUnavailable)
			return
		}
		personID := ""
		if identity != nil {
			personID = identity.PersonID
		}
		prefillTicketAttendeeDefaults(rows, personID)
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Content-Language", checkoutLocale(r, conf))
	w.WriteHeader(status)
	if err = renderCheckout(w, r, app, "attendee_details.tmpl", page, conf); err != nil {
		app.Err.Printf("render attendee details: %s", err)
	}
}

// Only a new, untouched order can automatically select the buyer's ticket.
// Saved choices and transferred tickets are never overwritten by defaults.
func prefillTicketAttendeeDefaults(rows []*types.TicketAttendeeDetails, personID string) {
	for _, row := range rows {
		if row.Version != 0 || row.IsBuyer {
			return
		}
	}
	for _, row := range rows {
		if (personID != "" && row.OwnerPersonID == personID) || (personID == "" && len(rows) == 1 && row.ProfileShirt != "") {
			row.IsBuyer = true
			row.Name, row.TShirt = row.ProfileName, row.ProfileShirt
			row.Diet, row.DietOther, row.Allergies = row.ProfileDiet, row.ProfileDietOther, row.ProfileAllergies
			return
		}
	}
}

func ticketDetailsReservationKey(tag string) string { return "ticket-details-reservation:" + tag }

// A typed checkout email or a query-string provider ID is not authorization to
// edit attendees. The same browser must hold the actual completed reservation.
func completedTicketDetails(app *config.AppContext, r *http.Request, conf *types.Conf) *types.TicketAttendeeDetails {
	reservation := app.Session.GetString(r.Context(), ticketDetailsReservationKey(conf.Tag))
	if reservation == "" {
		return nil
	}
	checkout, email, err := getters.CompletedReservationCheckout(app, reservation, conf.Ref)
	if err != nil || checkout == "" {
		return nil
	}
	if supplied := strings.TrimSpace(r.URL.Query().Get("session_id")); supplied != "" && supplied != checkout {
		return nil
	}
	regs, err := getters.ListRegistrationsByCheckoutID(app, checkout)
	if err != nil {
		return nil
	}
	for _, reg := range regs {
		if reg.Revoked || reg.ConfRef != conf.Ref || !strings.EqualFold(reg.Email, email) || types.IsSponsoredTicketType(reg.Type) {
			continue
		}
		row, e := getters.GetTicketAttendeeDetails(app, reg.RefID)
		if e == nil {
			return row
		}
	}
	return nil
}

type attendeeCount struct {
	Label string
	Count int
}
type attendeeSummary struct {
	Shirts, Meals               []attendeeCount
	MissingShirts, MissingMeals int
}

func summarizeAttendees(rows []*types.TicketAttendeeDetails) *attendeeSummary {
	s := &attendeeSummary{}
	shirts := map[string]int{}
	meals := map[string]int{}
	for _, row := range rows {
		if row.EffectiveShirt == "" {
			s.MissingShirts++
		} else {
			shirts[types.ShirtSizeChoiceCode(row.EffectiveShirt)]++
		}
		if row.Diet == "" {
			s.MissingMeals++
		} else {
			meals[row.Diet]++
		}
	}
	for _, size := range types.ShirtSizeOptions() {
		if n := shirts[size.Code]; n > 0 {
			s.Shirts = append(s.Shirts, attendeeCount{size.Label, n})
		}
	}
	if n := shirts["MXXXL"]; n > 0 {
		s.Shirts = append(s.Shirts, attendeeCount{"XXXL", n})
	}
	for _, diet := range []string{"none", "vegetarian", "vegan", "other"} {
		if n := meals[diet]; n > 0 {
			s.Meals = append(s.Meals, attendeeCount{types.DietaryPreferenceLabel(diet), n})
		}
	}
	return s
}

func attachAdminAttendeeDetails(app *config.AppContext, confID string, rows []*RegistrationAdminRow) *attendeeSummary {
	details, err := getters.ConferenceAttendeeDetails(app, confID)
	if err != nil {
		app.Err.Printf("load attendee planning totals: %s", err)
		return nil
	}
	byRef := map[string]*types.TicketAttendeeDetails{}
	for _, d := range details {
		byRef[d.RefID] = d
	}
	for _, row := range rows {
		row.Attendee = byRef[row.RefID]
	}
	return summarizeAttendees(details)
}
