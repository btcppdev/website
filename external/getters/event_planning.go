package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/eventplan"
	"btcpp-web/internal/types"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"strings"
	"time"
)

var ErrEventPlanConflict = errors.New("Someone else saved this event. Reload before making further changes.")

type EventPlanning struct {
	Revision int64          `json:"revision"`
	Plan     eventplan.Plan `json:"plan"`
}

func LoadEventPlanning(ctx *config.AppContext, confID string) (EventPlanning, error) {
	out := EventPlanning{Plan: eventplan.Plan{ShiftHours: 4, Tasks: eventplan.DefaultTasks()}}
	var raw []byte
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT revision,data FROM event_plans WHERE conference_id=$1`, confID).Scan(&out.Revision, &raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, nil
	}
	if err != nil {
		return out, err
	}
	err = json.Unmarshal(raw, &out.Plan)
	return out, err
}
func CreatePlannedEvent(ctx *config.AppContext, b eventplan.Basics, actor string) (string, error) {
	_, start, end, err := eventplan.ValidateBasics(b)
	if err != nil {
		return "", err
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return "", err
	}
	defer tx.Rollback(ctx.DatabaseContext())
	var id string
	err = tx.QueryRow(ctx.DatabaseContext(), `INSERT INTO conferences(tag,description,edition_type,location,timezone,start_date,end_date,publication_status,active,conference_email_campaigns_enabled,about_body,date_desc) VALUES($1,$2,$3,$4,$5,$6,$7,'draft',false,false,$8,$9) RETURNING id::text`, b.Tag, b.Name, b.Edition, b.Location, b.Timezone, start, end, b.Description, eventplan.DateLabel(start, end)).Scan(&id)
	if err != nil {
		return "", fmt.Errorf("could not create event; the URL may already be in use: %w", err)
	}
	raw, _ := json.Marshal(eventplan.Plan{Country: b.Country, ShiftHours: 4, Tasks: eventplan.DefaultTasks()})
	_, err = tx.Exec(ctx.DatabaseContext(), `INSERT INTO event_plans(conference_id,data,updated_by) VALUES($1,$2,$3::uuid)`, id, raw, nullableUUID(actor))
	if err != nil {
		return "", err
	}
	return id, tx.Commit(ctx.DatabaseContext())
}
func nullableUUID(s string) any {
	if s == "" {
		return nil
	}
	return s
}
func SaveEventPlanning(ctx *config.AppContext, conf *types.Conf, revision int64, actor, section string, payload json.RawMessage) (int64, error) {
	c := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(c)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback(c)
	// Lock the event first: two first-time edits must not race to initialize its plan.
	if _, err = tx.Exec(c, `SELECT id FROM conferences WHERE id=$1 FOR UPDATE`, conf.Ref); err != nil {
		return 0, err
	}
	initial, _ := json.Marshal(eventplan.Plan{ShiftHours: 4, Tasks: eventplan.DefaultTasks()})
	if _, err = tx.Exec(c, `INSERT INTO event_plans(conference_id,data) VALUES($1,$2) ON CONFLICT DO NOTHING`, conf.Ref, initial); err != nil {
		return 0, err
	}
	var current int64
	var raw []byte
	if err = tx.QueryRow(c, `SELECT revision,data FROM event_plans WHERE conference_id=$1 FOR UPDATE`, conf.Ref).Scan(&current, &raw); err != nil {
		return 0, err
	}
	if current != revision {
		return 0, ErrEventPlanConflict
	}
	var plan eventplan.Plan
	if err = json.Unmarshal(raw, &plan); err != nil {
		return 0, err
	}
	switch section {
	case "basics":
		var b eventplan.Basics
		if err = json.Unmarshal(payload, &b); err != nil {
			return 0, err
		}
		b.Tag = conf.Tag
		loc, start, end, e := eventplan.ValidateBasics(b)
		if e != nil {
			return 0, e
		}
		if e = eventplan.ValidateVenues(plan.Venues, loc); e != nil {
			return 0, e
		}
		changed := *conf
		changed.Timezone = b.Timezone
		changed.StartDate = time.Time{}
		changed.EndDate = time.Time{}
		if start != nil {
			changed.StartDate = *start
			changed.EndDate = *end
		}
		if len(plan.Venues) > 0 {
			if start == nil {
				return 0, fmt.Errorf("keep event dates while venue days are configured")
			}
			if e = syncPlanningDays(c, tx, &changed, plan.Venues); e != nil {
				return 0, e
			}
		}
		plan.Country = b.Country
		_, err = tx.Exec(c, `UPDATE conferences SET description=$2,edition_type=$3,location=$4,timezone=$5,start_date=$6,end_date=$7,about_body=$8,date_desc=$9 WHERE id=$1`, conf.Ref, b.Name, b.Edition, b.Location, b.Timezone, start, end, b.Description, eventplan.DateLabel(start, end))
	case "venues":
		var in struct {
			Venues []eventplan.Venue `json:"venues"`
		}
		if err = json.Unmarshal(payload, &in); err != nil {
			return 0, err
		}
		for i := range in.Venues {
			if in.Venues[i].ID == "" {
				in.Venues[i].ID = uuid.NewString()
			}
		}
		if err = eventplan.ValidateVenues(in.Venues, conf.Loc()); err != nil {
			return 0, err
		}
		plan.Venues = in.Venues
		err = syncPlanningDays(c, tx, conf, plan.Venues)
		if err == nil && len(plan.Venues) > 0 {
			v := plan.Venues[0]
			_, err = tx.Exec(c, `UPDATE conferences SET venue=$2,venue_website_url=$3,pickup_address_line1=$4,pickup_address_city=$5,pickup_address_region=$6,pickup_address_postal_code=$7,pickup_address_country=$8 WHERE id=$1`, conf.Ref, v.Name, v.Website, v.Address, v.City, v.Region, v.PostalCode, v.Country)
		}
	case "applications":
		var a eventplan.Applications
		if err = json.Unmarshal(payload, &a); err != nil {
			return 0, err
		}
		values := make([]*time.Time, 4)
		for i, s := range []string{a.TalksOpen, a.TalksClose, a.VolunteersOpen, a.VolunteersClose} {
			values[i], err = eventplan.LocalTime(s, conf.Loc())
			if err != nil {
				return 0, err
			}
		}
		for _, i := range []int{0, 2} {
			if values[i] != nil && values[i+1] != nil && !values[i+1].After(*values[i]) {
				return 0, fmt.Errorf("application closing time must follow opening time")
			}
		}
		if !conf.StartDate.IsZero() {
			talkClose := values[1]
			if talkClose == nil {
				t := *conf
				t.SpeakerApplicationsClose = nil
				due := t.TalksDueDate()
				talkClose = &due
			}
			if values[0] != nil && !talkClose.After(*values[0]) {
				return 0, fmt.Errorf("talk applications must open before their effective deadline")
			}
			for _, value := range values {
				if value != nil && value.After(conf.StartDate) {
					return 0, fmt.Errorf("application windows must finish by the event start")
				}
			}
		}
		if a.ShiftHours < 1 || a.ShiftHours > 8 {
			return 0, fmt.Errorf("shift length must be 1–8 hours")
		}
		plan.ShiftHours = a.ShiftHours
		plan.VolunteerNotes = a.Notes
		plan.MerchDesk = a.MerchDesk
		_, err = tx.Exec(c, `UPDATE conferences SET speaker_applications_open=$2,speaker_applications_close=$3,volunteer_applications_open=$4,volunteer_applications_close=$5 WHERE id=$1`, conf.Ref, values[0], values[1], values[2], values[3])
	case "sponsors":
		var in struct {
			Sponsors []eventplan.Sponsor `json:"sponsors"`
		}
		if err = json.Unmarshal(payload, &in); err != nil {
			return 0, err
		}
		if len(in.Sponsors) > 100 {
			return 0, fmt.Errorf("use at most 100 sponsors")
		}
		for _, s := range in.Sponsors {
			if strings.TrimSpace(s.Name) == "" {
				return 0, fmt.Errorf("sponsor name is required")
			}
			switch s.Status {
			case "prospect", "contacted", "confirmed":
			default:
				return 0, fmt.Errorf("invalid sponsor status")
			}
		}
		plan.Sponsors = in.Sponsors
	case "tickets":
		var in eventplan.TicketSettings
		if err = json.Unmarshal(payload, &in); err != nil {
			return 0, err
		}
		err = savePlanningTickets(c, tx, conf, in.Tickets)
		plan.LocalEligibility = in.LocalEligibility
		plan.StudentEligibility = in.StudentEligibility
		if err == nil {
			_, err = tx.Exec(c, `UPDATE conferences SET local_ticket_body=$2,student_ticket_body=$3 WHERE id=$1`, conf.Ref, in.LocalEligibility, in.StudentEligibility)
		}
	case "merch":
		var in struct {
			ProductIDs []string `json:"product_ids"`
		}
		if err = json.Unmarshal(payload, &in); err != nil {
			return 0, err
		}
		err = savePlanningMerch(c, tx, conf.Ref, in.ProductIDs)
	case "readiness":
		var in struct {
			Tasks []eventplan.Task `json:"tasks"`
		}
		if err = json.Unmarshal(payload, &in); err != nil {
			return 0, err
		}
		if err = eventplan.ValidateTasks(in.Tasks); err != nil {
			return 0, err
		}
		for i := range in.Tasks {
			if in.Tasks[i].ID == "" {
				in.Tasks[i].ID = uuid.NewString()
			}
		}
		plan.Tasks = in.Tasks
	default:
		return 0, fmt.Errorf("unknown setup step")
	}
	if err != nil {
		return 0, err
	}
	raw, err = json.Marshal(plan)
	if err != nil {
		return 0, err
	}
	_, err = tx.Exec(c, `UPDATE event_plans SET data=$2,revision=revision+1,updated_by=$3::uuid,updated_at=now() WHERE conference_id=$1`, conf.Ref, raw, nullableUUID(actor))
	if err != nil {
		return 0, err
	}
	return current + 1, tx.Commit(c)
}
func syncPlanningDays(c context.Context, tx pgx.Tx, conf *types.Conf, venues []eventplan.Venue) error {
	if conf.StartDate.IsZero() {
		for _, v := range venues {
			for _, d := range v.Days {
				if d.DoorsOpen != "" {
					return fmt.Errorf("set event dates before adding public door hours")
				}
			}
		}
		return nil
	}
	type dayData struct {
		open, close string
		stages      []string
	}
	days := map[int]*dayData{}
	anchor := time.Date(conf.StartDate.Year(), conf.StartDate.Month(), conf.StartDate.Day(), 0, 0, 0, 0, time.UTC)
	for _, v := range venues {
		for _, d := range v.Days {
			if d.DoorsOpen == "" {
				continue
			}
			date, _ := time.Parse("2006-01-02", d.Date)
			n := int(date.Sub(anchor).Hours()/24) + 1
			if n < 1 {
				return fmt.Errorf("public event days cannot be before the event start")
			}
			if !conf.EndDate.IsZero() && d.Date > conf.EndDate.Format("2006-01-02") {
				return fmt.Errorf("public event days cannot be after the event end")
			}
			item := days[n]
			if item == nil {
				item = &dayData{open: d.DoorsOpen, close: d.DoorsClose}
				days[n] = item
			}
			if d.DoorsOpen < item.open {
				item.open = d.DoorsOpen
			}
			if d.DoorsClose > item.close {
				item.close = d.DoorsClose
			}
			for _, stage := range strings.Split(v.Stages, ",") {
				stage = strings.TrimSpace(stage)
				if stage != "" {
					item.stages = append(item.stages, stage)
				}
			}
		}
	}
	numbers := []int{}
	for n := range days {
		numbers = append(numbers, n)
	}
	if _, e := tx.Exec(c, `UPDATE conference_days SET doors_start=NULL,doors_end=NULL,venues='{}',event_plan_managed=false WHERE conference_id=$1 AND event_plan_managed AND NOT(day_number=ANY($2::int[]))`, conf.Ref, numbers); e != nil {
		return e
	}
	for n, d := range days {
		_, e := tx.Exec(c, `INSERT INTO conference_days(conference_id,day_number,doors_start,doors_end,venues,event_plan_managed) VALUES($1,$2,$3::time,$4::time,$5,true) ON CONFLICT(conference_id,day_number) DO UPDATE SET doors_start=excluded.doors_start,doors_end=excluded.doors_end,venues=excluded.venues,event_plan_managed=true`, conf.Ref, n, d.open, d.close, d.stages)
		if e != nil {
			return e
		}
	}
	return nil
}
func savePlanningTickets(c context.Context, tx pgx.Tx, conf *types.Conf, tickets []eventplan.Ticket) error {
	if len(tickets) > 30 {
		return fmt.Errorf("use at most 30 ticket tiers")
	}
	seen := map[string]bool{}
	currency := ""
	for i, t := range tickets {
		if strings.TrimSpace(t.Tier) == "" || t.Price < 0 || t.LocalPrice < 0 || t.Capacity < 1 || t.Capacity > 100000 || t.Price > 100000000 || t.LocalPrice > 100000000 || t.CardSurchargeBPS < 0 || t.CardSurchargeBPS > 10000 {
			return fmt.Errorf("check ticket names, nonnegative whole-unit prices and positive cumulative capacities")
		}
		if t.StudentPrice != nil && (*t.StudentPrice <= 0 || *t.StudentPrice > 100000000) {
			return fmt.Errorf("invalid student price")
		}
		if !eventplan.CurrencyPattern.MatchString(t.Currency) {
			return fmt.Errorf("use a three-letter uppercase currency")
		}
		if currency != "" && currency != t.Currency {
			return fmt.Errorf("all event ticket tiers must use the same currency")
		}
		currency = t.Currency
		end, e := eventplan.LocalTime(t.SalesEnd, conf.Loc())
		if e != nil || end == nil {
			return fmt.Errorf("each ticket tier needs a sales deadline")
		}
		if !eventplan.TaxCodePattern.MatchString(t.TaxCode) {
			return fmt.Errorf("choose a tax classification for each tier")
		}
		if t.ID != "" {
			if seen[t.ID] {
				return fmt.Errorf("duplicate ticket tier")
			}
			seen[t.ID] = true
			var owner string
			if e = tx.QueryRow(c, `SELECT conference_id::text FROM conference_tickets WHERE id=$1`, t.ID).Scan(&owner); e != nil || owner != conf.Ref {
				return fmt.Errorf("ticket tier does not belong to this event")
			}
		}
		symbol := map[string]string{"USD": "$", "EUR": "€", "BRL": "R$", "JPY": "¥"}[t.Currency]
		if symbol == "" {
			symbol = t.Currency + " "
		}
		if t.ID == "" {
			_, e = tx.Exec(c, `INSERT INTO conference_tickets(conference_id,ticket_key,tier,base_price,btc_price,usd_price,local_price,student_price,max_count,currency,symbol,sales_end_at,stripe_tax_code,card_surcharge_bps) VALUES($1,$2,$3,$4,$4,$4,$5,$6,$7,$8,$9,$10,$11,$12)`, conf.Ref, fmt.Sprintf("setup-%d-%s", i, uuid.NewString()), t.Tier, t.Price, t.LocalPrice, t.StudentPrice, t.Capacity, t.Currency, symbol, end, t.TaxCode, t.CardSurchargeBPS)
		} else {
			_, e = tx.Exec(c, `UPDATE conference_tickets SET tier=$2,base_price=$3,btc_price=$3,usd_price=$3,local_price=$4,student_price=$5,max_count=$6,currency=$7,symbol=$8,sales_end_at=$9,stripe_tax_code=$10,card_surcharge_bps=$11 WHERE id=$1`, t.ID, t.Tier, t.Price, t.LocalPrice, t.StudentPrice, t.Capacity, t.Currency, symbol, end, t.TaxCode, t.CardSurchargeBPS)
		}
		if e != nil {
			return e
		}
	}
	var currencies int
	if err := tx.QueryRow(c, `SELECT count(DISTINCT currency) FROM conference_tickets WHERE conference_id=$1`, conf.Ref).Scan(&currencies); err != nil {
		return err
	}
	if currencies > 1 {
		return fmt.Errorf("all saved tiers for an event must use the same currency")
	}

	return nil
}
func savePlanningMerch(c context.Context, tx pgx.Tx, confID string, ids []string) error {
	if len(ids) > 3 {
		return fmt.Errorf("choose up to three checkout products")
	}
	if _, e := tx.Exec(c, `DELETE FROM conference_merch_upsells WHERE conference_id=$1`, confID); e != nil {
		return e
	}
	seen := map[string]bool{}
	for i, id := range ids {
		if seen[id] {
			return fmt.Errorf("choose each product only once")
		}
		seen[id] = true
		var available bool
		if e := tx.QueryRow(c, `SELECT status='published' FROM merch_products WHERE id=$1`, id).Scan(&available); e != nil || !available {
			return fmt.Errorf("choose published products from the catalog")
		}
		if _, e := tx.Exec(c, `INSERT INTO conference_merch_upsells(conference_id,product_id,display_order) VALUES($1,$2,$3)`, confID, id, i); e != nil {
			return e
		}
	}
	return nil
}
