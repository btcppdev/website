package getters

import (
	"btcpp-web/internal/types"
	"errors"
	"testing"
)

func TestDatabaseSmokeManualRosterAndTemporaryPassword(t *testing.T) {
	ctx := databaseSmokeContext(t)
	confID, _ := insertSmokeConference(t, ctx)
	email := "roster-" + databaseSmokeSuffix() + "@example.test"
	vol := &types.Volunteer{Name: "Manual Staff", Email: email, ScheduleFor: []*types.Conf{{Ref: confID}}}
	if err := AddVolunteerToRoster(ctx, vol); err != nil {
		t.Fatal(err)
	}
	var person string
	var verified bool
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT person_id::text,verified_at IS NOT NULL FROM person_emails WHERE email=$1::citext`, email).Scan(&person, &verified); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = ctx.DB.Exec(ctx.DatabaseContext(), `DELETE FROM volunteers WHERE person_id=$1::uuid`, person)
		_, _ = ctx.DB.Exec(ctx.DatabaseContext(), `DELETE FROM people WHERE id=$1::uuid`, person)
	})
	if !verified || vol.Status != "PendingShifts" {
		t.Fatal("coordinator-confirmed roster has incorrect email or status", verified, vol.Status)
	}
	if err := AddVolunteerToRoster(ctx, vol); !errors.Is(err, ErrVolunteerAlreadyApplied) {
		t.Fatal("duplicate allowed", err)
	}
	// Adding an existing account preserves its canonical profile.
	conf2, _ := insertSmokeConference(t, ctx)
	other := &types.Volunteer{Name: "Do not overwrite", Email: email, ScheduleFor: []*types.Conf{{Ref: conf2}}}
	if err := AddVolunteerToRoster(ctx, other); err != nil {
		t.Fatal(err)
	}
	var name string
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT name FROM people WHERE id=$1::uuid`, person).Scan(&name); err != nil {
		t.Fatal(err)
	}
	if name != "Manual Staff" {
		t.Fatal("existing profile overwritten", name)
	}
	before, err := PersonSessionVersion(ctx, person)
	if err != nil {
		t.Fatal(err)
	}
	if err := IssueTemporaryPassword(ctx, person, "temporary-hash"); err != nil {
		t.Fatal(err)
	}
	credential, err := GetPersonPasswordCredential(ctx, person)
	if err != nil {
		t.Fatal(err)
	}
	if !credential.MustChangePassword || credential.TemporaryExpiresAt == nil {
		t.Fatal("temporary restriction missing")
	}
	after, err := PersonSessionVersion(ctx, person)
	if err != nil || after <= before {
		t.Fatal("sessions not revoked", before, after, err)
	}
	if err := IssueTemporaryPassword(ctx, person, "replacement-hash"); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceTemporaryPassword(ctx, person, "temporary-hash", "new-hash"); !errors.Is(err, ErrTemporaryPasswordInvalid) {
		t.Fatal("stale proof allowed", err)
	}
	if err := ReplaceTemporaryPassword(ctx, person, "replacement-hash", "new-hash"); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceTemporaryPassword(ctx, person, "replacement-hash", "replayed"); !errors.Is(err, ErrTemporaryPasswordInvalid) {
		t.Fatal("proof replay allowed", err)
	}
	credential, err = GetPersonPasswordCredential(ctx, person)
	if err != nil || credential.MustChangePassword || credential.TemporaryExpiresAt != nil || credential.PasswordHash != "new-hash" {
		t.Fatal("password change failed", credential, err)
	}
	if err := IssueTemporaryPassword(ctx, person, "expired"); err != nil {
		t.Fatal(err)
	}
	if _, err := ctx.DB.Exec(ctx.DatabaseContext(), `UPDATE person_password_credentials SET temporary_expires_at=now()-interval '1 minute' WHERE person_id=$1::uuid`, person); err != nil {
		t.Fatal(err)
	}
	if err := ReplaceTemporaryPassword(ctx, person, "expired", "new"); !errors.Is(err, ErrTemporaryPasswordInvalid) {
		t.Fatal("expired proof allowed", err)
	}
	if _, _, err := SetPersonPassword(ctx, person, "recovered"); err != nil {
		t.Fatal(err)
	}
	credential, err = GetPersonPasswordCredential(ctx, person)
	if err != nil || credential.MustChangePassword {
		t.Fatal("normal password change retained temporary restriction", err)
	}
}
