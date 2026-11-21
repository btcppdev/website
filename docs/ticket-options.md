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
- Special Offers have an editable label, optional restrictions (up to 500 characters),
  and a percentage from 1 to 99. Restrictions appear beneath the bold label at checkout.
  Customers attest by checking a box; no ticket reference is collected. Offers
  are advertised under standard tickets. A purchase can use one offer or one
  discount code. Percent calculations use the existing whole-unit rounding.
- Removing an option pauses it and preserves its purchase history. A checked
  offer is revalidated before payment, including its event and current price.

When an event has no available add-ons, checkout goes directly from registration
to payment. The progress indicator (including confirmation) has three steps;
events with available products retain the add-ons step.

Migration 171 adds optional offer restrictions without changing existing offer text.

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


Checkout lets buyers combine the current standard tier and available capped ticket options in one order (1–8 tickets total). The event-page selection starts at quantity one; the other rows start at zero. Discounts apply only to the options that permit them, and card surcharges are computed per option. Newsletter signup is a separate, initially checked, free option inside Special Offers.

Migration 172 adds an immutable ticket-line snapshot to each cart reservation. All options are reserved under the event's existing inventory lock, including the combined event cap. Payment fulfillment checks the provider subtotal and currency against that snapshot and records each registration's own ticket ID, ticket kind, amount, and discount. Existing single-option reservations remain supported. Provider-confirmed cancellation releases the entire cart.

Signed-in buyers see their account's primary email as the delivery address instead of an email input. Checkout resolves the session again on submission and uses that address for reservations, payments, and ticket delivery. Guests and accounts without an email still enter one. Personalized checkout responses are private and uncached.

Migration 173 adds shirt size, attendee name, food preference, other food needs,
and a revision to each registration. After verified fulfillment, the purchasing
browser opens a compact attendee table; a delayed payment callback is polled for
up to two minutes, with a manual retry and ticket-email link available afterward.
One Save details action updates the whole displayed order atomically. Partial
answers can be saved; an unknown food preference is distinct from No preference.
This ticket is for me reuses the buyer's known name and shirt size on one row and
saves changes to their profile size. Other guests never inherit that profile.

The dashboard and ticket emails link back to this form. Email links are private
capabilities scoped to the current owner's order; ticket QR codes and public
ticket references do not grant editing access. A completed reservation in the
original browser session authorizes the post-payment redirect. CSRF tokens and
row revisions protect saves. Ownership changes and revocation clear the prior
answers and rotate access keys; the original checkout session cannot regain
access to transferred tickets. This does not add a self-service transfer UI.

Check-in reads each ticket's shirt size and keeps dietary details private. The registrations admin
page totals shirt sizes and meal preferences, separately counts missing answers,
and excludes revoked tickets. Legacy single tickets retain their profile-size
fallback. Run TestTicketAttendeeDetailsLifecycle with BTCPP_EVENT_WIZARD_TEST=1
against a migrated disposable database. The local preview's /preview/attendees
route opens three sample tickets without taking a payment or sending emails.

Migration 174 adds optional conference-booklet acknowledgements to Supporter
registrations. Each supporter can stay private or provide the exact name/username to print.
An unanswered choice remains unpublished. Only an explicit List me choice is publishable. An optional
profile-avatar choice is available for the ticket marked This ticket is for me;
group guests never inherit the buyer's avatar. The existing profile editor opens
in a new tab for photo and username updates. Booklet text stays specific to the
ticket and does not change the account's display name. Staff can read the saved
choice, print name, and opted-in current profile avatar in registrations admin.
Ownership changes and revocation clear acknowledgements along with other attendee
details. The local preview's first sample ticket is a Supporter ticket.

Migration 175 adds private dietary defaults to each person. Profile editing can
set or clear shirt size and food preference. These
fields are loaded separately from the public Speaker representation. A signed-in
buyer opening a new, untouched order gets their defaults on exactly one ticket;
other guests stay blank. Existing answers and transferred tickets are not replaced
by defaults. Selecting This ticket is for me also fills those defaults, and saving
that ticket remembers its completed preferences on the owner's profile.

Dashboard tickets display their individual saved shirt and food selections with
an edit link. Check-in only loads and displays the ticket's shirt size; dietary
details remain in the private dashboard, attendee form, and catering admin view.

Shirt pickers offer S, M, L, XL, and XXL, retaining compatible storage codes.
Legacy gendered selections display their matching short size. An existing XXXL
selection is preserved through a hidden option rather than silently resized.
Planning totals combine legacy cuts under the same size label.

Check-in highlights supporters and shows saved proof requirements: local ID,
student ID, and the restriction text from an applied Special Offer. Requirements
are snapshotted when the registration is issued; editing an offer later does not
rewrite an existing ticket's requirements. Previously checked-in tickets retain
their admission. New tickets requiring proof wait for a staff confirmation or a
verified full-ticket upgrade, including in bulk check-in and pickup actions.

Upgrade quotes use the current standard Bitcoin ticket price minus the recorded
amount paid, with no additional card surcharge. Once public sales have ended,
the highest enabled standard price is used. A missing original payment or a
currency mismatch requires admin review rather than an invented price. A zero
balance needs a staff click but no payment. Original ticket revenue and allocation
remain intact; `ticket_checkin_upgrades` records the additional payment and the
operator separately. Local/student upgrades become general admission.

The upgrade dialog defaults to Lightning and can switch to an on-chain QR from
the same OpenNode charge. Cancel closes the dialog; it does not void a provider
charge or grant admission. Reopening reuses the outstanding invoice. Signed
callbacks, authenticated provider lookups, and background reconciliation grant
admission only after payment is confirmed. Pending, underpaid, expired, or
unverifiable payments do not grant access. Transfers/revocations and late payments
are checked again before admission. Ambiguous charge creation stays pending for
admin reconciliation rather than creating a second invoice.

Provider references: [creating a charge](https://developers.opennode.com/docs/creating-a-charge)
and [charge lifecycle](https://developers.opennode.com/docs/charge-lifecycle).
Local review: `/preview/check-in/student`, `/preview/check-in/local`,
`/preview/check-in/offer`, and `/preview/check-in/supporter` in the opt-in test
preview use mock invoices only.
