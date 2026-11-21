package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
	"strings"
)

func normalizeRegistrationEmails(emails []string) []string {
	seen := make(map[string]bool, len(emails))
	clean := make([]string, 0, len(emails))
	for _, email := range emails {
		email = strings.ToLower(strings.TrimSpace(email))
		if email == "" || seen[email] {
			continue
		}
		seen[email] = true
		clean = append(clean, email)
	}
	return clean
}

func SoldTix(ctx *config.AppContext, conf *types.Conf) (uint, error) {
	if conf == nil {
		return 0, nil
	}
	soldTixCount, err := SoldTixCount(ctx, conf.Ref)
	if err != nil {
		return conf.TixSold, err
	}
	return soldTixCount, nil
}

func UpdateSoldTix(ctx *config.AppContext, conf *types.Conf) {
	soldTixCount, err := SoldTixCount(ctx, conf.Ref)
	if err != nil {
		ctx.Err.Printf("error fetching sold tix %s %s", conf.Ref, err)
	} else {
		ctx.Infos.Printf("Loaded sold tix count %s %d!", conf.Ref, soldTixCount)
		conf.TixSold = soldTixCount
	}
}

// EmailHasRegistration reports whether the email appears at all in the
// registration rows. Used by the talk-apply form to hide the "first bitcoin++"
// checkbox for returning attendees.
func EmailHasRegistration(ctx *config.AppContext, email string) (bool, error) {
	regs, err := ListRegistrationsByEmail(ctx, email)
	if err != nil {
		return false, err
	}
	return len(regs) > 0, nil
}

func ticketMatch(tickets []string, rez *types.Registration) bool {
	for _, tix := range tickets {
		if strings.Contains(rez.ItemBought, tix) {
			return true
		}
	}

	return false
}

func FetchRegistrationsConf(ctx *config.AppContext, confRef string) ([]*types.Registration, error) {
	return FetchRegistrations(ctx, confRef)
}

func FetchBtcppRegistrations(ctx *config.AppContext, activeOnly bool) ([]*types.Registration, error) {
	var btcppres []*types.Registration
	filter := "conf"
	if activeOnly {
		filter = "active"
	}
	rezzies, err := queryRegistrationsPostgres(ctx, filter, "")

	if err != nil {
		return nil, err
	}

	for _, r := range rezzies {
		if r.RefID == "" {
			continue
		}
		if types.IsSponsoredTicketType(r.Type) {
			continue
		}

		btcppres = append(btcppres, r)
	}

	return btcppres, nil
}

func CheckIn(ctx *config.AppContext, ticket string) (string, bool, error) {
	if ctx == nil || ctx.DB == nil {
		return "", false, fmt.Errorf("database is not configured")
	}
	var ticketType string
	var checkedInAt pgtype.Timestamptz
	var revoked bool
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT type, checked_in_at, revoked
		FROM registrations
		WHERE ref_id = $1
	`, ticket).Scan(&ticketType, &checkedInAt, &revoked)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", true, fmt.Errorf("Ticket not found")
	}
	if err != nil {
		return "", false, fmt.Errorf("query registration: %w", err)
	}
	if revoked {
		return "", true, fmt.Errorf("Ticket was revoked")
	}
	if types.IsSponsoredTicketType(ticketType) {
		return "", true, fmt.Errorf("Sponsored builder passes must be distributed before check-in")
	}
	if checkedInAt.Valid {
		return "", true, fmt.Errorf("Already checked in")
	}
	eligibility, err := CheckInEligibility(ctx, ticket)
	if err != nil {
		return "", false, err
	}
	if eligibility.NeedsProof() {
		return ticketType, true, nil
	}
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE registrations
		SET checked_in_at = now()
		WHERE ref_id = $1 AND checked_in_at IS NULL AND revoked = false
		 AND (jsonb_array_length(checkin_requirements)=0 OR proof_checked_at IS NOT NULL OR full_ticket_upgraded_at IS NOT NULL)
	`, ticket)
	if err != nil {
		return "", false, fmt.Errorf("check in registration: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", true, fmt.Errorf("Already checked in")
	}
	return ticketType, true, nil
}

func GetRegistrationCheckIn(ctx *config.AppContext, ticket string) (*types.RegistrationCheckIn, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	ticket = strings.TrimSpace(ticket)
	if ticket == "" {
		return nil, fmt.Errorf("ticket reference is required")
	}
	var out types.RegistrationCheckIn
	var checkedInAt pgtype.Timestamptz
	var shirtPickedUpAt pgtype.Timestamptz
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT r.ref_id, r.type, r.email::text,
			attendee.attendee_name, attendee.tshirt,
			r.conference_id::text, conferences.tag, r.revoked, r.checked_in_at,
			r.conference_shirt_picked_up_at
		FROM registrations r
		JOIN conferences ON conferences.id = r.conference_id
		JOIN registration_attendee_fulfillment attendee ON attendee.ref_id = r.ref_id
		WHERE r.ref_id = $1
	`, ticket).Scan(
		&out.TicketRef, &out.TicketType, &out.Email,
		&out.AttendeeName, &out.TShirtSize,
		&out.ConferenceID, &out.ConferenceTag, &out.Revoked, &checkedInAt, &shirtPickedUpAt,
	)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, fmt.Errorf("ticket not found")
	}
	if err != nil {
		return nil, fmt.Errorf("load registration check-in: %w", err)
	}
	if shirtPickedUpAt.Valid {
		pickedUpAt := shirtPickedUpAt.Time
		out.ShirtPickedUpAt = &pickedUpAt
	}
	if checkedInAt.Valid {
		checkedIn := checkedInAt.Time
		out.CheckedInAt = &checkedIn
	}
	return &out, nil
}

func ListDevRegistrationCheckInPreviews(ctx *config.AppContext) ([]*types.RegistrationCheckIn, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT r.ref_id, r.type, r.email::text,
			attendee.attendee_name, attendee.tshirt,
			r.conference_id::text, conferences.tag, r.revoked, r.checked_in_at,
			r.conference_shirt_picked_up_at
		FROM registrations r
		JOIN conferences ON conferences.id = r.conference_id
		JOIN registration_attendee_fulfillment attendee ON attendee.ref_id = r.ref_id
		WHERE r.platform = 'dev-checkin-preview'
		ORDER BY CASE r.type
			WHEN 'genpop' THEN 1 WHEN 'local' THEN 2 WHEN 'sponsor' THEN 3
			WHEN 'volunteer' THEN 4 WHEN 'speaker' THEN 5 ELSE 6 END,
			r.ref_id
	`)
	if err != nil {
		return nil, fmt.Errorf("list development check-in previews: %w", err)
	}
	defer rows.Close()

	var previews []*types.RegistrationCheckIn
	for rows.Next() {
		var preview types.RegistrationCheckIn
		var checkedInAt pgtype.Timestamptz
		var shirtPickedUpAt pgtype.Timestamptz
		if err := rows.Scan(
			&preview.TicketRef, &preview.TicketType, &preview.Email,
			&preview.AttendeeName, &preview.TShirtSize,
			&preview.ConferenceID, &preview.ConferenceTag, &preview.Revoked,
			&checkedInAt, &shirtPickedUpAt,
		); err != nil {
			return nil, fmt.Errorf("scan development check-in preview: %w", err)
		}
		if checkedInAt.Valid {
			checkedIn := checkedInAt.Time
			preview.CheckedInAt = &checkedIn
		}
		if shirtPickedUpAt.Valid {
			pickedUp := shirtPickedUpAt.Time
			preview.ShirtPickedUpAt = &pickedUp
		}
		previews = append(previews, &preview)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("list development check-in previews: %w", err)
	}
	return previews, nil
}

func BulkCheckInRegistrations(ctx *config.AppContext, confRef string, emails []string) (int64, error) {
	if ctx == nil || ctx.DB == nil {
		return 0, fmt.Errorf("database is not configured")
	}
	if strings.TrimSpace(confRef) == "" {
		return 0, fmt.Errorf("conference ref is required")
	}
	clean := normalizeRegistrationEmails(emails)
	if len(clean) == 0 {
		return 0, nil
	}
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE registrations
		SET checked_in_at = now()
		WHERE conference_id = $1::uuid
			AND lower(email::text) = ANY($2::text[])
			AND checked_in_at IS NULL
			AND revoked = false
			AND lower(type) <> 'sponsored'
			AND (jsonb_array_length(checkin_requirements)=0 OR proof_checked_at IS NOT NULL OR full_ticket_upgraded_at IS NOT NULL)
	`, confRef, clean)
	if err != nil {
		return 0, fmt.Errorf("bulk check in registrations: %w", err)
	}
	return tag.RowsAffected(), nil
}

func SoldTixCount(ctx *config.AppContext, confRef string) (uint, error) {
	if ctx == nil || ctx.DB == nil {
		return 0, fmt.Errorf("database is not configured")
	}
	var count int64
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT count(*)
		FROM registrations
		WHERE conference_id = $1::uuid
			AND lower(type) <> 'sponsored'
			AND revoked = false
	`, confRef).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("count registrations: %w", err)
	}
	return uint(count), nil
}

func FetchRegistrations(ctx *config.AppContext, confRef string) ([]*types.Registration, error) {
	return queryRegistrationsPostgres(ctx, "conf", confRef)
}

func ListRegistrationsByEmail(ctx *config.AppContext, email string) ([]*types.Registration, error) {
	if email == "" {
		return nil, nil
	}
	return queryRegistrationsPostgres(ctx, "email", email)
}

func ListRegistrationsForPerson(ctx *config.AppContext, personID string) ([]*types.Registration, error) {
	if strings.TrimSpace(personID) == "" {
		return nil, nil
	}
	return queryRegistrationsPostgres(ctx, "person", personID)
}

func ListRegistrationsByCheckoutID(ctx *config.AppContext, checkoutID string) ([]*types.Registration, error) {
	if strings.TrimSpace(checkoutID) == "" {
		return nil, nil
	}
	return queryRegistrationsPostgres(ctx, "checkout", checkoutID)
}

func queryRegistrationsPostgres(ctx *config.AppContext, filter string, value string) ([]*types.Registration, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	sql := `
		SELECT r.ref_id, r.checkout_id, coalesce(r.conference_id::text, ''), r.type,
			r.email::text, r.item_bought, coalesce(r.amount_paid, 0),
			r.currency, r.platform, r.registered_at, r.revoked, r.checked_in_at, r.locale
		FROM registrations r
	`
	args := []any{}
	switch filter {
	case "conf":
		if value != "" {
			sql += " WHERE r.conference_id = $1::uuid"
			args = append(args, value)
		}
	case "email":
		sql += ` WHERE r.person_id = (SELECT person_id FROM person_emails WHERE email = $1::citext)
			OR ((SELECT person_id FROM person_emails WHERE email = $1::citext) IS NULL AND r.email = $1::citext)`
		args = append(args, value)
	case "person":
		sql += " WHERE r.person_id = $1::uuid"
		args = append(args, value)
	case "checkout":
		sql += " WHERE r.checkout_id = $1"
		args = append(args, value)
	case "active":
		sql += ` JOIN conferences c ON c.id = r.conference_id
			WHERE c.publication_status = 'published'
			  AND (c.end_date IS NULL OR c.end_date >= now())`
	default:
		return nil, fmt.Errorf("unknown registrations filter %q", filter)
	}
	sql += " ORDER BY r.registered_at DESC NULLS LAST, r.ref_id"

	rows, err := ctx.DB.Query(ctx.DatabaseContext(), sql, args...)
	if err != nil {
		return nil, fmt.Errorf("query registrations: %w", err)
	}
	defer rows.Close()

	var out []*types.Registration
	for rows.Next() {
		var registration types.Registration
		var registeredAt pgtype.Timestamptz
		var checkedInAt pgtype.Timestamptz
		err := rows.Scan(
			&registration.RefID,
			&registration.CheckoutID,
			&registration.ConfRef,
			&registration.Type,
			&registration.Email,
			&registration.ItemBought,
			&registration.Amount,
			&registration.Currency,
			&registration.Platform,
			&registeredAt,
			&registration.Revoked,
			&checkedInAt,
			&registration.Locale,
		)
		if err != nil {
			return nil, fmt.Errorf("scan registration: %w", err)
		}
		if checkedInAt.Valid {
			registration.CheckedInAt = &checkedInAt.Time
		}
		if registeredAt.Valid {
			registration.RegisteredAt = &registeredAt.Time
		}
		out = append(out, &registration)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate registrations: %w", err)
	}
	return out, nil
}

func AddTickets(ctx *config.AppContext, entry *types.Entry, src string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	if entry == nil {
		return fmt.Errorf("AddTickets: entry is nil")
	}
	email := strings.TrimSpace(entry.Email)
	if email == "" {
		return fmt.Errorf("AddTickets: entry email is required")
	}
	if strings.TrimSpace(entry.ConfRef) == "" {
		return fmt.Errorf("AddTickets: entry conference ref is required")
	}

	c := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(c)
	if err != nil {
		return err
	}
	defer tx.Rollback(c)
	var locked string
	if err = tx.QueryRow(c, `SELECT id::text FROM conferences WHERE id=$1 FOR UPDATE`, entry.ConfRef).Scan(&locked); err != nil {
		return err
	}
	if entry.ReservationID != "" {
		var ticketID, providerID, provider, status string
		var qty int
		var linesJSON []byte
		var reservedEmail string
		if err = tx.QueryRow(c, `SELECT ticket_id::text,quantity,provider,provider_id,status,ticket_lines,email FROM ticket_reservations WHERE id=$1 AND conference_id=$2 FOR UPDATE`, entry.ReservationID, entry.ConfRef).Scan(&ticketID, &qty, &provider, &providerID, &status, &linesJSON, &reservedEmail); err != nil {
			return err
		}
		if qty != len(entry.Items) || provider != src || (providerID != "" && providerID != entry.ID) || (entry.TicketID != "" && ticketID != entry.TicketID) {
			return fmt.Errorf("ticket payment does not match its reservation")
		}
		var lines []types.TicketCartLine
		if err = json.Unmarshal(linesJSON, &lines); err != nil {
			return err
		}
		if len(lines) > 0 {
			var expected int64
			var items []types.Item
			for _, line := range lines {
				if !strings.EqualFold(entry.Currency, line.Currency) {
					return fmt.Errorf("ticket currency does not match reservation")
				}
				expected += int64(line.UnitCents) * int64(line.Quantity)
				for n := uint(0); n < line.Quantity; n++ {
					items = append(items, types.Item{TicketID: line.TicketID, DiscountRef: line.DiscountRef, Total: int64(line.UnitCents), Desc: line.Title, Type: line.Kind, BaseCents: line.BaseCents, OriginalCents: line.OriginalCents})
				}
			}
			if len(items) != qty || expected != entry.Total || !strings.EqualFold(strings.TrimSpace(reservedEmail), email) {
				return fmt.Errorf("ticket cart payment does not match reservation")
			}
			entry.Items = items
			entry.Cart = true
			entry.DiscountRef = ""
		}
		entry.TicketID = ticketID
		if status == "fulfilled" {
			entry.AlreadyFulfilled = true
			return tx.Commit(c)
		}
		if _, err = tx.Exec(c, `UPDATE ticket_reservations SET status='fulfilled',provider_id=$2,updated_at=now() WHERE id=$1`, entry.ReservationID, entry.ID); err != nil {
			return err
		}
	}
	if entry.ReservationID == "" && len(entry.Items) > 0 && (entry.Items[0].Type == types.TicketTypeLocal || entry.Items[0].Type == types.TicketTypeStudent) {
		var mapped string
		if e := tx.QueryRow(c, `SELECT id::text FROM conference_tickets WHERE conference_id=$1 AND evergreen AND ticket_kind=$2 ORDER BY ticket_key LIKE 'evergreen-%' DESC,id LIMIT 1`, entry.ConfRef, entry.Items[0].Type).Scan(&mapped); e == nil {
			entry.TicketID = mapped
		}
	}
	for i, item := range entry.Items {
		ticketID, discountRef := entry.TicketID, entry.DiscountRef
		if entry.Cart {
			ticketID, discountRef = item.TicketID, item.DiscountRef
		}
		refID := types.UniqueID(entry.Email, entry.ID, int32(i))
		amountPaid := float64(item.Total) / 100
		_, err := tx.Exec(c, `
			INSERT INTO registrations (
				ref_id, checkout_id, conference_id, discount_id, type, email, person_id,
				item_bought, amount_paid, currency, platform, registered_at, revoked, locale, ticket_id
			)
			VALUES (
				$1, $2, $3::uuid,
				NULLIF($4, '')::uuid,
				$5, $6, (SELECT person_id FROM person_emails WHERE email = $6::citext),
				$7, $8, $9, $10, $11, false, $12, NULLIF($13,'')::uuid
			)
			ON CONFLICT (ref_id) DO UPDATE SET
				ticket_id = coalesce(EXCLUDED.ticket_id,registrations.ticket_id),
				checkout_id = EXCLUDED.checkout_id,
				conference_id = EXCLUDED.conference_id,
				discount_id = EXCLUDED.discount_id,
				type = EXCLUDED.type,
				email = EXCLUDED.email,
				person_id = coalesce(EXCLUDED.person_id, registrations.person_id),
				item_bought = EXCLUDED.item_bought,
				amount_paid = EXCLUDED.amount_paid,
				currency = EXCLUDED.currency,
				platform = EXCLUDED.platform,
				registered_at = EXCLUDED.registered_at,
				locale = EXCLUDED.locale,
				revoked = false
			WHERE registrations.revoked_before_account_deletion IS NULL
		`, refID, entry.ID, entry.ConfRef, discountRef, item.Type, email,
			item.Desc, amountPaid, entry.Currency, src, entry.Created, types.RegistrationLocale(entry.Locale), ticketID)
		if err != nil {
			return fmt.Errorf("upsert registration %q: %w", refID, err)
		}
	}
	return tx.Commit(c)
}

func RevokeTicket(ctx *config.AppContext, lookupID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE registrations
		SET revoked = true
		WHERE checkout_id = $1
	`, lookupID)
	if err != nil {
		return fmt.Errorf("revoke ticket %q: %w", lookupID, err)
	}
	if tag.RowsAffected() == 0 {
		return fmt.Errorf("ticket lookup %s not found", lookupID)
	}
	return nil
}

// RevokeVolunteerTicket is retryable even when scheduling failed before a
// ticket was issued. It only touches volunteer tickets from this signup.
func RevokeVolunteerTicket(ctx *config.AppContext, lookupID string) error {
	if ctx == nil || ctx.DB == nil {
		return fmt.Errorf("database is not configured")
	}
	_, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE registrations SET revoked = true WHERE checkout_id = $1 AND type = 'volunteer'`, lookupID)
	return err
}
