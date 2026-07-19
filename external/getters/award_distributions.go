package getters

import (
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5/pgtype"
)

func ListTicketEntitlementsForPerson(ctx *config.AppContext, personID string) ([]*types.HackathonTicketEntitlement, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT e.id::text, e.person_id::text, e.award_distribution_id::text,
			a.title, projects.id::text, projects.title, award_conferences.tag, e.quantity,
			coalesce(e.claimed_conference_id::text, ''), coalesce(claimed_conferences.tag, ''),
			coalesce(e.claimed_registration_id::text, ''), e.claimed_at, e.voided_at, e.created_at
		FROM hackathon_ticket_entitlements e
		JOIN award_distributions d ON d.id = e.award_distribution_id
		JOIN competitions c ON c.id = d.competition_id
		JOIN conferences award_conferences ON award_conferences.id = c.conference_id
		JOIN awards a ON a.id = d.award_id
		JOIN projects ON projects.id = d.project_id
		LEFT JOIN conferences claimed_conferences ON claimed_conferences.id = e.claimed_conference_id
		WHERE e.person_id = $1::uuid AND c.results_finalized_at IS NOT NULL
		ORDER BY e.claimed_at NULLS FIRST, e.created_at
	`, personID)
	if err != nil {
		return nil, fmt.Errorf("list ticket entitlements: %w", err)
	}
	defer rows.Close()
	var out []*types.HackathonTicketEntitlement
	for rows.Next() {
		var e types.HackathonTicketEntitlement
		var claimed, voided pgtype.Timestamptz
		if err := rows.Scan(&e.ID, &e.PersonID, &e.AwardDistributionID, &e.AwardTitle,
			&e.ProjectID, &e.ProjectTitle, &e.HackathonConferenceTag, &e.Quantity,
			&e.ClaimedConferenceID, &e.ClaimedConferenceTag,
			&e.ClaimedRegistrationID, &claimed, &voided, &e.CreatedAt); err != nil {
			return nil, fmt.Errorf("scan ticket entitlement: %w", err)
		}
		e.ClaimedAt = pgTimePtr(claimed)
		e.VoidedAt = pgTimePtr(voided)
		out = append(out, &e)
	}
	return out, rows.Err()
}

func ClaimTicketEntitlement(ctx *config.AppContext, entitlementID, personID, conferenceID, email string) error {
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return fmt.Errorf("begin ticket claim: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	var quantity int
	var distributionID string
	if err := tx.QueryRow(ctx.DatabaseContext(), `
		SELECT e.quantity, e.award_distribution_id::text
		FROM hackathon_ticket_entitlements e
		JOIN award_distributions d ON d.id = e.award_distribution_id
		JOIN competitions c ON c.id = d.competition_id
		WHERE e.id = $1::uuid AND e.person_id = $2::uuid
			AND e.claimed_at IS NULL AND e.voided_at IS NULL
			AND d.status <> 'cancelled' AND c.results_finalized_at IS NOT NULL
		FOR UPDATE
	`, entitlementID, personID).Scan(&quantity, &distributionID); err != nil {
		return fmt.Errorf("ticket entitlement is not available to claim")
	}
	var confDesc string
	if err := tx.QueryRow(ctx.DatabaseContext(), `
		SELECT description FROM conferences
		WHERE id = $1::uuid AND active = true AND (end_date IS NULL OR end_date > now())
	`, conferenceID).Scan(&confDesc); err != nil {
		return fmt.Errorf("selected conference is not eligible")
	}
	var firstRegistrationID string
	for i := 0; i < quantity; i++ {
		refID := types.UniqueID(email, entitlementID, int32(i))
		var registrationID string
		if err := tx.QueryRow(ctx.DatabaseContext(), `
			INSERT INTO registrations (ref_id, checkout_id, conference_id, type, email, item_bought, amount_paid, currency, platform, registered_at, revoked)
			VALUES ($1, $2, $3::uuid, $4, $5, $6, 0, 'USD', 'hackathon-award', $7, false)
			ON CONFLICT (ref_id) DO UPDATE SET revoked = false
			RETURNING id::text
		`, refID, "hackathon-entitlement-"+entitlementID, conferenceID, "genpop", strings.TrimSpace(email), confDesc, time.Now()).Scan(&registrationID); err != nil {
			return fmt.Errorf("issue claimed ticket: %w", err)
		}
		if firstRegistrationID == "" {
			firstRegistrationID = registrationID
		}
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `
		UPDATE hackathon_ticket_entitlements
		SET claimed_conference_id = $2::uuid, claimed_registration_id = $3::uuid, claimed_at = now()
		WHERE id = $1::uuid
	`, entitlementID, conferenceID, firstRegistrationID); err != nil {
		return fmt.Errorf("complete ticket claim: %w", err)
	}
	if _, err := tx.Exec(ctx.DatabaseContext(), `UPDATE award_distributions SET status = 'claimed', completed_at = now() WHERE id = $1::uuid`, distributionID); err != nil {
		return fmt.Errorf("complete ticket distribution: %w", err)
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return fmt.Errorf("commit ticket claim: %w", err)
	}
	return nil
}

type AwardDistributionInput struct {
	CompetitionID    string
	AwardID          string
	ProjectID        string
	PrizeID          string
	PersonID         string
	DistributionType string
	AmountSats       *int64
	TicketQuantity   *int
	Notes            string
}

func CreateAwardDistribution(ctx *config.AppContext, in AwardDistributionInput) (string, error) {
	in.DistributionType = normalizePrizeType(in.DistributionType)
	if in.CompetitionID == "" || in.AwardID == "" || in.ProjectID == "" || in.PrizeID == "" || in.PersonID == "" {
		return "", fmt.Errorf("award, project, prize, and recipient are required")
	}
	if in.DistributionType == PrizeTypeSats && (in.AmountSats == nil || *in.AmountSats <= 0) {
		return "", fmt.Errorf("a positive satoshi amount is required")
	}
	if in.DistributionType == PrizeTypeTickets && (in.TicketQuantity == nil || *in.TicketQuantity <= 0) {
		return "", fmt.Errorf("a positive ticket quantity is required")
	}
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return "", fmt.Errorf("begin award distribution: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())
	var configuredPrizeType, configuredValueText string
	if err := tx.QueryRow(ctx.DatabaseContext(), `
		SELECT p.prize_type, p.value_text
		FROM project_awards pa
		JOIN awards a ON a.id = pa.award_id
		JOIN prizes p ON p.award_id = a.id
		JOIN project_members pm ON pm.project_id = pa.project_id
		WHERE a.competition_id = $1::uuid AND pa.award_id = $2::uuid
			AND pa.project_id = $3::uuid AND p.id = $4::uuid AND pm.person_id = $5::uuid
		FOR UPDATE OF p
	`, in.CompetitionID, in.AwardID, in.ProjectID, in.PrizeID, in.PersonID).Scan(&configuredPrizeType, &configuredValueText); err != nil {
		if err == sql.ErrNoRows {
			return "", fmt.Errorf("recipient, project, award, and prize do not match")
		}
		return "", fmt.Errorf("validate award distribution: %w", err)
	}
	configuredPrizeType = normalizePrizeType(configuredPrizeType)
	if configuredPrizeType == "" || configuredPrizeType != in.DistributionType {
		return "", fmt.Errorf("distribution type must match the configured prize type")
	}
	if configuredPrizeType == PrizeTypeSats {
		configuredAmount, err := strconv.ParseInt(strings.TrimSpace(configuredValueText), 10, 64)
		if err != nil || configuredAmount <= 0 {
			return "", fmt.Errorf("cash prize does not have a valid satoshi value")
		}
		rows, err := tx.Query(ctx.DatabaseContext(), `
			SELECT person_id::text, amount_sats, status
			FROM award_distributions
			WHERE competition_id = $1::uuid AND award_id = $2::uuid
				AND project_id = $3::uuid AND prize_id = $4::uuid
			FOR UPDATE
		`, in.CompetitionID, in.AwardID, in.ProjectID, in.PrizeID)
		if err != nil {
			return "", fmt.Errorf("lock existing prize distributions: %w", err)
		}
		var allocated int64
		duplicate := false
		for rows.Next() {
			var personID, status string
			var amount sql.NullInt64
			if err := rows.Scan(&personID, &amount, &status); err != nil {
				rows.Close()
				return "", fmt.Errorf("scan existing prize distribution: %w", err)
			}
			if personID == in.PersonID {
				duplicate = true
			}
			if status != "cancelled" && amount.Valid {
				allocated += amount.Int64
			}
		}
		if err := rows.Err(); err != nil {
			rows.Close()
			return "", fmt.Errorf("iterate existing prize distributions: %w", err)
		}
		rows.Close()
		if duplicate {
			return "", fmt.Errorf("this recipient already has a distribution for this prize")
		}
		if allocated+*in.AmountSats > configuredAmount {
			return "", fmt.Errorf("distribution exceeds the prize value: %d sats remain unallocated", configuredAmount-allocated)
		}
	}
	var id string
	if err := tx.QueryRow(ctx.DatabaseContext(), `
		INSERT INTO award_distributions (
			competition_id, award_id, project_id, prize_id, person_id,
			distribution_type, amount_sats, ticket_quantity, notes
		) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $7, $8, $9)
		RETURNING id::text
	`, in.CompetitionID, in.AwardID, in.ProjectID, in.PrizeID, in.PersonID,
		in.DistributionType, in.AmountSats, in.TicketQuantity, strings.TrimSpace(in.Notes)).Scan(&id); err != nil {
		return "", fmt.Errorf("create award distribution: %w", err)
	}
	if in.DistributionType == PrizeTypeTickets {
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO hackathon_ticket_entitlements (person_id, award_distribution_id, quantity)
			VALUES ($1::uuid, $2::uuid, $3)
		`, in.PersonID, id, *in.TicketQuantity); err != nil {
			return "", fmt.Errorf("create ticket entitlement: %w", err)
		}
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return "", fmt.Errorf("commit award distribution: %w", err)
	}
	return id, nil
}

// PrepareCashPrizeDistributions divides the unallocated portion of a cash
// prize among team members who do not yet have an active distribution. The
// prize row is locked so concurrent manual or automatic allocations cannot
// exceed the configured value.
func PrepareCashPrizeDistributions(ctx *config.AppContext, competitionID, awardID, projectID, prizeID string) (int, error) {
	tx, err := ctx.DB.Begin(ctx.DatabaseContext())
	if err != nil {
		return 0, fmt.Errorf("begin prepare cash distributions: %w", err)
	}
	defer tx.Rollback(ctx.DatabaseContext())

	var valueText string
	if err := tx.QueryRow(ctx.DatabaseContext(), `
		SELECT prizes.value_text
		FROM project_awards
		JOIN awards ON awards.id = project_awards.award_id
		JOIN prizes ON prizes.award_id = awards.id
		WHERE awards.competition_id = $1::uuid
			AND project_awards.award_id = $2::uuid
			AND project_awards.project_id = $3::uuid
			AND prizes.id = $4::uuid
			AND awards.archived_at IS NULL
			AND prizes.prize_type = $5
		FOR UPDATE OF prizes
	`, competitionID, awardID, projectID, prizeID, PrizeTypeSats).Scan(&valueText); err != nil {
		return 0, fmt.Errorf("cash prize assignment not found")
	}
	configuredAmount, err := strconv.ParseInt(strings.TrimSpace(valueText), 10, 64)
	if err != nil || configuredAmount <= 0 {
		return 0, fmt.Errorf("cash prize does not have a valid satoshi value")
	}

	memberRows, err := tx.Query(ctx.DatabaseContext(), `
		SELECT people.id::text
		FROM project_members
		JOIN people ON people.id = project_members.person_id
		WHERE project_members.project_id = $1::uuid
		ORDER BY lower(people.name), people.id
	`, projectID)
	if err != nil {
		return 0, fmt.Errorf("load payout team members: %w", err)
	}
	var memberIDs []string
	for memberRows.Next() {
		var id string
		if err := memberRows.Scan(&id); err != nil {
			memberRows.Close()
			return 0, fmt.Errorf("scan payout team member: %w", err)
		}
		memberIDs = append(memberIDs, id)
	}
	if err := memberRows.Err(); err != nil {
		memberRows.Close()
		return 0, fmt.Errorf("iterate payout team members: %w", err)
	}
	memberRows.Close()
	if len(memberIDs) == 0 {
		return 0, fmt.Errorf("winning project has no team members")
	}

	distributionRows, err := tx.Query(ctx.DatabaseContext(), `
		SELECT person_id::text, amount_sats, status
		FROM award_distributions
		WHERE competition_id = $1::uuid AND award_id = $2::uuid
			AND project_id = $3::uuid AND prize_id = $4::uuid
		FOR UPDATE
	`, competitionID, awardID, projectID, prizeID)
	if err != nil {
		return 0, fmt.Errorf("lock cash prize distributions: %w", err)
	}
	activeByPerson := map[string]bool{}
	existingByPerson := map[string]bool{}
	var allocated int64
	for distributionRows.Next() {
		var personID, status string
		var amount sql.NullInt64
		if err := distributionRows.Scan(&personID, &amount, &status); err != nil {
			distributionRows.Close()
			return 0, fmt.Errorf("scan cash prize distribution: %w", err)
		}
		existingByPerson[personID] = true
		if status != "cancelled" {
			activeByPerson[personID] = true
			if amount.Valid {
				allocated += amount.Int64
			}
		}
	}
	if err := distributionRows.Err(); err != nil {
		distributionRows.Close()
		return 0, fmt.Errorf("iterate cash prize distributions: %w", err)
	}
	distributionRows.Close()

	missing := make([]string, 0, len(memberIDs))
	for _, personID := range memberIDs {
		if !activeByPerson[personID] {
			missing = append(missing, personID)
		}
	}
	if len(missing) == 0 {
		return 0, fmt.Errorf("all team members already have distributions for this prize")
	}
	remaining := configuredAmount - allocated
	if remaining <= 0 {
		return 0, fmt.Errorf("the full prize value has already been allocated")
	}
	baseShare := remaining / int64(len(missing))
	remainder := remaining % int64(len(missing))
	if baseShare <= 0 {
		return 0, fmt.Errorf("not enough unallocated sats to create a positive distribution for each remaining team member")
	}

	for i, personID := range missing {
		share := baseShare
		if int64(i) < remainder {
			share++
		}
		if existingByPerson[personID] {
			if _, err := tx.Exec(ctx.DatabaseContext(), `
				UPDATE award_distributions
				SET amount_sats = $6, status = 'pending', completed_at = NULL,
					completed_by = NULL, notes = 'Automatically prepared as an equal team share.'
				WHERE competition_id = $1::uuid AND award_id = $2::uuid
					AND project_id = $3::uuid AND prize_id = $4::uuid AND person_id = $5::uuid
			`, competitionID, awardID, projectID, prizeID, personID, share); err != nil {
				return 0, fmt.Errorf("restore cash distribution: %w", err)
			}
			continue
		}
		if _, err := tx.Exec(ctx.DatabaseContext(), `
			INSERT INTO award_distributions (
				competition_id, award_id, project_id, prize_id, person_id,
				distribution_type, amount_sats, notes
			) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::uuid, $5::uuid, $6, $7,
				'Automatically prepared as an equal team share.')
		`, competitionID, awardID, projectID, prizeID, personID, PrizeTypeSats, share); err != nil {
			return 0, fmt.Errorf("create cash distribution: %w", err)
		}
	}
	if err := tx.Commit(ctx.DatabaseContext()); err != nil {
		return 0, fmt.Errorf("commit prepared cash distributions: %w", err)
	}
	return len(missing), nil
}

func ListAwardDistributions(ctx *config.AppContext, competitionID string) ([]*types.AwardDistribution, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT d.id::text, d.competition_id::text, d.award_id::text, a.title,
			d.project_id::text, projects.title, d.prize_id::text, prizes.title,
			d.person_id::text, people.name, coalesce(people.email::text, ''),
			people.signal, people.telegram,
			d.distribution_type, d.amount_sats, d.ticket_quantity, d.status, d.notes,
			people.lightning_address, people.bitcoin_address, people.tax_form_type,
			people.tax_form_original_name, people.tax_form_uploaded_at,
			d.completed_at, coalesce(d.completed_by::text, ''), d.created_at, d.updated_at
		FROM award_distributions d
		JOIN awards a ON a.id = d.award_id
		JOIN projects ON projects.id = d.project_id
		JOIN prizes ON prizes.id = d.prize_id
		JOIN people ON people.id = d.person_id
		WHERE d.competition_id = $1::uuid
		ORDER BY people.name, a.title, projects.title, d.created_at
	`, competitionID)
	if err != nil {
		return nil, fmt.Errorf("list award distributions: %w", err)
	}
	defer rows.Close()
	var out []*types.AwardDistribution
	for rows.Next() {
		var d types.AwardDistribution
		var amount sql.NullInt64
		var tickets sql.NullInt32
		var taxUploaded, completed pgtype.Timestamptz
		if err := rows.Scan(&d.ID, &d.CompetitionID, &d.AwardID, &d.AwardTitle,
			&d.ProjectID, &d.ProjectTitle, &d.PrizeID, &d.PrizeTitle,
			&d.PersonID, &d.PersonName, &d.PersonEmail, &d.PersonSignal,
			&d.PersonTelegram, &d.DistributionType,
			&amount, &tickets, &d.Status, &d.Notes, &d.LightningAddress,
			&d.BitcoinAddress, &d.TaxFormType, &d.TaxFormOriginalName,
			&taxUploaded, &completed, &d.CompletedBy, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan award distribution: %w", err)
		}
		if amount.Valid {
			value := amount.Int64
			d.AmountSats = &value
		}
		if tickets.Valid {
			value := int(tickets.Int32)
			d.TicketQuantity = &value
		}
		d.TaxFormUploadedAt = pgTimePtr(taxUploaded)
		d.CompletedAt = pgTimePtr(completed)
		out = append(out, &d)
	}
	return out, rows.Err()
}

func CashPrizeValueSats(ctx *config.AppContext, competitionID, awardID, prizeID string) (int64, error) {
	var valueText string
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT prizes.value_text
		FROM prizes
		JOIN awards ON awards.id = prizes.award_id
		WHERE prizes.id = $1::uuid AND prizes.award_id = $2::uuid
			AND awards.competition_id = $3::uuid
			AND awards.archived_at IS NULL AND prizes.prize_type = $4
	`, prizeID, awardID, competitionID, PrizeTypeSats).Scan(&valueText); err != nil {
		if err == sql.ErrNoRows {
			return 0, fmt.Errorf("cash prize not found")
		}
		return 0, fmt.Errorf("load cash prize value: %w", err)
	}
	value, err := strconv.ParseInt(strings.TrimSpace(valueText), 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("cash prize does not have a valid satoshi value")
	}
	return value, nil
}

func ListCashPayoutRecipients(ctx *config.AppContext, competitionID string) (map[string][]*types.HackathonPayoutRecipient, error) {
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT DISTINCT project_members.project_id::text, people.id::text,
			people.name, coalesce(people.email::text, ''), people.signal,
			people.telegram, people.lightning_address, people.bitcoin_address,
			people.tax_form_type, people.tax_form_original_name,
			people.tax_form_uploaded_at
		FROM project_awards
		JOIN awards ON awards.id = project_awards.award_id
		JOIN prizes ON prizes.award_id = awards.id AND prizes.prize_type = $2
		JOIN project_members ON project_members.project_id = project_awards.project_id
		JOIN people ON people.id = project_members.person_id
		WHERE awards.competition_id = $1::uuid AND awards.archived_at IS NULL
		ORDER BY project_members.project_id::text, people.name, people.id::text
	`, competitionID, PrizeTypeSats)
	if err != nil {
		return nil, fmt.Errorf("list cash payout recipients: %w", err)
	}
	defer rows.Close()
	out := make(map[string][]*types.HackathonPayoutRecipient)
	for rows.Next() {
		var recipient types.HackathonPayoutRecipient
		var taxUploaded pgtype.Timestamptz
		if err := rows.Scan(&recipient.ProjectID, &recipient.PersonID, &recipient.Name,
			&recipient.Email, &recipient.Signal, &recipient.Telegram,
			&recipient.LightningAddress, &recipient.BitcoinAddress,
			&recipient.TaxFormType, &recipient.TaxFormOriginalName,
			&taxUploaded); err != nil {
			return nil, fmt.Errorf("scan cash payout recipient: %w", err)
		}
		recipient.TaxFormUploadedAt = pgTimePtr(taxUploaded)
		out[recipient.ProjectID] = append(out[recipient.ProjectID], &recipient)
	}
	return out, rows.Err()
}

func UpdateAwardDistribution(ctx *config.AppContext, competitionID, distributionID, status, notes, completedBy string) error {
	status = strings.TrimSpace(status)
	switch status {
	case "pending", "ready", "sent", "claimed", "cancelled":
	default:
		return fmt.Errorf("invalid distribution status")
	}
	if status == "sent" || status == "claimed" {
		var taxFormReady bool
		if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
			SELECT people.tax_form_uploaded_at IS NOT NULL
			FROM award_distributions
			JOIN people ON people.id = award_distributions.person_id
			WHERE award_distributions.id = $1::uuid
				AND award_distributions.competition_id = $2::uuid
		`, distributionID, competitionID).Scan(&taxFormReady); err != nil {
			return fmt.Errorf("load payout recipient tax status: %w", err)
		}
		if !taxFormReady {
			return fmt.Errorf("a W-9 or W-8BEN must be on file before this payout can be marked %s", status)
		}
	}
	commandTag, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE award_distributions
		SET status = $3, notes = $4,
			completed_at = CASE WHEN $3 IN ('sent', 'claimed') THEN coalesce(completed_at, now()) ELSE NULL END,
			completed_by = CASE WHEN $3 IN ('sent', 'claimed') THEN NULLIF($5, '')::uuid ELSE NULL END
		WHERE id = $1::uuid AND competition_id = $2::uuid
	`, distributionID, competitionID, status, strings.TrimSpace(notes), completedBy)
	if err != nil {
		return fmt.Errorf("update award distribution: %w", err)
	}
	if commandTag.RowsAffected() == 0 {
		return fmt.Errorf("award distribution not found")
	}
	return nil
}
