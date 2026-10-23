# Sponsor report recording highlights

Open `/{event}/admin/report`, expand the editor, and find **Recording highlights · X + YouTube**. Existing program recording URLs are matched automatically. Add other event recordings (including full-day broadcasts) as one HTTPS URL per line. For a YouTube moment, append a space and the start time in seconds, for example:

```
https://www.youtube.com/watch?v=abcdefghijk 135
https://x.com/i/broadcasts/1example
```

**Save draft & refresh recording views** saves narrative edits and collects a dated snapshot. Public YouTube videos use the existing YouTube OAuth connection and Data API readonly permission. X broadcasts use the existing Studio cookie configuration and its analytics permission. Unsupported private Studio endpoints can change; refresh errors keep the previous platform snapshot and display a notice. Unstarted and locked X broadcasts, and private/unlisted YouTube videos, are excluded.

Thumbnails are downloaded without account cookies from allowlisted platform image hosts, resized, and stored with the snapshot so expiring X thumbnail links do not break archived reports. A failed thumbnail download retains an existing thumbnail where possible. No raw Studio responses, cookies, or ingest credentials are saved in report content.

Annual drafts automatically include media snapshots from selected events. Refresh in each event editor, then reload the annual draft. Freeze an annual edition to preserve its media, counts, and text. No network requests are made when viewing or printing reports.

Counts are lifetime views of matched recordings, collected on the displayed date. They are not period-specific annual views, impressions, unique audience, or attendance. They are not added to manually supplied reach totals. YouTube Analytics watch time, traffic sources, and impressions are not fetched by this integration.

Selected moments link into YouTube at a timestamp; they do not create video files or embed playable media inside a PDF. PDFs retain clickable watch links and thumbnail images.

## Sponsor presentation

Report pages emphasize aggregate platform views (minimum 1,000) and aggregate registrations (minimum 100). Individual media cards show titles, thumbnails and watch links without per-video counters; at most four recordings are highlighted. Annual media totals deduplicate platform/video IDs across included events and use the latest dated snapshot if the same recording appears more than once. Counts are lifetime views of the matched library, not a claim to complete playlist/channel coverage or views earned during that calendar year.

Annual participation leads with total registrations and unique registered people rather than event comparisons. Smaller events still contribute fully to the annual totals. Raw recording counts and event registration/check-in figures remain in the admin editors. Thresholds affect presentation, not saved source data.

## Annual review layout

The annual document uses a cover, two event-recap pages for six completed events, and seven core sections: opening letter, community scale and map, technical program, online reach, hackathon funding and participation, winning-project imagery and amounts, and closing reflections. The payout donut sums only recorded sent/claimed distributions from included events with reviewed database coverage. Per-project awards remain separately labeled. Winner portraits use existing project-member profile photos; absent portraits are not replaced with fabricated photographs. Project images are labeled as artwork. Longer editorial copy can extend the printed document.

## Reviewed 2026 X broadcast inventory

`2026-report-broadcasts.json` contains public IDs, event attribution, and the dated view-count snapshot for 17 broadcasts (12,306 views). No cookies or signed thumbnail URLs are stored. The local preview matches its Studio export against this inventory. Other local event statistics remain illustrative.

For production, add any missing URLs below to each event report’s **Additional event recordings** field, then use **Save draft & refresh recording views**. The annual report rolls up the selected event snapshots and deduplicates recordings. This also captures full-day broadcasts that are not attached to a single talk.

### toronto

```text
https://x.com/i/broadcasts/1OxwbbMVymvJB
https://x.com/i/broadcasts/1nGeLLAkVYLKX
https://x.com/i/broadcasts/1oKMvvyRQXnGQ
https://x.com/i/broadcasts/1jGXggNOqgEKZ
```

### floripa26

```text
https://x.com/i/broadcasts/1XGygmwjEQVxM
```

### vegas

```text
https://x.com/i/broadcasts/1pKdRbrbaQrJW
```

### berlin26

```text
https://x.com/i/broadcasts/1nxnRBMEoZNxO
https://x.com/i/broadcasts/1AKEmvLVqgvKL
https://x.com/i/broadcasts/1pJdRDwrEvvKW
https://x.com/i/broadcasts/1PKqrNbePkAGb
https://x.com/i/broadcasts/1qKVmyqEQvlxB
https://x.com/i/broadcasts/1mxPaZMXjNyKN
https://x.com/i/broadcasts/1lJQRWDVnyqxE
```

### vienna

```text
https://x.com/i/broadcasts/1NGarrEzrmEJj
```

### nairobi

```text
https://x.com/i/broadcasts/1nGnRRgBNOjGO
https://x.com/i/broadcasts/1qJVmyrwXgaGB
https://x.com/i/broadcasts/1AGRnZVowNyGl
```

## X post impressions

The existing Studio session also authenticates to `x.com/i/account_analytics`. Confirmed on 23 October 2026: the account analytics page uses a read-only `contentPageQuery` GraphQL operation with monthly `from_time`/`to_time` windows and organic post metrics. Match the site's UTC timestamp format (`YYYY-MM-DDTHH:MM:SS.000Z`); skip unavailable post records and deduplicate by post ID. No private API key or new login was required.

The sanitized summary is in `2026-report-x-post-impressions.json`: 1,160,550 organic impressions across 774 available posts, covering January through collection on October 23. This is lifetime engagement on posts published in that window, not a time-series sum of impressions earned during the window. One unavailable May post is omitted. Includes account-wide material and upcoming-event promotion; do not claim all impressions belong to completed events. Broadcast views remain separate.

The local preview populates the annual report's existing X impressions fields from this snapshot. Production can use those same fields with the summary's source, period, date, and coverage. Event admins can now use **X post attribution → Save draft & refresh X posts** to update a shared annual snapshot through the connected session. Refresh checks the account is `btcplusplus`, discovers X's current query, fetches monthly windows, and retains the previous snapshot on failure. A window reaching 1,000 posts fails explicitly instead of silently truncating.

**Review and assign posts** suggests matches using event URLs, matched broadcasts/YouTube videos, program titles, and location/tag mentions. Suggestions are not counted until explicitly included. **Include suggested matches & save** approves pending suggestions without overriding explicit exclusions. Administrators can search, filter, and include otherwise ambiguous posts manually. Decisions are event-specific and survive refreshes; unavailable posts retain their decision but do not contribute to a new snapshot.

Event report headlines use approved post impressions instead of manual X impressions, and feature the top three posts. Annual event-based totals deduplicate IDs using the latest snapshot; an explicit account-wide annual total takes precedence. Frozen annual editions retain their saved post data. The initial public October 23 snapshot is bundled for immediate review; subsequent snapshots live in `sponsor_report_drafts` under `x-posts/YEAR`. The local visual preview preselects Nairobi suggestions and labels them as requiring review. Raw responses and session credentials stay outside the repository. The web GraphQL operation ID can change when X deploys its client. CSV exports remain an alternative.

## Demand for speaking slots

Event reports use the existing verified applications count and source from **Attendance & applications**. The database proposal status breakdown supports intake review, but proposal records include invitations and organizer-created sessions and cannot establish a pure CFP count. Without a verified intake count, the report labels the database figure **program proposals** and omits the application-to-session ratio. With verification, it shows **speaking applications** and, where applications exceed the program size, applications per program session. This is a demand comparison, not an acceptance rate. Sessions are deduplicated even if they have multiple recordings.

The annual demand figure sums verified application counts from included events and uses only those same events for its session denominator; it identifies how many events were reviewed. If none have verified application counts, it uses the aggregate program-proposal count. Upcoming/excluded events are not included. Local Nairobi's 84 applications are explicitly illustrative and are not seeded into production.

## Event recap pages

Completed events appear three per page after the opening letter, using their landing artwork, a 45–65 word draft recap and a recording/project/program link. Each event's text is editable in the annual Markdown editor and saved independently per recipient. Upcoming events such as Seoul have a recap in the closing section and stay outside completed-event totals. Default text is persisted when saving/freezing an edition. Page numbering adjusts to the number of included events. Long edited text may expand beyond the nominal page count.

Preview event dates and edition labels were checked against the public event pages on October 23, 2026. The 2026 Florianópolis event is `/floripa26` (not the 2025 `/floripa` event). Vienna is economics edition; Vegas is villains edition. The broadcast attribution inventory was corrected accordingly. Other preview event metrics remain illustrative.
