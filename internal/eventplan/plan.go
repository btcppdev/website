// Package eventplan contains the editable logistics for an event. Public event,
// ticket and application settings continue to live in their existing records.
package eventplan

import (
	"fmt"
	"net/url"
	"regexp"
	"strings"
	"time"
)

type Plan struct {
	Country            string    `json:"country"`
	Venues             []Venue   `json:"venues"`
	Sponsors           []Sponsor `json:"sponsors"`
	LocalEligibility   string    `json:"local_eligibility"`
	StudentEligibility string    `json:"student_eligibility"`
	VolunteerNotes     string    `json:"volunteer_notes"`
	ShiftHours         int       `json:"shift_hours"`
	MerchDesk          bool      `json:"merch_desk"`
}
type Venue struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Address     string `json:"address"`
	City        string `json:"city"`
	Region      string `json:"region"`
	PostalCode  string `json:"postal_code"`
	Country     string `json:"country"`
	Website     string `json:"website"`
	Contact     string `json:"contact"`
	Capacity    int    `json:"capacity"`
	Stages      string `json:"stages"`
	ContractURL string `json:"contract_url"`
	Notes       string `json:"notes"`
	Days        []Day  `json:"days"`
}
type Day struct {
	Date       string `json:"date"`
	StaffOpen  string `json:"staff_open"`
	DoorsOpen  string `json:"doors_open"`
	DoorsClose string `json:"doors_close"`
	StaffClose string `json:"staff_close"`
}
type Sponsor struct {
	OrganizationID string `json:"organization_id"`
	Name           string `json:"name"`
	Level          string `json:"level"`
	Status         string `json:"status"`
	Contact        string `json:"contact"`
	Commitments    string `json:"commitments"`
}
type Ticket struct {
	Evergreen        bool   `json:"evergreen"`
	Disabled         bool   `json:"disabled"`
	AllowDiscounts   bool   `json:"allow_discounts"`
	TicketKind       string `json:"ticket_kind"`
	Description      string `json:"description"`
	ID               string `json:"id"`
	Key              string `json:"key"`
	Tier             string `json:"tier"`
	Price            int64  `json:"price"`
	LocalPrice       int64  `json:"local_price"`
	StudentPrice     *int64 `json:"student_price"`
	Capacity         int64  `json:"capacity"`
	Currency         string `json:"currency"`
	TaxCode          string `json:"tax_code"`
	SalesEnd         string `json:"sales_end"`
	CardSurchargeBPS int64  `json:"card_surcharge_bps"`
}

var CurrencyPattern = regexp.MustCompile(`^[A-Z]{3}$`)
var TaxCodePattern = regexp.MustCompile(`^txcd_[0-9]{8}$`)
var CountryPattern = regexp.MustCompile(`^[A-Z]{2}$`)
var TagPattern = regexp.MustCompile(`^[a-z][a-z0-9-]{1,39}$`)

func ValidURL(s string) bool {
	if s == "" {
		return true
	}
	u, e := url.Parse(s)
	return e == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != "" && u.User == nil
}
func LocalTime(s string, loc *time.Location) (*time.Time, error) {
	if s == "" {
		return nil, nil
	}
	t, e := time.ParseInLocation("2006-01-02T15:04", s, loc)
	if e != nil {
		return nil, fmt.Errorf("enter a valid local date and time")
	}
	if t.Format("2006-01-02T15:04") != s {
		return nil, fmt.Errorf("that local time does not exist because of a clock change")
	}
	return &t, nil
}
func ValidateVenues(venues []Venue, loc *time.Location) error {
	if len(venues) > 30 {
		return fmt.Errorf("use at most 30 venues")
	}
	ids := map[string]bool{}
	for _, v := range venues {
		if strings.TrimSpace(v.Name) == "" || v.ID == "" || ids[v.ID] {
			return fmt.Errorf("each venue needs a name and unique identity")
		}
		ids[v.ID] = true
		if v.Capacity < 0 || v.Capacity > 100000 {
			return fmt.Errorf("venue capacity must be between 0 and 100,000")
		}
		if !ValidURL(v.Website) || !ValidURL(v.ContractURL) {
			return fmt.Errorf("venue links must be complete http or https URLs")
		}
		if len(v.Days) > 60 {
			return fmt.Errorf("use at most 60 access days per venue")
		}
		stages := map[string]bool{}
		for _, stage := range strings.Split(v.Stages, ",") {
			stage = strings.TrimSpace(stage)
			if stage == "" {
				continue
			}
			if stages[stage] {
				return fmt.Errorf("stage names must be unique within a venue")
			}
			stages[stage] = true
		}
		dates := map[string]bool{}
		for _, d := range v.Days {
			if _, e := time.Parse("2006-01-02", d.Date); e != nil || dates[d.Date] {
				return fmt.Errorf("each venue day needs a valid, unique date")
			}
			dates[d.Date] = true
			if d.DoorsOpen != "" && d.DoorsOpen == d.DoorsClose {
				return fmt.Errorf("public doors must close after they open")
			}
			if d.StaffOpen != "" && d.StaffOpen == d.StaffClose {
				return fmt.Errorf("staff access must end after it starts")
			}
			var prev *time.Time
			for _, s := range []string{d.StaffOpen, d.DoorsOpen, d.DoorsClose, d.StaffClose} {
				if s == "" {
					continue
				}
				t, e := LocalTime(d.Date+"T"+s, loc)
				if e != nil {
					return e
				}
				if prev != nil && t.Before(*prev) {
					return fmt.Errorf("%s on %s: access and doors times must run in order", v.Name, d.Date)
				}
				prev = t
			}
			if (d.DoorsOpen == "") != (d.DoorsClose == "") {
				return fmt.Errorf("enter both public door times, or leave both blank for a setup-only day")
			}
			if (d.StaffOpen == "") != (d.StaffClose == "") {
				return fmt.Errorf("enter both staff access times")
			}
		}
	}
	return nil
}

type Basics struct {
	Tag         string `json:"tag"`
	Name        string `json:"name"`
	Edition     string `json:"edition"`
	Location    string `json:"location"`
	Country     string `json:"country"`
	Timezone    string `json:"timezone"`
	Start       string `json:"start"`
	End         string `json:"end"`
	Description string `json:"description"`
}
type Applications struct {
	TalksOpen       string `json:"talks_open"`
	TalksClose      string `json:"talks_close"`
	VolunteersOpen  string `json:"volunteers_open"`
	VolunteersClose string `json:"volunteers_close"`
	Notes           string `json:"notes"`
	ShiftHours      int    `json:"shift_hours"`
	MerchDesk       bool   `json:"merch_desk"`
}
type CheckoutOffer struct {
	ID      string `json:"id"`
	Label   string `json:"label"`
	Percent int    `json:"percent"`
	Enabled bool   `json:"enabled"`
	Code    string `json:"code"`
}
type TicketSettings struct {
	Offers             []CheckoutOffer `json:"offers"`
	Tickets            []Ticket        `json:"tickets"`
	LocalEligibility   string          `json:"local_eligibility"`
	StudentEligibility string          `json:"student_eligibility"`
}

func ValidateBasics(b Basics) (*time.Location, *time.Time, *time.Time, error) {
	if !TagPattern.MatchString(b.Tag) || strings.TrimSpace(b.Name) == "" || len(b.Name) > 160 {
		return nil, nil, nil, fmt.Errorf("enter an event name and a URL using 2–40 lowercase letters, numbers or hyphens")
	}
	for _, s := range []string{"admin", "api", "dashboard", "shop", "events", "whois", "login", "static", "media", "social-cards", "watch", "talk", "volunteer", "sponsors", "terms", "privacy", "contact", "developers", "event-invites"} {
		if b.Tag == s {
			return nil, nil, nil, fmt.Errorf("that event URL is reserved")
		}
	}
	if b.Country != "" && !CountryPattern.MatchString(b.Country) {
		return nil, nil, nil, fmt.Errorf("use a two-letter country code, such as BR, DE, JP or US")
	}
	loc, err := time.LoadLocation(b.Timezone)
	if err != nil || b.Timezone == "" {
		return nil, nil, nil, fmt.Errorf("choose an IANA timezone, such as Europe/Berlin")
	}
	start, err := LocalTime(b.Start, loc)
	if err != nil {
		return nil, nil, nil, err
	}
	end, err := LocalTime(b.End, loc)
	if err != nil {
		return nil, nil, nil, err
	}
	if (start == nil) != (end == nil) {
		return nil, nil, nil, fmt.Errorf("enter both event dates or leave both undecided")
	}
	if start != nil && !end.After(*start) {
		return nil, nil, nil, fmt.Errorf("event end must be after its start")
	}
	switch b.Edition {
	case "global", "local":
	default:
		return nil, nil, nil, fmt.Errorf("choose an event type")
	}
	return loc, start, end, nil
}

func DateLabel(start, end *time.Time) string {
	if start == nil || end == nil {
		return "Dates to be announced"
	}
	if start.Format("2006-01-02") == end.Format("2006-01-02") {
		return start.Format("January 2, 2006")
	}
	if start.Year() == end.Year() && start.Month() == end.Month() {
		return fmt.Sprintf("%s–%d, %d", start.Format("January 2"), end.Day(), end.Year())
	}
	return start.Format("Jan 2, 2006") + " – " + end.Format("Jan 2, 2006")
}
