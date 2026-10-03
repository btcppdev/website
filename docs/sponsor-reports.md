# Event sponsor reports

`/admin/reports` is an event chooser. `/admin/reports?event=nairobi` opens the first event draft. The global admin dashboard links here. Only global administrators can view or save reports.

The report is now an event recap: cover, event scorecard, nifty’s note, highlighted speakers and popular sessions, core panels, hackathon winning projects, judge portraits, closing note, and definitions. It prints to PDF. Migration `111_sponsor_reports.sql` stores the draft; no production deployment or migration has been performed.

## Nairobi first edition

The initial letter uses nifty’s own reflection that bringing bitcoin++ to Africa was a highlight of 2026. Her earlier annual letter, including Jake’s contribution and the economics edition, is preserved in `2026-sponsor-letter-draft.md`; those facts are not automatically attributed to Nairobi.

First-use Nairobi defaults select six speakers and its program’s panels, with all choices editable. Default selection is editorial, never a popularity ranking. The draft context identifies open source as the event theme and points readers to the program and hackathon pages.

A public-source preview lives in `nairobi-report-draft.json`. It includes six selected speakers, three panels, the three published overall winners, and eleven listed judges with their actual profile photos. Its private metrics remain blank. It is not a production database fixture and is never loaded by the application handler.

Sources retrieved 16 September 2026:

- [Nairobi program and speaker profiles](https://btcpp.dev/nairobi).
- [Hackathon results, project descriptions, and judge roster](https://btcpp.dev/nairobi/hackathon).

The public event heading says 17–19 June; structured metadata spans into early 20 June and older emails describe four days. The PDF uses the advertised program dates. The live report uses the event’s display dates. Daily attendance and attendee-days are not collected or reported.

The PDF draft is at `artifacts/bitcoin-plus-plus-nairobi-2026-sponsor-report-draft.pdf`. Its project and judge pages have been rendered and visually checked.

The report adapts the visual design of `bitcoinpp-sponsor-kit-2027-4.pdf`: landscape pages, black frames and page tabs, lavender/gold gradients, an oversized italic wordmark, Archivo headings, and Space Mono body text. Fonts are served locally with their OFL licenses in `static/fonts`, so PDF export does not depend on a font service. The 1200 × 675 CSS-pixel print canvas preserves the kit’s widescreen proportions; choose fit-to-page for physical paper. Reference-kit statistics are not used as event data.

## Metric definitions

| Item | How it works |
| --- | --- |
| Unique attendees | Checked-in registrations that were not revoked before account deletion deduplicated by linked person, email alias, or normalized email. Unlinked aliases can still overcount. |
| Daily attendance | Not collected; omitted from the report and editor. |
| Delivered talks/workshops/panels | Select sessions actually delivered and confirm the program review. Until reviewed, totals are blank. Talks include keynotes; panels and workshops are separate. Future or undated session endings cannot be confirmed. |
| Applications | Verified intake count with a source. Current proposal records include invitations and admin-created sessions, so the editor sees their count but sponsors do not receive it mislabeled as applications. |
| Volunteers | Unique people assigned to event shifts, including leaders; not verified on-site attendance. |
| Hackathon teams | Submitted/advanced project entries; one project team per entry. Not every registered hacker or unsubmitted idea. Multiple submissions from the same people may count more than once. |
| Participants | Distinct members of included projects within the event. |
| Sats distributed | Sats distribution records marked sent/claimed. Excludes pending/cancelled awards, non-sats prizes, and announced pool totals. Reconcile historical, pooled, or external payments separately. |
| Winners | Awarded, unarchived prizes assigned to submitted/advanced projects, only after results are finalized. Multiple awards do not multiply project cards or payouts. |
| Popular sessions | Top five among imported session analytics, sorted using one comparable metric (YouTube views or X video views), reporting period, export date, and source. Does not establish in-room popularity. |
| Speaker/judge portraits | Existing profile photographs. Missing photos are marked rather than synthesized. Judge roster combines ranked judges and sponsor-award judges without duplicate people. |

## Editing and export

Drafts store narrative, speaker/panel choices, delivery confirmations, judge notes, application count, and per-session view counts. Save before printing. Counts require non-negative integers and imports require provenance. Draft revisions prevent stale saves overwriting another administrator’s work.

The editor includes a source checklist for each pending chart and metric. It is hidden in print. Public previews cannot load private event counts; use the authenticated live report for database metrics. Nairobi’s default closing suggests follow-up priorities, without presenting them as measured outcomes. Existing saved narratives take precedence over updated defaults.

The event draft key uses `event-v2/<conference ID>`; earlier annual drafts are retained under their original keys. Database metrics and content load in a read-only repeatable-read transaction. The report never includes contact details, tax records, or payment addresses.

Print / Save as PDF freezes the displayed edition. Enable background graphics and turn off browser-added headers/footers. The live view refreshes from current records; this is not a server-side immutable snapshot or an automatic analytics integration.

## Verification

`nix develop --command go test ./internal/handlers ./internal/db -count=1`

Set `REPORT_TEST_DATABASE_URL` to an isolated PostgreSQL instance for the report SQL and persistence checks. Tests use session-local tables and cover cross-event isolation, check-in deduplication, project status, volunteer deduplication, finalized awards, duplicate awards, payout states, stale saves, local dates, count provenance, and output escaping.

`REPORT_PREVIEW=1 go test ./internal/handlers -run TestReportRender -count=1 -timeout=0 -v` serves the public-source Nairobi draft on port 8102.

## Charts

Dedicated print pages show delivered talks/keynotes, workshops and panels; the top five sessions by one sourced view metric; and recorded sats distributed by finalized winning project. Bars start at zero, values are printed beside them, and prize charts paginate after five projects with a shared scale. Public-source previews show explicit pending-data panels rather than fabricated values. Daily attendance and attendee-days are omitted because they are not collected.

### Reference design verification

Checked against the supplied PDF’s embedded fonts, drawing colors, and rendered pages: Archivo display type (including the real italic face), Space Mono regular/bold/italic, paper #ececec, black #000000, lavender #c8b3f5, gold #f7b500, pink #f6b9dc. Display headings use expanded Archivo; CSS gradients reproduce the reference’s color treatment rather than embedding flattened page images. All font files are served locally under the existing OFL licenses.

## Paper and slide editions

The default format is an A4 portrait paper report. The Format selector switches to the original landscape slide layout without changing saved content. `?event=nairobi&layout=slides` links directly to slides; `layout=paper` selects paper. Print / Save as PDF uses the selected format. Paper uses Archivo body text for sustained reading, Space Mono labels, the existing palette, a full-page cover, and stacked panel/project profiles.

Exports: `artifacts/bitcoin-plus-plus-nairobi-2026-sponsor-report-paper.pdf` (paper) and `artifacts/bitcoin-plus-plus-nairobi-2026-sponsor-report-draft.pdf` (slides). Both remain drafts with the same pending data.

## Database-backed synthetic demo

The local demo is explicitly fictional and never replaces the public-source draft or production data. It uses the existing report SQL against a persistent, minimal report-query schema in the dedicated local `btcpp_report_demo` database (`report_demo` schema). It is not a full application database. Each start resets only that demo schema. The harness refuses non-loopback hosts and any other database name.

```sh
createdb -h 127.0.0.1 -p 55432 -U btcpp btcpp_report_demo # once
REPORT_DEMO=1 REPORT_PREVIEW=1 \
REPORT_TEST_DATABASE_URL='postgres://btcpp@127.0.0.1:55432/btcpp_report_demo?sslmode=disable' \
go test ./internal/handlers -run '^TestReportDemo$' -count=1 -timeout=0 -v
```

Open http://127.0.0.1:8106. Both paper and slides carry a synthetic-data banner in print. The preview reloads counts from PostgreSQL on each request; it is read-only and its editor is hidden. Change the fixture in `internal/handlers/admin_report_demo_test.go` and restart to reseed. Omit `REPORT_PREVIEW=1` to seed and validate without serving.

Example totals: 320 registrations, 286 unique check-ins, 24 talks, eight workshops, three panels, 18 volunteers, 84 applications, 32 project teams, 96 project members, six challenges, three winning projects and 5,000,000 sats. Sample per-session views and account-level YouTube/X figures are stored in the draft JSON; they are invented, not analytics imports. Public speaker/judge portraits are retained for visual testing only; invented sessions are marked Demo.

Demo exports are `artifacts/nairobi-synthetic-demo-paper.pdf` and `artifacts/nairobi-synthetic-demo-slides.pdf`. Never use their metrics as actual Nairobi outcomes.

### Populated review edition (22 September 2026)

The synthetic demo now renders five charts: program delivery, comparable recording views, sats paid by winning project, valid registrations versus unique check-ins, and submitted versus winning project teams. Attendance compares records with deduplicated people, not a conversion rate; winners are a subset of project teams. Local SQL supplies 320 registrations, 286 check-ins, 32 teams, 96 members, six challenges, 18 volunteers and 5 million sent/claimed sats. Session delivery is editor-confirmed; application and social/view counts remain explicitly synthetic editorial inputs, not automatic platform imports.

For populated layout review, open `artifacts/nairobi-synthetic-demo-paper.pdf` or `http://127.0.0.1:8106/?layout=paper`. The public-source paper draft intentionally retains pending private metrics.

The visual edition adds oversized takeaway figures and a sixth chart: a team-size histogram from distinct project members (0, 1, 2, 3, 4, 5+). Demo membership varies across 32 teams while preserving 96 distinct members. Paper charts start on fresh pages; slide charts retain the landscape layout.

Brand typography: every visible `bitcoin++` in the report body uses the local Ubuntu Bold Italic face. The report script wraps matching text nodes, including saved narrative, without rewriting HTML or form values. The second plus overlaps by 0.23em to join the inner arms. This overrides the reference kit's Archivo wordmark; other headings retain Archivo. PDF export must load the report script and fonts before printing.

Paper export uses zero PDF page margins with internal content padding, so the paper color reaches every edge. Chart pages include subtle lavender/gold gradients; text stays inset 16mm. Physical edge-to-edge output requires borderless printing or trimming oversized stock.

Program mix and winning-project share now use SVG donut charts, with center totals and a count/percentage legend. Winning and other submitted projects partition the team total. Attendance retains bars because registration records and distinct check-ins are different units. Zero/invalid totals do not create a donut.

Event imagery: the report reuses `confImagePath(tag, "leading")` and `confVenueImages(tag)`, matching the public event page. The lead artwork has a dedicated full-page spread followed by a four-image setting gallery. Nairobi's venue-section files depict city/landscape scenes, so the report does not describe them as conference-action photos. The public-source fixture supplies the same asset URLs for test previews whose working directory differs from the application root.

## Event-admin Markdown editor

Global administrators can open **Sponsor report** from `/{event}/admin` or event details; Nairobi's URL is `/nairobi/admin/report`. This uses the same per-event saved draft as `/admin/reports?event=nairobi`. Opening and closing messages and the other narrative sections now accept Markdown. Each section has a formatting toolbar, a side-by-side live preview, and updates the corresponding section in the report below. Click **Save event draft** to persist edits; leaving with unsaved edits prompts a warning. Counts and source fields still use the existing form and revision protection.

The authenticated, CSRF-protected `/admin/reports/preview` endpoint renders Markdown with the same HTML allowlist used for saved reports; raw HTML and unsafe link protocols are not enabled. Previews are debounced and stale responses cannot overwrite newer text. Printing is blocked while a preview is pending or failed.

The local synthetic demo exposes the same editor and renderer on loopback for previewing only; its Save buttons are disabled. It never writes editorial changes to the real application database.
