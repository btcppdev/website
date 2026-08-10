package getters

import (
	"context"
	"strings"
	"testing"
	"time"

	"btcpp-web/internal/types"
)

func TestDatabaseSmokeVolunteerApplicationRequiresConfirmation(t *testing.T) {
	ctx := databaseSmokeContext(t)
	conferenceID, _ := insertSmokeConference(t, ctx)
	email := "volunteer-confirm-" + databaseSmokeSuffix() + "@example.test"
	vol := &types.Volunteer{
		Name: "Confirmation Volunteer", Email: email, Phone: "+1 555 0110",
		Signal: "confirm.01", Availability: []string{"Friday"}, Shirt: "ML",
		ScheduleFor: []*types.Conf{{Ref: conferenceID}}, Subscribe: true,
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM volunteer_application_requests WHERE email = $1::citext`, email)
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM volunteers WHERE person_id IN (SELECT person_id FROM person_emails WHERE email = $1::citext)`, email)
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM people WHERE id IN (SELECT person_id FROM person_emails WHERE email = $1::citext)`, email)
	})

	token, err := CreateVolunteerApplicationRequest(ctx, vol)
	if err != nil {
		t.Fatalf("CreateVolunteerApplicationRequest: %v", err)
	}
	resolution, err := ResolvePersonByEmail(ctx, email)
	if err != nil {
		t.Fatalf("resolve staged volunteer email: %v", err)
	}
	if resolution.Person != nil || resolution.Alias != nil {
		t.Fatalf("staged application created a verified identity: %+v", resolution)
	}
	pending, err := GetPendingVolunteerApplication(ctx, token)
	if err != nil {
		t.Fatalf("GetPendingVolunteerApplication: %v", err)
	}
	if pending.Email != email || pending.Name != vol.Name || len(pending.ScheduleFor) != 1 || pending.ScheduleFor[0].Ref != conferenceID {
		t.Fatalf("pending volunteer = %+v", pending)
	}
	if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		UPDATE volunteer_application_requests
		SET created_at = now() - interval '2 minutes', expires_at = now() - interval '1 minute'
		WHERE email = $1::citext
	`, email); err != nil {
		t.Fatalf("expire volunteer confirmation: %v", err)
	}
	if _, err := GetPendingVolunteerApplication(ctx, token); err == nil || !strings.Contains(err.Error(), "expired") {
		t.Fatalf("expired volunteer confirmation = %v, want expired error", err)
	}
	resendable, err := GetResendableVolunteerApplication(ctx, token)
	if err != nil || resendable.Email != email {
		t.Fatalf("resendable volunteer = %+v, %v", resendable, err)
	}
	_, renewedToken, err := RenewVolunteerApplicationConfirmation(ctx, token)
	if err != nil {
		t.Fatalf("RenewVolunteerApplicationConfirmation: %v", err)
	}
	if renewedToken == token || renewedToken == "" {
		t.Fatalf("renewed token = %q, want a new token", renewedToken)
	}
	if _, err := GetPendingVolunteerApplication(ctx, token); err == nil {
		t.Fatal("old volunteer confirmation token remained valid after renewal")
	}
	var expiresAt time.Time
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `
		SELECT expires_at FROM volunteer_application_requests WHERE email = $1::citext
	`, email).Scan(&expiresAt); err != nil {
		t.Fatalf("load renewed expiry: %v", err)
	}
	remaining := time.Until(expiresAt)
	if remaining < 23*time.Hour+59*time.Minute || remaining > VolunteerApplicationConfirmationTTL {
		t.Fatalf("renewed confirmation lifetime = %s, want about %s", remaining, VolunteerApplicationConfirmationTTL)
	}
	confirmed, err := ConfirmVolunteerApplication(ctx, renewedToken)
	if err != nil {
		t.Fatalf("ConfirmVolunteerApplication: %v", err)
	}
	if confirmed.Ref == "" {
		t.Fatal("confirmed volunteer is missing its application ID")
	}
	resolution, err = ResolvePersonByEmail(ctx, email)
	if err != nil || resolution.Person == nil || resolution.Alias == nil {
		t.Fatalf("confirmed volunteer identity = %+v, %v", resolution, err)
	}
	if _, err := ConfirmVolunteerApplication(ctx, renewedToken); err == nil || !strings.Contains(err.Error(), "already used") {
		t.Fatalf("reused volunteer confirmation = %v, want already-used error", err)
	}
}

func TestDatabaseSmokePersonMergeRejectsDuplicateVolunteerEvent(t *testing.T) {
	ctx := databaseSmokeContext(t)
	conferenceID, _ := insertSmokeConference(t, ctx)
	suffix := databaseSmokeSuffix()
	canonicalID, err := CreateSpeaker(ctx, SpeakerInput{Name: "Volunteer Merge A", Email: "vol-merge-a-" + suffix + "@example.test"})
	if err != nil {
		t.Fatalf("create canonical volunteer person: %v", err)
	}
	sourceID, err := CreateSpeaker(ctx, SpeakerInput{Name: "Volunteer Merge B", Email: "vol-merge-b-" + suffix + "@example.test"})
	if err != nil {
		t.Fatalf("create source volunteer person: %v", err)
	}
	var canonicalVolunteerID, sourceVolunteerID string
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `INSERT INTO volunteers (person_id, status) VALUES ($1::uuid, 'Applied') RETURNING id::text`, canonicalID).Scan(&canonicalVolunteerID); err != nil {
		t.Fatalf("insert canonical volunteer: %v", err)
	}
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `INSERT INTO volunteers (person_id, status) VALUES ($1::uuid, 'Declined') RETURNING id::text`, sourceID).Scan(&sourceVolunteerID); err != nil {
		t.Fatalf("insert source volunteer: %v", err)
	}
	if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `
		INSERT INTO volunteers_conferences (volunteer_id, conference_id, kind)
		VALUES ($1::uuid, $3::uuid, 'schedule_for'), ($2::uuid, $3::uuid, 'schedule_for')
	`, canonicalVolunteerID, sourceVolunteerID, conferenceID); err != nil {
		t.Fatalf("insert volunteer conference links: %v", err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM volunteers WHERE id = ANY($1::uuid[])`, []string{canonicalVolunteerID, sourceVolunteerID})
		_, _ = ctx.DB.Exec(context.Background(), `DELETE FROM people WHERE id = ANY($1::uuid[])`, []string{canonicalID, sourceID})
	})

	preview, err := PreviewPersonMerge(ctx, canonicalID, sourceID)
	if err != nil {
		t.Fatalf("PreviewPersonMerge: %v", err)
	}
	found := false
	for _, conflict := range preview.Conflicts {
		if conflict.Kind == "volunteer_application" {
			found = true
		}
	}
	if !found {
		t.Fatalf("merge conflicts = %+v, want volunteer_application", preview.Conflicts)
	}
}
