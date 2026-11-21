# Homepage spacing

The homepage uses a small spacing scale to keep related content close while preserving separation between sections. Values are scoped to `.home-shell` in `static/css/home-redesign.css`; the event archive keeps its existing layout.

| Relationship | Desktop (>980px) | Tablet | Mobile (≤640px) |
| --- | ---: | ---: | ---: |
| Section padding, top and bottom | 48px | 40px | 32px |
| Section padding, left and right | 48px | 24px | 20px |
| Columns or stacked layout groups | 32px | 24px | 24px |
| Section introduction to cards/gallery | 24px | 24px | 20px |
| Heading to supporting copy | 16px | 16px | 12px |
| Copy/gallery to action buttons | 24px | 24px | 16px |
| Between action buttons | 12px | 12px | 8px |
| Card content padding | 20px | 20px | 16px |

These are per-side section insets, not additional margins between sections. For adjacent padded sections, the content-to-content separation is 96px on desktop or 64px on mobile, with the section divider between them. Card grids that run to a section boundary do not add another inset.

Start each section with its heading; omit decorative eyebrow labels and section numbers.

Use `home-section-head--inset` when a heading is inside an already padded section. Avoid inline padding or adding a second top margin to a content group. Headings and their descriptions stay together before their action on narrow screens.

Buttons keep a minimum height of 48px, with 16px bold labels on the homepage. Filled buttons use the existing compact orange token for contrast with smaller white labels. Body and heading typography follow the existing styles. The map frame and its marker overlay share `--home-map-inset` so internal padding cannot displace the location markers.

The scale follows the grouping approach in [Carbon's spacing guidance](https://www.carbondesignsystem.com/building-blocks/foundations/spacing/overview), with page-specific values chosen through desktop and mobile review. Keeping controls at least 48px high also preserves room beyond the [WCAG enhanced target-size recommendation of 44px](https://www.w3.org/WAI/WCAG22/Understanding/target-size-enhanced.html). There is no universal optimal section spacing; use this scale as the homepage's baseline.
