package getters

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"math/big"
	"time"

	"btcpp-web/internal/config"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type PersonDeletionPreview struct {
	ID, Name                                string
	Emails                                  []string
	Talks, Projects, Judging, Registrations int
}

func PreviewPersonDeletion(app *config.AppContext, personID string) (*PersonDeletionPreview, error) {
	if _, err := uuid.Parse(personID); err != nil {
		return nil, fmt.Errorf("choose an existing person")
	}
	p := &PersonDeletionPreview{ID: personID}
	err := app.DB.QueryRow(app.DatabaseContext(), `SELECT name,
 ARRAY(SELECT email::text FROM person_emails WHERE person_id=p.id ORDER BY email),
 (SELECT count(DISTINCT ps.proposal_id) FROM speaker_confs s JOIN proposals_speaker_confs ps ON ps.speaker_conf_id=s.id WHERE s.speaker_id=p.id),
 (SELECT count(*) FROM projects pr WHERE pr.created_by_person_id=p.id OR EXISTS(SELECT 1 FROM project_members pm WHERE pm.project_id=pr.id AND pm.person_id=p.id)),
 (SELECT count(*) FROM scorecards WHERE judge_person_id=p.id),
 (SELECT count(*) FROM registrations WHERE person_id=p.id)
 FROM people p WHERE id=$1::uuid AND NOT is_deleted_account`, personID).Scan(&p.Name, &p.Emails, &p.Talks, &p.Projects, &p.Judging, &p.Registrations)
	if err != nil {
		return nil, fmt.Errorf("person not available for deletion")
	}
	return p, nil
}

// DeletePerson creates a fresh, non-authenticating attribution identity and
// removes the source account in one transaction. It deliberately does not use
// MergePeople, which retains credentials and reversible profile snapshots.
func DeletePerson(app *config.AppContext, personID, actorID string) error {
	parsedPerson, err := uuid.Parse(personID)
	if err != nil {
		return fmt.Errorf("invalid person")
	}
	personID = parsedPerson.String()
	parsedActor, err := uuid.Parse(actorID)
	if err != nil {
		return fmt.Errorf("invalid administrator")
	}
	actorID = parsedActor.String()
	if personID == actorID {
		return fmt.Errorf("you cannot delete your own account")
	}
	ctx, cancel := context.WithTimeout(app.DatabaseContext(), 30*time.Second)
	defer cancel()
	tx, err := app.DB.BeginTx(ctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	var deleted bool
	if err = tx.QueryRow(ctx, `SELECT is_deleted_account FROM people WHERE id=$1::uuid FOR UPDATE`, personID).Scan(&deleted); err != nil {
		return fmt.Errorf("person no longer exists")
	}
	if deleted {
		return fmt.Errorf("this is already a deleted account")
	}
	var admin bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM people_roles WHERE person_id=$1::uuid AND scope='global' AND position='admin')`, actorID).Scan(&admin); err != nil {
		return err
	}
	if !admin {
		return fmt.Errorf("global administrator required")
	}
	var conflicts bool
	if err = tx.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM person_email_conflicts WHERE person_id=$1::uuid)`, personID).Scan(&conflicts); err != nil {
		return err
	}
	if conflicts {
		return fmt.Errorf("resolve this person's shared email conflicts before deleting the account")
	}
	var emails []string
	if err = tx.QueryRow(ctx, `SELECT ARRAY(SELECT lower(email::text) FROM person_emails WHERE person_id=$1::uuid)`, personID).Scan(&emails); err != nil {
		return err
	}
	var replacement string
	// Generate once and persist the alias so historical credits stay consistent.
	// The serializable transaction also protects the collision check.
	for attempt := 0; attempt < 20; attempt++ {
		number, randomErr := rand.Int(rand.Reader, big.NewInt(100000))
		if randomErr != nil {
			return fmt.Errorf("generate anonymous alias: %w", randomErr)
		}
		alias := fmt.Sprintf("anon%05d", number.Int64())
		err = tx.QueryRow(ctx, `INSERT INTO people(name,is_deleted_account)
			SELECT $1,true WHERE NOT EXISTS (SELECT 1 FROM people WHERE name=$1)
			RETURNING id::text`, alias).Scan(&replacement)
		if errors.Is(err, pgx.ErrNoRows) {
			continue
		}
		if err != nil {
			return err
		}
		break
	}
	if replacement == "" {
		return fmt.Errorf("could not allocate an anonymous alias")
	}
	anonymousEmail := "deleted-" + replacement + "@invalid.invalid"
	// Relinquish ownership before transferring attribution. Pick the oldest live
	// teammate deterministically; a solo project remains administrable without
	// assigning permissions to its anonymous historical contributor.
	rows, err := tx.Query(ctx, `UPDATE project_members SET role='member'
		WHERE person_id=$1::uuid AND role='owner' RETURNING project_id::text`, personID)
	if err != nil {
		return err
	}
	ownedProjects, err := pgx.CollectRows(rows, pgx.RowTo[string])
	if err != nil {
		return err
	}
	for _, projectID := range ownedProjects {
		if _, err = tx.Exec(ctx, `UPDATE project_members SET role='owner'
			WHERE project_id=$1::uuid AND person_id=(
				SELECT m.person_id FROM project_members m JOIN people p ON p.id=m.person_id
				WHERE m.project_id=$1::uuid AND m.person_id<>$2::uuid AND NOT p.is_deleted_account
				ORDER BY m.created_at,m.person_id LIMIT 1)`, projectID, personID); err != nil {
			return err
		}
	}
	// Keep only historical contribution relationships, never credentials, roles,
	// consents, or membership permissions. Each replacement is unique.
	for _, rel := range [][2]string{{"speaker_confs", "speaker_id"}, {"projects", "created_by_person_id"}, {"project_members", "person_id"}, {"scorecards", "judge_person_id"}, {"award_votes", "judge_person_id"}, {"judge_ballot_submissions", "judge_person_id"}, {"competition_judges", "person_id"}, {"award_judges", "person_id"}, {"award_distributions", "person_id"}} {
		q := fmt.Sprintf(`UPDATE %s SET %s=$2::uuid WHERE %s=$1::uuid`, pgx.Identifier{rel[0]}.Sanitize(), pgx.Identifier{rel[1]}.Sanitize(), pgx.Identifier{rel[1]}.Sanitize())
		if _, err = tx.Exec(ctx, q, personID, replacement); err != nil {
			return fmt.Errorf("preserve %s: %w", rel[0], err)
		}
	}
	// Provider response snapshots can contain the same names and addresses as
	// the normalized order fields. Clear them before changing the order owner.
	orderWhere := `order_id IN (SELECT id FROM shop_orders WHERE buyer_person_id=$1::uuid OR (buyer_person_id IS NULL AND lower(buyer_email::text)=ANY($4::text[])))`
	orderQueries := []string{
		`UPDATE shipping_rate_quotes SET destination_country='',destination_region='',destination_postal_code='',raw_response='{}' WHERE ` + orderWhere,
		`UPDATE tax_quotes SET destination_country='',destination_region='',destination_postal_code='',raw_tax_response='{}',raw_import_response='{}' WHERE ` + orderWhere,
		`UPDATE tax_transactions SET raw_response='{}' WHERE ` + orderWhere,
		`UPDATE refunds SET raw_response='{}',reason='',requested_by='' WHERE ` + orderWhere,
		`DELETE FROM merch_sale_notifications WHERE ` + orderWhere,
		`UPDATE shop_events SET actor_email=NULL,metadata='{}' WHERE ` + orderWhere + ` OR lower(actor_email::text)=ANY($4::text[])`,
		`DELETE FROM easyship_webhook_events WHERE easyship_shipment_id IN (SELECT provider_shipment_id FROM shipments WHERE ` + orderWhere + `)`,
		`UPDATE shipments SET raw_response='{}',tracking_number='',tracking_url='',label_url='',last_error='' WHERE ` + orderWhere,
	}
	for _, q := range orderQueries {
		if _, err = tx.Exec(ctx, `WITH deletion_parameters AS (SELECT $1::uuid,$2::uuid,$3::text,$4::text[]) `+q, personID, replacement, anonymousEmail, emails); err != nil {
			return fmt.Errorf("clear order contact snapshots: %w", err)
		}
	}
	queries := []string{
		`UPDATE speaker_confs SET organization_id=NULL,coming_from='',availability='{}',visa='',company='',org_photo_path='',dinner_rsvp=false,featured_rank=NULL WHERE speaker_id=$2::uuid`,
		`UPDATE award_distributions SET notes='' WHERE person_id=$2::uuid`,
		`UPDATE scorecards SET comments='' WHERE judge_person_id=$2::uuid`,
		`UPDATE award_votes SET notes='' WHERE judge_person_id=$2::uuid`,
		`UPDATE proposals p SET
 invite_token=CASE WHEN a.shared AND p.invite_token<>'' THEN gen_random_uuid()::text ELSE '' END,
 comments=CASE WHEN a.shared THEN p.comments ELSE '' END,
 setup=CASE WHEN a.shared THEN p.setup ELSE '' END
 FROM (
 SELECT p.id, EXISTS(SELECT 1 FROM proposals_speaker_confs ps JOIN speaker_confs s ON s.id=ps.speaker_conf_id JOIN people person ON person.id=s.speaker_id WHERE ps.proposal_id=p.id AND NOT person.is_deleted_account) AS shared
 FROM proposals p WHERE EXISTS(SELECT 1 FROM proposals_speaker_confs ps JOIN speaker_confs s ON s.id=ps.speaker_conf_id WHERE ps.proposal_id=p.id AND s.speaker_id=$2::uuid)
 ) a WHERE p.id=a.id`,
		`UPDATE registrations SET person_id=$2::uuid,email=$3,revoked_before_account_deletion=revoked,revoked=true WHERE person_id=$1::uuid OR (person_id IS NULL AND lower(email::text)=ANY($4::text[]))`,
		`UPDATE volunteers SET person_id=$2::uuid,availability='{}',contact_at='',comments='',discovered_via='',hometown='',subscribe=false WHERE person_id=$1::uuid`,
		`DELETE FROM shop_order_addresses WHERE order_id IN (SELECT id FROM shop_orders WHERE buyer_person_id=$1::uuid OR (buyer_person_id IS NULL AND lower(buyer_email::text)=ANY($4::text[])))`,
		`UPDATE shop_orders SET buyer_person_id=$2::uuid,buyer_email=$3,buyer_name=(SELECT name FROM people WHERE id=$2::uuid),admin_notes='' WHERE buyer_person_id=$1::uuid OR (buyer_person_id IS NULL AND lower(buyer_email::text)=ANY($4::text[]))`,
		`UPDATE discounts SET affiliate_person_id=NULL,affiliate_email=NULL WHERE affiliate_person_id=$1::uuid OR lower(affiliate_email::text)=ANY($4::text[])`,
		`UPDATE affiliate_usages SET affiliate_person_id=NULL,affiliate_email=$3 WHERE affiliate_person_id=$1::uuid OR lower(affiliate_email::text)=ANY($4::text[])`,
		`DELETE FROM organization_applications WHERE submitted_by_person_id=$1::uuid`,
		`DELETE FROM auth_audit_events WHERE person_id=$1::uuid`,
		`DELETE FROM sponsor_audit_events WHERE actor_person_id=$1::uuid`,
		`DELETE FROM person_merge_requests WHERE requester_person_id=$1::uuid OR target_person_id=$1::uuid`,
		// Snapshots can embed this person's profile/aliases even in another person's
		// merge history. Removing those snapshots also prevents a later undo restoring it.
		`DELETE FROM person_merge_events WHERE canonical_person_id=$1::uuid OR source_person_id=$1::uuid OR position($1::text in relationship_manifest::text)>0`,
		`DELETE FROM magic_login_tokens WHERE lower(email::text)=ANY($4::text[])`,
		`DELETE FROM subscribers WHERE lower(email::text)=ANY($4::text[])`,
		`DELETE FROM project_invites WHERE accepted_by_person_id=$1::uuid OR lower(email::text)=ANY($4::text[])`,
		`DELETE FROM competition_judge_invites WHERE accepted_by_person_id=$1::uuid OR lower(email::text)=ANY($4::text[])`,
		`DELETE FROM organization_member_invites WHERE accepted_by_person_id=$1::uuid OR lower(email::text)=ANY($4::text[])`,
		`DELETE FROM conference_email_deliveries WHERE lower(email::text)=ANY($4::text[])`,
		`DELETE FROM conference_email_occurrences WHERE lower(target_email::text)=ANY($4::text[])`,
		`UPDATE sponsor_ticket_issuances SET recipient_email=$3 WHERE lower(recipient_email::text)=ANY($4::text[])`,
		// SCS's default GobCodec stores string values in the bytea payload. Remove
		// both UUID-based and legacy email-only sessions. Missing accounts also fail
		// session-version validation, including concurrently saved sessions.
		`DELETE FROM sessions WHERE position(convert_to($1::text,'UTF8') in data)>0 OR EXISTS(SELECT 1 FROM unnest($4::text[]) e WHERE position(convert_to(e,'UTF8') in data)>0)`,
	}
	for _, q := range queries {
		// A typed parameter CTE lets statements use any subset without pgx's unused
		// parameter type inference failures.
		q = `WITH deletion_parameters AS (SELECT $1::uuid,$2::uuid,$3::text,$4::text[]) ` + q
		if _, err = tx.Exec(ctx, q, personID, replacement, anonymousEmail, emails); err != nil {
			return fmt.Errorf("remove account data: %w", err)
		}
	}
	if _, err = tx.Exec(ctx, `DELETE FROM people WHERE id=$1::uuid`, personID); err != nil {
		return fmt.Errorf("delete account: %w", err)
	}
	return tx.Commit(ctx)
}
