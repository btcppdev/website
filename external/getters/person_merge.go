package getters

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgtype"
)

const PersonMergeUndoWindow = 7 * 24 * time.Hour

type PersonMergeFieldSpec struct {
	Key    string
	Column string
	Label  string
	Kind   string
}

var PersonMergeFieldSpecs = []PersonMergeFieldSpec{
	{Key: "name", Column: "name", Label: "Display name", Kind: "text"},
	{Key: "photo", Column: "norm_photo_path", Label: "Photo", Kind: "text"},
	{Key: "phone", Column: "phone", Label: "Phone", Kind: "text"},
	{Key: "signal", Column: "signal", Label: "Signal", Kind: "text"},
	{Key: "telegram", Column: "telegram", Label: "Telegram", Kind: "text"},
	{Key: "twitter", Column: "twitter_handle", Label: "Twitter / X", Kind: "text"},
	{Key: "nostr", Column: "nostr", Label: "Nostr", Kind: "text"},
	{Key: "github", Column: "github_url", Label: "GitHub", Kind: "text"},
	{Key: "instagram", Column: "instagram", Label: "Instagram", Kind: "text"},
	{Key: "linkedin", Column: "linkedin", Label: "LinkedIn", Kind: "text"},
	{Key: "leetcode", Column: "leetcode", Label: "LeetCode", Kind: "text"},
	{Key: "website", Column: "website_url", Label: "Website", Kind: "text"},
	{Key: "company", Column: "company", Label: "Company", Kind: "text"},
	{Key: "org_logo", Column: "org_logo_path", Label: "Organization logo", Kind: "text"},
	{Key: "bio", Column: "bio", Label: "Bio", Kind: "textarea"},
	{Key: "available_to_hire", Column: "avail_to_hire", Label: "Available to hire", Kind: "bool"},
	{Key: "looking_to_hire", Column: "looking_to_hire", Label: "Looking to hire", Kind: "bool"},
	{Key: "tshirt", Column: "tshirt", Label: "T-shirt size", Kind: "text"},
	{Key: "lightning_address", Column: "lightning_address", Label: "Lightning address", Kind: "text"},
	{Key: "bitcoin_address", Column: "bitcoin_address", Label: "Bitcoin address", Kind: "text"},
	{Key: "tax_form_type", Column: "tax_form_type", Label: "Tax form type", Kind: "text"},
	{Key: "tax_form_object", Column: "tax_form_object_key", Label: "Tax form file", Kind: "text"},
	{Key: "tax_form_name", Column: "tax_form_original_name", Label: "Tax form filename", Kind: "text"},
	{Key: "tax_form_uploaded", Column: "tax_form_uploaded_at", Label: "Tax form uploaded at", Kind: "value"},
}

type PersonMergeField struct {
	Spec      PersonMergeFieldSpec
	Canonical any
	Source    any
}

type PersonMergeConflict struct {
	Kind        string
	Description string
}

type PersonMergePreview struct {
	Canonical       *types.Speaker
	Source          *types.Speaker
	CanonicalEmails []string
	SourceEmails    []string
	Fields          []PersonMergeField
	Conflicts       []PersonMergeConflict
}

type PersonMergeDecision struct {
	Choice string `json:"choice"`
	Value  any    `json:"value"`
}

type PersonMergeInput struct {
	CanonicalPersonID string
	SourcePersonID    string
	MergedByPersonID  string
	MergeRequestID    string
	Decisions         map[string]PersonMergeDecision
}

type PersonMergeEvent struct {
	ID                string
	CanonicalPersonID string
	SourcePersonID    string
	CanonicalName     string
	SourceName        string
	MergedByPersonID  string
	MergedByName      string
	Status            string
	UndoExpiresAt     time.Time
	CreatedAt         time.Time
	RevertedAt        *time.Time
	RestoreWarning    json.RawMessage
	Decisions         map[string]PersonMergeDecision
}

type PersonMergeChangeGroup struct {
	Label   string
	Count   int
	Details []string
}

type PersonMergeUndoPreview struct {
	Event      *PersonMergeEvent
	Changed    bool
	Groups     []PersonMergeChangeGroup
	Overwrites []string
	Retains    []string
}

type mergeRelationshipSpec struct {
	Table        string
	PersonColumn string
	PrimaryKey   []string
	DuplicateKey []string
	Singleton    bool
	Label        string
}

type mergeRelationshipSnapshot struct {
	Spec    mergeRelationshipSpec `json:"spec"`
	Before  []map[string]any      `json:"before"`
	After   []map[string]any      `json:"after"`
	Deleted []map[string]any      `json:"deleted"`
}

type personMergeManifest struct {
	CanonicalAfter     map[string]any              `json:"canonical_after"`
	Relationships      []mergeRelationshipSnapshot `json:"relationships"`
	SourceEmailsBefore []map[string]any            `json:"source_emails_before"`
	SourceEmailsAfter  []map[string]any            `json:"source_emails_after"`
	ConflictRowsBefore []map[string]any            `json:"conflict_rows_before"`
	GeneratedEmailIDs  []string                    `json:"generated_email_ids"`
}

var personMergeRelationshipSpecs = []mergeRelationshipSpec{
	{Table: "affiliate_usages", PersonColumn: "affiliate_person_id", PrimaryKey: []string{"id"}, Label: "affiliate usages"},
	{Table: "auth_audit_events", PersonColumn: "person_id", PrimaryKey: []string{"id"}, Label: "authentication audit events"},
	{Table: "award_distributions", PersonColumn: "completed_by", PrimaryKey: []string{"id"}, Label: "award completions"},
	{Table: "award_distributions", PersonColumn: "person_id", PrimaryKey: []string{"id"}, DuplicateKey: []string{"award_id", "project_id", "prize_id"}, Label: "award distributions"},
	{Table: "award_judges", PersonColumn: "person_id", PrimaryKey: []string{"award_id", "person_id"}, DuplicateKey: []string{"award_id"}, Label: "award judging roles"},
	{Table: "award_votes", PersonColumn: "judge_person_id", PrimaryKey: []string{"award_id", "judge_person_id"}, DuplicateKey: []string{"award_id"}, Label: "award votes"},
	{Table: "competition_hackers", PersonColumn: "person_id", PrimaryKey: []string{"competition_id", "person_id"}, DuplicateKey: []string{"competition_id"}, Label: "hackathon participation"},
	{Table: "competition_judge_invites", PersonColumn: "accepted_by_person_id", PrimaryKey: []string{"id"}, Label: "judge invitations"},
	{Table: "competition_judges", PersonColumn: "person_id", PrimaryKey: []string{"competition_id", "person_id", "judge_type"}, DuplicateKey: []string{"competition_id", "judge_type"}, Label: "hackathon judging roles"},
	{Table: "competition_results_publication_events", PersonColumn: "performed_by", PrimaryKey: []string{"id"}, Label: "results publication events"},
	{Table: "competitions", PersonColumn: "results_finalized_by", PrimaryKey: []string{"id"}, Label: "finalized results"},
	{Table: "discounts", PersonColumn: "affiliate_person_id", PrimaryKey: []string{"id"}, Label: "affiliate codes"},
	{Table: "hackathon_ticket_entitlements", PersonColumn: "person_id", PrimaryKey: []string{"id"}, Label: "ticket entitlements"},
	{Table: "homepage_featured_speakers", PersonColumn: "person_id", PrimaryKey: []string{"position"}, Label: "homepage speaker slots"},
	{Table: "judge_event_deliberations", PersonColumn: "updated_by_person_id", PrimaryKey: []string{"competition_id"}, Label: "judging deliberation updates"},
	{Table: "people_roles", PersonColumn: "person_id", PrimaryKey: []string{"person_id", "scope", "position"}, DuplicateKey: []string{"scope", "position"}, Label: "roles"},
	{Table: "person_email_verifications", PersonColumn: "person_id", PrimaryKey: []string{"id"}, Label: "pending email verifications"},
	{Table: "person_merge_requests", PersonColumn: "reviewed_by_person_id", PrimaryKey: []string{"id"}, Label: "merge request reviewers"},
	{Table: "person_merge_events", PersonColumn: "canonical_person_id", PrimaryKey: []string{"id"}, Label: "prior profile merges"},
	{Table: "person_merge_events", PersonColumn: "merged_by_person_id", PrimaryKey: []string{"id"}, Label: "merge audit actors"},
	{Table: "person_merge_events", PersonColumn: "reverted_by_person_id", PrimaryKey: []string{"id"}, Label: "merge restore actors"},
	{Table: "person_oauth_identities", PersonColumn: "person_id", PrimaryKey: []string{"id"}, DuplicateKey: []string{"provider"}, Label: "OAuth identities"},
	{Table: "person_nostr_credentials", PersonColumn: "person_id", PrimaryKey: []string{"id"}, Singleton: true, Label: "Nostr credential"},
	{Table: "person_auth_security", PersonColumn: "person_id", PrimaryKey: []string{"person_id"}, Singleton: true, Label: "authentication security state"},
	{Table: "person_password_credentials", PersonColumn: "person_id", PrimaryKey: []string{"person_id"}, Singleton: true, Label: "password credential"},
	{Table: "person_passkey_credentials", PersonColumn: "person_id", PrimaryKey: []string{"id"}, DuplicateKey: []string{"credential_id"}, Label: "passkey credentials"},
	{Table: "person_api_tokens", PersonColumn: "person_id", PrimaryKey: []string{"id"}, DuplicateKey: []string{"token_selector"}, Label: "API tokens"},
	{Table: "password_reset_tokens", PersonColumn: "person_id", PrimaryKey: []string{"id"}, Label: "password reset tokens"},
	{Table: "project_invites", PersonColumn: "accepted_by_person_id", PrimaryKey: []string{"id"}, Label: "project invitations"},
	{Table: "project_members", PersonColumn: "person_id", PrimaryKey: []string{"project_id", "person_id"}, DuplicateKey: []string{"project_id"}, Label: "project memberships"},
	{Table: "projects", PersonColumn: "created_by_person_id", PrimaryKey: []string{"id"}, Label: "created projects"},
	{Table: "registrations", PersonColumn: "person_id", PrimaryKey: []string{"id"}, Label: "conference registrations"},
	{Table: "satellite_events", PersonColumn: "submitter_person_id", PrimaryKey: []string{"id"}, Label: "satellite event submissions"},
	{Table: "scorecards", PersonColumn: "judge_person_id", PrimaryKey: []string{"id"}, Label: "judge ballots"},
	{Table: "shop_orders", PersonColumn: "buyer_person_id", PrimaryKey: []string{"id"}, Label: "shop orders"},
	{Table: "speaker_confs", PersonColumn: "speaker_id", PrimaryKey: []string{"id"}, Label: "conference speaker records"},
	{Table: "volunteers", PersonColumn: "person_id", PrimaryKey: []string{"id"}, Label: "volunteer applications"},
}

func PreviewPersonMerge(ctx *config.AppContext, canonicalPersonID, sourcePersonID string) (*PersonMergePreview, error) {
	canonical, err := FetchSpeakerByID(ctx, canonicalPersonID)
	if err != nil {
		return nil, err
	}
	source, err := FetchSpeakerByID(ctx, sourcePersonID)
	if err != nil {
		return nil, err
	}
	if canonical == nil || source == nil || canonical.ID == source.ID {
		return nil, fmt.Errorf("choose two different existing people")
	}
	canonicalJSON, err := personSnapshot(ctx.DatabaseContext(), ctx.DB, canonical.ID)
	if err != nil {
		return nil, err
	}
	sourceJSON, err := personSnapshot(ctx.DatabaseContext(), ctx.DB, source.ID)
	if err != nil {
		return nil, err
	}
	preview := &PersonMergePreview{Canonical: canonical, Source: source}
	preview.CanonicalEmails, err = listMergeCandidateEmails(ctx.DatabaseContext(), ctx.DB, canonical.ID)
	if err != nil {
		return nil, err
	}
	preview.SourceEmails, err = listMergeCandidateEmails(ctx.DatabaseContext(), ctx.DB, source.ID)
	if err != nil {
		return nil, err
	}
	for _, spec := range PersonMergeFieldSpecs {
		preview.Fields = append(preview.Fields, PersonMergeField{
			Spec: spec, Canonical: canonicalJSON[spec.Column], Source: sourceJSON[spec.Column],
		})
	}
	preview.Conflicts, err = personMergeConflicts(ctx.DatabaseContext(), ctx.DB, canonical.ID, source.ID)
	if err != nil {
		return nil, err
	}
	return preview, nil
}

type personMergeQuerier interface {
	Query(context.Context, string, ...any) (pgx.Rows, error)
	QueryRow(context.Context, string, ...any) pgx.Row
}

func personSnapshot(queryCtx context.Context, db personMergeQuerier, personID string) (map[string]any, error) {
	var raw []byte
	if err := db.QueryRow(queryCtx, `SELECT to_jsonb(person) FROM people person WHERE id = $1::uuid`, personID).Scan(&raw); err != nil {
		return nil, fmt.Errorf("snapshot person %s: %w", personID, err)
	}
	var snapshot map[string]any
	if err := json.Unmarshal(raw, &snapshot); err != nil {
		return nil, fmt.Errorf("decode person snapshot: %w", err)
	}
	return snapshot, nil
}

func listMergeCandidateEmails(queryCtx context.Context, db personMergeQuerier, personID string) ([]string, error) {
	rows, err := db.Query(queryCtx, `
		SELECT email::text FROM person_emails WHERE person_id = $1::uuid
		UNION
		SELECT email::text FROM person_email_conflicts WHERE person_id = $1::uuid
		ORDER BY 1
	`, personID)
	if err != nil {
		return nil, fmt.Errorf("list merge candidate emails: %w", err)
	}
	defer rows.Close()
	var emails []string
	for rows.Next() {
		var email string
		if err := rows.Scan(&email); err != nil {
			return nil, err
		}
		emails = append(emails, email)
	}
	return emails, rows.Err()
}

func personMergeConflicts(queryCtx context.Context, db personMergeQuerier, canonicalPersonID, sourcePersonID string) ([]PersonMergeConflict, error) {
	rows, err := db.Query(queryCtx, `
		SELECT source_project.title, canonical_project.title, competition.title
		FROM project_members source_member
		JOIN projects source_project ON source_project.id = source_member.project_id
		JOIN project_members canonical_member ON canonical_member.person_id = $1::uuid
		JOIN projects canonical_project ON canonical_project.id = canonical_member.project_id
		JOIN competitions competition ON competition.id = source_project.competition_id
		WHERE source_member.person_id = $2::uuid
			AND source_project.competition_id = canonical_project.competition_id
			AND source_project.id <> canonical_project.id
	`, canonicalPersonID, sourcePersonID)
	if err != nil {
		return nil, fmt.Errorf("check project merge conflicts: %w", err)
	}
	var conflicts []PersonMergeConflict
	for rows.Next() {
		var sourceProject, canonicalProject, competition string
		if err := rows.Scan(&sourceProject, &canonicalProject, &competition); err != nil {
			rows.Close()
			return nil, err
		}
		conflicts = append(conflicts, PersonMergeConflict{
			Kind:        "project_membership",
			Description: fmt.Sprintf("Both people belong to different projects in %s: %q and %q. Remove or consolidate one membership first.", competition, canonicalProject, sourceProject),
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = db.Query(queryCtx, `
		SELECT DISTINCT conference.description
		FROM volunteers source_volunteer
		JOIN volunteers_conferences source_link
			ON source_link.volunteer_id = source_volunteer.id
			AND source_link.kind = 'schedule_for'
		JOIN volunteers canonical_volunteer
			ON canonical_volunteer.person_id = $1::uuid
		JOIN volunteers_conferences canonical_link
			ON canonical_link.volunteer_id = canonical_volunteer.id
			AND canonical_link.kind = 'schedule_for'
			AND canonical_link.conference_id = source_link.conference_id
		JOIN conferences conference ON conference.id = source_link.conference_id
		WHERE source_volunteer.person_id = $2::uuid
	`, canonicalPersonID, sourcePersonID)
	if err != nil {
		return nil, fmt.Errorf("check volunteer application merge conflicts: %w", err)
	}
	for rows.Next() {
		var conference string
		if err := rows.Scan(&conference); err != nil {
			rows.Close()
			return nil, err
		}
		conflicts = append(conflicts, PersonMergeConflict{
			Kind:        "volunteer_application",
			Description: fmt.Sprintf("Both people have volunteer applications for %s. Keep one application and resolve its status before merging the profiles.", conference),
		})
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, err = db.Query(queryCtx, `
		SELECT DISTINCT event.name
		FROM scorecards source_score
		JOIN scorecards canonical_score
			ON canonical_score.judge_person_id = $1::uuid
			AND canonical_score.judge_event_id = source_score.judge_event_id
		JOIN judge_events event ON event.id = source_score.judge_event_id
		WHERE source_score.judge_person_id = $2::uuid
	`, canonicalPersonID, sourcePersonID)
	if err != nil {
		return nil, fmt.Errorf("check ballot merge conflicts: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		var event string
		if err := rows.Scan(&event); err != nil {
			return nil, err
		}
		conflicts = append(conflicts, PersonMergeConflict{
			Kind:        "judge_ballot",
			Description: fmt.Sprintf("Both people submitted rankings in %q. Remove one person's ballot for that event first.", event),
		})
	}
	return conflicts, rows.Err()
}

func MergePeople(ctx *config.AppContext, input PersonMergeInput) (string, error) {
	if ctx == nil || ctx.DB == nil {
		return "", fmt.Errorf("database is not configured")
	}
	canonicalID := strings.TrimSpace(input.CanonicalPersonID)
	sourceID := strings.TrimSpace(input.SourcePersonID)
	actorID := strings.TrimSpace(input.MergedByPersonID)
	if canonicalID == "" || sourceID == "" || actorID == "" || canonicalID == sourceID {
		return "", fmt.Errorf("canonical, source, and actor people are required")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.BeginTx(dbctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return "", fmt.Errorf("begin person merge: %w", err)
	}
	defer tx.Rollback(dbctx)
	rows, err := tx.Query(dbctx, `SELECT id::text FROM people WHERE id = ANY($1::uuid[]) ORDER BY id FOR UPDATE`, []string{canonicalID, sourceID})
	if err != nil {
		return "", fmt.Errorf("lock merge people: %w", err)
	}
	locked := 0
	for rows.Next() {
		locked++
	}
	rows.Close()
	if locked != 2 {
		return "", fmt.Errorf("one of the selected people no longer exists")
	}
	conflicts, err := personMergeConflicts(dbctx, tx, canonicalID, sourceID)
	if err != nil {
		return "", err
	}
	if len(conflicts) > 0 {
		return "", fmt.Errorf("merge has %d unresolved relationship conflict(s)", len(conflicts))
	}
	canonicalBefore, err := personSnapshot(dbctx, tx, canonicalID)
	if err != nil {
		return "", err
	}
	sourceBefore, err := personSnapshot(dbctx, tx, sourceID)
	if err != nil {
		return "", err
	}
	decisionsJSON, err := json.Marshal(input.Decisions)
	if err != nil {
		return "", fmt.Errorf("encode merge decisions: %w", err)
	}
	canonicalJSON, _ := json.Marshal(canonicalBefore)
	sourceJSON, _ := json.Marshal(sourceBefore)
	var eventID string
	err = tx.QueryRow(dbctx, `
		INSERT INTO person_merge_events (
			canonical_person_id, source_person_id, merged_by_person_id,
			canonical_snapshot, source_snapshot, decisions, relationship_manifest,
			undo_expires_at
		) VALUES ($1::uuid, $2::uuid, $3::uuid, $4::jsonb, $5::jsonb, $6::jsonb, '{}'::jsonb, $7)
		RETURNING id::text
	`, canonicalID, sourceID, actorID, canonicalJSON, sourceJSON, decisionsJSON, time.Now().UTC().Add(PersonMergeUndoWindow)).Scan(&eventID)
	if err != nil {
		return "", fmt.Errorf("create person merge audit: %w", err)
	}
	if err := applyMergeProfileDecisions(dbctx, tx, canonicalID, input.Decisions); err != nil {
		return "", err
	}
	manifest := personMergeManifest{}
	for _, spec := range personMergeRelationshipSpecs {
		snapshot, err := mergeRelationship(dbctx, tx, spec, canonicalID, sourceID)
		if err != nil {
			return "", err
		}
		if len(snapshot.Before) > 0 {
			manifest.Relationships = append(manifest.Relationships, snapshot)
		}
	}
	var verifiedNostrPubkey string
	err = tx.QueryRow(dbctx, `
		SELECT pubkey_hex FROM person_nostr_credentials
		WHERE person_id = $1::uuid AND pubkey_hex IS NOT NULL AND verified_at IS NOT NULL
	`, canonicalID).Scan(&verifiedNostrPubkey)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return "", fmt.Errorf("load merged Nostr credential: %w", err)
	}
	if err == nil {
		if err := setPersonNostrProfile(dbctx, tx, canonicalID, verifiedNostrPubkey); err != nil {
			return "", err
		}
	}
	manifest.SourceEmailsBefore, err = snapshotRows(dbctx, tx, "person_emails", "person_id", sourceID)
	if err != nil {
		return "", err
	}
	manifest.ConflictRowsBefore, err = snapshotRows(dbctx, tx, "person_email_conflicts", "person_id", sourceID)
	if err != nil {
		return "", err
	}
	canonicalConflicts, err := snapshotRows(dbctx, tx, "person_email_conflicts", "person_id", canonicalID)
	if err != nil {
		return "", err
	}
	manifest.ConflictRowsBefore = append(manifest.ConflictRowsBefore, canonicalConflicts...)
	if _, err := tx.Exec(dbctx, `
		UPDATE person_emails
		SET person_id = $1::uuid, is_primary = false, origin_merge_event_id = $3::uuid, updated_at = now()
		WHERE person_id = $2::uuid
	`, canonicalID, sourceID, eventID); err != nil {
		return "", fmt.Errorf("move person emails: %w", err)
	}
	if _, err := tx.Exec(dbctx, `DELETE FROM person_email_conflicts WHERE person_id = $1::uuid`, sourceID); err != nil {
		return "", fmt.Errorf("clear source email conflicts: %w", err)
	}
	conflictEmails := uniqueSnapshotStrings(manifest.ConflictRowsBefore, "email")
	for _, email := range conflictEmails {
		var remaining int
		if err := tx.QueryRow(dbctx, `SELECT count(*) FROM person_email_conflicts WHERE email = $1::citext`, email).Scan(&remaining); err != nil {
			return "", err
		}
		if remaining != 1 {
			continue
		}
		var remainingPersonID string
		if err := tx.QueryRow(dbctx, `SELECT person_id::text FROM person_email_conflicts WHERE email = $1::citext`, email).Scan(&remainingPersonID); err != nil {
			return "", err
		}
		if remainingPersonID != canonicalID {
			continue
		}
		var generatedID string
		err := tx.QueryRow(dbctx, `
			INSERT INTO person_emails (person_id, email, is_primary, verified_at, origin_merge_event_id)
			VALUES ($1::uuid, $2::citext, false, now(), $3::uuid)
			ON CONFLICT (email) DO NOTHING
			RETURNING id::text
		`, canonicalID, email, eventID).Scan(&generatedID)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return "", fmt.Errorf("resolve merged email conflict: %w", err)
		}
		if generatedID != "" {
			manifest.GeneratedEmailIDs = append(manifest.GeneratedEmailIDs, generatedID)
		}
		if _, err := tx.Exec(dbctx, `DELETE FROM person_email_conflicts WHERE email = $1::citext`, email); err != nil {
			return "", err
		}
	}
	if _, err := tx.Exec(dbctx, `
		UPDATE person_emails SET is_primary = true, updated_at = now()
		WHERE id = (
			SELECT id FROM person_emails WHERE person_id = $1::uuid
			ORDER BY is_primary DESC, created_at, id LIMIT 1
		) AND NOT EXISTS (
			SELECT 1 FROM person_emails WHERE person_id = $1::uuid AND is_primary
		)
	`, canonicalID); err != nil {
		return "", fmt.Errorf("ensure merged primary email: %w", err)
	}
	manifest.SourceEmailsAfter, err = snapshotRowsByOriginMerge(dbctx, tx, eventID)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(dbctx, `DELETE FROM people WHERE id = $1::uuid`, sourceID); err != nil {
		return "", fmt.Errorf("delete merged source person (an unhandled relationship may remain): %w", err)
	}
	manifest.CanonicalAfter, err = personSnapshot(dbctx, tx, canonicalID)
	if err != nil {
		return "", err
	}
	manifestJSON, err := json.Marshal(manifest)
	if err != nil {
		return "", err
	}
	if _, err := tx.Exec(dbctx, `UPDATE person_merge_events SET relationship_manifest = $2::jsonb WHERE id = $1::uuid`, eventID, manifestJSON); err != nil {
		return "", fmt.Errorf("store person merge manifest: %w", err)
	}
	if requestID := strings.TrimSpace(input.MergeRequestID); requestID != "" {
		tag, err := tx.Exec(dbctx, `
			UPDATE person_merge_requests
			SET status = 'merged', reviewed_by_person_id = $4::uuid,
				merge_event_id = $5::uuid, reviewed_at = now()
			WHERE id = $1::uuid AND requester_person_id = $2::uuid
				AND target_person_id = $3::uuid AND status = 'pending'
		`, requestID, canonicalID, sourceID, actorID, eventID)
		if err != nil {
			return "", fmt.Errorf("complete person merge request: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return "", fmt.Errorf("merge request is no longer pending or does not match these people")
		}
	}
	if _, err := tx.Exec(dbctx, `
		UPDATE person_merge_requests
		SET status = 'superseded', reviewed_by_person_id = $2::uuid,
			review_note = 'One of the accounts was merged through another review.', reviewed_at = now()
		WHERE status IN ('awaiting_confirmation', 'pending')
			AND (NULLIF($3, '') IS NULL OR id <> NULLIF($3, '')::uuid)
			AND (requester_person_id = $1::uuid OR target_person_id = $1::uuid)
	`, sourceID, actorID, strings.TrimSpace(input.MergeRequestID)); err != nil {
		return "", fmt.Errorf("supersede related person merge requests: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return "", fmt.Errorf("commit person merge: %w", err)
	}
	return eventID, nil
}

func applyMergeProfileDecisions(queryCtx context.Context, tx pgx.Tx, personID string, decisions map[string]PersonMergeDecision) error {
	allowed := make(map[string]PersonMergeFieldSpec, len(PersonMergeFieldSpecs))
	for _, spec := range PersonMergeFieldSpecs {
		allowed[spec.Key] = spec
	}
	keys := make([]string, 0, len(decisions))
	for key := range decisions {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		spec, ok := allowed[key]
		if !ok {
			return fmt.Errorf("unknown profile merge field %q", key)
		}
		if _, err := tx.Exec(queryCtx, `UPDATE people SET `+quoteMergeIdentifier(spec.Column)+` = $2 WHERE id = $1::uuid`, personID, decisions[key].Value); err != nil {
			return fmt.Errorf("apply profile merge field %s: %w", spec.Label, err)
		}
	}
	return nil
}

func mergeRelationship(queryCtx context.Context, tx pgx.Tx, spec mergeRelationshipSpec, canonicalID, sourceID string) (mergeRelationshipSnapshot, error) {
	snapshot := mergeRelationshipSnapshot{Spec: spec}
	var err error
	snapshot.Before, err = snapshotRows(queryCtx, tx, spec.Table, spec.PersonColumn, sourceID)
	if err != nil || len(snapshot.Before) == 0 {
		return snapshot, err
	}
	if spec.Singleton {
		var canonicalExists bool
		if err := tx.QueryRow(queryCtx, `SELECT EXISTS (SELECT 1 FROM `+quoteMergeIdentifier(spec.Table)+`
			WHERE `+quoteMergeIdentifier(spec.PersonColumn)+` = $1::uuid)`, canonicalID).Scan(&canonicalExists); err != nil {
			return snapshot, fmt.Errorf("check canonical %s: %w", spec.Label, err)
		}
		if canonicalExists {
			snapshot.Deleted = snapshot.Before
			if _, err := tx.Exec(queryCtx, `DELETE FROM `+quoteMergeIdentifier(spec.Table)+`
				WHERE `+quoteMergeIdentifier(spec.PersonColumn)+` = $1::uuid`, sourceID); err != nil {
				return snapshot, fmt.Errorf("deduplicate %s: %w", spec.Label, err)
			}
			return snapshot, nil
		}
	}
	if len(spec.DuplicateKey) > 0 {
		snapshot.Deleted, err = duplicateRelationshipRows(queryCtx, tx, spec, canonicalID, sourceID)
		if err != nil {
			return snapshot, err
		}
		if len(snapshot.Deleted) > 0 {
			where := duplicateRelationshipExistsSQL(spec, "source", "canonical")
			_, err = tx.Exec(queryCtx, `DELETE FROM `+quoteMergeIdentifier(spec.Table)+` source
				WHERE source.`+quoteMergeIdentifier(spec.PersonColumn)+` = $2::uuid
				AND EXISTS (SELECT 1 FROM `+quoteMergeIdentifier(spec.Table)+` canonical
					WHERE canonical.`+quoteMergeIdentifier(spec.PersonColumn)+` = $1::uuid AND `+where+`)`, canonicalID, sourceID)
			if err != nil {
				return snapshot, fmt.Errorf("deduplicate %s: %w", spec.Label, err)
			}
		}
	}
	if _, err := tx.Exec(queryCtx, `UPDATE `+quoteMergeIdentifier(spec.Table)+`
		SET `+quoteMergeIdentifier(spec.PersonColumn)+` = $1::uuid
		WHERE `+quoteMergeIdentifier(spec.PersonColumn)+` = $2::uuid`, canonicalID, sourceID); err != nil {
		return snapshot, fmt.Errorf("move %s: %w", spec.Label, err)
	}
	deleted := snapshotKeySet(spec, snapshot.Deleted)
	for _, before := range snapshot.Before {
		if deleted[relationshipRowKey(spec, before)] {
			continue
		}
		after, found, err := loadRelationshipRow(queryCtx, tx, spec, before, canonicalID)
		if err != nil {
			return snapshot, err
		}
		if found {
			snapshot.After = append(snapshot.After, after)
		}
	}
	return snapshot, nil
}

func snapshotRows(queryCtx context.Context, db personMergeQuerier, table, column, personID string) ([]map[string]any, error) {
	rows, err := db.Query(queryCtx, `SELECT to_jsonb(row) FROM `+quoteMergeIdentifier(table)+` row WHERE `+quoteMergeIdentifier(column)+` = $1::uuid`, personID)
	if err != nil {
		return nil, fmt.Errorf("snapshot %s.%s: %w", table, column, err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var row map[string]any
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func snapshotRowsByOriginMerge(queryCtx context.Context, db personMergeQuerier, eventID string) ([]map[string]any, error) {
	return snapshotRows(queryCtx, db, "person_emails", "origin_merge_event_id", eventID)
}

func duplicateRelationshipRows(queryCtx context.Context, tx pgx.Tx, spec mergeRelationshipSpec, canonicalID, sourceID string) ([]map[string]any, error) {
	where := duplicateRelationshipExistsSQL(spec, "source", "canonical")
	rows, err := tx.Query(queryCtx, `SELECT to_jsonb(source) FROM `+quoteMergeIdentifier(spec.Table)+` source
		WHERE source.`+quoteMergeIdentifier(spec.PersonColumn)+` = $2::uuid
		AND EXISTS (SELECT 1 FROM `+quoteMergeIdentifier(spec.Table)+` canonical
			WHERE canonical.`+quoteMergeIdentifier(spec.PersonColumn)+` = $1::uuid AND `+where+`)`, canonicalID, sourceID)
	if err != nil {
		return nil, fmt.Errorf("find duplicate %s: %w", spec.Label, err)
	}
	defer rows.Close()
	var out []map[string]any
	for rows.Next() {
		var raw []byte
		if err := rows.Scan(&raw); err != nil {
			return nil, err
		}
		var row map[string]any
		if err := json.Unmarshal(raw, &row); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func duplicateRelationshipExistsSQL(spec mergeRelationshipSpec, sourceAlias, canonicalAlias string) string {
	parts := make([]string, 0, len(spec.DuplicateKey))
	for _, column := range spec.DuplicateKey {
		quoted := quoteMergeIdentifier(column)
		parts = append(parts, sourceAlias+"."+quoted+" = "+canonicalAlias+"."+quoted)
	}
	return strings.Join(parts, " AND ")
}

func loadRelationshipRow(queryCtx context.Context, db personMergeQuerier, spec mergeRelationshipSpec, before map[string]any, currentPersonID string) (map[string]any, bool, error) {
	where, args := relationshipWhere(spec, before, currentPersonID, 1)
	var raw []byte
	err := db.QueryRow(queryCtx, `SELECT to_jsonb(row) FROM `+quoteMergeIdentifier(spec.Table)+` row WHERE `+where, args...).Scan(&raw)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	var row map[string]any
	if err := json.Unmarshal(raw, &row); err != nil {
		return nil, false, err
	}
	return row, true, nil
}

func relationshipWhere(spec mergeRelationshipSpec, before map[string]any, currentPersonID string, start int) (string, []any) {
	parts := make([]string, 0, len(spec.PrimaryKey))
	args := make([]any, 0, len(spec.PrimaryKey))
	for _, column := range spec.PrimaryKey {
		value := before[column]
		if column == spec.PersonColumn {
			value = currentPersonID
		}
		if value == nil {
			parts = append(parts, "row."+quoteMergeIdentifier(column)+" IS NULL")
			continue
		}
		args = append(args, fmt.Sprint(value))
		parts = append(parts, fmt.Sprintf("row.%s::text = $%d", quoteMergeIdentifier(column), start+len(args)-1))
	}
	return strings.Join(parts, " AND "), args
}

func relationshipRowKey(spec mergeRelationshipSpec, row map[string]any) string {
	parts := make([]string, 0, len(spec.PrimaryKey))
	for _, column := range spec.PrimaryKey {
		parts = append(parts, column+"="+fmt.Sprint(row[column]))
	}
	return strings.Join(parts, "|")
}

func snapshotKeySet(spec mergeRelationshipSpec, rows []map[string]any) map[string]bool {
	out := make(map[string]bool, len(rows))
	for _, row := range rows {
		out[relationshipRowKey(spec, row)] = true
	}
	return out
}

func uniqueSnapshotStrings(rows []map[string]any, key string) []string {
	seen := map[string]bool{}
	var out []string
	for _, row := range rows {
		value := strings.TrimSpace(fmt.Sprint(row[key]))
		if value != "" && value != "<nil>" && !seen[value] {
			seen[value] = true
			out = append(out, value)
		}
	}
	return out
}

func quoteMergeIdentifier(identifier string) string {
	return pgx.Identifier{identifier}.Sanitize()
}

func ListPersonMergeEvents(ctx *config.AppContext, limit int) ([]*PersonMergeEvent, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, fmt.Errorf("database is not configured")
	}
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT merge.id::text, merge.canonical_person_id::text, merge.source_person_id::text,
			coalesce(canonical.name, merge.canonical_snapshot->>'name', ''),
			coalesce(merge.source_snapshot->>'name', ''),
			coalesce(merge.merged_by_person_id::text, ''), coalesce(actor.name, ''),
			merge.status, merge.undo_expires_at, merge.created_at, merge.reverted_at,
			coalesce(merge.restore_warning, '{}'::jsonb), merge.decisions
		FROM person_merge_events merge
		LEFT JOIN people canonical ON canonical.id = merge.canonical_person_id
		LEFT JOIN people actor ON actor.id = merge.merged_by_person_id
		ORDER BY merge.created_at DESC
		LIMIT $1
	`, limit)
	if err != nil {
		return nil, fmt.Errorf("list person merge events: %w", err)
	}
	defer rows.Close()
	var events []*PersonMergeEvent
	for rows.Next() {
		event, err := scanPersonMergeEvent(rows)
		if err != nil {
			return nil, err
		}
		events = append(events, event)
	}
	return events, rows.Err()
}

type personMergeEventScanner interface{ Scan(...any) error }

func scanPersonMergeEvent(row personMergeEventScanner) (*PersonMergeEvent, error) {
	var event PersonMergeEvent
	var reverted pgtype.Timestamptz
	var decisionsRaw []byte
	if err := row.Scan(&event.ID, &event.CanonicalPersonID, &event.SourcePersonID,
		&event.CanonicalName, &event.SourceName, &event.MergedByPersonID, &event.MergedByName,
		&event.Status, &event.UndoExpiresAt, &event.CreatedAt, &reverted, &event.RestoreWarning,
		&decisionsRaw); err != nil {
		return nil, fmt.Errorf("scan person merge event: %w", err)
	}
	if reverted.Valid {
		event.RevertedAt = &reverted.Time
	}
	if err := json.Unmarshal(decisionsRaw, &event.Decisions); err != nil {
		return nil, fmt.Errorf("decode person merge decisions: %w", err)
	}
	return &event, nil
}

func GetPersonMergeUndoPreview(ctx *config.AppContext, eventID string) (*PersonMergeUndoPreview, error) {
	event, canonicalBefore, manifest, err := loadPersonMergeEvent(ctx, eventID)
	if err != nil {
		return nil, err
	}
	preview := &PersonMergeUndoPreview{
		Event: event,
		Overwrites: []string{
			"The canonical profile fields are restored to their pre-merge values.",
			"Rows that existed on the source profile at merge time are restored to their audited values.",
		},
		Retains: []string{
			"Records created after the merge remain attached to the canonical person.",
			"New email aliases added after the merge remain on the canonical person.",
		},
	}
	currentCanonical, err := personSnapshot(ctx.DatabaseContext(), ctx.DB, event.CanonicalPersonID)
	if err != nil {
		return nil, err
	}
	if !jsonValuesEqual(currentCanonical, manifest.CanonicalAfter) {
		preview.Groups = append(preview.Groups, PersonMergeChangeGroup{Label: "Canonical profile", Count: 1, Details: []string{"Profile fields changed after the merge."}})
	}
	for _, relation := range manifest.Relationships {
		changed := 0
		var details []string
		for _, before := range relation.Before {
			if snapshotKeySet(relation.Spec, relation.Deleted)[relationshipRowKey(relation.Spec, before)] {
				continue
			}
			current, found, err := loadRelationshipRow(ctx.DatabaseContext(), ctx.DB, relation.Spec, before, event.CanonicalPersonID)
			if err != nil {
				return nil, err
			}
			expected := findAfterSnapshot(relation, before, event.CanonicalPersonID)
			if !found || expected == nil || !jsonValuesEqual(current, expected) {
				changed++
				if len(details) < 5 {
					details = append(details, relationshipRowKey(relation.Spec, before))
				}
			}
		}
		if changed > 0 {
			preview.Groups = append(preview.Groups, PersonMergeChangeGroup{Label: relation.Spec.Label, Count: changed, Details: details})
		}
	}
	currentEmails, err := snapshotRowsByOriginMerge(ctx.DatabaseContext(), ctx.DB, event.ID)
	if err != nil {
		return nil, err
	}
	if !jsonValuesEqual(currentEmails, manifest.SourceEmailsAfter) {
		preview.Groups = append(preview.Groups, PersonMergeChangeGroup{Label: "Merged email addresses", Count: 1, Details: []string{"One or more source-origin email rows changed."}})
	}
	preview.Changed = len(preview.Groups) > 0
	_ = canonicalBefore
	return preview, nil
}

func findAfterSnapshot(relation mergeRelationshipSnapshot, before map[string]any, canonicalID string) map[string]any {
	for _, after := range relation.After {
		match := true
		for _, column := range relation.Spec.PrimaryKey {
			want := before[column]
			if column == relation.Spec.PersonColumn {
				want = canonicalID
			}
			if fmt.Sprint(after[column]) != fmt.Sprint(want) {
				match = false
				break
			}
		}
		if match {
			return after
		}
	}
	return nil
}

func jsonValuesEqual(a, b any) bool {
	left, _ := json.Marshal(a)
	right, _ := json.Marshal(b)
	return string(left) == string(right)
}

func loadPersonMergeEvent(ctx *config.AppContext, eventID string) (*PersonMergeEvent, map[string]any, personMergeManifest, error) {
	if ctx == nil || ctx.DB == nil {
		return nil, nil, personMergeManifest{}, fmt.Errorf("database is not configured")
	}
	var canonicalRaw, manifestRaw, decisionsRaw []byte
	row := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT merge.id::text, merge.canonical_person_id::text, merge.source_person_id::text,
			coalesce(canonical.name, merge.canonical_snapshot->>'name', ''),
			coalesce(merge.source_snapshot->>'name', ''),
			coalesce(merge.merged_by_person_id::text, ''), coalesce(actor.name, ''),
			merge.status, merge.undo_expires_at, merge.created_at, merge.reverted_at,
			coalesce(merge.restore_warning, '{}'::jsonb), merge.decisions,
			merge.canonical_snapshot, merge.relationship_manifest
		FROM person_merge_events merge
		LEFT JOIN people canonical ON canonical.id = merge.canonical_person_id
		LEFT JOIN people actor ON actor.id = merge.merged_by_person_id
		WHERE merge.id = $1::uuid
	`, eventID)
	var event PersonMergeEvent
	var reverted pgtype.Timestamptz
	if err := row.Scan(&event.ID, &event.CanonicalPersonID, &event.SourcePersonID,
		&event.CanonicalName, &event.SourceName, &event.MergedByPersonID, &event.MergedByName,
		&event.Status, &event.UndoExpiresAt, &event.CreatedAt, &reverted, &event.RestoreWarning,
		&decisionsRaw, &canonicalRaw, &manifestRaw); err != nil {
		return nil, nil, personMergeManifest{}, fmt.Errorf("load person merge event: %w", err)
	}
	if reverted.Valid {
		event.RevertedAt = &reverted.Time
	}
	var canonical map[string]any
	var manifest personMergeManifest
	if err := json.Unmarshal(decisionsRaw, &event.Decisions); err != nil {
		return nil, nil, manifest, err
	}
	if err := json.Unmarshal(canonicalRaw, &canonical); err != nil {
		return nil, nil, manifest, err
	}
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return nil, nil, manifest, err
	}
	return &event, canonical, manifest, nil
}

func UndoPersonMerge(ctx *config.AppContext, eventID, actorPersonID string, warning *PersonMergeUndoPreview) error {
	event, canonicalBefore, manifest, err := loadPersonMergeEvent(ctx, eventID)
	if err != nil {
		return err
	}
	if event.Status != "merged" {
		return fmt.Errorf("merge has already been restored")
	}
	if time.Now().After(event.UndoExpiresAt) {
		return fmt.Errorf("the seven-day undo window has expired")
	}
	dbctx := ctx.DatabaseContext()
	tx, err := ctx.DB.BeginTx(dbctx, pgx.TxOptions{IsoLevel: pgx.Serializable})
	if err != nil {
		return err
	}
	defer tx.Rollback(dbctx)
	var sourceExists bool
	if err := tx.QueryRow(dbctx, `SELECT EXISTS(SELECT 1 FROM people WHERE id = $1::uuid)`, event.SourcePersonID).Scan(&sourceExists); err != nil {
		return err
	}
	if sourceExists {
		return fmt.Errorf("source person ID is already in use")
	}
	var sourceRaw []byte
	if err := tx.QueryRow(dbctx, `SELECT source_snapshot FROM person_merge_events WHERE id = $1::uuid FOR UPDATE`, event.ID).Scan(&sourceRaw); err != nil {
		return err
	}
	if _, err := tx.Exec(dbctx, `INSERT INTO people SELECT (jsonb_populate_record(NULL::people, $1::jsonb)).*`, sourceRaw); err != nil {
		return fmt.Errorf("restore source person: %w", err)
	}
	for _, relation := range manifest.Relationships {
		deleted := snapshotKeySet(relation.Spec, relation.Deleted)
		for _, before := range relation.Before {
			if deleted[relationshipRowKey(relation.Spec, before)] {
				if err := insertSnapshotRow(dbctx, tx, relation.Spec.Table, before); err != nil {
					return err
				}
				continue
			}
			if err := restoreRelationshipRow(dbctx, tx, relation.Spec, before, event.CanonicalPersonID); err != nil {
				return err
			}
		}
	}
	for _, generatedID := range manifest.GeneratedEmailIDs {
		if _, err := tx.Exec(dbctx, `DELETE FROM person_emails WHERE id = $1::uuid`, generatedID); err != nil {
			return err
		}
	}
	for _, before := range manifest.SourceEmailsBefore {
		spec := mergeRelationshipSpec{Table: "person_emails", PersonColumn: "person_id", PrimaryKey: []string{"id"}, Label: "person emails"}
		if err := restoreRelationshipRow(dbctx, tx, spec, before, event.CanonicalPersonID); err != nil {
			return err
		}
	}
	for _, conflict := range manifest.ConflictRowsBefore {
		if err := insertSnapshotRowOnConflict(dbctx, tx, "person_email_conflicts", conflict); err != nil {
			return err
		}
	}
	canonicalRaw, _ := json.Marshal(canonicalBefore)
	if _, err := tx.Exec(dbctx, `
		UPDATE people current SET
			name = restored.name, norm_photo_path = restored.norm_photo_path,
			phone = restored.phone, signal = restored.signal, telegram = restored.telegram,
			twitter_handle = restored.twitter_handle, nostr = restored.nostr, github_url = restored.github_url,
			instagram = restored.instagram, linkedin = restored.linkedin, leetcode = restored.leetcode,
			website_url = restored.website_url, company = restored.company, org_logo_path = restored.org_logo_path,
			bio = restored.bio, avail_to_hire = restored.avail_to_hire, looking_to_hire = restored.looking_to_hire,
			tshirt = restored.tshirt, lightning_address = restored.lightning_address,
			bitcoin_address = restored.bitcoin_address, tax_form_type = restored.tax_form_type,
			tax_form_object_key = restored.tax_form_object_key,
			tax_form_original_name = restored.tax_form_original_name,
			tax_form_uploaded_at = restored.tax_form_uploaded_at,
			created_at = restored.created_at, updated_at = restored.updated_at
		FROM (SELECT (jsonb_populate_record(NULL::people, $2::jsonb)).*) restored
		WHERE current.id = $1::uuid
	`, event.CanonicalPersonID, canonicalRaw); err != nil {
		return fmt.Errorf("restore canonical profile: %w", err)
	}
	warningJSON, _ := json.Marshal(warning)
	if _, err := tx.Exec(dbctx, `
		UPDATE person_merge_events
		SET status = 'reverted', reverted_at = now(), reverted_by_person_id = $2::uuid,
			restore_warning = $3::jsonb
		WHERE id = $1::uuid
	`, event.ID, actorPersonID, warningJSON); err != nil {
		return fmt.Errorf("complete person merge restore: %w", err)
	}
	if _, err := tx.Exec(dbctx, `
		UPDATE person_merge_requests
		SET status = 'reverted'
		WHERE merge_event_id = $1::uuid AND status = 'merged'
	`, event.ID); err != nil {
		return fmt.Errorf("restore person merge request status: %w", err)
	}
	if err := tx.Commit(dbctx); err != nil {
		return fmt.Errorf("commit person merge restore: %w", err)
	}
	return nil
}

func restoreRelationshipRow(queryCtx context.Context, tx pgx.Tx, spec mergeRelationshipSpec, before map[string]any, currentPersonID string) error {
	where, args := relationshipWhere(spec, before, currentPersonID, 2)
	raw, _ := json.Marshal(before)
	columns := make([]string, 0, len(before))
	for column := range before {
		columns = append(columns, column)
	}
	sort.Strings(columns)
	assignments := make([]string, 0, len(columns))
	for _, column := range columns {
		quoted := quoteMergeIdentifier(column)
		assignments = append(assignments, quoted+" = restored."+quoted)
	}
	queryArgs := append([]any{raw}, args...)
	tag, err := tx.Exec(queryCtx, `
		UPDATE `+quoteMergeIdentifier(spec.Table)+` row
		SET `+strings.Join(assignments, ", ")+`
		FROM (SELECT (jsonb_populate_record(NULL::`+quoteMergeIdentifier(spec.Table)+`, $1::jsonb)).*) restored
		WHERE `+where, queryArgs...)
	if err != nil {
		return fmt.Errorf("restore %s: %w", spec.Label, err)
	}
	if tag.RowsAffected() == 0 {
		return insertSnapshotRow(queryCtx, tx, spec.Table, before)
	}
	return nil
}

func insertSnapshotRow(queryCtx context.Context, tx pgx.Tx, table string, snapshot map[string]any) error {
	raw, _ := json.Marshal(snapshot)
	if _, err := tx.Exec(queryCtx, `INSERT INTO `+quoteMergeIdentifier(table)+` SELECT (jsonb_populate_record(NULL::`+quoteMergeIdentifier(table)+`, $1::jsonb)).*`, raw); err != nil {
		return fmt.Errorf("restore deleted %s row: %w", table, err)
	}
	return nil
}

func insertSnapshotRowOnConflict(queryCtx context.Context, tx pgx.Tx, table string, snapshot map[string]any) error {
	raw, _ := json.Marshal(snapshot)
	if _, err := tx.Exec(queryCtx, `INSERT INTO `+quoteMergeIdentifier(table)+` SELECT (jsonb_populate_record(NULL::`+quoteMergeIdentifier(table)+`, $1::jsonb)).* ON CONFLICT DO NOTHING`, raw); err != nil {
		return fmt.Errorf("restore deleted %s row: %w", table, err)
	}
	return nil
}
