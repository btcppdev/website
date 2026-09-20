# Loot / Floripa 2026 import investigation

Checked 16 September 2026. No Loot records have been imported into the database.

## Retrieval status

The source event is `https://loot.fund/hackathons/bitcoin-exploits-edition`.
DNS resolves, but connections to the public site's HTTP and HTTPS ports time out,
including outside the local sandbox. Browser access also timed out. This does
not establish that the service is unavailable for everyone. No working API,
export schema, or associated source repository has been verified.

Two participant posts independently preserve original application links:

| Loot application | Project | Corroborating public record |
| --- | --- | --- |
| `8` | Stealth — Bitcoin Wallet Privacy Analyzer | https://stacker.news/items/1444710 |
| `13` | localprobe | https://stacker.news/items/1444781 |

Application IDs are source identifiers, not counts. These posts are partial
recovery evidence, not a complete gallery or an authoritative award roster.
The reported total of 21 projects remains provisional.

## Practical import route

Obtain an organizer export (JSON/CSV, including all pages), a working public
gallery/API, or an event-scoped export from the operator. Preserve the original
export before conversion. Needed data:

- Event ID, title, dates, and timezone.
- Project/application ID, submission status, title, description, repository,
  demo/video links, images, and team membership.
- Award/challenge IDs and names, winner project IDs, and final-result status.
- Judge profiles and assignments if available.
- Prize amounts and denominations. Actual payout records are separate and
  optional; an award amount must not be labeled a completed payout.

Use stable source IDs such as `loot-application-8` for project slugs when
converting to an import manifest. Exclude drafts/withdrawn entries according to
the actual source schema, and reconcile the accepted set with the source total.
Resolve award references against that set; count unique winning projects rather
than award assignments. Preserve unknown values rather than substituting zero.

The repository already has a useful import path:

- `internal/devpostimport/model.go`: event/project/award manifest types.
- `internal/devpostimport/importer.go`: transactional upserts for competitions,
  projects, members, awards, and judge assignments.
- `cmd/import-devpost/main.go`: accepts a local `-manifest`, supports `-dry-run`,
  and defaults to hidden visibility.

A Loot adapter can reuse the destination model, but should be implemented against
an actual payload rather than guessed field names. Before reuse, account for:

- Devpost-specific identity fields (`DevpostID`, `DevpostURL`) must not be
  populated with Loot identities as though they were Devpost profiles.
- The current importer can match people by name and assign project ownership;
  avoid automatic identity/ownership assignment for an unreviewed archive.
- Preserve original Loot links explicitly: project `SourceURL` currently is not
  written to the projects table by this importer; `docs_url` is available.
- The importer does not finalize competition results. Finalization must follow
  verification of the actual complete award roster.
- Importing into an existing conference updates its competition and matching
  project slugs. Inspect existing Floripa records before applying an import.

After conversion, run a database dry run, review projects and distinct winners,
then import as a hidden historical event for review. Do not infer sent/claimed
payouts from prizes or archive narrative.

## Current dependency

A reachable Loot source or organizer export is required to build and verify the
adapter and recover the complete project/winner counts. Ask the organizer which
access path they have; do not request account passwords in chat.

## Archive.org recovery check

The organizer confirmed they do not have a Loot login, export, or operator
contact. Checked Wayback on 16 September 2026:

- The CDX capture index request for `loot.fund/*` timed out, including outside
  the sandbox.
- The availability API returned HTTP 429 for the event URL, homepage, and
  application 8. A direct replay lookup also failed through the web tool.
- No archived snapshot has been verified or downloaded. These access failures
  do not establish that snapshots are absent.

When available, query the event path and its application pages, including both
HTTP/HTTPS URL variants. Recover the gallery and each linked application from
post-event captures; check pagination, missing assets, and whether any rendered
content depended on uncaptured API requests. A captured HTML shell alone is not
enough to establish a complete roster or final awards.
