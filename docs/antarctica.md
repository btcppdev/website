# Antarctica expedition draft

The expedition dates are January 24–February 5, 2028.

The expedition uses a separate visual template inside the existing application.
Account and speaker links use the existing site. No external frontend or duplicate
identity system is needed.

## Preview

- Global admins: `/admin/antarctica/preview`.
- Local standalone preview: `ANTARCTICA_DEMO=1 go test ./internal/handlers -run '^TestAntarcticaDemo$' -count=1 -timeout=0 -v`, then open `http://127.0.0.1:8110`.
- The standalone preview reads no database and collects no information or payments.
  Account and sponsor links need the full application to function.
- A published conference tagged `antarctica` or `antarctica27` will use this template,
  its existing date description, and accepted/scheduled speaker profiles. Unpublished
  conferences retain the usual 404 behavior. No conference was created or published
  by this change. The draft includes noindex metadata.

## Confirmed pricing and payment schedule

All prices are USD per person. Discounts are relative to the shared berth.

| Berth | Total | Each 20% payment |
| --- | ---: | ---: |
| Shared | $16,429 | $3,285.80 |
| Three-person bunk | $15,599 | $3,119.80 |
| Four-person bunk | $14,429 | $2,885.80 |

The confirmed standard fares above remain unchanged. Payment in full at signup
receives 5% off only when paid with bitcoin: shared $15,607.55 (save $821.45),
three-person $14,819.05 (save $779.95), and four-person $13,707.55 (save $721.45).
Full payments through Stripe use the standard fare, with no discount or added
surcharge. The organizer reports a 3% Stripe processing cost. The discount does
not apply to installments.
Discounted totals are rounded to cents on the server and displayed consistently
when switching berths. These are prototype displays; future checkout must enforce
eligibility using the actual payment method and plan on the server.

The payment plan is five equal payments: 20% on reservation, then January 1,
April 1, July 1, and October 1, 2027.

On October 22, 2026, the organizer canceled the held-bitcoin plan entirely.
Only installments and full payment remain. There is no custody, appreciation
calculator, future settlement, or December 2026 bitcoin contribution deadline.

Prices are currently draft constants in `internal/handlers/antarctica.go`, calculated
in integer cents. They are not ticket records and do not enable checkout.

## Booking work still required

Actual booking needs persisted berth inventory and reservations, installment
obligations, payment reconciliation, reminders, and an explicit
forfeiture workflow. The existing single-payment checkout does not implement this
plan. This draft intentionally creates no payment invoices.

Confirm departure port, route, fare inclusions, final parka artwork,
patch dimensions and prices. The parka deck shows a concept illustration with
ten proposed placements; it is not live sponsor inventory. Sponsor inquiries
currently link to the existing `/sponsor` page.

## Checks

`go test ./internal/handlers -run '^TestAntarctica(Fares|Template)$' -count=1`

Browser checks cover responsive layout, local font/image loading, fare selection,
installment amount updates, and sponsorship rates, deck preview, and download.

## Parka sponsorship pricing proposal — September 30, 2026

Organizer estimates approximately 30 attendees/parkas, with the garments featured
in expedition photos. Route, garment design, and whether attendees
keep the parkas remain unconfirmed. These are proposed asking prices, not approved
offers, guaranteed audience valuations, or live inventory.

| Placement across all expedition parkas | Proposed USD price | Slots |
| --- | ---: | ---: |
| Upper arm | $4,000 | 2 |
| Middle arm | $3,500 | 2 |
| Lower arm | $3,000 | 2 |
| Left chest | $7,000 | 1 |
| Right front | $5,000 | 2 |
| Large back | $21,000 | 1 |

The existing PowerPoint deck is the source for these updated proposed rates:
10 placements, showing only the price per placement. Public decks and the website
must not include revenue columns, fundraising totals, or aggregate package proceeds. The prototype now displays the deck's front/back placement diagram and
these rates, with an inline image preview and a PowerPoint download at
`/static/downloads/antarctica/parka-sponsorship.pptx`. This supersedes the earlier
four-placement proposal. Rates and placements remain proposed, not live inventory.

Proposed package benefits: placement on approximately 30 parkas, linked website
acknowledgment, inclusion in a sponsor announcement and expedition recap, and an
agreed selection of high-resolution photos licensed for sponsor promotional use.
Confirm photographer/participant permissions and achievable deliverables before
contracting. Include planned front/back group photographs, since individual photos
will not show every placement and backpacks can hide back panels. No guaranteed
impression count. Expedition tickets are separate.

Pricing judgment: the case for these amounts depends on compelling photography,
bitcoin++ audience distribution, and a small number of sponsors. Test demand before
selling out the garment. Confirm production cost and photo budget before quoting.

Directional references, not directly comparable audience valuations:
- [LOGNET 2026 merchandise sponsorships](https://conference.lognetglobal.com/lognet2026/info/details_MerchandiseSponsor.php): $4,700 backpack package, $4,800 lanyard package, with other benefits.
- [CVPR 2024 conference shirts](https://envision.freeman.com/show/cvpr-2024/conference-and-event-sponsorships/promotional-opportunities/conference-t-shirts): $10,000 exclusive sleeve-logo package on attendee shirts. Different conference scale and audience.

## Basecamp itinerary and photography

Source: https://www.adventure-life.com/antarctica/cruises/16115/antarctica-basecamp
Reviewed October 2, 2026. The source gives an example 13-day itinerary; the prototype
maps its day groups onto the organizer-confirmed January 24–February 5, 2028 dates.
Departure-specific stops and activities are not guaranteed. The vessel name is
omitted from page copy, image names, and image descriptions at the organizer's request.
Conference programming is separate from the expedition itinerary.

Locally copied prototype photographs, with page-level source attribution:
- kayaking.webp: https://cdn.adventure-life.com/16/52/95/ANT-Nov19-49/index.webp
- penguins.webp: https://cdn.adventure-life.com/16/07/32/ANT-Day4-PM_GourdinIsland_19/index.webp
- hiking.webp: https://cdn.adventure-life.com/15/27/45/iStock-1127819207/index.webp

Publication/reuse permission for these photographs has not been established.
Confirm licensed promotional use with the supplier before public launch, particularly
the stock-origin hiking photograph. Existing pricing remains the organizer's pricing.

## Booking terms working draft — October 19, 2026

Status: internal proposal for organizer and legal review. Not customer terms and
not authorization to open payments. Confirm the signed supplier/group agreement
before setting cancellation promises. Public source checked October 19, 2026:
https://oceanwide-expeditions.com/page/terms-and-conditions (section 6).
The public trip listing separately states final payment 90 days before departure:
https://www.adventure-life.com/antarctica/cruises/16115/antarctica-basecamp.

### Confirmed commercial terms

Dates and fares remain as above. Only full payment in bitcoin at signup qualifies
for 5% off. Stripe payments use the standard fare; no processing surcharge has
been approved. The held-bitcoin plan was canceled October 22, 2026; it is
not part of launch scope.

### Inventory worksheet — allocation confirmed October 22, 2026

Enter passenger berths, not cabin counts. If the operator supplies cabin counts,
convert to berths and account for any cabins that cannot be split before opening
sales. The three categories must describe disjoint allocations; do not count the
same physical berth under both shared and triple/quad.

| Fare category | Allocated passenger berths | Staff/guest allocation | Other unavailable berths | Public capacity |
| --- | --- | --- | --- | --- |
| Two-person (9 cabins) | 18 | Pending | Pending | Pending |
| Three-person (1 cabin) | 3 | Pending | Pending | Pending |
| Four-person (1 cabin) | 4 | Pending | Pending | Pending |

Public capacity = allocated berths minus staff/guest allocation and other
unavailable berths. Database `capacity` is this public allocation, including
already-confirmed public bookings; it is **not** a remaining-availability count.
The store subtracts active holds and confirmed bookings itself. Do not subtract
those bookings a second time when configuring capacity. Sponsor placement counts
and the approximately 30 parkas are not evidence of passenger capacity.

Before configuring inventory, confirm:

- Shared now means a two-person cabin. Guests choose M/F/co-ed cabin sharing.
  They can book one berth or multiple berths together in the same cabin.
  The remaining berths retain the same preference while a hold or confirmed
  reservation occupies that cabin. Empty cabins can take any of the three modes.
- Staff/guest allocations by category, and whether these need separate passenger
  records even though they are excluded from public sale.
- Public booking cutoff and its timezone. The current code needs an explicit
  timestamp per category; none is configured.
- Checkout hold duration. Proposed starting point: 30 minutes, subject to testing
  bitcoin invoice/confirmation timing. This is not yet an active policy. Customer
  copy must distinguish a temporary checkout hold from confirmed passage.
- Who can change allocations and reconcile payments received after hold expiry.
  Changing capacity must never silently displace existing confirmed passengers.

Do not seed placeholder counts. Keep sales closed until the allocation and
customer terms are approved and the shared checkout is connected.

### Customer booking section and acceptance draft

The prototype now has a `#booking-terms` section after payment options and before
sponsorships. It explains per-person booking, the agreed payment choices and the
remaining terms without presenting a cancellation promise or live availability.

Proposed checkout acknowledgments, to attach to versioned final text rather than
activate on the preview:

1. “I have read and agree to the booking terms, including changes, cancellations
   and refunds.” Link the exact terms version shown with the booking summary.
2. “I agree to the total fare and payment schedule shown above.” Show the selected
   berth, provider, discount if eligible, amount due now and each later amount and
   date. A fully paid booking must show no remaining installments.
3. “I have read the expedition's traveler requirements.” Final text must identify
   the operator's actual documentation, insurance and participation requirements.

Keep these unchecked and record each required acknowledgment with the accepted
terms version. Do not treat acceptance as automatic card-debit authorization.
Marketing consent remains separate and optional. No checkbox or payment action
is added to the prototype until there is actual approved text to accept.

### Proposed cancellation approach — awaiting supplier contract

Use a clearly published cancellation charge schedule rather than blanket
forfeiture. Public supplier tiers, mapped to January 24, 2028 departure:

| Cancellation received | Public supplier charge as share of trip price |
| --- | --- |
| Through October 26, 2027 | 20% |
| October 27–November 25, 2027 | 50% |
| November 26, 2027–departure | 100% |

Written group terms can override those tiers. These are supplier charges, not yet
our retail cancellation policy. Reconcile supplier net costs with retail fares
and the discounted bitcoin fare before adoption. Do not apply a supplier cost
percentage to a retail fare silently. Set the receipt timezone and contact channel.

Recommended customer-policy basis for review: calculate cancellation charges on
the customer's agreed fare, including any full-payment discount, and disclose the
result before payment. Refund payments received minus the applicable charge,
never below zero; separately define whether any unpaid cancellation liability is
collectible. Decide treatment of re-sold berths, substitutions, and supplier fees.

### Payment administration proposals — not approved

- Late signups pay the deposit plus installments whose dates have passed. After
  October 1, 2027, require full payment. Confirm the final booking cutoff.
- Prefer invoiced reminders over implied automatic debit. If automatic collection
  is offered, obtain express authorization and disclose amounts and dates.
- Send written overdue notice and propose a seven-day cure period, subject to
  supplier deadlines. Cancel only through a recorded workflow with the applicable
  cancellation calculation; do not silently retain every payment by default.
- Recommend USD-denominated cancellation entitlements for the simple bitcoin
  pay-in-full plan. Decide refund currency, conversion source/time, network fees,
  and timing explicitly; do not promise return of the original BTC quantity.
### Remaining customer-term decisions

1. Contracting business name/address, booking contact, supplier role, and when a
   reservation becomes binding. Collect these rather than inventing placeholders.
2. Actual berth allocation and roommate matching; individual versus whole-cabin
   bookings; substitutions and the effect of a roommate cancellation.
3. Final inclusions/exclusions and all-in charges. Confirm the parka is our own
   inclusion or separately supplied, not assumed from the cruise listing.
4. Organizer cancellation, minimum enrollment, material changes, weather changes,
   and refunds. Review against supplier recovery and applicable customer rights.
5. Operator insurance requirements, medical/documents deadlines, activity
   eligibility and onward travel guidance. Use departure-specific requirements.
6. Privacy notice and sharing necessary passenger data with the operator. Keep
   marketing consent and sponsor promotional-photo permission optional/separate.
7. Applicable law, dispute process and any liability/assumption-of-risk clauses
   require counsel review; do not copy the operator's exclusions wholesale.

### Checkout acceptance and launch acceptance criteria

- Display itinerary, berth, passenger count, fare, payment method, amount due now,
  remaining schedule, cancellation table and included/excluded services together.
- Required unchecked acknowledgments: booking/cancellation terms; chosen payment
  obligation; expedition/traveler requirements. Separate authorization for any
  automatic card collection.
- Persist immutable terms version, acceptance timestamp, booking identity, fare,
  currency, plan and method. Email the same terms and receipt. Each adult completes
  personal travel/medical acknowledgments even if a lead traveler pays.
- Server-enforced bitcoin full-payment discount; integer-cent math; quote expiry,
  confirmations, under/overpayment handling and refunds defined for BTC invoices.
- Transactional inventory holds, expiry/release, duplicate webhook protection,
  late-payment handling and no overselling. A canceled payment must not issue a berth.
- Dashboard balances, reminder delivery/retries, manual reconciliation and
  cancellations/refunds have auditable records and named operational owners.
- Exercise card and BTC success/failure, duplicate callbacks, last-berth contention,
  expired invoices, discount ineligibility and cancellation/refund scenarios.
- Confirm photo licenses, public route, sharing metadata, contact links and final
  copy; replace misleading geographic promises (the route visits the Antarctic
  Peninsula, not the geographic South Pole). Keep sponsor prices per placement.
- Keep noindex and reservation-closed messaging until paid-booking readiness is
  demonstrated. Approval of terms alone is not production deployment approval.

Open question: signed supplier terms vs public listing. Payment-plan scope is
resolved: installments and full payment only. Cancellation and administration
policies above marked proposed are not yet accepted.

## Expedition route map

The local SVG map uses public-domain Natural Earth 1:50m land geometry:
https://raw.githubusercontent.com/nvkelso/natural-earth-vector/master/geojson/ne_50m_land.geojson
License: https://www.naturalearthdata.com/about/terms-of-use/
The route and shaded peninsula region are illustrative, not navigational.
Possible locations follow the Adventure Life itinerary; none are guaranteed stops.
The SVG includes accessible descriptions and a full-size link for mobile readers.

## Expedition product foundation — October 22, 2026

Implemented on the isolated `antartica` branch in `internal/expedition`:

- One server-owned fare catalog, also used by the landing page. Full bitcoin
  payment receives 5% off; Stripe and both installment methods use standard fares.
- Immutable per-reservation schedules: one full payment, or five payments at
  booking and January 1, April 1, July 1 and October 1, 2027. No subscriptions or
  automatic debits. Dates currently use UTC. Until a catch-up policy is approved,
  new installment signups on/after January 1 are rejected; existing installments
  can still be paid when overdue. This guard does not approve the proposed late
  booking policy above.
- PostgreSQL berth holds, configurable capacity/closing time/hold duration,
  customer-scoped request idempotency, and row locks against last-berth races.
  One reservation means one passenger berth, not a whole cabin. Expired holds
  release capacity when the next reservation checks availability. Paid bookings
  continue occupying capacity, including when later payment needs review.
- A `Charge` adapter output with fixed product code
  `expedition.antarctica2028.passage`, opaque reservation resource ID, immutable
  fulfillment payload and deterministic purchase key per installment. Prices
  come from the persisted schedule, never browser amounts. Only the next due,
  unpaid installment is payable. Full-payment provider choice is locked to the
  booking; changing providers/plans needs an explicit replacement workflow.
- `Fulfill` accepts an order ID and a trusted shared-commerce reader. It verifies
  purchaser, product, provider, resource, USD amount, key and original quote
  expiry, then durably projects that order onto exactly one installment. Replays
  are safe. Late/out-of-sequence payments require manual review; they never
  automatically take a newly available berth. Processing after hold expiry is
  conservatively reviewed even if the provider settled earlier.
- `Due` lists the earliest unpaid installment for confirmed bookings for a future
  reminder worker. It sends no emails and creates no charges. Fully paid bookings
  drop out; overdue balances remain visible for reconciliation. There is no
  automatic delinquency cancellation.

Migration `132_expedition_reservations.sql` intentionally seeds **no inventory**.
It follows the shared branch's currently pending migration 130; recheck numbering
at merge time. It creates product-owned tables only, with opaque shared customer
and order IDs, and no dependency on email schema or copied financial ledger.
The migration has been exercised in disposable test schemas, not the application
database. The public prototype still has reservations closed.

### Shared-commerce bridge still required before enabling checkout

The working shared API at
`/Users/niftynei/dev/notifs/target/btcpp-alerts-current` was inspected read-only.
Its uncommitted email/account-free management work has not been copied or changed.
The Antarctica branch does not yet contain the shared commerce implementation.

1. After the shared work is ready, register the expedition product in shared
   sales/quote validation and fulfillment dispatch. Map `Charge` to `ServiceQuote`
   with quantity 1, `UnitCents=TotalCents`, no billing months/recurring flag/period,
   and the exact deterministic purchase key. The bridge must resolve the verified
   customer through the shared account-free flow; never take buyer IDs from a form.
2. Keep legal acceptance and tax determination in shared commerce. Confirm gross
   fare/tax treatment before enabling sales; do not silently add charges to these
   advertised totals. Require the reviewed expedition terms through shared policy
   activation, with no prelaunch acceptance bypass.
3. Implement `PaidChargeReader` using durable verified shared settlement evidence
   and immutable original quotes, rejecting refunded or mismatched orders. Dispatch
   `Fulfill` and acknowledge only after its commit. Route `ErrReview` and conflicts
   to an operator reconciliation queue; do not acknowledge them as delivered.
   Never invoke fulfillment from a browser success redirect. The initial quote
   expires with the hold. Later installment quotes have a stable January 24, 2028
   UTC expiry; provider checkout sessions need their own shorter expiry/retry
   handling inside shared commerce, without changing the purchase key.
4. Add the authenticated reservation/checkout/manage UI, balances, reminder
   delivery through the evolving shared email service, and recorded cancellation/
   refund/reconciliation workflows. `Due` is a bounded ordered query, not a
   delivery queue; the worker must manage batching and deduplicated reminders.
5. Configure actual berth inventory, booking cutoff and hold duration; approve
   milestone timezone, late signup policy and terms. Exercise provider sandboxes
   and the integrated tax/legal/email path before opening sales.

Tests cover all fare/plan/provider combinations, exact finite totals, rejected
late installment signups, concurrent last-berth requests and exact retries,
customer isolation, expiry/replacement/late-payment replay, verified-payment
requirements, wrong amounts, duplicate orders, finite due-payment discovery and
retention of confirmed inventory during review. PostgreSQL tests opt in with
`EXPEDITION_TEST_DATABASE_URL` and create/drop unique schemas on that database.

## Cabin allocation and group selection — October 22, 2026

The organizer confirmed nine twin cabins, one triple, and one quad: 11 cabins,
25 berths. Staff/guest deductions and sellable capacity still need confirmation.
Migration 132 seeds the physical cabins with internal labels only, without
opening sales or configuring a booking cutoff. These labels are not ship cabin
numbers. No application database has been migrated.

`ReserveGroup` accepts an explicit sharing mode (`male`, `female`, `coed`) and
a quantity from 1 to the cabin capacity. All berths in the booking are allocated
in one compatible cabin atomically; groups are never split. Compatible occupied
cabins are filled first to preserve empty cabins for groups. Expired unpaid holds
release both their berths and their sharing restriction. Confirmed bookings keep
their cabin allocation even during payment review. Group prices and installments
are the sum of the per-person prices, including per-person bitcoin rounding.
One payer owns the group payment schedule. Passenger names, invitations, and
each adult's individual acknowledgments still need the shared checkout flow.
Groups larger than one cabin and joining an existing friend's booking are not
implemented; the current group flow reserves together in a single transaction.

This supersedes the earlier single-passenger-only foundation description. Shared
commerce still receives one order item for the group charge, with quantity 1 and
the group installment total; physical passenger count lives on the reservation
and immutable schedule. It must not multiply the group amount a second time.

The organizer-provided `static/img/antarctica/berths.png` displays 20 numbered
cabins, more than our allocation. The prototype includes it as a zoomable reference
and a cabin-type/sharing/group-size planner with group payment estimates. It does
not invent available cabin numbers, persist holds or claim live availability.
The organizer cannot guarantee specific assignments. The diagram is therefore
collapsed by default under “Explore the ship layout,” labeled reference-only.
Do not add clickable numbered-cabin selection or promise specific deck locations.
Booking selects cabin type, sharing preference and group size; internal cabin
labels support allocation without promising a vessel cabin number.

## Checkout flow — revised October 22, 2026

Cabin selection is no longer on the landing page. “Reserve Your Spot” opens a
separate four-step checkout preview: party size → cabin arrangements → merch →
payment. A party can span cabin types; three guests can choose a full twin plus
one berth in another twin. The sum of assigned berths must equal party size.
The preview checks the known cabin counts and calculates combined passage totals
with the 5% discount only for full bitcoin payment.

Roommate choices are non-guaranteed requests for unfilled twin cabins. Triple and
quad cabins default to co-ed. Full cabins need no outside roommate preference.
This supersedes the earlier strict sharing-mode policy: the single-cabin store
now prefers a compatible cabin but can fall back to other available space.

The merch screen is skippable and currently collects only a preview interest
choice; products, variants, prices and fulfillment must be configured before
adding merchandise to payable totals. Do not reuse the passage discount on merch.
Future insurance or hotel offers can be separate add-ons; none are displayed or
sold yet. Newsletter consent is separate, optional and unchecked by default.
The preview sends no newsletter subscriptions, personal details or payments.

The sample 25 remaining spots are explicitly labeled demonstration inventory, not
live availability. Public capacities/staff deductions are still not configured.
Before enabling checkout, connect current inventory, reserve every selected
cabin atomically as one party, and bridge its combined payment and fulfillment
to shared commerce. Existing ReserveGroup handles one cabin per transaction;
QuoteParty validates/prices multiple cabins but does not reserve them. Never loop
over ReserveGroup to create a partially held multi-cabin checkout. Payment stays
disabled until this transaction, shared commerce, consent persistence and terms
acceptance are implemented.

### Hat upsells — October 22 update

The organizer chose hats as the initial merch default. Checkout now offers Core,
libbitcoin and Libre Relay hats with quantities. Database-backed previews load
published USD products and active variants from the existing merchandise catalog,
respecting availability dates and stock limits. Catalog lookup failures do not
fall back to fictional stock. The database-free local preview uses a clearly
labeled $38-per-hat snapshot verified against https://btcpp.dev/shop on October 22.
These are current shop hats, not a claimed reconstruction of Berlin26's historical
conference-specific selection.

Selected hats appear in the payment summary and estimated signup amount. Merch
is paid upfront in this preview, excluded from the passage-only bitcoin discount
and subsequent passage installments. Shipping, merchandise tax and delivery
arrangements remain unconfigured and are explicitly excluded from the estimate.
Actual collection remains disabled. Live checkout must re-read variant prices,
reserve stock and apply merchandise tax/fulfillment through shared commerce.
This replaces the earlier interest-only merch placeholder.

### Onboard fulfillment and Texas seller — organizer confirmation

Hats will be collected onboard. Do not collect a merchandise shipping address or
add a customer shipping charge. Checkout now states onboard pickup and shipping
$0. Billing details needed by the payment processor remain separate. Live
fulfillment should use an expedition pickup record, not a Texas pickup location.

The organizer says trips will be sold from Texas. This establishes a seller
location fact, not a determination that Texas sales tax applies to all charges.
Do not source the overseas pickup to the Texas business address. Confirm the
contracting entity, treatment of the international expedition/conference package,
actual merchandise handover location and supporting export/delivery records with
the tax adviser before activating tax collection. No blanket Texas rate or
blanket exemption has been configured.

Texas Comptroller guidance reviewed: https://comptroller.texas.gov/taxes/publications/94-171.php
(seller delivery outside Texas) and https://comptroller.texas.gov/taxes/publications/96-259.php
(taxable services). These do not establish a final tax result for this bundled
expedition or rule out obligations in the delivery jurisdiction.

### Configurable staff reserve — October 23

Organizer confirmed 3 staff berths initially: 25 physical berths minus 3 staff
holds leaves 22 public spots before customer holds/bookings. Migration 133 adds
`expedition_settings.staff_berths`, default 3. Staff allocation is deliberately
expedition-wide until cabin types are assigned; do not assume the triple belongs
to staff. The checkout preview reads its maximum from page data instead of a
hard-coded 25.

`Store.SetStaffBerths` changes the reserve transactionally and rejects increases
that would displace unexpired public holds or confirmed bookings. `ReserveGroup`
locks the same setting and checks expedition-wide remaining space before its
category/cabin checks. Staff holds must not also be subtracted from category caps
unless the global/category accounting is revised to avoid double subtraction.

An authenticated admin editing screen is still to be connected to this operator
method; no public endpoint changes the reserve. Staff cabin placement is still a
launch decision, since a global count alone does not preserve a particular cabin
configuration. Multi-cabin atomic booking remains pending as noted above.
