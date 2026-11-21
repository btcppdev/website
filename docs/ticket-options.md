# Event ticket options

Configure tickets in `/{event}/admin/setup?step=tickets`.

- Standard tiers use sales deadlines and cumulative ticket limits. The current
  tier is the earliest unexpired tier with space remaining.
- Evergreen tickets have independent prices, descriptions, categories and caps.
  They remain available until the latest active standard-tier deadline, subject
  to both their own cap and the event limit (the largest active standard limit).
  Keep at least one standard tier. All non-revoked, non-sponsored registrations
  count toward the overall limit, including evergreen purchases.
- New evergreen tickets disallow discounts by default. The toggle enables both
  typed codes and checkable offers. Card surcharges follow the existing rules.
- Checkable offers have an eligibility label and a percentage from 1 to 99.
  Customers attest by checking a box; no ticket reference is collected. Offers
  are advertised under standard tickets. A purchase can use one offer or one
  discount code. Percent calculations use the existing whole-unit rounding.
- Removing an option pauses it and preserves its purchase history. A checked
  offer is revalidated before payment, including its event and current price.

Migration 169 separates existing local/student options using the current tier's
price (or the latest tier for closed events). The event's previous maximum is the
initial per-type cap, and prior local/student sales are linked to it. Review caps
in setup before introducing a smaller student allocation. Existing legacy links
resolve to the independent allocation, including its paused/sold-out state.

Checkout reserves tickets under the same event lock as setup changes. Stripe and
OpenNode metadata carry the reservation and ticket IDs. Successful fulfillment
atomically converts the hold to registrations; repeated callbacks do not consume
additional inventory. The shared commerce maintenance worker checks abandoned
reservations against their provider. Paid checkouts awaiting a callback retain
inventory; confirmed expiration releases it. Definitive creation rejection also
releases the hold. An ambiguous creation failure retains inventory and logs the
reservation ID for provider reconciliation: do not release it merely on elapsed
time, since a charge might have been created and paid. The reservation UUID is
Stripe's idempotency key and OpenNode's order ID for tracing such cases.

Validation uses a disposable PostgreSQL database with `BTCPP_POSTGRES_SMOKE=1`
and `BTCPP_EVENT_WIZARD_TEST=1`. Run the `TestEvergreen` tests in getters and
handlers. `EVERGREEN_MIGRATION_TEST_DATABASE_URL` enables an upgrade rehearsal
against an explicitly empty scratch database. `BTCPP_EVERGREEN_PREVIEW=1` enables
`TestEvergreenPreview`, a local-only demo on port 8127 with payment submission
unregistered; `/preview/login` opens the ticket setup screen. The Eurocrypt offer
in that preview is sample data, not a production configuration change.
