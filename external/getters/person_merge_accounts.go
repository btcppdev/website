package getters

import (
	"context"
	"fmt"
)

// Duplicate history must not be discarded and conflicting permissions/consent
// must be resolved explicitly before combining identities.
func personMergeAccountConflicts(ctx context.Context, db personMergeQuerier, canonical, source string) ([]PersonMergeConflict, error) {
	checks := []struct{ kind, description, query string }{
		{"organization_membership", "The profiles have different roles or membership statuses in the same organization. Resolve those memberships before merging.", `SELECT EXISTS(SELECT 1 FROM organization_memberships a JOIN organization_memberships b USING(organization_id) WHERE a.person_id=$1 AND b.person_id=$2 AND (a.role<>b.role OR a.status<>b.status))`},
		{"sponsor_contact_consent", "The profiles have different sponsor contact-sharing choices for the same hackathon. Resolve those consent choices before merging.", `SELECT EXISTS(SELECT 1 FROM hackathon_sponsor_contact_consents a JOIN hackathon_sponsor_contact_consents b USING(competition_id) WHERE a.person_id=$1 AND b.person_id=$2 AND (a.all_hackathon_sponsors<>b.all_hackathon_sponsors OR a.entered_award_sponsors<>b.entered_award_sponsors))`},
		{"oauth_consent", "The profiles have different permissions or revocation states for the same connected app. Resolve those app permissions before merging.", `SELECT EXISTS(SELECT 1 FROM oauth_consents a JOIN oauth_consents b USING(client_id) WHERE a.person_id=$1 AND b.person_id=$2 AND (NOT(a.scopes @> b.scopes AND b.scopes @> a.scopes) OR (a.revoked_at IS NULL)<>(b.revoked_at IS NULL)))`},
		{"organization_membership_request", "Both profiles have a pending membership request for the same organization. Resolve one request before merging.", `SELECT EXISTS(SELECT 1 FROM organization_membership_requests a JOIN organization_membership_requests b USING(organization_id) WHERE a.person_id=$1 AND b.person_id=$2 AND a.status='pending' AND b.status='pending')`},
		{"direct_speaker_invitation", "Both profiles have an outstanding direct speaker invitation for the same conference. Resolve one invitation before merging.", `SELECT EXISTS(SELECT 1 FROM proposals a JOIN proposals b USING(conference_id) WHERE a.direct_invitee_person_id=$1 AND b.direct_invitee_person_id=$2 AND a.status='Invited' AND b.status='Invited')`},
	}
	var conflicts []PersonMergeConflict
	for _, check := range checks {
		var exists bool
		if err := db.QueryRow(ctx, check.query, canonical, source).Scan(&exists); err != nil {
			return nil, fmt.Errorf("check %s merge conflicts: %w", check.kind, err)
		}
		if exists {
			conflicts = append(conflicts, PersonMergeConflict{Kind: check.kind, Description: check.description})
		}
	}
	return conflicts, nil
}
