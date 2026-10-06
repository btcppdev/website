# Technical SEO review — October 6, 2026

Reviewed production HTML for the homepage, events index, Seoul landing page, and
Berlin's DropShop hackathon project, plus the live sitemap and robots.txt. Reviewed
the server-side routing, metadata, structured data, and indexing rules locally.
This is a technical review, not a search-performance or Core Web Vitals measurement.
No Search Console data was available and no ranking improvement is guaranteed.

## Findings and implemented changes

| Area | Observed issue | Change |
| --- | --- | --- |
| Discovery | The live sitemap contained 50 URLs, primarily event landing pages and agendas. | Add translated event landing pages, reciprocal English/Korean/x-default alternatives, speaker pages with accepted speakers, public hackathon landing pages/projects, shop and public directory indexes. |
| Sitemap correctness | URLs were interpolated directly into XML. | Encode XML fields and attributes. Buffer the complete response; database failures return an error instead of a cached partial sitemap. |
| Public projects | DropShop's live page had a title, but no meta description or canonical URL. | Add project-specific descriptions, canonical URLs, Open Graph, and Twitter metadata; project editors remain noindex. |
| Event dates | Seoul advertised midnight start and end timestamps although the stored values represent dates. | Use date-only schema values when a time was not specified. Keep explicitly configured times and timezone offsets. Omit unknown dates. |
| Ticket discovery | Event offers linked directly to `/tix/.../checkout`, which robots.txt blocks. | Link offers to the crawlable event ticket section, preserving the page language. |
| Korean consistency | Event schema still referenced the English landing URL. | Use the localized canonical event URL in schema, matching page metadata and sitemap language alternatives. |
| Venue detail | Event schema used only the general location string. | Emit PostalAddress using configured venue pickup-address fields when street and country are available. Do not invent missing addresses. |
| Missing copy | Events without OGFlavor emitted empty descriptions. | Provide an event-specific metadata description fallback. |
| Canonical consistency | Legacy talks template advertised a `/talks` canonical that redirects. | Point it at `/agenda`. |
| Operational pages | Some cart, payment, signer, judging, and project-edit routes lacked noindex headers. | Extend the centralized noindex rules while keeping public project, agenda, shop, and translated pages indexable. |

The homepage and events index already had distinct titles, descriptions, one H1,
and self-referencing canonical URLs. Existing 404 handling returns an actual 404
and noindex. These did not need changes. Existing robots exclusions for ticket and
account workflows remain intact.

## Visibility and cost

Project sitemap entries require a published conference, public competition, open
public gallery, and a project status other than created/hidden. The public project
lookup uses one database query instead of running visibility queries per project.
No participant email addresses or private project content enter the sitemap.

No artificial `lastmod` dates were added: project edits alone do not capture every
change to rendered pages, and event start dates are not content modification dates.
Existing priority/changefreq hints remain, but Google ignores them; they do not
control Google crawl frequency or rankings.

## Verification

- Template parsing and focused SEO/localized rendering tests passed.
- Sitemap XML tests check proper escaping and reciprocal language alternatives.
- JSON-LD tests parse the rendered JSON and check dates, real ticket price,
  structured address fields, and English/Korean event/offer URLs.
- Project metadata and operational/public indexing route tests passed.
- Local PostgreSQL integration test passed for public projects and exclusions:
  draft/hidden projects, closed galleries, hidden competitions, and draft events.
- Production was inspected read-only. Changes have not been deployed.

## After deployment

1. Fetch `/sitemap.xml`; confirm the new canonical URLs return 200 and reciprocal
   language annotations agree with `/seoul` and `/ko/seoul`.
2. Validate Seoul and another event with Google's Rich Results Test. Check actual
   venue address data in event settings, especially street address and country.
3. Submit the sitemap in Search Console and inspect the Korean landing page and
   a public project. Review indexing exclusions before requesting individual recrawls.
4. Track impressions, clicks, and indexed pages against a pre-deployment baseline.
   Measure Core Web Vitals before choosing performance changes.
5. A separate content-discovery pass could cover recording watch pages and video
   structured data, public project internal linking, and reviewed Korean editorial
   copy. These were not changed in this pass.

## References

- [Google: localized versions and sitemap language annotations](https://developers.google.com/search/docs/specialty/international/localized-versions)
- [Google: event structured data, dates, addresses, and ticket offers](https://developers.google.com/search/docs/appearance/structured-data/event)
- [Google: canonical URLs](https://developers.google.com/search/docs/crawling-indexing/consolidate-duplicate-urls)
