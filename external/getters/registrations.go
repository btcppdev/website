package getters

import (
	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
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
	tag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE registrations
		SET checked_in_at = now()
		WHERE ref_id = $1 AND checked_in_at IS NULL AND revoked = false
	`, ticket)
	if err != nil {
		return "", false, fmt.Errorf("check in registration: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return "", true, fmt.Errorf("Already checked in")
	}
	return ticketType, true, nil
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
		SELECT r.ref_id, coalesce(r.conference_id::text, ''), r.type,
			r.email::text, r.item_bought, coalesce(r.amount_paid, 0),
			r.currency, r.revoked, r.checked_in_at
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
		sql += " WHERE r.email = $1"
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
		var checkedInAt pgtype.Timestamptz
		err := rows.Scan(
			&registration.RefID,
			&registration.ConfRef,
			&registration.Type,
			&registration.Email,
			&registration.ItemBought,
			&registration.Amount,
			&registration.Currency,
			&registration.Revoked,
			&checkedInAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scan registration: %w", err)
		}
		if checkedInAt.Valid {
			registration.CheckedInAt = &checkedInAt.Time
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

	for i, item := range entry.Items {
		refID := types.UniqueID(entry.Email, entry.ID, int32(i))
		amountPaid := float64(item.Total) / 100
		_, err := ctx.DB.Exec(ctx.DatabaseContext(), `
			INSERT INTO registrations (
				ref_id, checkout_id, conference_id, discount_id, type, email,
				item_bought, amount_paid, currency, platform, registered_at, revoked
			)
			VALUES (
				$1, $2, $3::uuid,
				NULLIF($4, '')::uuid,
				$5, $6, $7, $8, $9, $10, $11, false
			)
			ON CONFLICT (ref_id) DO UPDATE SET
				checkout_id = EXCLUDED.checkout_id,
				conference_id = EXCLUDED.conference_id,
				discount_id = EXCLUDED.discount_id,
				type = EXCLUDED.type,
				email = EXCLUDED.email,
				item_bought = EXCLUDED.item_bought,
				amount_paid = EXCLUDED.amount_paid,
				currency = EXCLUDED.currency,
				platform = EXCLUDED.platform,
				registered_at = EXCLUDED.registered_at,
				revoked = false
		`, refID, entry.ID, entry.ConfRef, entry.DiscountRef, item.Type, email,
			item.Desc, amountPaid, entry.Currency, src, entry.Created)
		if err != nil {
			return fmt.Errorf("upsert registration %q: %w", refID, err)
		}
	}
	return nil
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
