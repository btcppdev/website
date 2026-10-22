# Annual sponsor reports

Open **Admin → Annual sponsor reports**, or `/admin/reports/annual?year=2026&recipient=NYDIG`. HRF has its own draft at the same URL with `recipient=HRF`. Event reports remain available at `/{conf}/admin/report`.

## Preparing an edition

1. Select the year and recipient. Only global admins can access reports, saved editions, or Markdown previews.
2. Open **Edit report**, set the cutoff, and select the completed events. Event start years and completion dates use the conference timezone. Future events are excluded from totals.
3. Review each event's hackathon coverage. Choose reviewed database records, no hackathon, or unreconciled/external data missing. Vienna and Vegas 2026 default to no hackathon based on the organizer's confirmation. Unknown events are not counted as zero in hackathon charts.
4. Edit the letter, technical themes, community stories, expansion lessons, and next-year plans. The 2026 letter uses the organizer's supplied highlights; bracketed prompts still need editorial input. Markdown previews update as you type; metrics refresh after saving.
5. Add annual YouTube/X exports if available, including account/source, reporting period, export date and content coverage. Event-report reach snapshots are included separately, including Nairobi's reviewed dated default when no event draft exists. Different periods and overlapping content are not summed automatically.
6. Save the draft, then **Save & freeze edition**. A new read-only edition preserves the collected figures and text. The live draft remains editable; previous editions appear below the editor. Print the frozen edition to PDF, with background graphics enabled, to preserve the exact appearance sent to the sponsor.

## Definitions and limits

- The cutoff selects events, not a historical database snapshot. Figures reflect records at collection time. All annual event metrics and deduplicated totals are read in a single repeatable-read transaction.
- Attendance bars count distinct check-ins per event. Annual identities are deduplicated by person, falling back to email. The donut compares people at one included event against those at multiple included events; it does not claim first-ever attendance. Incomplete check-in records and account changes limit historical comparisons.
- Program counts are accepted, non-archived scheduled entries, deduplicated across multiple recordings. They do not claim confirmed delivery. Speakers are distinct profile identities across programs.
- Projects are submitted/advanced entries, not necessarily distinct teams. Participants are distinct project members across reviewed events. Winning projects require finalized results. Sats are sent/claimed distribution records, not announced prizes or independently verified settlement.
- The map uses approximate city centres for recognized event cities, with check-in-sized circles. Its coverage label identifies how many events were mapped; all selected events appear in the adjacent timeline.
- A frozen edition stores text and aggregate/event evidence, never attendee emails, and clears the CSRF token. It does not pin CSS/fonts or external project destinations; retain the exported PDF for exact visual archival.
- No new migration is required beyond `111_sponsor_reports.sql`. Annual drafts use `annual-v1/<year>/<recipient>` and immutable editions append `/frozen/<uuid>`. Draft updates use optimistic revision checks, and draft-save plus snapshot-insert are atomic.

## Local verification

Run handler tests matching `TestAnnualReport` and `TestReport`. PostgreSQL tests use temporary tables with `REPORT_TEST_DATABASE_URL` pointing to the isolated report-test database. `ANNUAL_REPORT_PREVIEW=1 go test ./internal/handlers -run '^TestAnnualReportPreview$' -v -timeout 0` starts a read-only layout preview at `http://127.0.0.1:8140/admin/reports/annual` with explicitly synthetic metrics. It does not seed or alter application data.
