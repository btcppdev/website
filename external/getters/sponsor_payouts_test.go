package getters

import (
	"errors"
	"strings"
	"testing"

	"btcpp-web/internal/types"
)

func TestSponsorPayoutsScopeAndReadiness(t *testing.T) {
	ctx := postgresSmokeContext(t)
	owner := insertSmokePerson(t, ctx, "payout-owner")
	member := insertSmokePerson(t, ctx, "payout-member")
	winner := insertSmokePerson(t, ctx, "payout-winner")
	teammate := insertSmokePerson(t, ctx, "payout-teammate")
	org := insertSmokeOrg(t, ctx, "payout-sponsor")
	otherOrg := insertSmokeOrg(t, ctx, "other-payout-sponsor")
	conf, _ := insertSmokeConference(t, ctx)
	competition := createSmokeCompetition(t, ctx, CompetitionInput{ConferenceID: conf, Title: "Payout test"})
	run := func(query string, args ...any) {
		t.Helper()
		if _, err := ctx.DB.Exec(ctx.DatabaseContext(), query, args...); err != nil {
			t.Fatal(err)
		}
	}
	run(`INSERT INTO organization_memberships(organization_id, person_id, role, status)
		VALUES ($1,$2,'owner','active'), ($1,$3,'member','active'), ($4,$2,'manager','active')`, org, owner, member, otherOrg)
	// Duplicate event agreements must not duplicate a prize or recipient.
	for _, sponsorOrg := range []string{org, org, otherOrg} {
		var sponsorship string
		if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `INSERT INTO sponsorships(organization_id,name,level,status)
			VALUES ($1,'Payout fixture','Gold','Paid') RETURNING id::text`, sponsorOrg).Scan(&sponsorship); err != nil {
			t.Fatal(err)
		}
		run(`INSERT INTO sponsorships_conferences(sponsorship_id,conference_id) VALUES($1,$2)`, sponsorship, conf)
	}
	project := createSmokeProject(t, ctx, ProjectInput{
		CompetitionID: competition, CreatedByPersonID: winner,
		Slug: "payout-" + postgresSmokeSuffix(), Title: "Winning project",
	})
	if err := AddProjectMember(ctx, project, teammate, ProjectMemberRoleMember); err != nil {
		t.Fatal(err)
	}
	makePrize := func(sponsorOrg, prizeType string) (string, string) {
		t.Helper()
		award, err := CreateAward(ctx, AwardInput{
			CompetitionID: competition, SponsoredByOrgID: sponsorOrg, Title: sponsorOrg,
			Status: AwardStatusAvailable,
		})
		if err != nil {
			t.Fatal(err)
		}
		if err := AssignProjectAward(ctx, award, project); err != nil {
			t.Fatal(err)
		}
		prize, err := CreatePrize(ctx, PrizeInput{
			AwardID: award, PrizeType: prizeType, Title: "Prize", ValueText: "1000000", Status: PrizeStatusAvailable,
		})
		if err != nil {
			t.Fatal(err)
		}
		return award, prize
	}
	award, prize := makePrize(org, PrizeTypeSats)
	_, otherPrize := makePrize(otherOrg, PrizeTypeSats)
	makePrize(org, PrizeTypeInKind)
	run(`UPDATE people SET lightning_address='winner@example.test', bitcoin_address='bc1qtest'
		WHERE id IN ($1,$2)`, winner, teammate)
	list := func(sponsorOrg, viewer string) []*types.SponsorPayout {
		t.Helper()
		payouts, err := ListSponsorPayouts(ctx, sponsorOrg, viewer)
		if err != nil {
			t.Fatal(err)
		}
		return payouts
	}
	if got := list(org, owner); len(got) != 0 {
		t.Fatal("unfinalized results exposed payouts")
	}
	run(`UPDATE competitions SET results_finalized_at=now() WHERE id=$1`, competition)
	payouts := list(org, owner)
	if len(payouts) != 1 || payouts[0].PrizeID != prize || len(payouts[0].Recipients) != 2 {
		t.Fatalf("expected one own cash prize with two recipients: %+v", payouts)
	}
	for _, recipient := range payouts[0].Recipients {
		if recipient.StatusLabel() != "Awaiting payout allocation" || recipient.LightningAddress != "" || recipient.BitcoinAddress != "" {
			t.Fatalf("unallocated payout exposes payment instructions: %+v", recipient)
		}
	}
	if got := list(otherOrg, owner); len(got) != 1 || got[0].PrizeID != otherPrize {
		t.Fatalf("organization switch leaked or lost prizes: %+v", got)
	}
	for _, viewer := range []string{member, winner} {
		if got := list(org, viewer); len(got) != 0 {
			t.Fatal("non-manager exposed payouts")
		}
	}
	run(`UPDATE organization_memberships SET role='manager' WHERE organization_id=$1 AND person_id=$2`, org, owner)
	if len(list(org, owner)) != 1 {
		t.Fatal("active manager cannot see payouts")
	}
	run(`UPDATE organization_memberships SET status='removed' WHERE organization_id=$1 AND person_id=$2`, org, owner)
	if len(list(org, owner)) != 0 {
		t.Fatal("removed manager exposed payouts")
	}
	run(`UPDATE organization_memberships SET status='active' WHERE organization_id=$1 AND person_id=$2`, org, owner)
	for _, status := range []string{"Pending", "Cancelled"} {
		run(`UPDATE sponsorships SET status=$2 WHERE organization_id=$1`, org, status)
		if len(list(org, owner)) != 0 {
			t.Fatalf("%s sponsorship exposed payouts", status)
		}
	}
	run(`UPDATE sponsorships SET status='Committed' WHERE organization_id=$1`, org)
	run(`UPDATE sponsorships SET archived_at=now() WHERE organization_id=$1`, org)
	if len(list(org, owner)) != 0 {
		t.Fatal("archived sponsorship exposed payouts")
	}
	run(`UPDATE sponsorships SET archived_at=NULL WHERE organization_id=$1`, org)
	if _, err := PrepareCashPrizeDistributions(ctx, competition, award, project, prize); err != nil {
		t.Fatal(err)
	}
	payouts = list(org, owner)
	for _, recipient := range payouts[0].Recipients {
		if recipient.AmountSats == nil || *recipient.AmountSats != 500000 || recipient.StatusLabel() != "Awaiting winner paperwork" || recipient.LightningAddress != "" || recipient.BitcoinAddress != "" {
			t.Fatalf("tax gate or split incorrect: %+v", recipient)
		}
	}
	run(`UPDATE people SET tax_form_uploaded_at=now(), tax_form_type='w9' WHERE id=$1`, winner)
	var ready *types.SponsorPayoutRecipient
	for _, recipient := range list(org, owner)[0].Recipients {
		if recipient.PersonID == winner {
			ready = recipient
		}
	}
	if ready == nil || !ready.ReadyToPay() || ready.LightningAddress != "winner@example.test" || ready.BitcoinAddress != "bc1qtest" {
		t.Fatalf("ready winner missing instructions: %+v", ready)
	}
	for _, tc := range []struct{ org, actor string }{{org, member}, {org, winner}, {otherOrg, owner}} {
		if changed, err := MarkSponsorPayoutPaid(ctx, tc.org, tc.actor, ready.DistributionID, "test-payment-hash"); changed || !errors.Is(err, ErrSponsorPayoutUnavailable) {
			t.Fatalf("unauthorized payout update: changed=%t err=%v", changed, err)
		}
	}
	for _, reference := range []string{"", "  ", strings.Repeat("x", 501)} {
		if changed, err := MarkSponsorPayoutPaid(ctx, org, owner, ready.DistributionID, reference); changed || !errors.Is(err, ErrSponsorPayoutReference) {
			t.Fatalf("invalid payment reference accepted: changed=%t err=%v", changed, err)
		}
	}
	for _, recipient := range list(org, owner)[0].Recipients {
		if recipient.PersonID == teammate {
			if changed, err := MarkSponsorPayoutPaid(ctx, org, owner, recipient.DistributionID, "test-payment-hash"); changed || !errors.Is(err, ErrSponsorPayoutUnavailable) {
				t.Fatalf("tax gate bypassed: changed=%t err=%v", changed, err)
			}
		}
	}
	run(`UPDATE people SET lightning_address='', bitcoin_address='' WHERE id=$1`, winner)
	if changed, err := MarkSponsorPayoutPaid(ctx, org, owner, ready.DistributionID, "test-payment-hash"); changed || !errors.Is(err, ErrSponsorPayoutUnavailable) {
		t.Fatalf("missing-address gate bypassed: changed=%t err=%v", changed, err)
	}
	run(`UPDATE people SET lightning_address='winner@example.test' WHERE id=$1`, winner)
	run(`UPDATE award_distributions SET notes='private admin note' WHERE id=$1`, ready.DistributionID)
	type result struct {
		changed bool
		err     error
	}
	results := make(chan result, 2)
	for i := 0; i < 2; i++ {
		go func() {
			changed, err := MarkSponsorPayoutPaid(ctx, org, owner, ready.DistributionID, "test-payment-hash")
			results <- result{changed, err}
		}()
	}
	changedCount := 0
	for i := 0; i < 2; i++ {
		result := <-results
		if result.err != nil {
			t.Fatal(result.err)
		}
		if result.changed {
			changedCount++
		}
	}
	if changedCount != 1 {
		t.Fatalf("concurrent submissions recorded %d payments, want 1", changedCount)
	}
	var recordedStatus, completedBy, notes string
	var completed bool
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT status, completed_by::text, completed_at IS NOT NULL, notes
		FROM award_distributions WHERE id=$1`, ready.DistributionID).Scan(&recordedStatus, &completedBy, &completed, &notes); err != nil {
		t.Fatal(err)
	}
	if recordedStatus != "sent" || completedBy != owner || !completed || notes != "private admin note\nSponsor reported payment: test-payment-hash" {
		t.Fatalf("admin payout record incorrect: %s %s %t %q", recordedStatus, completedBy, completed, notes)
	}
	var auditCount int
	if err := ctx.DB.QueryRow(ctx.DatabaseContext(), `SELECT count(*) FROM sponsor_audit_events
		WHERE target_id=$1 AND organization_id=$2 AND actor_person_id=$3
		AND action='sponsor.payout_marked_paid' AND details->>'payment_reference'='test-payment-hash'`,
		ready.DistributionID, org, owner).Scan(&auditCount); err != nil || auditCount != 1 {
		t.Fatalf("expected exactly one payment audit event, got %d: %v", auditCount, err)
	}
	for _, status := range []string{"ready", "sent", "claimed", "cancelled"} {
		if err := UpdateAwardDistribution(ctx, competition, ready.DistributionID, status, "private admin note", owner); err != nil {
			t.Fatal(err)
		}
		for _, recipient := range list(org, owner)[0].Recipients {
			if recipient.PersonID != winner {
				continue
			}
			if recipient.ReadyToPay() != (status == "ready") {
				t.Fatalf("incorrect readiness for %s", status)
			}
			if status != "ready" && (recipient.LightningAddress != "" || recipient.BitcoinAddress != "") {
				t.Fatalf("%s payout still exposes payment addresses", status)
			}
		}
		if status == "cancelled" {
			if changed, err := MarkSponsorPayoutPaid(ctx, org, owner, ready.DistributionID, "test-payment-hash"); changed || !errors.Is(err, ErrSponsorPayoutUnavailable) {
				t.Fatalf("cancelled payout accepted payment: changed=%t err=%v", changed, err)
			}
		}
	}
	run(`UPDATE awards SET archived_at=now() WHERE id=$1`, award)
	if len(list(org, owner)) != 0 {
		t.Fatal("archived award exposed payouts")
	}
}
