package getters

import (
	"context"
	"errors"
	"io"
	"log"
	"os"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/config"
	"btcpp-web/internal/types"
	"github.com/jackc/pgx/v5/pgxpool"
)

func TestDatabaseSmokeSpeakerCreateAndLookup(t *testing.T) {
	ctx := databaseSmokeContext(t)
	suffix := databaseSmokeSuffix()
	email := "speaker-" + suffix + "@example.test"

	speakerID, err := CreateSpeaker(ctx, SpeakerInput{
		Name:     "Smoke Speaker " + suffix,
		Email:    email,
		Phone:    "+15551230000",
		Signal:   "smoke." + suffix,
		Twitter:  "smoketest",
		Website:  "https://example.test/smoke",
		Bio:      "Smoke-test profile text.",
		TShirt:   "MM",
		Photo:    "smoke.jpg",
		Telegram: "smoke_tg",
	})
	if err != nil {
		t.Fatalf("CreateSpeaker postgres: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM people WHERE id::text = $1`, speakerID)
	})

	got, err := GetPersonByEmail(ctx, strings.ToUpper(email))
	if err != nil {
		t.Fatalf("GetPersonByEmail postgres: %v", err)
	}
	if got == nil {
		t.Fatal("GetPersonByEmail returned nil")
	}
	if got.ID != speakerID || got.Email != email || got.Signal != "smoke."+suffix || got.TShirt != "MM" || got.Bio != "Smoke-test profile text." {
		t.Fatalf("speaker mismatch: %+v", got)
	}
	contact, err := GetShopRefundContactByEmail(ctx, strings.ToUpper(email))
	if err != nil {
		t.Fatalf("GetShopRefundContactByEmail postgres: %v", err)
	}
	if contact.Signal != "smoke."+suffix || contact.Telegram != "smoke_tg" {
		t.Fatalf("refund contact mismatch: %+v", contact)
	}
	resolution, err := ResolvePersonByEmail(ctx, strings.ToUpper(email))
	if err != nil {
		t.Fatalf("ResolvePersonByEmail postgres: %v", err)
	}
	if resolution.Person == nil || resolution.Person.ID != speakerID || resolution.Alias == nil || !resolution.Alias.IsPrimary {
		t.Fatalf("person email resolution mismatch: %+v", resolution)
	}
	aliases, err := ListPersonEmails(ctx, speakerID)
	if err != nil {
		t.Fatalf("ListPersonEmails postgres: %v", err)
	}
	if len(aliases) != 1 || aliases[0].Email != email || !aliases[0].IsPrimary {
		t.Fatalf("person aliases = %+v, want one primary %s", aliases, email)
	}
	secondary := "speaker-alias-" + suffix + "@example.test"
	token, err := CreatePersonEmailVerification(ctx, speakerID, secondary, true)
	if err != nil {
		t.Fatalf("CreatePersonEmailVerification postgres: %v", err)
	}
	competitorEmail := "speaker-competitor-" + suffix + "@example.test"
	competitorID, err := CreateSpeaker(ctx, SpeakerInput{Name: "Verification Competitor " + suffix, Email: competitorEmail})
	if err != nil {
		t.Fatalf("create verification competitor: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM people WHERE id::text = $1`, competitorID)
	})
	if _, err := CreatePersonEmailVerification(ctx, competitorID, secondary, false); err != nil {
		t.Fatalf("create competing person email verification: %v", err)
	}
	pendingEmail, requesterEmail, err := GetPendingPersonEmailVerification(ctx, token)
	if err != nil {
		t.Fatalf("GetPendingPersonEmailVerification postgres: %v", err)
	}
	if pendingEmail != secondary {
		t.Fatalf("pending verification email = %q, want %q", pendingEmail, secondary)
	}
	if requesterEmail != email {
		t.Fatalf("requesting account email = %q, want %q", requesterEmail, email)
	}
	pendingEmails, err := ListPendingPersonEmailVerifications(ctx, speakerID)
	if err != nil {
		t.Fatalf("ListPendingPersonEmailVerifications postgres: %v", err)
	}
	if len(pendingEmails) != 1 || pendingEmails[0] != secondary {
		t.Fatalf("pending verification emails = %v, want [%s]", pendingEmails, secondary)
	}
	verifiedPersonID, verifiedEmail, err := ConsumePersonEmailVerification(ctx, token)
	if err != nil {
		t.Fatalf("ConsumePersonEmailVerification postgres: %v", err)
	}
	if verifiedPersonID != speakerID || verifiedEmail != secondary {
		t.Fatalf("verified identity = %s/%s, want %s/%s", verifiedPersonID, verifiedEmail, speakerID, secondary)
	}
	pendingEmails, err = ListPendingPersonEmailVerifications(ctx, speakerID)
	if err != nil || len(pendingEmails) != 0 {
		t.Fatalf("pending verification emails after consume = %v, %v; want none", pendingEmails, err)
	}
	competingPendingEmails, err := ListPendingPersonEmailVerifications(ctx, competitorID)
	if err != nil || len(competingPendingEmails) != 0 {
		t.Fatalf("competing pending verifications after consume = %v, %v; want none", competingPendingEmails, err)
	}
	primary, err := GetPrimaryPersonEmail(ctx, speakerID)
	if err != nil || primary != secondary {
		t.Fatalf("primary after verification = %q, %v; want %q", primary, err, secondary)
	}
	if err := SetPrimaryPersonEmail(ctx, speakerID, email); err != nil {
		t.Fatalf("SetPrimaryPersonEmail postgres: %v", err)
	}
	if err := SetPrimaryPersonEmail(ctx, speakerID, secondary); err != nil {
		t.Fatalf("SetPrimaryPersonEmail back to secondary: %v", err)
	}
	if err := SetPrimaryPersonEmail(ctx, speakerID, email); err != nil {
		t.Fatalf("SetPrimaryPersonEmail back to original: %v", err)
	}
	if err := RemovePersonEmail(ctx, speakerID, secondary); err != nil {
		t.Fatalf("RemovePersonEmail postgres: %v", err)
	}
	aliases, err = ListPersonEmails(ctx, speakerID)
	if err != nil || len(aliases) != 1 || aliases[0].Email != email || !aliases[0].IsPrimary {
		t.Fatalf("person aliases after removal = %+v, %v", aliases, err)
	}
}

func TestDatabaseSmokeNostrIdentityLookup(t *testing.T) {
	ctx := databaseSmokeContext(t)
	suffix := databaseSmokeSuffix()
	const npub = "npub10elfcs4fr0l0r8af98jlmgdh9c8tcxjvz9qkw038js35mp4dma8qzvjptg"
	const hexKey = "7e7e9c42a91bfef19fa929e5fda1b72e0ebc1a4c1141673e2794234d86addf4e"
	firstID, err := CreateSpeaker(ctx, SpeakerInput{Name: "Nostr Smoke " + suffix, Email: "nostr-" + suffix + "@example.test", Nostr: npub})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = ctx.DB.Exec(context.Background(), `DELETE FROM people WHERE id::text = $1`, firstID) })
	person, err := FindPersonByNostrPubkey(ctx, hexKey)
	if err != nil || person == nil || person.ID != firstID {
		t.Fatalf("Nostr lookup = %+v, %v", person, err)
	}

	secondID, err := CreateSpeaker(ctx, SpeakerInput{Name: "Nostr Duplicate " + suffix, Email: "nostr-duplicate-" + suffix + "@example.test", Nostr: "nostr:" + npub})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = ctx.DB.Exec(context.Background(), `DELETE FROM people WHERE id::text = $1`, secondID) })
	if _, err := FindPersonByNostrPubkey(ctx, hexKey); !errors.Is(err, ErrNostrPubkeyConflict) {
		t.Fatalf("duplicate Nostr lookup returned %v", err)
	}
}

func TestDatabaseSmokePersonMergeAndUndo(t *testing.T) {
	ctx := databaseSmokeContext(t)
	suffix := databaseSmokeSuffix()
	canonicalEmail := "merge-destination-" + suffix + "@example.test"
	sourceEmail := "merge-source-" + suffix + "@example.test"
	canonicalID, err := CreateSpeaker(ctx, SpeakerInput{Name: "Merge Destination " + suffix, Email: canonicalEmail})
	if err != nil {
		t.Fatalf("create merge destination: %v", err)
	}
	sourceID, err := CreateSpeaker(ctx, SpeakerInput{Name: "Merge Source " + suffix, Email: sourceEmail})
	if err != nil {
		t.Fatalf("create merge source: %v", err)
	}
	refBefore := "merge-before-" + suffix
	refAfter := "merge-after-" + suffix
	var eventID string
	var mergeRequestIDs []string
	var oauthIdentityID string
	var authAuditEventID string
	t.Cleanup(func() {
		if authAuditEventID != "" {
			_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM auth_audit_events WHERE id = $1::uuid`, authAuditEventID)
		}
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM registrations WHERE ref_id = ANY($1::text[])`, []string{refBefore, refAfter})
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM people_roles WHERE person_id = ANY($1::uuid[])`, []string{canonicalID, sourceID})
		if len(mergeRequestIDs) > 0 {
			_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM person_merge_requests WHERE id = ANY($1::uuid[])`, mergeRequestIDs)
		}
		if eventID != "" {
			_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM person_merge_events WHERE id = $1::uuid`, eventID)
		}
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM people WHERE id = ANY($1::uuid[])`, []string{canonicalID, sourceID})
	})
	linkedOAuth, err := LinkOAuthIdentity(ctx, sourceID, &types.PersonOAuthIdentity{
		Provider: "github", Subject: "smoke-" + suffix, Username: "merge-source-" + suffix,
		Email: sourceEmail, EmailVerified: true,
	})
	if err != nil {
		t.Fatalf("link source OAuth identity: %v", err)
	}
	if _, err := LinkOAuthIdentity(ctx, canonicalID, &types.PersonOAuthIdentity{
		Provider: "github", Subject: "smoke-" + suffix, Username: "attacker",
	}); !errors.Is(err, ErrOAuthIdentityLinked) {
		t.Fatalf("link another person's OAuth identity returned %v, want ErrOAuthIdentityLinked", err)
	}
	oauthIdentityID = linkedOAuth.ID
	if err := RecordAuthAuditEvent(ctx, &types.AuthAuditEvent{PersonID: sourceID, Method: "github", Event: "smoke_login"}); err != nil {
		t.Fatalf("record source auth audit: %v", err)
	}
	if err := ctx.DB.QueryRow(context.Background(), `SELECT id::text FROM auth_audit_events WHERE person_id = $1::uuid AND event = 'smoke_login'`, sourceID).Scan(&authAuditEventID); err != nil {
		t.Fatalf("load source auth audit: %v", err)
	}

	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO registrations (ref_id, email, item_bought, person_id)
		VALUES ($1, $2::citext, 'source ticket', $3::uuid)
	`, refBefore, sourceEmail, sourceID); err != nil {
		t.Fatalf("insert source registration: %v", err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO people_roles (person_id, scope, position)
		VALUES ($1::uuid, 'global', 'staff'), ($2::uuid, 'global', 'staff')
	`, canonicalID, sourceID); err != nil {
		t.Fatalf("insert duplicate role: %v", err)
	}

	preview, err := PreviewPersonMerge(ctx, canonicalID, sourceID)
	if err != nil {
		t.Fatalf("PreviewPersonMerge: %v", err)
	}
	if len(preview.Conflicts) != 0 {
		t.Fatalf("unexpected merge conflicts: %+v", preview.Conflicts)
	}
	mergeRequest, confirmationToken, err := CreatePersonMergeRequest(ctx, canonicalID, sourceEmail)
	if err != nil {
		t.Fatalf("CreatePersonMergeRequest: %v", err)
	}
	if mergeRequest.RequesterPersonID != canonicalID || mergeRequest.TargetPersonID != sourceID || mergeRequest.Status != "awaiting_confirmation" {
		t.Fatalf("merge request = %+v", mergeRequest)
	}
	mergeRequestIDs = append(mergeRequestIDs, mergeRequest.ID)
	competitorEmail := "merge-competitor-" + suffix + "@example.test"
	competitorID, err := CreateSpeaker(ctx, SpeakerInput{Name: "Merge Competitor " + suffix, Email: competitorEmail})
	if err != nil {
		t.Fatalf("create merge competitor: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM people WHERE id::text = $1`, competitorID)
	})
	competingRequest, _, err := CreatePersonMergeRequest(ctx, competitorID, sourceEmail)
	if err != nil {
		t.Fatalf("create competing merge request: %v", err)
	}
	mergeRequestIDs = append(mergeRequestIDs, competingRequest.ID)
	mergeRequest, newlyConfirmed, err := ConfirmPersonMergeRequest(ctx, confirmationToken)
	if err != nil {
		t.Fatalf("ConfirmPersonMergeRequest: %v", err)
	}
	if !newlyConfirmed || mergeRequest.Status != "pending" || mergeRequest.ConfirmedAt == nil {
		t.Fatalf("confirmed merge request = %+v", mergeRequest)
	}
	decisions := make(map[string]PersonMergeDecision, len(preview.Fields))
	for _, field := range preview.Fields {
		value := field.Canonical
		choice := "canonical"
		if field.Spec.Key == "name" {
			value = field.Source
			choice = "source"
		}
		decisions[field.Spec.Key] = PersonMergeDecision{Choice: choice, Value: value}
	}
	eventID, err = MergePeople(ctx, PersonMergeInput{
		CanonicalPersonID: canonicalID,
		SourcePersonID:    sourceID,
		MergedByPersonID:  canonicalID,
		MergeRequestID:    mergeRequest.ID,
		Decisions:         decisions,
	})
	if err != nil {
		t.Fatalf("MergePeople: %v", err)
	}
	completedRequest, err := GetPersonMergeRequest(ctx, mergeRequest.ID)
	if err != nil || completedRequest == nil || completedRequest.Status != "merged" || completedRequest.MergeEventID != eventID {
		t.Fatalf("completed merge request = %+v, %v", completedRequest, err)
	}
	competingRequest, err = GetPersonMergeRequest(ctx, competingRequest.ID)
	if err != nil || competingRequest == nil || competingRequest.Status != "superseded" {
		t.Fatalf("competing merge request after merge = %+v, %v; want superseded", competingRequest, err)
	}

	resolution, err := ResolvePersonByEmail(ctx, sourceEmail)
	if err != nil || resolution.Person == nil || resolution.Person.ID != canonicalID {
		t.Fatalf("source alias after merge = %+v, %v", resolution, err)
	}
	var registrationPersonID, itemBought string
	if err := ctx.DB.QueryRow(context.Background(), `SELECT person_id::text, item_bought FROM registrations WHERE ref_id = $1`, refBefore).Scan(&registrationPersonID, &itemBought); err != nil {
		t.Fatalf("load moved registration: %v", err)
	}
	if registrationPersonID != canonicalID || itemBought != "source ticket" {
		t.Fatalf("moved registration = %s/%s, want %s/source ticket", registrationPersonID, itemBought, canonicalID)
	}
	mergedOAuth, err := FindOAuthIdentity(ctx, "github", "smoke-"+suffix)
	if err != nil || mergedOAuth == nil || mergedOAuth.PersonID != canonicalID || mergedOAuth.ID != oauthIdentityID {
		t.Fatalf("merged OAuth identity = %+v, %v", mergedOAuth, err)
	}
	var mergedAuditPersonID string
	if err := ctx.DB.QueryRow(context.Background(), `SELECT person_id::text FROM auth_audit_events WHERE id = $1::uuid`, authAuditEventID).Scan(&mergedAuditPersonID); err != nil || mergedAuditPersonID != canonicalID {
		t.Fatalf("merged auth audit owner = %q, %v", mergedAuditPersonID, err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `
		INSERT INTO registrations (ref_id, email, item_bought, person_id)
		VALUES ($1, $2::citext, 'post-merge ticket', $3::uuid)
	`, refAfter, canonicalEmail, canonicalID); err != nil {
		t.Fatalf("insert post-merge registration: %v", err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `UPDATE registrations SET item_bought = 'changed after merge' WHERE ref_id = $1`, refBefore); err != nil {
		t.Fatalf("change moved registration: %v", err)
	}
	if _, err := ctx.DB.Exec(context.Background(), `UPDATE people SET name = 'Changed Destination' WHERE id = $1::uuid`, canonicalID); err != nil {
		t.Fatalf("change destination profile: %v", err)
	}

	undoPreview, err := GetPersonMergeUndoPreview(ctx, eventID)
	if err != nil {
		t.Fatalf("GetPersonMergeUndoPreview: %v", err)
	}
	if !undoPreview.Changed || len(undoPreview.Groups) < 2 {
		t.Fatalf("undo warning = %+v, want profile and relationship changes", undoPreview.Groups)
	}
	if err := UndoPersonMerge(ctx, eventID, canonicalID, undoPreview); err != nil {
		t.Fatalf("UndoPersonMerge: %v", err)
	}
	revertedRequest, err := GetPersonMergeRequest(ctx, mergeRequest.ID)
	if err != nil || revertedRequest == nil || revertedRequest.Status != "reverted" {
		t.Fatalf("reverted merge request = %+v, %v", revertedRequest, err)
	}

	canonical, err := FetchSpeakerByID(ctx, canonicalID)
	if err != nil || canonical == nil || canonical.Name != "Merge Destination "+suffix {
		t.Fatalf("restored destination = %+v, %v", canonical, err)
	}
	source, err := FetchSpeakerByID(ctx, sourceID)
	if err != nil || source == nil || source.Name != "Merge Source "+suffix {
		t.Fatalf("restored source = %+v, %v", source, err)
	}
	if err := ctx.DB.QueryRow(context.Background(), `SELECT person_id::text, item_bought FROM registrations WHERE ref_id = $1`, refBefore).Scan(&registrationPersonID, &itemBought); err != nil {
		t.Fatalf("load restored registration: %v", err)
	}
	if registrationPersonID != sourceID || itemBought != "source ticket" {
		t.Fatalf("restored registration = %s/%s, want %s/source ticket", registrationPersonID, itemBought, sourceID)
	}
	restoredOAuth, err := FindOAuthIdentity(ctx, "github", "smoke-"+suffix)
	if err != nil || restoredOAuth == nil || restoredOAuth.PersonID != sourceID || restoredOAuth.ID != oauthIdentityID {
		t.Fatalf("restored OAuth identity = %+v, %v", restoredOAuth, err)
	}
	var restoredAuditPersonID string
	if err := ctx.DB.QueryRow(context.Background(), `SELECT person_id::text FROM auth_audit_events WHERE id = $1::uuid`, authAuditEventID).Scan(&restoredAuditPersonID); err != nil || restoredAuditPersonID != sourceID {
		t.Fatalf("restored auth audit owner = %q, %v", restoredAuditPersonID, err)
	}
	if err := ctx.DB.QueryRow(context.Background(), `SELECT person_id::text FROM registrations WHERE ref_id = $1`, refAfter).Scan(&registrationPersonID); err != nil {
		t.Fatalf("load retained registration: %v", err)
	}
	if registrationPersonID != canonicalID {
		t.Fatalf("post-merge registration owner = %s, want %s", registrationPersonID, canonicalID)
	}
}

func TestDatabaseSmokePersonMergeManifestCoversPeopleForeignKeys(t *testing.T) {
	ctx := databaseSmokeContext(t)
	rows, err := ctx.DB.Query(ctx.DatabaseContext(), `
		SELECT constraint_table.table_name, constraint_column.column_name
		FROM information_schema.table_constraints constraint_table
		JOIN information_schema.key_column_usage constraint_column
			USING (constraint_catalog, constraint_schema, constraint_name)
		JOIN information_schema.constraint_column_usage referenced_column
			USING (constraint_catalog, constraint_schema, constraint_name)
		WHERE constraint_table.constraint_type = 'FOREIGN KEY'
			AND referenced_column.table_name = 'people'
			AND referenced_column.column_name = 'id'
	`)
	if err != nil {
		t.Fatalf("list people foreign keys: %v", err)
	}
	defer rows.Close()
	covered := map[string]bool{
		"person_emails.person_id":          true,
		"person_email_conflicts.person_id": true,
	}
	for _, spec := range personMergeRelationshipSpecs {
		covered[spec.Table+"."+spec.PersonColumn] = true
	}
	for rows.Next() {
		var table, column string
		if err := rows.Scan(&table, &column); err != nil {
			t.Fatalf("scan people foreign key: %v", err)
		}
		if !covered[table+"."+column] {
			t.Errorf("person merge manifest does not cover %s.%s", table, column)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterate people foreign keys: %v", err)
	}
}

func TestDatabaseSmokeDiscountScopedToConference(t *testing.T) {
	ctx := databaseSmokeContext(t)
	confID, tag := insertSmokeConference(t, ctx)
	code := "SMOKE" + strings.ToUpper(databaseSmokeSuffix())

	discountID, err := CreateDiscount(ctx, DiscountInput{
		CodeName:     code,
		DiscountExpr: "%42",
		ConfRef:      confID,
	})
	if err != nil {
		t.Fatalf("CreateDiscount postgres: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM discounts WHERE id::text = $1 OR code_name = $2`, discountID, code)
	})

	found, err := FindDiscount(ctx, strings.ToLower(code))
	if err != nil {
		t.Fatalf("FindDiscount postgres: %v", err)
	}
	if found == nil {
		t.Fatalf("FindDiscount(%q) returned nil", code)
	}
	if found.Ref != discountID || found.CodeName != code || found.DiscType != '%' || found.Amount != 42 {
		t.Fatalf("discount mismatch: %+v", found)
	}
	if len(found.ConfRef) != 1 || found.ConfRef[0] != confID {
		t.Fatalf("discount conf refs = %v, want [%s] for %s", found.ConfRef, confID, tag)
	}
}

func TestDatabaseSmokeVolunteerInfoOrientationUpdate(t *testing.T) {
	ctx := databaseSmokeContext(t)
	confID, _ := insertSmokeConference(t, ctx)

	var volInfoID string
	err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO volunteer_info (conference_id, notes)
		VALUES ($1::uuid, 'smoke volunteer info')
		RETURNING id::text
	`, confID).Scan(&volInfoID)
	if err != nil {
		t.Fatalf("insert volunteer_info: %v", err)
	}

	start := time.Date(2026, 7, 1, 14, 0, 0, 0, time.UTC)
	end := start.Add(90 * time.Minute)
	link := "https://example.test/orientation/" + databaseSmokeSuffix()
	if err := UpdateVolInfoOrientation(ctx, volInfoID, start, end, link); err != nil {
		t.Fatalf("UpdateVolInfoOrientation postgres: %v", err)
	}

	info, err := GetVolInfo(ctx, confID)
	if err != nil {
		t.Fatalf("GetVolInfo postgres: %v", err)
	}
	if info.Ref != volInfoID || info.OrientLink != link {
		t.Fatalf("volinfo mismatch: %+v", info)
	}
	if info.OrientTimes == nil || !info.OrientTimes.Start.Equal(start) || info.OrientTimes.End == nil || !info.OrientTimes.End.Equal(end) {
		t.Fatalf("volinfo orientation times = %+v, want %s - %s", info.OrientTimes, start, end)
	}
}

func TestDatabaseSmokeConfTalkScheduleUsesConferenceTimezone(t *testing.T) {
	ctx := databaseSmokeContext(t)
	tag := "smoke-nairobi-" + databaseSmokeSuffix()

	var confID string
	err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO conferences (
			tag, active, description, date_desc, start_date, end_date, timezone, location, venue
		)
		VALUES (
			$1, true, 'Nairobi Smoke Test Conf', 'July 1-2, 2026',
			'2026-07-01 00:00:00+03', '2026-07-02 23:59:00+03',
			'Africa/Nairobi', 'Nairobi', 'Smoke Venue'
		)
		RETURNING id::text
	`, tag).Scan(&confID)
	if err != nil {
		t.Fatalf("insert nairobi conference: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM conferences WHERE id::text = $1 OR tag = $2`, confID, tag)
	})

	var confTalkID string
	err = ctx.DB.QueryRow(context.Background(), `
		INSERT INTO conf_talks (conference_id, scheduled_start, scheduled_end, venue)
		VALUES ($1::uuid, '2026-07-01 10:00:00+03', '2026-07-01 10:45:00+03', 'Mainstage')
		RETURNING id::text
	`, confID).Scan(&confTalkID)
	if err != nil {
		t.Fatalf("insert nairobi conf talk: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM conf_talks WHERE id::text = $1`, confTalkID)
	})

	talks, err := queryConfTalksPostgres(ctx, "WHERE conf_talks.id::text = $1", []interface{}{confTalkID}, map[string]*types.Proposal{})
	if err != nil {
		t.Fatalf("queryConfTalksPostgres: %v", err)
	}
	if len(talks) != 1 {
		t.Fatalf("queryConfTalksPostgres returned %d talks, want 1", len(talks))
	}

	sched := talks[0].Sched
	if sched == nil || sched.End == nil {
		t.Fatalf("schedule missing: %+v", talks[0])
	}
	if got := sched.Start.Location().String(); got != "Africa/Nairobi" {
		t.Fatalf("start location = %q, want Africa/Nairobi", got)
	}
	if sched.Start.Hour() != 10 || sched.Start.Minute() != 0 {
		t.Fatalf("start time = %s, want 10:00 Africa/Nairobi", sched.Start)
	}
	if got := sched.End.Location().String(); got != "Africa/Nairobi" {
		t.Fatalf("end location = %q, want Africa/Nairobi", got)
	}
	if sched.End.Hour() != 10 || sched.End.Minute() != 45 {
		t.Fatalf("end time = %s, want 10:45 Africa/Nairobi", *sched.End)
	}
}

func TestDatabaseSmokeCreateConfTalkReusesScheduledProposalRow(t *testing.T) {
	ctx := databaseSmokeContext(t)
	confID, tag := insertSmokeConference(t, ctx)
	suffix := databaseSmokeSuffix()

	var proposalID string
	err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO proposals (conference_id, title, status)
		VALUES ($1::uuid, $2, 'Accepted')
		RETURNING id::text
	`, confID, "Scheduled Proposal "+suffix).Scan(&proposalID)
	if err != nil {
		t.Fatalf("insert proposal: %v", err)
	}

	start := time.Date(2026, 7, 1, 10, 0, 0, 0, time.UTC)
	end := start.Add(30 * time.Minute)
	var scheduledID string
	err = ctx.DB.QueryRow(context.Background(), `
		INSERT INTO conf_talks (conference_id, proposal_id, scheduled_start, scheduled_end, venue)
		VALUES ($1::uuid, $2::uuid, $3, $4, 'Mainstage')
		RETURNING id::text
	`, confID, proposalID, start, end).Scan(&scheduledID)
	if err != nil {
		t.Fatalf("insert scheduled conf talk: %v", err)
	}

	gotID, err := CreateConfTalk(ctx, ConfTalkInput{
		ConfTag:    tag,
		ProposalID: proposalID,
	})
	if err != nil {
		t.Fatalf("CreateConfTalk: %v", err)
	}
	if gotID != scheduledID {
		t.Fatalf("CreateConfTalk returned %s, want existing scheduled row %s", gotID, scheduledID)
	}
	if err := UpdateConfTalkSchedule(ctx, gotID, "Mainstage", start, end); err != nil {
		t.Fatalf("UpdateConfTalkSchedule existing row: %v", err)
	}

	var count int
	if err := ctx.DB.QueryRow(context.Background(), `
		SELECT count(*)
		FROM conf_talks
		WHERE proposal_id = $1::uuid
			AND archived_at IS NULL
	`, proposalID).Scan(&count); err != nil {
		t.Fatalf("count conf talks: %v", err)
	}
	if count != 1 {
		t.Fatalf("active conf_talks for proposal = %d, want 1", count)
	}
}

func TestDatabaseSmokeUpsertSpeakerConfNormalizesNilAvailability(t *testing.T) {
	ctx := databaseSmokeContext(t)
	confID, tag := insertSmokeConference(t, ctx)
	suffix := databaseSmokeSuffix()

	speakerID, err := CreateSpeaker(ctx, SpeakerInput{
		Name:  "Smoke SpeakerConf " + suffix,
		Email: "speakerconf-" + suffix + "@example.test",
	})
	if err != nil {
		t.Fatalf("insert person: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM people WHERE id::text = $1`, speakerID)
	})

	proposalID, err := CreateProposal(ctx, ProposalInput{
		Title:          "Invited Talk " + suffix,
		Description:    "Placeholder",
		Status:         "Invited",
		ScheduleForTag: tag,
		TalkType:       "Talk",
	})
	if err != nil {
		t.Fatalf("CreateProposal: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM proposals WHERE id::text = $1`, proposalID)
	})

	scID, err := UpsertSpeakerConf(ctx, SpeakerConfInput{
		SpeakerID:  speakerID,
		ConfTag:    tag,
		ProposalID: proposalID,
	})
	if err != nil {
		t.Fatalf("UpsertSpeakerConf with nil availability: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM speaker_confs WHERE id::text = $1`, scID)
	})

	var availability []string
	err = ctx.DB.QueryRow(context.Background(), `
		SELECT availability
		FROM speaker_confs
		WHERE id::text = $1 AND EXISTS (
			SELECT 1
			FROM conferences
			WHERE id::text = $2
		)
	`, scID, confID).Scan(&availability)
	if err != nil {
		t.Fatalf("select speaker_conf availability: %v", err)
	}
	if len(availability) != 0 {
		t.Fatalf("availability = %v, want empty array", availability)
	}
}

func TestDatabaseSmokeWorkShiftScheduleUsesConferenceTimezone(t *testing.T) {
	ctx := databaseSmokeContext(t)
	tag := "smoke-shift-nairobi-" + databaseSmokeSuffix()

	var confID string
	err := ctx.DB.QueryRow(context.Background(), `
		INSERT INTO conferences (
			tag, active, description, date_desc, start_date, end_date, timezone, location, venue
		)
		VALUES (
			$1, true, 'Nairobi Shift Smoke Test Conf', 'July 1-2, 2026',
			'2026-07-01 00:00:00+03', '2026-07-02 23:59:00+03',
			'Africa/Nairobi', 'Nairobi', 'Smoke Venue'
		)
		RETURNING id::text
	`, tag).Scan(&confID)
	if err != nil {
		t.Fatalf("insert nairobi conference: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM conferences WHERE id::text = $1 OR tag = $2`, confID, tag)
	})

	var shiftID string
	err = ctx.DB.QueryRow(context.Background(), `
		INSERT INTO work_shifts (conference_id, name, max_vols, shift_start, shift_end, priority)
		VALUES ($1::uuid, 'Registration Desk', 2, '2026-07-01 10:00:00+03', '2026-07-01 11:30:00+03', 1)
		RETURNING id::text
	`, confID).Scan(&shiftID)
	if err != nil {
		t.Fatalf("insert nairobi work shift: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM work_shifts WHERE id::text = $1`, shiftID)
	})

	shifts, err := ListWorkShifts(ctx)
	if err != nil {
		t.Fatalf("ListWorkShifts: %v", err)
	}
	var got *types.WorkShift
	for _, shift := range shifts {
		if shift.Ref == shiftID {
			got = shift
			break
		}
	}
	if got == nil {
		t.Fatalf("shift %s not returned", shiftID)
	}
	if got.ShiftTime == nil || got.ShiftTime.End == nil {
		t.Fatalf("shift time missing: %+v", got)
	}
	if loc := got.ShiftTime.Start.Location().String(); loc != "Africa/Nairobi" {
		t.Fatalf("start location = %q, want Africa/Nairobi", loc)
	}
	if got.ShiftTime.Start.Hour() != 10 || got.ShiftTime.Start.Minute() != 0 {
		t.Fatalf("start time = %s, want 10:00 Africa/Nairobi", got.ShiftTime.Start)
	}
	if loc := got.ShiftTime.End.Location().String(); loc != "Africa/Nairobi" {
		t.Fatalf("end location = %q, want Africa/Nairobi", loc)
	}
	if got.ShiftTime.End.Hour() != 11 || got.ShiftTime.End.Minute() != 30 {
		t.Fatalf("end time = %s, want 11:30 Africa/Nairobi", *got.ShiftTime.End)
	}
	if desc := got.TimeDesc(); desc != "10:00am - 11:30am" {
		t.Fatalf("TimeDesc = %q, want local Nairobi time", desc)
	}
}

func databaseSmokeContext(t *testing.T) *config.AppContext {
	t.Helper()
	if os.Getenv("BTCPP_POSTGRES_SMOKE") != "1" {
		t.Skip("set BTCPP_POSTGRES_SMOKE=1 to run local database smoke tests")
	}
	databaseURL := os.Getenv("DATABASE_URL")
	if databaseURL == "" {
		t.Skip("DATABASE_URL is required for local database smoke tests")
	}

	pool, err := pgxpool.New(context.Background(), databaseURL)
	if err != nil {
		t.Fatalf("connect postgres: %v", err)
	}
	if err := pool.Ping(context.Background()); err != nil {
		pool.Close()
		t.Fatalf("ping postgres: %v", err)
	}
	t.Cleanup(pool.Close)

	var schemaReady bool
	if err := pool.QueryRow(context.Background(), `SELECT to_regclass('public.conferences') IS NOT NULL`).Scan(&schemaReady); err != nil {
		t.Fatalf("check schema: %v", err)
	}
	if !schemaReady {
		t.Fatalf("postgres schema is not migrated; run btcpp_pg_migrate first")
	}

	return &config.AppContext{
		Env:   &types.EnvConfig{},
		DB:    pool,
		Err:   log.New(io.Discard, "", 0),
		Infos: log.New(io.Discard, "", 0),
	}
}

func insertSmokeConference(t *testing.T, app *config.AppContext) (string, string) {
	t.Helper()
	tag := "smoke-" + databaseSmokeSuffix()
	var id string
	err := app.DB.QueryRow(context.Background(), `
		INSERT INTO conferences (
			tag, active, description, date_desc, start_date, end_date, timezone, location, venue
		)
		VALUES (
			$1, true, 'Smoke Test Conf', 'July 1-2, 2026',
			'2026-07-01 09:00:00+00', '2026-07-02 17:00:00+00',
			'UTC', 'Smoke City', 'Smoke Venue'
		)
		RETURNING id::text
	`, tag).Scan(&id)
	if err != nil {
		t.Fatalf("insert conference: %v", err)
	}
	t.Cleanup(func() {
		_, _ = app.DB.Exec(context.Background(), `DELETE FROM conferences WHERE id::text = $1 OR tag = $2`, id, tag)
	})
	return id, tag
}

func databaseSmokeSuffix() string {
	return strings.ToLower(strings.ReplaceAll(time.Now().UTC().Format("20060102T150405.000000000"), ".", ""))
}
