# Nairobi paper report — editorial review

The paper edition is fourteen A4 pages using the public-source fixture in
`nairobi-report-draft.json`. The format selector retains the slide edition.
The preview supports temporary Markdown edits; saving requires the actual
event admin report editor. Preview edits are not persisted.

## Public evidence reviewed 19 October 2026

Source: https://btcpp.dev/nairobi/hackathon

- 36 projects in the public gallery. This is not a verified count of participating teams or people.
- 11 judges in the published roster.
- Three overall winners: WestToEast, Robin and nairobi2.
- Overall awards: 2.5M, 1.5M and 1M sats, respectively. The 5M total excludes other awards and does not establish payment settlement.

Program source: https://btcpp.dev/nairobi

Panel selections are editorial highlights, not view rankings. The technical
significance paragraphs are interpretations of project descriptions, not
claims about adoption or production readiness. Portraits and project artwork
come from published profiles/entries. Location imagery is labeled as such.

## Before sending

- Approve the opening letter, technical interpretations and proposed next steps.
- Reconcile registrations versus unique check-ins; do not invent daily attendance.
- Confirm submitted teams, team members, volunteers and delivered sessions from event records.
- Reconcile paid prizes against actual payout records.
- Add dated YouTube/X exports with metric definitions and reporting windows.
- Event photography is unavailable; use existing portraits, project screenshots and labeled location artwork.
- Follow up with winning teams for subsequent progress and contribution outcomes.

The online reach page includes a dated YouTube view snapshot. Impressions, watch time and X analytics remain incomplete. Other missing charts are omitted from the paper edition. Public evidence remains
separate from live database metrics. The fixture does not seed production or
create synthetic attendees, views or payouts.

Validation: report handler tests; browser checks for loaded images, mobile
overflow and Markdown updates in both formats; A4 PDF export.

## Program and YouTube update — 19 October 2026

The complete published agenda (https://btcpp.dev/nairobi/agenda) contains 44
distinct scheduled sessions: 31 talks, one keynote, nine workshops and three
panels. The roster contains 44 distinct speaker profile links. Each session
has a recording link; this does not prove delivery or viewership.

The configured YouTube account successfully returned public video statistics:
2,811 lifetime views across 43 distinct linked videos at 18:29:39 UTC. The
per-video evidence is in `nairobi-youtube-views-2026-10-19.json`. One scheduled
session had no matched YouTube embed. This excludes X, direct Spaces playback
and unlinked videos. The YouTube Analytics endpoint returned HTTP 403 because
the Analytics API is disabled in the configured Google project; watch time and
detailed traffic reports were not retrieved.

Production attendance comes from live registration/check-in records. For local
review, opt-in `REPORT_REGISTRATION_DEMO=1` seeds 320 sample registrations and
286 unique check-ins in `btcpp_report_demo.report_attendance_preview`. Revoked
and other-event examples verify exclusions. The preview queries the same
attendance SQL as production, and explicitly labels these two figures as sample
data on screen and in print. Program and YouTube snapshots retain their actual
sources. The seed refuses non-loopback hosts and any other database name; it
resets only its dedicated preview tables. No production seed or migration runs.

Run the mixed-source local preview with:

```sh
REPORT_REGISTRATION_DEMO=1 REPORT_PREVIEW=1 REPORT_PREVIEW_ADDR=127.0.0.1:8130 \
REPORT_TEST_DATABASE_URL='postgres://btcpp@127.0.0.1:55432/btcpp_report_demo?sslmode=disable' \
go test ./internal/handlers -run '^TestReportRender$' -count=1 -timeout=0 -v
```

Speaker geography defaults to `speaker_confs.coming_from` from the event-linked
speaker forms: self-reported travel origin, not nationality or residence.
Free-text origins need coordinates before plotting. Manual report overrides
can resolve ambiguous places without changing the original form. Origins
without coordinates remain listed and can be saved. The public snapshot has
no access to these private form responses.
The globe outlines use public-domain Natural Earth 1:110m land geometry:
https://github.com/nvkelso/natural-earth-vector/blob/master/geojson/ne_110m_land.geojson
Two orthographic hemispheres, centred on 30°E and 150°W, show all locations.

## Community pages

The paper edition now includes all 44 published speakers, a three-column
program timeline (17 / 13 / 14 sessions across the three days, in Africa/Nairobi
time), and 36 public project entries across three gallery pages. Schedule
entries retain stage labels and link to recordings. Project cards link to their
full entries; short descriptions are excerpts of the builders’ text. Images
are existing profile portraits and project artwork, including default artwork
where that is what the entry provides. No participant quotes or follow-up
outcomes were invented. The production report queries submitted/advanced
projects, excluding created and hidden entries.
