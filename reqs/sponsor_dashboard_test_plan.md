# Sponsor dashboard handoff and test plan

## Scope in this branch

This branch establishes the sponsor-platform foundation:

- Organization owners, managers, and members.
- Event-specific sponsorship entitlements.
- Sponsor dashboard overview, team invitations, and public organization editing.
- Past sponsorship trophy case.
- Individual hackathon sponsor-contact preferences.
- Append-only, policy-versioned consent history.
- Sponsor audit events for sensitive management operations.

Participant contact views/exports, ticket issuance, sponsor prize creation,
invitation email delivery, and pending-invitation management are intentionally
deferred.

## Prepare a local environment

The draft sponsor migrations were consolidated before being committed. If the
local database previously applied an earlier version of migrations 074-076,
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

1. Email sponsor invitations and add pending/resend/revoke controls.
2. Add consent-filtered participant contact views with mandatory audit logging.
3. Add sponsor ticket allocation, claiming, and issuance.
4. Add sponsor-managed prize proposals and organizer approval.
5. Add sponsor headlines and project/prize outcome reporting.
