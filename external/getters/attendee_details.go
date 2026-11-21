package getters

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5"
)

var ErrAttendeeDetailsConflict = errors.New("these tickets changed; reload before saving")

const attendeeSelect = `SELECT r.ref_id,r.checkout_id,r.conference_id::text,r.email::text,
 coalesce(r.person_id::text,''),r.attendee_access_key::text,r.type,r.attendee_name,
 r.attendee_tshirt,r.dietary_preference,r.dietary_other,r.food_allergies,r.attendee_is_buyer,r.attendee_version,
 coalesce(p.name,''),coalesce(p.tshirt,''),f.attendee_name,f.tshirt,
 coalesce(p.norm_photo_path,''),coalesce(p.twitter_handle,''),r.booklet_choice,r.booklet_name,r.booklet_use_avatar,
 coalesce(p.dietary_preference,''),coalesce(p.dietary_other,''),coalesce(p.food_allergies,'')
 FROM registrations r LEFT JOIN people p ON p.id=r.person_id
 JOIN registration_attendee_fulfillment f ON f.ref_id=r.ref_id
 WHERE NOT r.revoked AND lower(r.type)<>'sponsored'`

type attendeeQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
}

func queryAttendeeDetails(c context.Context, q attendeeQuerier, condition string, args ...any) ([]*types.TicketAttendeeDetails, error) {
	rows, err := q.Query(c, attendeeSelect+condition, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*types.TicketAttendeeDetails
	for rows.Next() {
		d := new(types.TicketAttendeeDetails)
		if err = rows.Scan(&d.RefID, &d.CheckoutID, &d.ConferenceID, &d.OwnerEmail, &d.OwnerPersonID, &d.AccessKey, &d.Type, &d.Name, &d.TShirt, &d.Diet, &d.DietOther, &d.Allergies, &d.IsBuyer, &d.Version, &d.ProfileName, &d.ProfileShirt, &d.EffectiveName, &d.EffectiveShirt, &d.ProfilePhoto, &d.ProfileHandle, &d.BookletChoice, &d.BookletName, &d.BookletUseAvatar, &d.ProfileDiet, &d.ProfileDietOther, &d.ProfileAllergies); err != nil {
			return nil, err
		}
		d.TShirt = types.ShirtSizeChoiceCode(d.TShirt)
		d.ProfileShirt = types.ShirtSizeChoiceCode(d.ProfileShirt)
		out = append(out, d)
	}
	return out, rows.Err()
}

func GetTicketAttendeeDetails(app *config.AppContext, ref string) (*types.TicketAttendeeDetails, error) {
	if app == nil || app.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	rows, err := queryAttendeeDetails(app.DatabaseContext(), app.DB, " AND r.ref_id=$1", ref)
	if err != nil {
		return nil, err
	}
	if len(rows) != 1 {
		return nil, pgx.ErrNoRows
	}
	return rows[0], nil
}

func TicketAttendeeGroup(app *config.AppContext, anchor *types.TicketAttendeeDetails) ([]*types.TicketAttendeeDetails, error) {
	return queryAttendeeDetails(app.DatabaseContext(), app.DB, ` AND r.conference_id=$1 AND r.email=$2::citext
 AND (r.ref_id=$3 OR ($4<>'' AND r.checkout_id=$4)) ORDER BY r.ref_id`, anchor.ConferenceID, anchor.OwnerEmail, anchor.RefID, anchor.CheckoutID)
}

func ConferenceAttendeeDetails(app *config.AppContext, confID string) ([]*types.TicketAttendeeDetails, error) {
	return queryAttendeeDetails(app.DatabaseContext(), app.DB, " AND r.conference_id=$1 ORDER BY r.ref_id", confID)
}

func PersonTicketAttendeeDetails(app *config.AppContext, personID string) (map[string]*types.TicketAttendeeDetails, error) {
	rows, err := queryAttendeeDetails(app.DatabaseContext(), app.DB, " AND r.person_id=$1::uuid ORDER BY r.ref_id", personID)
	if err != nil {
		return nil, err
	}
	result := make(map[string]*types.TicketAttendeeDetails, len(rows))
	for _, row := range rows {
		result[row.RefID] = row
	}
	return result, nil
}

func GetPersonAttendeePreferences(app *config.AppContext, personID string) (types.AttendeePreferences, error) {
	var p types.AttendeePreferences
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT tshirt,dietary_preference,dietary_other,food_allergies FROM people WHERE id=$1::uuid AND NOT is_deleted_account`, personID).Scan(&p.TShirt, &p.Diet, &p.DietOther, &p.Allergies)
	p.TShirt = types.ValidShirtSizeCode(p.TShirt)
	return p, err
}

func CompletedReservationCheckout(app *config.AppContext, reservationID, confID string) (string, string, error) {
	var checkout, email string
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT provider_id,email::text FROM ticket_reservations WHERE id=$1 AND conference_id=$2 AND status='fulfilled'`, reservationID, confID).Scan(&checkout, &email)
	return checkout, email, err
}

func TicketReservationAwaitingConfirmation(app *config.AppContext, reservationID, confID string) bool {
	if reservationID == "" {
		return false
	}
	var pending bool
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT EXISTS(SELECT 1 FROM ticket_reservations WHERE id=$1 AND conference_id=$2 AND status='pending' AND provider_id<>'')`, reservationID, confID).Scan(&pending)
	return err == nil && pending
}

// Save the complete displayed group atomically. Each row's revision and access
// key protect against stale tabs, concurrent edits, revocation and transfers.
func SaveTicketAttendeeDetails(app *config.AppContext, anchor *types.TicketAttendeeDetails, input []*types.TicketAttendeeDetails) error {
	if len(input) == 0 || len(input) > 100 {
		return fmt.Errorf("invalid number of tickets")
	}
	c := app.DatabaseContext()
	tx, err := app.DB.Begin(c)
	if err != nil {
		return err
	}
	defer tx.Rollback(c)
	current, err := queryAttendeeDetails(c, tx, ` AND r.conference_id=$1 AND r.email=$2::citext
 AND (r.ref_id=$3 OR ($4<>'' AND r.checkout_id=$4)) ORDER BY r.ref_id FOR UPDATE OF r`, anchor.ConferenceID, anchor.OwnerEmail, anchor.RefID, anchor.CheckoutID)
	if err != nil {
		return err
	}
	if len(current) != len(input) {
		return ErrAttendeeDetailsConflict
	}
	allowed := make(map[string]*types.TicketAttendeeDetails, len(current))
	foundAnchor := false
	for _, row := range current {
		allowed[row.RefID] = row
		if row.RefID == anchor.RefID && row.AccessKey == anchor.AccessKey {
			foundAnchor = true
		}
	}
	if !foundAnchor {
		return ErrAttendeeDetailsConflict
	}
	buyerCount := 0
	for _, row := range input {
		old, ok := allowed[row.RefID]
		if !ok || old.Version != row.Version || old.AccessKey != row.AccessKey {
			return ErrAttendeeDetailsConflict
		}
		delete(allowed, row.RefID)
		row.Name = strings.TrimSpace(row.Name)
		row.DietOther = strings.TrimSpace(row.DietOther)
		row.Allergies = strings.TrimSpace(row.Allergies)
		if utf8.RuneCountInString(row.Name) > 100 || utf8.RuneCountInString(row.DietOther) > 500 || utf8.RuneCountInString(row.Allergies) > 500 {
			return fmt.Errorf("attendee details are too long")
		}
		if row.TShirt != "" && types.ValidShirtSizeCode(row.TShirt) != row.TShirt {
			return fmt.Errorf("choose a valid shirt size")
		}
		if !types.ValidDietaryPreference(row.Diet) {
			return fmt.Errorf("choose a valid food preference")
		}
		if row.Diet != "other" {
			row.DietOther = ""
		} else if row.DietOther == "" {
			return fmt.Errorf("describe the other food preference")
		}
		if row.IsBuyer {
			if row.TShirt == "" {
				row.TShirt = old.ProfileShirt
			}
			buyerCount++
			if buyerCount > 1 {
				return fmt.Errorf("only one ticket can use your profile")
			}
		}
		// Eligibility comes from the locked registration, never from form input.
		if !old.IsSupporter() {
			row.BookletChoice, row.BookletName, row.BookletUseAvatar = "", "", false
		} else {
			switch row.BookletChoice {
			case "", "private":
				row.BookletName, row.BookletUseAvatar = "", false
			case "named":
				row.BookletName = strings.TrimSpace(row.BookletName)
				if row.BookletName == "" || utf8.RuneCountInString(row.BookletName) > 100 || strings.ContainsAny(row.BookletName, "\r\n") {
					return fmt.Errorf("enter a booklet name or username of up to 100 characters")
				}
				// Group guests must not inherit the purchaser's avatar.
				row.BookletUseAvatar = row.BookletUseAvatar && row.IsBuyer
			default:
				return fmt.Errorf("choose a valid booklet acknowledgement")
			}
		}
		_, err = tx.Exec(c, `UPDATE registrations SET attendee_name=$2,attendee_tshirt=$3,dietary_preference=$4,
 dietary_other=$5,food_allergies=$6,attendee_is_buyer=$7,booklet_choice=$8,booklet_name=$9,booklet_use_avatar=$10,
 attendee_version=attendee_version+1 WHERE ref_id=$1`, row.RefID, row.Name, row.TShirt, row.Diet, row.DietOther, row.Allergies, row.IsBuyer, row.BookletChoice, row.BookletName, row.BookletUseAvatar)
		if err != nil {
			return err
		}
		// Only the explicitly selected personal ticket can update the purchaser's profile.
		if row.IsBuyer && old.OwnerPersonID != "" {
			if _, err = tx.Exec(c, `UPDATE people SET tshirt=CASE WHEN $2<>'' THEN $2 ELSE tshirt END,
 dietary_preference=CASE WHEN $3<>'' THEN $3 ELSE dietary_preference END,
 dietary_other=CASE WHEN $3<>'' THEN $4 ELSE dietary_other END,food_allergies=$5 WHERE id=$1`, old.OwnerPersonID, row.TShirt, row.Diet, row.DietOther, row.Allergies); err != nil {
				return err
			}
		}
	}
	return tx.Commit(c)
}
