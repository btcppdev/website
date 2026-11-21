package getters

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"btcpp-web/internal/config"
	"btcpp-web/internal/eventplan"
	"btcpp-web/internal/types"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

func ListCheckoutOffers(app *config.AppContext, confID string, public bool) ([]eventplan.CheckoutOffer, error) {
	rows, err := app.DB.Query(app.DatabaseContext(), `SELECT id::text,checkout_label,amount,checkout_enabled,code_name::text,checkout_restrictions FROM discounts WHERE checkout_conference_id=$1 AND (NOT $2 OR checkout_enabled) ORDER BY created_at,id`, confID, public)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []eventplan.CheckoutOffer{}
	for rows.Next() {
		var o eventplan.CheckoutOffer
		if err = rows.Scan(&o.ID, &o.Label, &o.Percent, &o.Enabled, &o.Code, &o.Restrictions); err != nil {
			return nil, err
		}
		out = append(out, o)
	}
	return out, rows.Err()
}

func saveCheckoutOffers(c context.Context, tx pgx.Tx, confID string, offers []eventplan.CheckoutOffer) error {
	if len(offers) > 10 {
		return fmt.Errorf("use at most 10 checkable offers")
	}
	ids := []string{}
	seen := map[string]bool{}
	for _, o := range offers {
		o.Label = strings.TrimSpace(o.Label)
		o.Restrictions = strings.TrimSpace(o.Restrictions)
		if o.Label == "" || len(o.Label) > 160 || o.Percent < 1 || o.Percent > 99 {
			return fmt.Errorf("each checkable offer needs a label and a percentage from 1 to 99")
		}
		if utf8.RuneCountInString(o.Restrictions) > 500 {
			return fmt.Errorf("offer restrictions must be 500 characters or fewer")
		}
		if o.ID == "" {
			o.ID = uuid.NewString()
			_, err := tx.Exec(c, `INSERT INTO discounts(id,code_name,discount_expr,disc_type,amount,checkout_conference_id,checkout_label,checkout_enabled,checkout_restrictions) VALUES($1,$2,$3,'%',$4,$5,$6,$7,$8)`, o.ID, "CHECK-"+o.ID, fmt.Sprintf("%%%d", o.Percent), o.Percent, confID, o.Label, o.Enabled, o.Restrictions)
			if err != nil {
				return err
			}
			if _, err = tx.Exec(c, `INSERT INTO discounts_conferences(discount_id,conference_id) VALUES($1,$2)`, o.ID, confID); err != nil {
				return err
			}
		} else {
			result, err := tx.Exec(c, `UPDATE discounts SET checkout_label=$3,amount=$4,discount_expr=$5,checkout_enabled=$6,checkout_restrictions=$7 WHERE id=$1 AND checkout_conference_id=$2`, o.ID, confID, o.Label, o.Percent, fmt.Sprintf("%%%d", o.Percent), o.Enabled, o.Restrictions)
			if err != nil {
				return err
			}
			if result.RowsAffected() != 1 {
				return fmt.Errorf("offer does not belong to this event")
			}
		}
		if seen[o.ID] {
			return fmt.Errorf("duplicate offer")
		}
		seen[o.ID] = true
		ids = append(ids, o.ID)
	}
	_, err := tx.Exec(c, `UPDATE discounts SET checkout_enabled=false WHERE checkout_conference_id=$1 AND NOT(id=ANY($2::uuid[]))`, confID, ids)
	return err
}

// TicketAvailability combines settled registrations and outstanding payment
// reservations. Pending payments are released only after the provider confirms
// expiration, never just because our own clock passed a deadline.
type TicketAvailability struct {
	Sold     uint
	Held     uint
	ByTicket map[string]uint
}

func TicketInventory(app *config.AppContext, confID string) (*TicketAvailability, error) {
	return ticketInventory(app.DatabaseContext(), app.DB, confID)
}

type ticketQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func ticketInventory(c context.Context, q ticketQuerier, confID string) (*TicketAvailability, error) {
	rows, err := q.Query(c, `SELECT coalesce(ticket_id::text,''),count(*),false,'[]'::jsonb FROM registrations WHERE conference_id=$1 AND NOT revoked AND lower(type)<>'sponsored' GROUP BY ticket_id UNION ALL SELECT ticket_id::text,quantity,true,ticket_lines FROM ticket_reservations WHERE conference_id=$1 AND status='pending'`, confID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := &TicketAvailability{ByTicket: map[string]uint{}}
	for rows.Next() {
		var id string
		var n uint
		var held bool
		var raw []byte
		if err = rows.Scan(&id, &n, &held, &raw); err != nil {
			return nil, err
		}
		var lines []types.TicketCartLine
		if err = json.Unmarshal(raw, &lines); err != nil {
			return nil, err
		}
		if len(lines) > 0 {
			for _, line := range lines {
				out.ByTicket[line.TicketID] += line.Quantity
			}
		} else {
			out.ByTicket[id] += n
		}
		if held {
			out.Held += n
		} else {
			out.Sold += n
		}
	}
	return out, rows.Err()
}
func (a *TicketAvailability) Remaining(t *types.ConfTicket, all []*types.ConfTicket, now time.Time) uint {
	if t == nil || t.Disabled {
		return 0
	}
	total := a.Sold + a.Held
	if !t.Evergreen {
		current := types.CurrentConfTicketAt(all, total, now)
		if current == nil || current.ID != t.ID {
			return 0
		}
		return t.Max - total
	}
	var end time.Time
	var cap uint
	for _, s := range all {
		if s == nil || s.Disabled || s.Evergreen {
			continue
		}
		if s.SalesEndAt.After(end) {
			end = s.SalesEndAt
		}
		if s.Max > cap {
			cap = s.Max
		}
	}
	if !end.After(now) || total >= cap || a.ByTicket[t.ID] >= t.Max {
		return 0
	}
	return min(cap-total, t.Max-a.ByTicket[t.ID])
}

func ReserveTickets(app *config.AppContext, conf *types.Conf, ticket *types.ConfTicket, quantity uint, email, provider string) (string, error) {
	return reserveTicketCart(app, conf, ticket, quantity, email, provider, nil)
}

func ReserveTicketCart(app *config.AppContext, conf *types.Conf, lines []types.TicketCartLine, email, provider string) (string, error) {
	if len(lines) == 0 {
		return "", fmt.Errorf("choose at least one ticket")
	}
	var quantity uint
	for _, line := range lines {
		quantity += line.Quantity
	}
	return reserveTicketCart(app, conf, lines[0].Ticket, quantity, email, provider, lines)
}

func reserveTicketCart(app *config.AppContext, conf *types.Conf, ticket *types.ConfTicket, quantity uint, email, provider string, lines []types.TicketCartLine) (string, error) {
	if quantity < 1 || quantity > 8 {
		return "", fmt.Errorf("choose between 1 and 8 tickets")
	}
	c := app.DatabaseContext()
	tx, err := app.DB.Begin(c)
	if err != nil {
		return "", err
	}
	defer tx.Rollback(c)
	var locked string
	if err = tx.QueryRow(c, `SELECT id::text FROM conferences WHERE id=$1 FOR UPDATE`, conf.Ref).Scan(&locked); err != nil {
		return "", err
	}
	// Re-read ticket settings under the same lock used by the setup wizard.
	rows, err := tx.Query(c, `SELECT id::text,evergreen,disabled,max_count,sales_end_at,base_price,allow_discounts,currency,card_surcharge_bps,btc_price,usd_price,ticket_kind,stripe_tax_code FROM conference_tickets WHERE conference_id=$1`, conf.Ref)
	if err != nil {
		return "", err
	}
	all := []*types.ConfTicket{}
	var selected *types.ConfTicket
	for rows.Next() {
		t := &types.ConfTicket{}
		var end *time.Time
		if err = rows.Scan(&t.ID, &t.Evergreen, &t.Disabled, &t.Max, &end, &t.BasePrice, &t.AllowDiscounts, &t.Currency, &t.CardSurchargeBPS, &t.BTC, &t.USD, &t.TicketKind, &t.StripeTaxCode); err != nil {
			rows.Close()
			return "", err
		}
		if end != nil {
			t.SalesEndAt = *end
		}
		all = append(all, t)
		if t.ID == ticket.ID {
			selected = t
		}
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return "", err
	}
	if selected == nil || selected.BasePrice != ticket.BasePrice || selected.AllowDiscounts != ticket.AllowDiscounts || selected.Currency != ticket.Currency || selected.CardSurchargeBPS != ticket.CardSurchargeBPS || selected.Evergreen != ticket.Evergreen {
		return "", fmt.Errorf("ticket settings changed; please refresh checkout")
	}
	inventory, err := ticketInventory(c, tx, conf.Ref)
	if err != nil {
		return "", err
	}
	now := time.Now()
	if len(lines) == 0 {
		if remaining := inventory.Remaining(selected, all, now); remaining < quantity {
			return "", fmt.Errorf("only %d tickets are available for this option; please return to the event and select an available ticket", remaining)
		}
	} else {
		// Check every option against the same snapshot. Then account for the
		// total cart against the event cap; never partially reserve a cart.
		seen := map[string]bool{}
		var cap uint
		for _, t := range all {
			if !t.Disabled && !t.Evergreen && t.Max > cap {
				cap = t.Max
			}
		}
		if inventory.Sold+inventory.Held+quantity > cap {
			return "", fmt.Errorf("not enough tickets remain for this order")
		}
		for _, line := range lines {
			var current *types.ConfTicket
			for _, t := range all {
				if t.ID == line.TicketID {
					current = t
					break
				}
			}
			quoted := line.Ticket
			if seen[line.TicketID] || line.Quantity == 0 || current == nil || quoted == nil ||
				current.BasePrice != quoted.BasePrice || current.StandardPrice() != quoted.StandardPrice() ||
				current.AllowDiscounts != quoted.AllowDiscounts || current.Evergreen != quoted.Evergreen ||
				current.CardSurchargeBPS != quoted.CardSurchargeBPS || current.Currency != quoted.Currency ||
				current.TicketKind != quoted.TicketKind || current.StripeTaxCode != quoted.StripeTaxCode {
				return "", fmt.Errorf("ticket settings changed; please refresh checkout")
			}
			if inventory.Remaining(current, all, now) < line.Quantity {
				return "", fmt.Errorf("not enough %s tickets remain; please adjust your selection", line.Title)
			}
			seen[line.TicketID] = true
		}
	}
	id := uuid.NewString()
	raw := []byte("[]")
	if len(lines) > 0 {
		raw, err = json.Marshal(lines)
		if err != nil {
			return "", err
		}
	}
	if _, err = tx.Exec(c, `INSERT INTO ticket_reservations(id,conference_id,ticket_id,quantity,email,provider,ticket_lines) VALUES($1,$2,$3,$4,$5,$6,$7)`, id, conf.Ref, ticket.ID, quantity, email, provider, raw); err != nil {
		return "", err
	}
	return id, tx.Commit(c)
}
func AttachTicketPayment(app *config.AppContext, id, providerID string) error {
	if id == "" {
		return nil
	}
	_, err := app.DB.Exec(app.DatabaseContext(), `UPDATE ticket_reservations SET provider_id=$2,updated_at=now() WHERE id=$1 AND (provider_id='' OR provider_id=$2)`, id, providerID)
	return err
}
func ReleaseTicketReservation(app *config.AppContext, id, provider, providerID string) error {
	if id == "" {
		return nil
	}
	_, err := app.DB.Exec(app.DatabaseContext(), `UPDATE ticket_reservations SET status='released',updated_at=now() WHERE id=$1 AND provider=$2 AND (provider_id='' OR provider_id=$3) AND status='pending'`, id, provider, providerID)
	return err
}
