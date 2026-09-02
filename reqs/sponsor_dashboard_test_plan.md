# Sponsor dashboard handoff and test plan

## Scope in this branch

This branch establishes the sponsor-platform foundation:

- Organization owners, managers, and members.
- Event-specific sponsorship entitlements.
- Sponsor dashboard overview, team invitations, and public organization editing.
- Past sponsorship trophy case.
- Sponsor ticket issuance with event-allocation accounting.
- Sponsor prize proposals with hackathon-organizer approval.
- Individual hackathon sponsor-contact preferences.
- Append-only, policy-versioned consent history.
- Sponsor audit events for sensitive management operations.

Consent-filtered participant contact views/exports and sponsor invitation
email delivery are included. Pending-invitation management is intentionally
deferred.

## Prepare a local environment

The sponsor migrations were renumbered above migration 076. If the local
database previously applied draft migrations 074 or 075,
rebuild it before testing:

```sh
just db-reset
go run ./cmd/dev-seed
```

Alternatively, `just dev-reset-db` rebuilds the entire local Postgres data
directory, migrates, seeds, and prints a development login link.

Never run the development application with `PROD=true`.

Start the site normally with:

```sh
just dev-up
```

Useful seeded identities:

- `mara.chen@example.test`: owner of Signet Systems and owner of the seeded
  Mempool Observatory project.
- `dev-admin@example.test`: manager of Signet Systems and global administrator.
- `rafael.silva@example.test`: useful as an invitation recipient or negative
  authorization test account.

## Automated checks

Run:

```sh
nix develop -c go run ./cmd/db-migrate
nix develop -c go run ./cmd/dev-seed
nix develop -c go run ./cmd/dev-seed
nix develop -c go test ./...
git diff --check
```

Expected results:

- Migrations report that the database is up to date.
- Both seed runs finish successfully.
- The full Go test suite passes.
- `git diff --check` produces no output.

## Dashboard navigation and authorization

1. Log in as `mara.chen@example.test` and open `/dashboard`.
2. Confirm the Sponsor tab is visible and opens `/dashboard/sponsor`.
3. Confirm it redirects to the Signet Systems workspace.
4. Log in as an account without an organization membership and confirm the
   Sponsor tab is absent.
5. Manually request `/dashboard/sponsor/{organization-id}` as that account and
   confirm access is denied.
6. Confirm a member can view their organization workspace but only owners and
   managers see invitation and organization-editing controls.

## Sponsor overview

As Mara, verify the Signet Systems workspace shows:

- The active `dev26` sponsorship and its status.
- Ticket allocation of 20.
- One seeded sponsor award against an allowance of two.
- Participant contact access described as opt-in only and not yet enabled.
- Mara as owner and the local admin as manager.
- The past Satoshi sponsorship in the trophy case.
- No internal sponsorship or organization notes anywhere in the HTML.

Archived sponsorships must not appear. Pending or in-progress sponsorships may
be visible as records, but only `Paid` and `Committed` sponsorships may grant
action capabilities.

## Sponsor tickets

1. As Mara, expand “Issue sponsor tickets” for `dev26`.
2. Issue two tickets to an email address you can inspect locally.
3. Confirm the dashboard changes from `0 / 20` to `2 / 20` and records the
   recipient and quantity in the issuance history.
4. Confirm two `sponsor` registrations share the issuance batch recorded in
   `sponsor_ticket_issuances` and that the normal ticket mailer picks them up.
5. Attempt to issue 19 more tickets and confirm the request is rejected with
   18 remaining.
6. Submit the same action simultaneously in two browsers near the limit and
   confirm only the batch that fits succeeds.
7. Confirm a plain organization member and a manager from another organization
   cannot issue from this sponsorship.

## Sponsor prize proposals

1. As Mara, expand “Propose a hackathon prize” for `dev26`.
2. Submit a one-winner satoshi prize with a positive whole-number value.
3. Confirm it appears as pending and is not yet visible on the public awards
   page.
4. As `dev-admin@example.test`, open `/dev26/admin/hackathon/awards` and review
   the proposal.
5. Reject one proposal with a note and confirm the sponsor sees the rejection
   and note without an award being created.
6. Submit another, approve it, and confirm an available challenge award and
   prize are created with Signet Systems as the sponsor.
7. Confirm the award can then be edited with the normal organizer award tools
   and appears publicly according to the existing award visibility rules.
8. Fill the sponsorship's two-award allowance and confirm another pending
   proposal is rejected.
9. Confirm a plain organization member, unrelated sponsor, and inactive
   sponsorship cannot submit proposals.

## Organization profile editing

1. As Mara or `dev-admin@example.test`, update the public organization name,
   tagline, website, social links, hiring flag, and logo URL.
2. Confirm the changes persist and appear on the sponsor workspace/public
   sponsor surfaces.
3. If Spaces is configured, upload light- and dark-background logo files and
   confirm both render correctly.
4. Submit an empty organization name and confirm it is rejected.
5. Confirm a plain organization member cannot submit the profile endpoint.
6. Confirm editing is denied when the only relevant sponsorship is inactive,
   archived, or lacks `can_edit_organization`.
7. Confirm internal organization notes remain unchanged after the public form
   is submitted.

## Organization invitations

### Event-admin invitation

1. As `dev-admin@example.test`, open `/dev26/admin/sponsors` and edit a
   sponsorship.
2. Under “Or invite a new manager,” enter a name and an email that does not
   belong to an existing development account, then save.
3. Confirm the page reports that the sponsorship was saved and the invitation
   was emailed.
4. Open the email and follow its 72-hour secure login link.
5. Confirm the email-login confirmation routes to “Set up your account,” the
   invited name is prefilled, and photo, phone, and Signal are optional.
6. Create the account, accept the one-time sponsor invitation, and confirm the
   browser lands on the organization sponsor dashboard.
7. Repeat with an email belonging to an existing account and confirm account
   setup is skipped.
8. Confirm the resulting organization membership is `manager`, a reused link
   fails, and an account authenticated with another email cannot accept it.

### Sponsor-manager invitation

1. As Mara, invite `rafael.silva@example.test` as a member.
2. Copy the generated link. Confirm it is shown only after creation and is not
   stored in plaintext in the database.
3. Create a second invitation for the same email as manager.
4. Confirm the first link is no longer usable.
5. Open the second link while logged out and confirm sign-in preserves the
   return destination.
6. Try accepting it through an account without the invited verified email and
   confirm acceptance fails without disclosing the email in the URL error.
7. Log in as Rafael, accept it, and confirm the Sponsor tab appears.
8. Confirm Rafael has the manager role and that the link cannot be reused.
9. Try inviting Rafael again and confirm an existing active member cannot be
   reinvited.
10. Confirm an owner cannot be reinvited as a lower role.
11. Inspect application logs and confirm the token appears only as
    `/sponsor-invites/[redacted]`.
12. Inspect the invitation response and confirm `Cache-Control: private,
    no-store` and `Referrer-Policy: no-referrer` are present.

## Participant sponsor-contact consent

1. Log in as Mara and open:
   `/dev26/hackathon/projects/00000000-0000-4000-8000-000000000b02/edit`
2. Open the Sponsor contact tab.
3. Confirm the seeded state allows only sponsors whose prizes the project
   entered.
4. Enable both options and save:
   - “Allow sponsors of this hackathon to contact me.”
   - “Allow sponsors whose prizes I enter to contact me.”
5. Reload and confirm both remain selected.
6. Disable both, save, and confirm the withdrawal persists.
7. Log in as a teammate and confirm they have independent unchecked choices.
8. Confirm a project owner, hackathon manager, or global administrator cannot
   set a teammate's preference on that teammate's behalf.
9. Confirm these preferences do not affect project visibility, submission,
   judging, or prize eligibility.
10. Confirm each save adds a row to
    `hackathon_sponsor_contact_consent_events` with policy version
    `sponsor-contact-v1`; prior events must remain unchanged.

## Privacy copy

Review `/privacy` and `/terms` and confirm they state that:

- Sponsor contact is optional.
- Consent is event-specific.
- All-sponsor and entered-prize-sponsor consent are separate.
- Each participant controls their own email.
- Organizers and judges may still contact participants as necessary to
  administer submissions and prizes.

## Next implementation slice

Recommended next work, in order:

1. Add pending/resend/revoke controls for sponsor invitations.
2. Add sponsor headlines and project/prize outcome reporting.
3. Add sponsor member role changes and ticket revocation/reallocation.
4. Keep sponsor-judge assignment in the hackathon organizer administration.
