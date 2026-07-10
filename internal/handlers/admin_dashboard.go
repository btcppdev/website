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
	"btcpp-web/internal/config"
	"btcpp-web/internal/helpers"
	"btcpp-web/internal/types"
)

type GlobalAdminDashboardPage struct {
	FlashMessage         string
	Year                 uint
	FeaturedSpeakerSlots []*types.Speaker
}

type EventDetailsPage struct {
	Conf         *types.Conf
	FlashMessage string
	Days         []*EventDetailsDay
	Venues       []string
	Tickets      []*EventDetailsTicket
	StartInput   string
	EndInput     string
	NextDay      int
	Year         uint
}

type EventDetailsTicket struct {
	Ticket        *types.ConfTicket
	ExpiresStart  string
	ExpiresEnd    string
	CardPrice     uint
	CardSurcharge uint
}

type EventDetailsDay struct {
	Info           *types.ConfInfo
	DateLabel      string
	StageCount     int
	DoorsStart     string
	DoorsEnd       string
	BreakfastStart string
	BreakfastEnd   string
	LunchStart     string
	LunchEnd       string
	CoffeeStart    string
	CoffeeEnd      string
	VenuesText     string
}

func GlobalAdminDashboard(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}

	featured, err := getters.ListHomepageFeaturedSpeakers(ctx)
	if err != nil {
		ctx.Err.Printf("/admin homepage featured speakers failed: %s", err)
		featured = nil
	}

	slots := make([]*types.Speaker, 8)
	for i := 0; i < len(featured) && i < len(slots); i++ {
		slots[i] = featured[i]
	}

	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/dashboard.tmpl", &GlobalAdminDashboardPage{
		FlashMessage:         r.URL.Query().Get("flash"),
		Year:                 helpers.CurrentYear(),
		FeaturedSpeakerSlots: slots,
	}); err != nil {
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
		ctx.Err.Printf("/admin template failed: %s", err)
	}
}

func GlobalAdminHomepageSpeakersUpdate(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	limitRequestBody(w, r, maxFormBodyBytes)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad form", http.StatusBadRequest)
		return
	}
	ids := make([]string, 0, 8)
	for i := 1; i <= 8; i++ {
		ids = append(ids, r.FormValue(fmt.Sprintf("speaker_%d_id", i)))
	}
	if err := getters.ReplaceHomepageFeaturedSpeakers(ctx, ids); err != nil {
		ctx.Err.Printf("/admin/homepage-speakers update failed: %s", err)
		http.Redirect(w, r, "/admin?flash="+url.QueryEscape("Could not update homepage speakers."), http.StatusFound)
		return
	}
	http.Redirect(w, r, "/admin?flash="+url.QueryEscape("Homepage speakers updated."), http.StatusFound)
}

func GlobalAdminEventDetails(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
		return
	}
	conf, err := helpers.FindConf(r, ctx)
	if err != nil {
		handle404(w, r, ctx)
		return
	}

	infos, err := getters.ListConfInfos(ctx, conf.Tag)
	if err != nil {
		ctx.Err.Printf("/%s/admin/details conf info failed: %s", conf.Tag, err)
		http.Error(w, "Unable to load event details", http.StatusInternalServerError)
		return
	}
	sort.SliceStable(infos, func(i, j int) bool {
		if infos[i] == nil {
			return false
		}
		if infos[j] == nil {
			return true
		}
		return infos[i].Day < infos[j].Day
	})

	venueSet := make(map[string]bool)
	days := make([]*EventDetailsDay, 0, len(infos))
	nextDay := 1
	for _, info := range infos {
		if info == nil {
			continue
		}
		if info.Day >= nextDay {
			nextDay = info.Day + 1
		}
		for _, venue := range info.Venues {
			if venue != "" {
				venueSet[venue] = true
			}
		}
		days = append(days, &EventDetailsDay{
			Info:           info,
			DateLabel:      conf.StartDate.In(conf.Loc()).AddDate(0, 0, info.Day-1).Format("Mon, Jan 2"),
			StageCount:     len(info.Venues),
			DoorsStart:     timeInputStart(info.Doors),
			DoorsEnd:       timeInputEnd(info.Doors),
			BreakfastStart: timeInputStart(info.Breakfast),
			BreakfastEnd:   timeInputEnd(info.Breakfast),
			LunchStart:     timeInputStart(info.Lunch),
			LunchEnd:       timeInputEnd(info.Lunch),
			CoffeeStart:    timeInputStart(info.Coffee),
			CoffeeEnd:      timeInputEnd(info.Coffee),
			VenuesText:     strings.Join(info.Venues, ", "),
		})
	}
	venues := make([]string, 0, len(venueSet))
	for venue := range venueSet {
		venues = append(venues, venue)
	}
	sort.Strings(venues)

	tickets := make([]*EventDetailsTicket, 0, len(conf.Tickets))
	for _, ticket := range conf.Tickets {
		if ticket == nil {
			continue
		}
		row := &EventDetailsTicket{
			Ticket:        ticket,
			CardPrice:     ticket.CardPrice(false),
			CardSurcharge: ticket.CardSurcharge(false),
		}
		if ticket.Expires != nil {
			row.ExpiresStart = datetimeLocalInput(ticket.Expires.Start)
			if ticket.Expires.End != nil {
				row.ExpiresEnd = datetimeLocalInput(*ticket.Expires.End)
			}
		}
		tickets = append(tickets, row)
	}

	if err := ctx.TemplateCache.ExecuteTemplate(w, "admin/event_details.tmpl", &EventDetailsPage{
		Conf:         conf,
		FlashMessage: r.URL.Query().Get("flash"),
		Days:         days,
		Venues:       venues,
		Tickets:      tickets,
		StartInput:   datetimeLocalInput(conf.StartDate),
		EndInput:     datetimeLocalInput(conf.EndDate),
		NextDay:      nextDay,
		Year:         helpers.CurrentYear(),
	}); err != nil {
		ctx.Err.Printf("/%s/admin/details template failed: %s", conf.Tag, err)
		http.Error(w, "Unable to load page", http.StatusInternalServerError)
	}
}

func GlobalAdminUpdateConfDetails(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
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

	timezoneName := strings.TrimSpace(r.FormValue("timezone"))
	loc := conf.Loc()
	if timezoneName != "" {
		if loaded, err := time.LoadLocation(timezoneName); err == nil {
			loc = loaded
		}
	}
	start, err := parseOptionalDatetimeLocal(r.FormValue("start_date"), loc)
	if err != nil {
		redirectEventDetails(w, r, conf, "Invalid start date.")
		return
	}
	end, err := parseOptionalDatetimeLocal(r.FormValue("end_date"), loc)
	if err != nil {
		redirectEventDetails(w, r, conf, "Invalid end date.")
		return
	}
	if start != nil && end != nil && end.Before(*start) {
		redirectEventDetails(w, r, conf, "End date must be after start date.")
		return
	}

	in := getters.ConfDetailsInput{
		Description:     strings.TrimSpace(r.FormValue("description")),
		EditionType:     strings.TrimSpace(r.FormValue("edition_type")),
		OGFlavor:        strings.TrimSpace(r.FormValue("og_flavor")),
		Emoji:           strings.TrimSpace(r.FormValue("emoji")),
		Tagline:         strings.TrimSpace(r.FormValue("tagline")),
		DateDesc:        strings.TrimSpace(r.FormValue("date_desc")),
		StartDate:       start,
		EndDate:         end,
		Timezone:        timezoneName,
		Location:        strings.TrimSpace(r.FormValue("location")),
		Venue:           strings.TrimSpace(r.FormValue("venue")),
		VenueMap:        strings.TrimSpace(r.FormValue("venue_map")),
		VenueWebsite:    strings.TrimSpace(r.FormValue("venue_website")),
		ShowHackathon:   r.FormValue("show_hackathon") == "1",
		HeroTitle:       strings.TrimSpace(r.FormValue("hero_title")),
		HeroCaption:     strings.TrimSpace(r.FormValue("hero_caption")),
		AboutTitle:      strings.TrimSpace(r.FormValue("about_title")),
		AboutBody:       strings.TrimSpace(r.FormValue("about_body")),
		AboutBody2:      strings.TrimSpace(r.FormValue("about_body_2")),
		VenueTitle:      strings.TrimSpace(r.FormValue("venue_title")),
		VenueSubtitle:   strings.TrimSpace(r.FormValue("venue_subtitle")),
		VenueBody:       strings.TrimSpace(r.FormValue("venue_body")),
		HotelsIntro:     strings.TrimSpace(r.FormValue("hotels_intro")),
		LocalTicketBody: strings.TrimSpace(r.FormValue("local_ticket_body")),
		SpeakersTitle:   strings.TrimSpace(r.FormValue("speakers_title")),
		SpeakersBody:    strings.TrimSpace(r.FormValue("speakers_body")),
		MapEmbedURL:     strings.TrimSpace(r.FormValue("map_embed_url")),
		MapLatitude:     parseOptionalFloat(r.FormValue("map_latitude")),
		MapLongitude:    parseOptionalFloat(r.FormValue("map_longitude")),
		MapXPercent:     parseOptionalFloat(r.FormValue("map_x_percent")),
		MapYPercent:     parseOptionalFloat(r.FormValue("map_y_percent")),
		MapLabel:        strings.TrimSpace(r.FormValue("map_label")),
		MapLabelSide:    normalizeMapLabelSide(r.FormValue("map_label_side")),
	}
	if err := getters.UpdateConfDetails(ctx, conf.Ref, in); err != nil {
		ctx.Err.Printf("/%s/admin/details update failed: %s", conf.Tag, err)
		redirectEventDetails(w, r, conf, "Could not update event details.")
		return
	}
	redirectEventDetails(w, r, conf, "Event details updated.")
}

func GlobalAdminUpdateConfInfo(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
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

	day, err := strconv.Atoi(strings.TrimSpace(r.FormValue("day")))
	if err != nil || day < 1 {
		redirectEventDetails(w, r, conf, "Day must be a positive number.")
		return
	}
	in := getters.ConfInfoInput{
		ID:             strings.TrimSpace(r.FormValue("id")),
		Day:            day,
		DoorsStart:     strings.TrimSpace(r.FormValue("doors_start")),
		DoorsEnd:       strings.TrimSpace(r.FormValue("doors_end")),
		BreakfastStart: strings.TrimSpace(r.FormValue("breakfast_start")),
		BreakfastEnd:   strings.TrimSpace(r.FormValue("breakfast_end")),
		LunchStart:     strings.TrimSpace(r.FormValue("lunch_start")),
		LunchEnd:       strings.TrimSpace(r.FormValue("lunch_end")),
		CoffeeStart:    strings.TrimSpace(r.FormValue("coffee_start")),
		CoffeeEnd:      strings.TrimSpace(r.FormValue("coffee_end")),
		Venues:         parseVenueList(r.FormValue("venues")),
	}
	if err := validateTimeInputs(in.DoorsStart, in.DoorsEnd, in.BreakfastStart, in.BreakfastEnd, in.LunchStart, in.LunchEnd, in.CoffeeStart, in.CoffeeEnd); err != nil {
		redirectEventDetails(w, r, conf, err.Error())
		return
	}
	if err := getters.UpsertConfInfo(ctx, conf.Ref, in); err != nil {
		ctx.Err.Printf("/%s/admin/details/confinfo update failed: %s", conf.Tag, err)
		redirectEventDetails(w, r, conf, "Could not update venue schedule.")
		return
	}
	redirectEventDetails(w, r, conf, "Venue schedule updated.")
}

func GlobalAdminUpdateConfTicket(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
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

	basePrice, err := parseUintFormValue(r.FormValue("base_price"))
	if err != nil {
		redirectEventDetails(w, r, conf, "Base price must be a non-negative whole number.")
		return
	}
	localPrice, err := parseUintFormValue(r.FormValue("local_price"))
	if err != nil {
		redirectEventDetails(w, r, conf, "Local price must be a non-negative whole number.")
		return
	}
	cardSurchargeBPS, err := parseUintFormValue(r.FormValue("card_surcharge_bps"))
	if err != nil {
		redirectEventDetails(w, r, conf, "Card surcharge must be a non-negative basis-point value.")
		return
	}
	maxCount, err := parseUintFormValue(r.FormValue("max_count"))
	if err != nil {
		redirectEventDetails(w, r, conf, "Max count must be a non-negative whole number.")
		return
	}

	loc := conf.Loc()
	expiresStart, err := parseOptionalDatetimeLocal(r.FormValue("expires_start"), loc)
	if err != nil {
		redirectEventDetails(w, r, conf, "Invalid ticket start date.")
		return
	}
	expiresEnd, err := parseOptionalDatetimeLocal(r.FormValue("expires_end"), loc)
	if err != nil {
		redirectEventDetails(w, r, conf, "Invalid ticket end date.")
		return
	}
	if expiresStart != nil && expiresEnd != nil && expiresEnd.Before(*expiresStart) {
		redirectEventDetails(w, r, conf, "Ticket end date must be after ticket start date.")
		return
	}

	in := getters.ConfTicketInput{
		ID:               strings.TrimSpace(r.FormValue("ticket_id")),
		Tier:             strings.TrimSpace(r.FormValue("tier")),
		LocalPrice:       localPrice,
		BasePrice:        basePrice,
		CardSurchargeBPS: cardSurchargeBPS,
		Max:              maxCount,
		Currency:         strings.ToUpper(strings.TrimSpace(r.FormValue("currency"))),
		Symbol:           strings.TrimSpace(r.FormValue("symbol")),
		PostSymbol:       strings.TrimSpace(r.FormValue("post_symbol")),
		ExpiresStart:     expiresStart,
		ExpiresEnd:       expiresEnd,
	}
	if in.Tier == "" {
		redirectEventDetails(w, r, conf, "Ticket tier is required.")
		return
	}
	if in.Currency == "" {
		redirectEventDetails(w, r, conf, "Ticket currency is required.")
		return
	}
	if err := getters.UpdateConfTicket(ctx, conf.Ref, in); err != nil {
		ctx.Err.Printf("/%s/admin/details/ticket update failed: %s", conf.Tag, err)
		redirectEventDetails(w, r, conf, "Could not update ticket pricing.")
		return
	}
	redirectEventDetails(w, r, conf, "Ticket pricing updated.")
}

func GlobalAdminUpdateConfState(w http.ResponseWriter, r *http.Request, ctx *config.AppContext) {
	if id := requireGlobalAdmin(w, r, ctx); id == nil {
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

	status := strings.TrimSpace(r.FormValue("state"))
	switch status {
	case "published", "draft":
	default:
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/details?flash=%s", conf.Tag, url.QueryEscape("Unknown event state.")), http.StatusSeeOther)
		return
	}

	if err := getters.UpdateConfPublicationStatus(ctx, conf.Ref, status); err != nil {
		ctx.Err.Printf("/%s/admin/state failed: %s", conf.Tag, err)
		http.Redirect(w, r, fmt.Sprintf("/%s/admin/details?flash=%s", conf.Tag, url.QueryEscape("Could not update event state.")), http.StatusSeeOther)
		return
	}

	http.Redirect(w, r, fmt.Sprintf("/%s/admin/details?flash=%s", conf.Tag, url.QueryEscape("Event marked "+status+".")), http.StatusSeeOther)
}

func redirectEventDetails(w http.ResponseWriter, r *http.Request, conf *types.Conf, msg string) {
	http.Redirect(w, r, fmt.Sprintf("/%s/admin/details?flash=%s", conf.Tag, url.QueryEscape(msg)), http.StatusSeeOther)
}

func parseOptionalFloat(raw string) float64 {
	v, err := strconv.ParseFloat(strings.TrimSpace(raw), 64)
	if err != nil {
		return 0
	}
	return v
}

func parseUintFormValue(raw string) (uint, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, nil
	}
	value, err := strconv.ParseUint(raw, 10, 32)
	if err != nil {
		return 0, err
	}
	return uint(value), nil
}

func normalizeMapLabelSide(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "left", "right", "top", "bottom":
		return strings.ToLower(strings.TrimSpace(raw))
	default:
		return "right"
	}
}

func datetimeLocalInput(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Format("2006-01-02T15:04")
}

func timeInputStart(t *types.Times) string {
	if t == nil {
		return ""
	}
	return t.Start.Format("15:04")
}

func timeInputEnd(t *types.Times) string {
	if t == nil || t.End == nil {
		return ""
	}
	return t.End.Format("15:04")
}

func parseOptionalDatetimeLocal(raw string, loc *time.Location) (*time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	t, err := time.ParseInLocation("2006-01-02T15:04", raw, loc)
	if err != nil {
		return nil, err
	}
	return &t, nil
}

func parseVenueList(raw string) []string {
	fields := strings.FieldsFunc(raw, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r'
	})
	out := make([]string, 0, len(fields))
	seen := make(map[string]bool)
	for _, field := range fields {
		venue := strings.TrimSpace(field)
		if venue == "" || seen[venue] {
			continue
		}
		seen[venue] = true
		out = append(out, venue)
	}
	return out
}

func validateTimeInputs(values ...string) error {
	for _, value := range values {
		if value == "" {
			continue
		}
		if _, err := time.Parse("15:04", value); err != nil {
			return fmt.Errorf("Invalid time %q.", value)
		}
	}
	return nil
}
