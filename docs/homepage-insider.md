# Homepage Insider section

The section keeps the existing `insiderVisible` feature gate and links to
`https://insider.btcpp.dev/`. Its visual reference is `https://beta.btcpp.dev/`,
reviewed November 21, 2026: dark newsroom background, white Insider lockup,
Montserrat headlines, newspaper body type, and orange links.
It deliberately uses Insider's identity within the main site's light homepage.
The logo sits at the bottom left, below the copy and episode preview.

The original logo and Montserrat Latin font are copied from the Insider repo's
`internal/insider/web/static/`. The logo keeps the same CSS crop as the beta
masthead. The font's OFL license is in `static/fonts/OFL-Montserrat.txt`.

## News Hour preview

- Account: https://x.com/btcinsider__
- Episode post: https://x.com/btcinsider__/status/2123839062238810148
- Full replay: https://x.com/i/broadcasts/2WXPjaJrnEyAYK
- Episode date: November 20, 2026.
- Guest/topic: @jungly; P2Pool v2 and testnet5.
- Source excerpt: 08:00–08:24 of the replay (after the opening countdown).
- Local video: `static/video/insider-news-hour-20261120.mp4`.
- Local poster: `static/img/home/insider-news-hour-20261120.jpg`.

This is a silent visual preview, not an audio excerpt. The adjacent full-episode
link opens the original recording with sound. It does not autoplay: the MP4
uses `preload="none"` and starts only after clicking Play preview. It pauses
when scrolled out of view or when the tab is hidden. Native controls remain
available if JavaScript is disabled. No X embed or tracking script is loaded.

The MP4 is H.264, 960×540, 24 fps, CRF 27 with faststart. The poster is a frame
16 seconds into the excerpt. Replace the clip, poster, date, caption and replay
link together when curating another episode; this is not an automatic feed.
