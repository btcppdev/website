package getters

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
)

// Both reads and writes use the same organization, membership, and event scope.
const sponsorPayoutAccessSQL = `
	awards.sponsored_by_org_id = $1::uuid AND awards.archived_at IS NULL
	AND competitions.results_finalized_at IS NOT NULL AND projects.status <> 'hidden'
	AND EXISTS (
		SELECT 1 FROM organization_memberships
		WHERE organization_id = $1::uuid AND person_id = $2::uuid
			AND status = 'active' AND role IN ('owner', 'manager')
	)
	AND EXISTS (
		SELECT 1 FROM sponsorships
		JOIN sponsorships_conferences links ON links.sponsorship_id = sponsorships.id
		WHERE sponsorships.organization_id = $1::uuid
			AND links.conference_id = competitions.conference_id
			AND sponsorships.archived_at IS NULL
			AND lower(sponsorships.status) IN ('paid', 'committed')
	)`

// ListSponsorPayouts only returns this organization's finalized cash winners
// to active owners/managers. Broad submission/contact entitlements do not grant
// access to any other sponsor's payouts.
func ListSponsorPayouts(ctx *config.AppContext, organizationID, viewerPersonID string) ([]*types.SponsorPayout, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	organizationID = strings.TrimSpace(organizationID)
	viewerPersonID = strings.TrimSpace(viewerPersonID)
	if organizationID == "" || viewerPersonID == "" {
		return nil, fmt.Errorf("organization and viewer are required")
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT awards.id::text, awards.title, projects.id::text, projects.title,
			prizes.id::text, prizes.title, prizes.value_text,
			coalesce(nullif(conferences.description, ''), conferences.tag),
			coalesce(people.id::text, ''), coalesce(people.name, ''),
			coalesce(d.id::text, ''), d.amount_sats, coalesce(d.status, ''),
			people.tax_form_uploaded_at IS NOT NULL,
			CASE WHEN people.tax_form_uploaded_at IS NOT NULL
				AND d.status IN ('pending', 'ready') AND d.amount_sats > 0
				THEN people.lightning_address ELSE '' END,
			CASE WHEN people.tax_form_uploaded_at IS NOT NULL
				AND d.status IN ('pending', 'ready') AND d.amount_sats > 0
				THEN people.bitcoin_address ELSE '' END,
			d.completed_at
		FROM awards
		JOIN competitions ON competitions.id = awards.competition_id
		JOIN conferences ON conferences.id = competitions.conference_id
		JOIN project_awards winners ON winners.award_id = awards.id
		JOIN projects ON projects.id = winners.project_id AND projects.competition_id = competitions.id
		JOIN prizes ON prizes.award_id = awards.id AND prizes.prize_type = 'sats'
		LEFT JOIN project_members members ON members.project_id = projects.id
		LEFT JOIN people ON people.id = members.person_id
		LEFT JOIN award_distributions d ON d.award_id = awards.id
			AND d.competition_id = competitions.id AND d.project_id = projects.id
			AND d.prize_id = prizes.id AND d.person_id = people.id AND d.distribution_type = 'sats'
		WHERE `+sponsorPayoutAccessSQL+`
		ORDER BY conferences.start_date DESC NULLS LAST, conferences.id,
			awards.title, awards.id, projects.title, projects.id, prizes.created_at, prizes.id,
			people.name, people.id
	`, organizationID, viewerPersonID)
	if err != nil {
		return nil, fmt.Errorf("list sponsor payouts: %w", err)
	}
	defer rows.Close()
	var out []*types.SponsorPayout
	byPrize := make(map[string]*types.SponsorPayout)
	for rows.Next() {
		var payout types.SponsorPayout
		var recipient types.SponsorPayoutRecipient
		var prizeValueText string
		if err := rows.Scan(&payout.AwardID, &payout.AwardTitle, &payout.ProjectID, &payout.ProjectTitle,
			&payout.PrizeID, &payout.PrizeTitle, &prizeValueText, &payout.ConferenceTitle,
			&recipient.PersonID, &recipient.Name, &recipient.DistributionID, &recipient.AmountSats,
			&recipient.Status, &recipient.TaxFormOnFile, &recipient.LightningAddress,
			&recipient.BitcoinAddress, &recipient.CompletedAt); err != nil {
			return nil, fmt.Errorf("scan sponsor payout: %w", err)
		}
		if sats, err := strconv.ParseInt(strings.TrimSpace(prizeValueText), 10, 64); err == nil && sats > 0 {
			payout.PrizeSats = sats
		}
		key := payout.ProjectID + ":" + payout.PrizeID
		group := byPrize[key]
		if group == nil {
			group = &payout
			byPrize[key] = group
			out = append(out, group)
		}
		if recipient.PersonID != "" {
			recipient.LightningAddress = strings.TrimSpace(recipient.LightningAddress)
			recipient.BitcoinAddress = strings.TrimSpace(recipient.BitcoinAddress)
			group.Recipients = append(group.Recipients, &recipient)
		}
	}
	return out, rows.Err()
}

var ErrSponsorPayoutUnavailable = errors.New("payout is not available to mark as paid")
var ErrSponsorPayoutReference = errors.New("enter a payment reference of 1 to 500 characters")

// MarkSponsorPayoutPaid records a payment made outside this application. The row
// lock makes repeated/concurrent submissions idempotent, and the audit event is
// committed with the existing admin distribution record.
func MarkSponsorPayoutPaid(ctx *config.AppContext, organizationID, viewerPersonID, distributionID, reference string) (bool, error) {
	if ctx == nil || ctx.DB == nil {
		return false, fmt.Errorf("database is not configured")
	}
	reference = strings.TrimSpace(reference)
	if reference == "" || len([]rune(reference)) > 500 {
		return false, ErrSponsorPayoutReference
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.Begin(dbctx)
	if err != nil {
		return false, fmt.Errorf("begin sponsor payout confirmation: %w", err)
	}
	defer tx.Rollback(dbctx)
	var status, conferenceID string
	var payable bool
	if err := tx.QueryRow(dbctx, `
		SELECT d.status, competitions.conference_id::text,
			coalesce(d.amount_sats > 0 AND people.tax_form_uploaded_at IS NOT NULL
				AND (trim(people.lightning_address) <> '' OR trim(people.bitcoin_address) <> ''), false)
		FROM award_distributions d
		JOIN awards ON awards.id = d.award_id
		JOIN competitions ON competitions.id = d.competition_id AND competitions.id = awards.competition_id
		JOIN projects ON projects.id = d.project_id AND projects.competition_id = competitions.id
		JOIN project_awards winners ON winners.project_id = projects.id AND winners.award_id = awards.id
		JOIN prizes ON prizes.id = d.prize_id AND prizes.award_id = awards.id AND prizes.prize_type = 'sats'
		JOIN project_members members ON members.project_id = projects.id AND members.person_id = d.person_id
		JOIN people ON people.id = d.person_id
		WHERE `+sponsorPayoutAccessSQL+` AND d.id = $3::uuid AND d.distribution_type = 'sats'
		FOR UPDATE OF d
	`, strings.TrimSpace(organizationID), strings.TrimSpace(viewerPersonID), strings.TrimSpace(distributionID)).Scan(&status, &conferenceID, &payable); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return false, ErrSponsorPayoutUnavailable
		}
		return false, fmt.Errorf("load sponsor payout for confirmation: %w", err)
	}
	if status == "sent" || status == "claimed" {
		return false, nil
	}
	if !payable || (status != "pending" && status != "ready") {
		return false, ErrSponsorPayoutUnavailable
	}
	if _, err := tx.Exec(dbctx, `
		UPDATE award_distributions SET status = 'sent', completed_at = now(), completed_by = $2::uuid,
			notes = concat_ws(E'\n', nullif(notes, ''), 'Sponsor reported payment: ' || $3::text)
		WHERE id = $1::uuid
	`, distributionID, viewerPersonID, reference); err != nil {
		return false, fmt.Errorf("record sponsor payout: %w", err)
	}
	if _, err := tx.Exec(dbctx, `
		INSERT INTO sponsor_audit_events (organization_id, conference_id, actor_person_id,
			action, target_type, target_id, details)
		VALUES ($1::uuid, $2::uuid, $3::uuid, 'sponsor.payout_marked_paid', 'award_distribution', $4,
			jsonb_build_object('payment_reference', $5::text, 'previous_status', $6::text))
	`, organizationID, conferenceID, viewerPersonID, distributionID, reference, status); err != nil {
		return false, fmt.Errorf("audit sponsor payout: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return false, fmt.Errorf("commit sponsor payout confirmation: %w", err)
	}
	return true, nil
}
