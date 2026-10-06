# Server-rendered conference translations

The English and Korean landing pages use the same Go HTML templates and live
conference data. Only Seoul currently opts into Korean: `/seoul` and `/ko/seoul`.
Ticket checkout, add-ons, payment instructions, and the confirmation page also
support Korean. Agenda, hackathon, account, and newsletter flows remain English;
the Korean landing page explains this. Speaker names and submitted talk titles remain
in their original language. Korean copy is a draft for native-speaker review.

## Shared UI copy

Edit `internal/i18n/messages/en.json` and `ko.json`. Templates reference stable
message IDs, not language conditionals:

```gotemplate
{{ msg "tickets.title" }}
{{ msg "tickets.remaining" (dict "Count" $p.TixLeft) }}
```

A message can
reorder named placeholders: `"티켓이 {{.Count}}장 남았습니다."`. Values are resolved
server-side and escaped by Go's HTML templates. Do not pass interpolated messages
through `ishtml`. The existing hackathon headline supports trusted, checked-in
markup; keep user-controlled values out of that message.

A missing translation falls back to English. Unknown IDs and missing required
placeholder values return errors. Catalogs are embedded in the binary, parsed at
startup, and require a rebuild/restart to update. Tests check placeholder parity.
Separate template caches bind each locale's helper functions once, so concurrent
requests never replace another request's language.

## Event editorial content and availability

`internal/i18n/events/seoul.ko.json` enables the locale, names the language switch,
and translates database-sourced editorial content. Keys in `content` are exact
English source strings, values are translated copy. This protects edited English
copy from being silently replaced by an outdated translation: changed or missing
source strings fall back to the current English text. Update this file when event
copy changes. Proper names, URLs, prices, inventory, and identifiers are preserved.
This is a file-based workflow, not an admin translation editor.

For another event/language, add its UI message catalog and an event catalog with
`event`, `locale`, `language`, and `content`. Routes and language links are generated
from these catalogs; no event-specific boolean or cloned template is required.
Only landing pages are registered, so translated prefixes never collide with
`/{conf}/agenda` or `/{conf}/hackathon`.

Each page emits its language, canonical URL, and alternate-language links.
Localization uses request-owned copies of event content and does not write to the
database or mutate cached records.

## Validation

Run `go test ./internal/i18n ./internal/handlers -run
'Test(Catalog|EventCatalog|LocalizedConferenceRendering)'` and review both desktop
and mobile pages. `BTCPP_TRANSLATION_PREVIEW=1` writes rendered fixture HTML to
`/tmp/btcpp-i18n-en.html` and `/tmp/btcpp-i18n-ko.html` during the handler test.

## Ticket checkout

Links from the Korean event page carry `?lang=ko` into checkout. The same parameter
is preserved in form actions, discount requests, tax requests, validation redirects,
and payment return URLs. Only languages enabled for that ticket's event are honored;
unsupported values fall back to English. This is per checkout, with no global
language cookie that could interfere with another tab.

`checkout.*` and `confirmation.*` messages cover the form, instructions, totals,
errors, and confirmation. The server injects the small subset of messages needed
for changing quantities and payment methods as escaped JSON; the shared JavaScript
fills only live count/price placeholders. Prices, currency, discounts, HMACs, and
payment calculations are unchanged.

Stripe receives the selected locale and returns to the localized confirmation.
OpenNode receives the localized return URL; its hosted payment interface remains
provider-controlled and may appear in English. Korean instructions explain the
handoff before payment. Ticket emails remain in English. Third-party receipt
emails remain provider-controlled. Merch titles/descriptions retain their catalog copy.

Run checkout checks with `go test ./internal/i18n ./internal/handlers
./external/getters -run 'Test(Catalog|EventCatalog|Localized|Checkout|TicketCheckout|InitOpenNodeCheckout|ValidateCheckoutDiscountPrice)'`.
The provider tests use mocked HTTP transports and do not create real payments.
