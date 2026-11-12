# Sponsor report recording highlights

Open `/{event}/admin/report`, expand the editor, and find **Recording highlights · X + YouTube**. Existing program recording URLs are matched automatically. Add other event recordings (including full-day broadcasts) as one HTTPS URL per line. For a YouTube moment, append a space and the start time in seconds, for example:

```
https://www.youtube.com/watch?v=abcdefghijk 135
https://x.com/i/broadcasts/1example
```

**Save draft & refresh recording views** saves narrative edits and collects a dated snapshot. Public YouTube videos use the existing YouTube OAuth connection and Data API readonly permission. X broadcasts use the existing Studio cookie configuration and its analytics permission. Unsupported private Studio endpoints can change; refresh errors keep the previous platform snapshot and display a notice. Unstarted and locked X broadcasts, and private/unlisted YouTube videos, are excluded.

Thumbnails are downloaded without account cookies from allowlisted platform image hosts, resized, and stored with the snapshot so expiring X thumbnail links do not break archived reports. A failed thumbnail download retains an existing thumbnail where possible. No raw Studio responses, cookies, or ingest credentials are saved in report content.

Annual drafts automatically collect recordings and X posts when opened, using a six-hour refresh cooldown and retaining previous snapshots on failures. Exclude incorrect sources in the annual editor and save. Freeze an annual edition to preserve its media, counts, and text; frozen editions do not collect new data.

Counts are lifetime views of matched recordings, collected on the displayed date. They are not period-specific annual views, impressions, unique audience, or attendance. They are not added to manually supplied reach totals. YouTube Analytics watch time, traffic sources, and impressions are not fetched by this integration.

Selected moments link into YouTube at a timestamp; they do not create video files or embed playable media inside a PDF. PDFs retain clickable watch links and thumbnail images.

## Sponsor presentation

Report pages emphasize aggregate platform views (minimum 1,000) and aggregate registrations (minimum 100). Individual media cards show titles, thumbnails and watch links without per-video counters; at most four recordings are highlighted. Annual media totals deduplicate platform/video IDs across included events and use the latest dated snapshot if the same recording appears more than once. Counts are lifetime views of the matched library, not a claim to complete playlist/channel coverage or views earned during that calendar year.

Annual participation leads with total registrations and total speakers (each speaker counted once across included programs) rather than event comparisons. Smaller events still contribute fully to the annual totals. Raw recording counts and event registration/check-in figures remain in the admin editors. Thresholds affect presentation, not saved source data.

## Annual review layout

The annual document uses a cover, two event-recap pages for six completed events, and seven core sections: opening letter, community scale and map, technical program, online reach, hackathon funding and participation, winning-project imagery and amounts, and closing reflections. The prize donut sums configured sats offered across available winner slots, including unawarded prizes, for included events with available hackathon records. Older frozen editions retain their original payout/award basis. Per-project awards remain separately labeled. Winner portraits use existing project-member profile photos; absent portraits are not replaced with fabricated photographs. Project images are labeled as artwork. Longer editorial copy can extend the printed document.

## Reviewed 2026 X broadcast inventory

`internal/handlers/reportdata/x-broadcasts-2026.json` contains public IDs, reviewed event attribution, and the November 12 view-count snapshot for 17 broadcasts (12,675 views). It is embedded in production and used in local previews. No cookies or signed thumbnail URLs are stored.

These recordings enter matching 2026 event reports automatically, including full-day broadcasts absent from talk links. A newer saved counter takes precedence. The first refresh includes the inventory and records its version; unavailable broadcasts removed by a successful refresh stay removed. Failed refreshes retain the dated snapshot. Annual totals deduplicate recordings and honor recipient-specific exclusions. Additional recordings can still be added in the event editor.

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

Live annual reports count all available @btcplusplus posts published in the selected year, including event-neutral posts and upcoming-event promotion. The bundled November 12 snapshot contains 808 posts and 1,203,010 impressions; five unavailable posts are omitted. The editor lists all posts for explicit exclusion. Collected account counts replace manual annual X impression totals; a manual value is only a fallback when no snapshot exists. The headline states publication year, collection date, and lifetime basis.

Automatic collection refreshes the shared snapshot through the connected session, validates the account, fetches monthly windows, and retains the previous snapshot on failure. A window reaching 1,000 posts fails explicitly instead of silently truncating. The newer of the bundled snapshot and the saved `x-posts/YEAR` snapshot is used.

Event reports still match posts using event URLs, recording links, program titles, and location/tag mentions. Matches count automatically unless explicitly excluded; administrators can also include ambiguous posts manually. Decisions survive refreshes. Annual account-wide reach is independent of event attribution and event exclusions, while annual post exclusions remain recipient-specific. Frozen editions preserve their original snapshot and scope. Raw responses and session credentials stay outside the repository. The web GraphQL operation ID can change when X deploys its client.

## Demand for speaking slots

Event reports use the existing verified applications count and source from **Attendance & applications**. The database proposal status breakdown supports intake review, but proposal records include invitations and organizer-created sessions and cannot establish a pure CFP count. Without a verified intake count, the report labels the database figure **program proposals** and omits the application-to-session ratio. With verification, it shows **speaking applications** and, where applications exceed the program size, applications per program session. This is a demand comparison, not an acceptance rate. Sessions are deduplicated even if they have multiple recordings.

The annual demand figure sums verified application counts from included events and uses only those same events for its session denominator; it identifies how many events were reviewed. If none have verified application counts, it uses the aggregate program-proposal count. Upcoming/excluded events are not included. Local Nairobi's 84 applications are explicitly illustrative and are not seeded into production.

## Event recap pages

Completed events appear three per page after the opening letter, using their landing artwork, a 45–65 word draft recap and a recording/project/program link. Each event's text is editable in the annual Markdown editor and saved independently per recipient. Upcoming events such as Seoul have a recap in the closing section and stay outside completed-event totals. Default text is persisted when saving/freezing an edition. Page numbering adjusts to the number of included events. Long edited text may expand beyond the nominal page count.

Preview event dates and edition labels were checked against the public event pages on October 23, 2026. The 2026 Florianópolis event is `/floripa26` (not the 2025 `/floripa` event). Vienna is economics edition; Vegas is villains edition. The broadcast attribution inventory was corrected accordingly. Other preview event metrics remain illustrative.
