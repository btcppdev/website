# Hackathon judging

The functional port of PR #40 keeps the current site navigation and page structure, with judging-specific submission controls and confirmation styling.

- Judges explicitly submit or update their rankings. Empty ballots are rejected. Browser edits remain local until submission; navigation warns about unsaved edits.
- The first successful submission for a judge/round shows confetti once. Reloading the confirmation or updating a ballot does not replay it. Reduced-motion preferences suppress the animation.
- Sponsor-only judges see their sponsor awards without an unrelated regular ballot.
- When no round is active, admin scoring selects the latest completed round.
- Results remain available only for closed rounds and authorized judges/staff. Submitting does not unlock open-round standings.

## Streaming results

The results page uses EventSource at `/{conf}/hackathon/judging/results/live`. It receives rendered result snapshots over SSE. There is no two-second results polling.

Migration 101 installs transactional PostgreSQL notifications for changes to scores, deliberation order, events, projects, competitions, and judging/staff roles. Row triggers ignore unchanged rows and changes only to `updated_at`: timeline reads synchronize event metadata and must not cause notification loops. Identical notifications in a transaction are coalesced, and rollback emits none.

Each application instance opens one dedicated LISTEN connection while it has connected viewers. Result snapshots are shared per competition/round, with concurrent computations coalesced. Authorization remains per viewer and is checked before every update. A listener reconnect invalidates snapshots to recover missed updates. The database connection must support PostgreSQL session-level LISTEN; no new application environment variables are required.

Streams send 15-second heartbeats and renew after 60 seconds, before the web server's write timeout. Renewal reloads sessions and handles time-based state changes. Hidden tabs disconnect; visible tabs reconnect. Role removal stops access, and the client removes previously displayed results. The SSE session middleware avoids scs response buffering and never commits a long-running stream's session over a concurrent browser request.

The application sends `X-Accel-Buffering: no` and `Cache-Control: private, no-store, no-transform`. A production rollout still needs a proxy-level smoke test to verify immediate delivery and reconnect behavior; local tests do not verify DigitalOcean's proxy.

## Migration and history

Apply migrations 100 and 101 with the normal application migration process. Their numbers follow master at 099; renumber if another branch ships migrations first.

Migration 100 backfills historical scorecards into submission history. Existing autosaved ballots therefore count as previous submissions. First/last timestamps are audit history; current ballot presence continues to derive from ranked scorecards.

Account merging transfers submission history through the existing merge manifest. For duplicate judge/round records, the canonical record remains and the source record is retained in the undo manifest. Undo restores both original records.

## Verification

Affected handler/getter/web package tests pass. Race-enabled shared-snapshot, SSE/session, real PostgreSQL notification, ballot persistence, and merge/undo tests pass against a disposable database. Notifications are tested for real changes, unchanged timeline reads, and rollback. SSE tests cover anonymous access, open-round denial, external-connection updates, and judge-role removal.

Browser verification covered desktop, 390px and 320px mobile layouts, submission/update, one-time confetti, and results streaming/reconnection. The fixture intentionally omits unrelated navigation/account and live-broadcast status endpoints.

Run the fixture only against a disposable, migrated database:

```sh
BTCPP_POSTGRES_SMOKE=1 JUDGING_BROWSER_PREVIEW=1 DATABASE_URL='<local test database>' \
  go test ./internal/handlers -run '^TestJudgingSSEFlow$' -v -timeout 0
```

Open http://127.0.0.1:8094/preview. The fixture's `/preview/close` route closes the round and opens results.

The account-merge manifest now covers badge recipients, badge grant creators, and profile badge presentation preferences as well as ballot history. The foreign-key coverage test and account merge/undo tests pass.

Merging preserves grant IDs, signed events, and correction links. Two non-canceled/non-corrected grants for the same organization, issuer and badge are reported as a conflict before merging, including revoked grants; they cannot be silently deduplicated. Resolve the grants through their normal lifecycle first. Signed recipient public keys and subject URLs are not rewritten by an account merge.

For display preferences, the canonical account's choice wins when both accounts configure the same badge. Its featured slots also take precedence: source badges that collide remain visible but unfeatured, while free source slots and hidden preferences transfer. Undo restores the original preferences. Undo restores badge account references without rolling back issuance, acceptance, correction, or revocation changes made after merging. No additional database migration is required for this handling.

## Project and prize share cards

Project, challenge, and winning-prize cards share the original heavy Arial Black headline style and position, fine grid, orange frame, event artwork, and black footer with a vertically centered tagline. Team labels and placement sashes retain the website's Plex typography. The original Ubuntu wordmark stays intact. The renderer embeds the Plex font files so previews and JPEGs use the same typography without external font requests. The bundled files and SIL Open Font Licenses come from Google Fonts' `ofl/ibmplexsans` and `ofl/ibmplexmono` directories; font changes are included in the image fingerprint.

Public project pages use generated 1200 × 630 JPEG cards with project artwork, title, summary, team names, and round profile photos. Project topic tags are omitted from the share images. Team portraits also appear on announced winning-prize cards; missing or unavailable portraits show initials. Up to six portraits appear, followed by a count for any additional team members. Missing or unavailable project artwork falls back to an orange graphic matching the frame. The image route is `/social-cards/project/{conference-tag}--{project-id}.jpg`; drafts, hidden projects, closed galleries, and unpublished events are excluded, including when an organizer views the page.

Prize links keep their existing prize card until results are published (`results_finalized_at`). Once published, the card features the public winning project and its artwork, with the prize and sponsor credit. Shared prizes name multiple winning projects; private projects remain excluded. Project cards gain award badges at the same publication boundary. Overall 1st, 2nd, and 3rd places get prominent gold, silver, and bronze sashes draped diagonally across the upper-left corner; honorable mentions get a purple sash. Projects with multiple overall awards use their best placement, while a prize card shows the placement for that prize. A prominent orange winner banner names additional awards above the project headline; the placement on the sash is not repeated. Sponsor challenge placements stay in the award details instead of using an overall-placement ribbon.

Sponsor-prize cards retain the sponsor logo after winners are announced, including shared prizes. Challenge and winning-prize cards share a fine grid, orange top and left borders, an upper-right sponsor logo, and a prominent lower-right prize value. The winning project takes the challenge headline's position and font, with round team portraits and a project thumbnail below. Sponsor logos and prize values have no background panel. The light-mode logo is preferred, falling back to the available dark-mode logo; unavailable images retain the sponsor's written credit.

Project, challenge, and announced winning-prize cards use the event's leading artwork as a background, fading to paper behind the text. Event artwork is softened behind the shared grid and headline. Project cards retain a summary in the lower-right column and the same thumbnail and team-photo layout as winning-prize cards. The existing conference image resolver selects the available `leading.avif`, `leading.png`, or other supported format; missing event artwork retains the plain grid.

The existing content fingerprint versions image URLs when project content, results, event artwork, or design changes. Withdrawing results restores the prize card. Social platforms may retain an earlier preview until they scrape the page again.

Run `TestHackathonProjectSocialCard*` and `TestHackathonAwardSocialCard*` for publication and metadata checks. `HACKATHON_CARD_TEST_DATABASE_URL` enables the image endpoint tests against a disposable database. Set `HACKATHON_CARD_PREVIEW_DIR` when running `TestHackathonSocialCardVisualFixtures` to export HTML examples for visual review.
