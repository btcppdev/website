package getters

import (
	"strings"
	"testing"

	"btcpp-web/internal/config"
)

func insertMergeBadgeGrant(t *testing.T, ctx *config.AppContext, org, recipient, creator, badge, state string) string {
	t.Helper()
	var id string
	err := ctx.DB.QueryRow(ctx.DatabaseContext(), `INSERT INTO organization_badge_grants
 (organization_id,recipient_person_id,created_by_person_id,issuer_pubkey,badge_identifier,badge_name,badge_image_url,subject_profile_url,state,award_event_id)
 VALUES ($1,$2,$3,$4,$5,'Test badge','https://example.test/badge.png','https://example.test/profile',$6,$7) RETURNING id::text`, org, recipient, creator, strings.Repeat("a", 64), badge, state, strings.Repeat("b", 64)).Scan(&id)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestPersonMergeBadgeRelationshipsAndUndo(t *testing.T) {
	f := newMergeAccountsFixture(t)
	ctx := f.app
	dbctx := ctx.DatabaseContext()
	grant := insertMergeBadgeGrant(t, ctx, f.org, f.source, f.source, "owned", "issued")
	created := insertMergeBadgeGrant(t, ctx, f.org, f.canonical, f.source, "created", "issued")
	canonicalGrant := insertMergeBadgeGrant(t, ctx, f.org, f.canonical, f.canonical, "historical", "issued")
	historical := insertMergeBadgeGrant(t, ctx, f.org, f.source, f.source, "historical", "corrected")
	if _, err := ctx.DB.Exec(dbctx, `UPDATE organization_badge_grants SET corrected_by_grant_id=$2 WHERE id=$1`, historical, canonicalGrant); err != nil {
		t.Fatal(err)
	}
	_, err := ctx.DB.Exec(dbctx, `INSERT INTO person_badge_presentations(person_id,badge_ref,featured_position,hidden) VALUES
 ($1,'shared',NULL,true),($1,'canonical-featured',1,false),
 ($2,'shared',2,false),($2,'source-collision',1,false),($2,'source-free',3,false),($2,'source-hidden',NULL,true)`, f.canonical, f.source)
	if err != nil {
		t.Fatal(err)
	}
	before, err := ListPersonBadgePresentations(ctx, f.source)
	if err != nil {
		t.Fatal(err)
	}
	event, err := MergePeople(ctx, PersonMergeInput{CanonicalPersonID: f.canonical, SourcePersonID: f.source, MergedByPersonID: f.canonical})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{grant, created, historical} {
		var recipient, creator, award string
		if err := ctx.DB.QueryRow(dbctx, `SELECT recipient_person_id::text,created_by_person_id::text,award_event_id FROM organization_badge_grants WHERE id=$1`, id).Scan(&recipient, &creator, &award); err != nil {
			t.Fatal(err)
		}
		if recipient != f.canonical || creator != f.canonical || award != strings.Repeat("b", 64) {
			t.Fatalf("grant %s references or signed event changed: %s %s %s", id, recipient, creator, award)
		}
	}
	var correctedBy string
	if err := ctx.DB.QueryRow(dbctx, `SELECT corrected_by_grant_id::text FROM organization_badge_grants WHERE id=$1`, historical).Scan(&correctedBy); err != nil || correctedBy != canonicalGrant {
		t.Fatal("correction history lost", err)
	}
	displays, err := ListPersonBadgePresentations(ctx, f.canonical)
	if err != nil || len(displays) != 5 {
		t.Fatal("missing merged display settings", err)
	}
	for _, d := range displays {
		switch d.BadgeRef {
		case "shared", "source-hidden":
			if !d.Hidden {
				t.Fatal("hidden preference lost", d)
			}
		case "source-collision":
			if d.FeaturedPosition != 0 || d.Hidden {
				t.Fatal("colliding badge must remain visible, unfeatured", d)
			}
		case "canonical-featured":
			if d.FeaturedPosition != 1 {
				t.Fatal("canonical position changed", d)
			}
		case "source-free":
			if d.FeaturedPosition != 3 {
				t.Fatal("free source position lost", d)
			}
		}
	}
	preview, err := GetPersonMergeUndoPreview(ctx, event)
	if err != nil || preview.Changed {
		t.Fatalf("unexpected undo changes: %+v %v", preview, err)
	}
	// Badge lifecycle changes after merging must survive undo.
	_, err = ctx.DB.Exec(dbctx, `UPDATE organization_badge_grants SET state='revoked',revocation_event_id=$2,revoked_at=now(),revocation_reason='Revoked after merge' WHERE id=$1`, grant, strings.Repeat("c", 64))
	if err != nil {
		t.Fatal(err)
	}
	preview, err = GetPersonMergeUndoPreview(ctx, event)
	if err != nil || !preview.Changed {
		t.Fatal("post-merge revocation not reported", err)
	}
	if err := UndoPersonMerge(ctx, event, f.canonical, preview); err != nil {
		t.Fatal(err)
	}
	var recipient, creator, state, revocation, award string
	if err := ctx.DB.QueryRow(dbctx, `SELECT recipient_person_id::text,created_by_person_id::text,state,revocation_event_id,award_event_id FROM organization_badge_grants WHERE id=$1`, grant).Scan(&recipient, &creator, &state, &revocation, &award); err != nil {
		t.Fatal(err)
	}
	if recipient != f.source || creator != f.source || state != "revoked" || revocation != strings.Repeat("c", 64) || award != strings.Repeat("b", 64) {
		t.Fatal("undo lost ownership or lifecycle", recipient, creator, state, revocation, award)
	}
	if err := ctx.DB.QueryRow(dbctx, `SELECT recipient_person_id::text,created_by_person_id::text FROM organization_badge_grants WHERE id=$1`, created).Scan(&recipient, &creator); err != nil {
		t.Fatal(err)
	}
	if recipient != f.canonical || creator != f.source {
		t.Fatal("creator-only relationship not restored")
	}
	restored, err := ListPersonBadgePresentations(ctx, f.source)
	if err != nil || len(restored) != len(before) {
		t.Fatal("display undo lost rows", err)
	}
	for i, d := range before {
		if *d != *restored[i] {
			t.Fatalf("display undo mismatch: %+v %+v", d, restored[i])
		}
	}
}

func TestPersonMergeDuplicateBadgeGrantsRequireResolution(t *testing.T) {
	for _, state := range []string{"issued", "revoked", "granted"} {
		t.Run(state, func(t *testing.T) {
			f := newMergeAccountsFixture(t)
			insertMergeBadgeGrant(t, f.app, f.org, f.canonical, f.canonical, "same-badge", "issued")
			source := insertMergeBadgeGrant(t, f.app, f.org, f.source, f.source, "same-badge", state)
			preview, err := PreviewPersonMerge(f.app, f.canonical, f.source)
			if err != nil {
				t.Fatal(err)
			}
			found := false
			for _, conflict := range preview.Conflicts {
				if conflict.Kind == "organization_badge_grant" {
					found = true
				}
			}
			if !found {
				t.Fatal("missing duplicate badge conflict")
			}
			if _, err := MergePeople(f.app, PersonMergeInput{CanonicalPersonID: f.canonical, SourcePersonID: f.source, MergedByPersonID: f.canonical}); err == nil {
				t.Fatal("duplicate grants silently merged")
			}
			var recipient string
			if err := f.app.DB.QueryRow(f.app.DatabaseContext(), `SELECT recipient_person_id::text FROM organization_badge_grants WHERE id=$1`, source).Scan(&recipient); err != nil || recipient != f.source {
				t.Fatal("failed merge modified source grant", err)
			}
		})
	}
}
