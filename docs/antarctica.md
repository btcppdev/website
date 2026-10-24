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

| Berth | Total | First four payments |
| --- | ---: | ---: |
| Shared | $16,700 | $3,340 |
| Three-person cabin | $15,800 | $3,160 |
| Four-person cabin | $14,900 | $2,980 |

The confirmed standard fares above remain unchanged. Payment in full at signup
receives 5% off only when paid with bitcoin: shared $15,865 (save $835),
three-person $15,010 (save $790), and four-person $14,155 (save $745).
Full payments through Stripe use the standard fare, with no discount or added
surcharge. The organizer reports a 3% Stripe processing cost. The discount does
not apply to installments.
Discounted totals are rounded to cents on the server and displayed consistently
when switching berths. These are prototype displays; future checkout must enforce
eligibility using the actual payment method and plan on the server.

These fares divide evenly into five payments; no rounding adjustment is needed.

The payment plan is five payments of approximately 20%: 20% on reservation, then January 1,
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

Organizer estimates approximately 22 attendees/parkas, with the garments featured
in expedition photos. Route, garment design, and whether attendees
keep the parkas remain unconfirmed. These are proposed asking prices, not approved
offers, guaranteed audience valuations, or live inventory.

| Placement across all expedition parkas | Proposed USD price | Slots |
| --- | ---: | ---: |
| Upper arm | $7,500 | 2 |
| Middle arm | $6,000 | 2 |
| Lower arm | $5,000 | 2 |
| Left chest | $12,500 | 1 |
| Right front | $10,000 | 2 |
| Large back | $25,000 | 1 |

The existing PowerPoint deck is the source for these updated proposed rates:
10 placements, showing only the price per placement. Public decks and the website
must not include revenue columns, fundraising totals, or aggregate package proceeds. The prototype now displays the deck's front/back placement diagram and
these rates, with an inline image preview and a PowerPoint download at
`/static/downloads/antarctica/btcpp-arctic-expedition-sponsor.pdf`. This supersedes the earlier
four-placement proposal. Rates and placements remain proposed, not live inventory.

Proposed package benefits: placement on approximately 22 parkas, linked website
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

## Booking terms working draft — updated October 23, 2026

### Booking confirmation email — October 23, 2026

The initial verified payment now uses the shared receipt queue to send an
enthusiastic expedition welcome **only after berth fulfillment confirms the
reservation**. Its absolute leading-image URL, booking and receipt links,
insurance requirements, Argentina travel guidance, optional paid travel help via
`inbox@btcpp.dev`, and future packing-list promise are in
`internal/emails/antarctica_booking.go`. HTML and plain text share the same prose.
Group buyers receive the email and are asked to share it with all passengers;
there is no automatic delivery to unnamed fellow travelers yet.

Delivery requires the existing `SERVICE_RECEIPTS_ENABLED=true`, mail
configuration and `SERVICE_FULFILLMENT_ENABLED=true`. Mail-off does not consume
jobs. Settlement alone queues the receipt; held bookings or unapplied installments
retry, review bookings stop for review, and refunded orders do not receive a
welcome. Later installments get expedition payment receipts without the welcome.
The immutable order recipient and `service-receipt:<public order ID>` key preserve
the shared system's leases, retry limits, deduplication and audited recovery.
No new email store, newsletter signup or migration is introduced. Previously
cancelled receipt jobs are not automatically resurrected.

Local email preview (sample data, no sending):
`http://127.0.0.1:8110/expeditions/antarctica/confirmation-preview`.
This route exists only in the opt-in standalone demo test server.

Travel guidance and sources checked October 23, 2026:

- Arrive in Ushuaia by **January 23, 2028**, before January 24 embarkation.
  [Oceanwide's Ushuaia guidance](https://oceanwide-expeditions.com/to-do/experiences/ushuaia)
  recommends the preceding day and warns that the ship cannot wait for delays.
- February 5 is scheduled disembarkation. **February 6 or later** is our
  conservative recommended earliest return-flight date, not an operator-required
  cutoff or guarantee against disruption. No departure-specific same-day flight
  cutoff was verified. Ask Oceanwide before authorizing February 5 flights.
- Check connecting airports in Buenos Aires:
  [Ushuaia tourism's arrival information](https://turismoushuaia.com/contenidos/como-llegar/)
  describes service from both Ezeiza and Aeroparque. The email does not promise
  flight availability, fares, or a connection duration for 2028.
- [Oceanwide insurance requirements](https://oceanwide-expeditions.com/faq/what-type-of-insurance-do-you-recommend-for-the-voyages)
  provide the Antarctic evacuation/repatriation minimum. Traveler proof collection
  and departure forms still need the onboarding workflow described below.

The email is a reservation confirmation in our booking system, not an invented
operator confirmation number, cabin assignment, boarding pass or proof of final
payment for installment customers. Before launch, reconcile paid bookings with
Oceanwide, confirm travel guidance, and verify the public image URL and monitored
reply mailbox. The packing-list promise is copy; its later delivery is not yet
automated. Paid travel help is offered by inquiry, with scope and fee quoted first.

Status: internal proposal for organizer and legal review. Not customer terms and
not authorization to open payments. Confirm the signed supplier/group agreement
before setting cancellation promises. Public source checked October 19, 2026:
https://oceanwide-expeditions.com/page/terms-and-conditions (section 6).
The public trip listing previously gave a different final-payment deadline.
Use the organizer-provided group correspondence below as the current working
basis, subject to retaining the written agreement and confirming its departure
date. Do not substitute a public listing's deadline for the group arrangement.

### Operator correspondence supplied October 23, 2026

- Cabins are on a **Right of First Refusal** hold as of this week. Oceanwide may
  request a cabin back when another passenger wants it. We have **24–48 hours**
  to confirm with a deposit or release it; record the exact deadline in each notice.
- Deposit: **20% per traveler as they book, non-refundable**, as described by
  the operator. The correspondence does not establish exceptions, charges beyond
  that deposit, or refunds when the operator cancels or cannot confirm a booking.
- Final supplier payment: **70 days before departure**. If January 24, 2028 is
  the contractual departure date, this is **November 15, 2027**. Confirm that the
  deadline is based on that date and whether funds must be received by then.
- Keep the existing customer installment dates, ending **October 1, 2027**.
  This provides 45 days before the supplier deadline. No payment schedule or
  automatic cancellation behavior is changed by this working draft.

Operational implications before launch:

- Operator holds are distinct from temporary checkout holds and paid bookings.
  Reconcile each allocated cabin with the operator; remove released inventory
  from sale without displacing existing paid passengers.
- Assign an owner for recall notices, deposit remittance and operator confirmation.
  Confirm whether booking one berth secures the whole cabin or only that berth.
- Decide how checkout payment and operator confirmation are coordinated, including
  the refund process if we collect payment but cannot confirm passage. Current
  payment fulfillment does not itself prove operator confirmation.
- Obtain the complete group cancellation schedule, traveler replacement rules,
  missed-payment consequences and operator-cancellation provisions. The phrase
  “non-refundable deposit” alone does not make every later payment non-refundable.
- Decide the customer cancellation-charge basis explicitly: supplier net cost
  versus the customer's agreed retail fare, including discounted bitcoin fares.
  Do not activate a retail policy or change archived accepted terms from this note.

### Supplier-source reconciliation — October 23, 2026

The organizer confirmed we are booking **directly with Oceanwide**. Adventure
Life is an itinerary/photography reference, not our booking intermediary. Its
contract, payment deadline, forms and waiver requirements do not govern this
direct booking. Use Oceanwide's written group agreement and applicable operator
requirements as the supplier basis for our separate customer agreement.

The operator's [departure schedule](https://oceanwide-expeditions.com/trips/dates-rates)
identifies **OTL28-28**, Basecamp, January 24–February 5, 2028, 12 nights / 13 days,
round-trip Ushuaia. The [vessel page](https://www.adventure-life.com/cruises/246/ortelius)
identifies Oceanwide as operator. Vessel identity is internal reference information;
continue omitting the name from public copy. Public availability is not our allocation.

The updated draft at `/expeditions/antarctica/terms` adds insurance, voyage
inclusions/exclusions, activity limits, traveler documents and a clearly labeled
supplier cancellation schedule. It does not activate a legal-policy version,
change payment fulfillment, or assert that customer terms are complete.

Source and conflict register:

| Source | Finding / action |
| --- | --- |
| [Adventure Life itinerary](https://www.adventure-life.com/antarctica/cruises/16115/antarctica-basecamp) | Generic example; 20% deposit, 90-day final balance. Keep our reported 70-day group arrangement pending written confirmation. Parkas excluded: resolve organizer-supplied garments before promising inclusion. Do not import its card surcharge, single-occupancy multiplier, or child discounts into our fares. |
| [Oceanwide terms](https://oceanwide-expeditions.com/page/terms-and-conditions), §§3–6 | Standard balance is 60 days; written variations prevail. Public cancellation tiers remain the comparison table below, not approved retail policy. Review operator cancellation, delays and price-change remedies separately. |
| [Exact departure](https://oceanwide-expeditions.com/antarctica/cruises/otl28-28-basecamp) | Basis for draft inclusions and activity limits. Its Basecamp overview says onboard signups, while embedded activity FAQs mention advance reservations. Obtain departure-specific registration instructions; promise neither guaranteed outings nor no prebooking. |
| [Insurance FAQ](https://oceanwide-expeditions.com/faq/what-type-of-insurance-do-you-recommend-for-the-voyages) | Antarctica evacuation/repatriation minimum: EUR/USD 100,000 per person. Require destination/activity coverage; establish proof collection and verification. Insurance is not currently sold by our checkout. |
| [Adventure Life contract and release](https://www.adventure-life.com/forms/terms-conditions) | Not applicable to this direct Oceanwide booking. Do not require its signatures, medical age threshold, release or invoice rules. Obtain the actual Oceanwide traveler forms and deadlines instead. |

Remaining contract and implementation work:

1. Obtain the direct Oceanwide invoice/group agreement. Archive applicable terms and departure documents
   with an effective date; links to changing websites alone are not the accepted
   customer contract.
2. Reconcile supplier remedies for operator cancellation, minimum enrollment,
   force majeure, itinerary changes, delays and surcharges with the retail policy.
   Do not add blanket no-refund language or pass through supplier penalties
   automatically. Have the final customer agreement reviewed for our selling entity.
3. Define the cancellation receipt address/timezone, refunds and processing times,
   replacement travelers, missed payments and the discounted bitcoin refund basis.
4. Add a per-traveler onboarding workflow for documents, operator forms and
   insurance proof. Define secure handling, recipients, retention and deadlines.
   Booking acceptance by one payer does not complete other adults' forms.
5. Keep organizer programming, parkas and merchandise distinct from voyage
   inclusions. Resolve delivery and garment suitability. Port/service charges in
   the cruise package do not establish a tax exemption for our retail sale.
6. Do not copy Adventure Life's Montana arbitration, photo release, liability
   waiver or later-term-update provisions into bitcoin++ terms. Similarly, do not
   transplant the operator's jurisdiction clause. Determine applicable agreements
   and use reviewed provisions for our own entity. Obtain separate participant
   permissions for sponsor photography; other businesses' photo rights are not ours.

### Guest cancellation decisions — October 23, 2026

- Organizer confirmed guest final payment and last cancellation day:
  **October 1, 2027**. The existing installment schedule already ends that day.
- **No passenger substitutions**. This is the current policy, even if the operator
  could permit a replacement. Any later relaxation must be explicitly adopted.
- Refund/charge percentages before and after the deadline remain unconfirmed.
  Asked whether to retain 20% through October 1 and 100% from October 2. Do not
  implement that proposal without the organizer's answer. Confirm cutoff timezone
  and written-notice channel; do not silently change installment timestamps.
- Retain supplier cancellation tiers below only as an internal cost comparison.
  The guest deadline does not change the November 15 supplier balance deadline.

### Verified traveler forms — October 23, 2026

Oceanwide's [health FAQ](https://oceanwide-expeditions.com/faq/do-i-have-to-be-completely-healthy-in-order-to-join-the-expedition)
requires a Personal Information Form (PIF) within one month of booking.
Its [privacy policy](https://oceanwide-expeditions.com/page/privacy-policy)
identifies passport, emergency-contact and medical information in the PIF.
The [booking-document FAQ](https://oceanwide-expeditions.com/faq/what-kind-of-documents-will-i-receive-when-i-book-an-expedition)
includes boot sizing and activity forms where applicable. Obtain the current
packet rather than reuse an old third-party PDF. Insurance details are requested
post-booking; confirm the submission deadline and acceptable proof with Oceanwide.

[Oceanwide's partner portal](https://oceanwide-expeditions.com/page/sales-info)
can generate passenger-specific digital PIFs, including in bulk. Request access
or ask our Oceanwide contact to generate each guest's link. Prefer submitting
medical/passport details directly there and tracking completion in our booking
workflow. Do not invent a general form URL or require guests to email health
records to inbox@btcpp.dev. Confirm the exact activity forms/waivers and whether
any individual requires medical clearance; no universal physician certificate
for this Basecamp departure has been verified.

### Organizer commercial terms

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
and the approximately 22 parkas are not evidence of passenger capacity.

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
collectible. Decide treatment of re-sold berths and supplier fees. Passenger substitutions are currently prohibited by the organizer.

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
   bookings and the effect of a roommate cancellation; substitutions are currently prohibited.
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

Migration `135_expedition_reservations.sql` intentionally seeds **no inventory**.
It now follows master’s purchase-invoice migration 131. It creates product-owned tables only, with opaque shared customer
and order IDs, and no dependency on email schema or copied financial ledger.
The migration has been exercised in disposable test schemas, not the application
database. The public prototype still has reservations closed.

### Shared-commerce bridge plan — superseded in part by October 23 integration

The working shared API at
`/Users/niftynei/dev/notifs/target/btcpp-alerts-current` was inspected read-only.
Its uncommitted email/account-free management work has not been copied or changed.
The branch now contains master’s shared commerce implementation; the October 23
integration status below identifies completed and outstanding work.

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
Migration 133 seeds the physical cabins with internal labels only, without
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
holds leaves 22 public spots before customer holds/bookings. Migration 134 adds
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

## Shared commerce integration — October 23, 2026

The Antarctica changes were checkpointed and rebased onto current origin/master,
including shared billing, email directory/preferences, and purchase invoices.
The original branch is preserved as `backup/antartica-before-commerce-20261023`.
Other worktrees were not edited. No production deployment or payment was made.

Migration allocation is now:
- 131: shared purchase invoices (master, unchanged).
- 132: expedition reservations and finite installments.
- 133: physical cabin allocation.
- 134: configurable staff reserve (initially three).
- 135: multiple cabin allocations per reservation/payment schedule.
- 136: allow expedition-specific reviewed policy revisions in shared commerce.

Implemented and tested:
- `ReserveParty` atomically reserves all requested cabin arrangements or none.
  One buyer, stable request key, hold expiry and finite schedule cover the party.
  Replays preserve the original selection; changed selections conflict. The
  global staff reserve and category/cabin capacities are checked in the same
  transaction. Preferences remain best-effort; cabin numbers remain internal.
- `CreateExpeditionPurchase` maps the next persisted installment to one shared
  service order. Its period describes January 24–February 5, 2028 (exclusive
  end February 6 UTC); it is not recurring. The shared product validation checks
  buyer, deterministic key, exact server amount, provider, dates and fulfillment
  resource against the held contract again inside the order transaction.
- Reviewed, current terms acceptance and shared tax-quote evidence are mandatory
  for expedition orders, including a zero-tax result if ultimately appropriate.
  No reviewed terms or tax exemption are seeded by these migrations. Tax is
  represented inside the supplied gross passage charge; final fare/tax policy
  must be confirmed before the customer-facing tax calculation is connected.
- Shared payment attempts recheck the payable reservation before creating a
  charge. Stripe and OpenNode verified settlement paths can dispatch expedition
  fulfillment via the existing durable worker. The worker does not need Insider
  credentials to fulfill an expedition order.
- `FulfillExpeditionPurchase` locks/revalidates the shared paid order and items,
  verifies settlement evidence and applies the installment/berth projection in
  one transaction. Duplicate callbacks and fulfillment retries are safe. Late
  payments go to review; refunded or altered orders cannot grant berths.
- Shared order pages distinguish confirmed expedition payments, pending berth
  confirmation and manual review. Shared financial records remain authoritative;
  no second payment ledger or provider credentials were added.

Validation: the complete migration sequence through 136 applied to a fresh
disposable PostgreSQL database. Tests cover mixed-category party rollback,
last-cabin contention, staff reserves, exact order replay before/after payment,
missing terms, changed prices, both Stripe and bitcoin totals, duplicate
settlement/fulfillment and rejection after refund. Expedition integration tests
ran with the race detector. Affected handler/getter/servicebilling package suites
also passed. Provider evidence in these tests is synthetic; this is not a live
provider sandbox rehearsal.

Still required before opening sales:
1. Connect the customer checkout to authenticated/account-free identity, CSRF,
   server quote review, hold creation and provider session creation. The current
   browser UI remains a preview; it does not invoke these backend entry points.
   Card session expiry must fit the selected hold/tax quote; confirm hold duration
   against provider session minimums and bitcoin confirmation timing.
2. Finalize gross fare/tax treatment and publish/activate reviewed booking terms.
   Configure category sale capacity, cutoff and hold duration; assign staff cabins
   or approve the consequences of a floating staff allocation before opening.
3. Connect merchandise as actual catalog/stock/tax/onboard-pickup order lines,
   without disguising hats as passage or applying the passage discount to merch.
   Persist newsletter consent via the shared email service, separately from terms.
4. Complete passenger records, each adult’s acknowledgments, receipt/reminder
   delivery, later-installment checkout and an operator reconciliation/cancellation
   workflow. Include expedition customer links in the shared account-merge and
   deletion lifecycle before enabling account changes for paying passengers.
5. Rehearse actual provider sandboxes and refund/tax/delivery operations, enable
   the relevant workers, then review launch settings. Reservations remain closed.

The earlier notes saying multi-cabin atomic reservation and the basic shared
purchase bridge were pending are historical; those backend pieces are now done.


### Booking terms and customer booking view (October 23, 2026)

- Checkout displays an unchecked policy agreement using the current shared legal
  revision. Without an activated policy it displays only a draft-review checkbox;
  that field cannot stand in for accepted terms. Payments remain disabled.
- `/expeditions/antarctica/terms` renders the active archived terms/privacy text,
  or a clearly labeled draft outline when no approved revision exists.
- `/expeditions/bookings/{reservation UUID}` requires the authenticated buyer.
  It uses saved reservation prices and shared order evidence, never today's fares.
  It lists cabin arrangements, passage payments applied, outstanding scheduled
  passage, and each order's exact accepted terms and privacy snapshot.
- Purchase history and expedition order receipts link to this booking view.
  Receipt quantity no longer implies one passenger for a group installment, and
  expedition dates replace subscription access dates.
- Refunds flag the booking for review; scheduled passage balances are not a
  refund settlement. Taxes and merchandise remain on individual receipts.
- Local demo `/?booking=1` shows a prominently labeled synthetic booking. It
  creates neither a reservation nor payments nor acceptance evidence.
- Still required: approved policy activation, authenticated checkout submission
  with explicit agreement/CSRF validation, provider sandbox tests, traveler-name
  assignment, and issuing later-installment payment links. Existing order links
  use the shared order/payment page; the booking view does not generate charges.


### Post-launch backlog

- [ ] Add ACH Direct Debit through Stripe-hosted Checkout alongside cards.
  Keep a single Stripe choice on the expedition page; once ACH is enabled and
  tested, rename it to **Pay with fiat**, with **Card or US bank account via
  Stripe** as supporting text. Stripe presents eligible payment methods and
  handles bank verification and debit authorization.
- [ ] Before enabling ACH, implement and test pending bank-payment reservations:
  retain berths while processing, confirm only on verified successful payment,
  and handle failures, returns, disputes and duplicate/delayed webhooks. Define
  the pending-hold timeout and release policy so unpaid inventory cannot remain
  blocked indefinitely. Verify both full-payment and finite-installment flows.
- [ ] Check Stripe account eligibility, transaction limits and actual fees;
  configure and test permitted methods for expedition checkout without changing
  other products. Keep standard fares for fiat; the 5% full-payment discount
  remains bitcoin-only.

These are deferred until after launch. The current checkout keeps its Card and
Bitcoin choices; no ACH activation or payment-label change is part of launch.
Reference: https://docs.stripe.com/payments/payment-methods/dynamic-payment-methods

### Real checkout wiring (October 23, 2026)

- `/expeditions/antarctica/checkout` now authenticates with the shared bitcoin++
  identity, validates CSRF and current terms, prices the party on the server,
  atomically holds berths, calculates tax, and creates the shared purchase.
  The resulting order page starts Stripe card or OpenNode payment. Existing
  verified callbacks and durable fulfillment confirm the reservation; redirects
  never do. Purchase history links to the booking and saved installment schedule.
- Reloads resume the same order. Retries reuse the reservation and provider
  attempt. Financial and legal snapshots cannot be replaced by a retry.
- Newsletter signup is removed. Real checkout currently sells passage only;
  hats remain in the design preview pending stock, tax and fulfillment wiring.
- Sales remain closed by default. Opening requires **all** of:
  checkout enabled in `/admin`, `ANTARCTICA_STRIPE_TAX_CODE` set to a
  reviewed Stripe tax code, and `ANTARCTICA_TAX_MODE=billing-inclusive` only if
  that tax location/treatment has been approved for this voyage. There is no
  default tax exemption or assumption that Texas is the place of supply. A
  different required tax treatment needs implementation before enabling sales.
  Stripe credentials are required for tax calculations even for bitcoin.
- Also required: an active approved shared legal policy, configured category
  inventory and sales cutoffs, provider credentials/webhooks, and running shared
  fulfillment/tax workers. Use a 3600-second inventory hold for card checkout;
  Stripe requires at least 30 minutes remaining when creating its session.
  Tax/order expiry is capped at 45 minutes and never exceeds the reservation.
- Automated HTTP tests use a disposable PostgreSQL database and fake Stripe API:
  authentication, CSRF, stale/missing terms, price tampering, durable retries,
  reload recovery, card handoff, and no confirmation from redirects. These are
  not a substitute for an actual provider sandbox payment and webhook exercise.
- Still needed before launch: tax/terms approval, production inventory/staff
  settings, provider sandbox end-to-end acceptance, traveler details, and issuing
  later installment orders/reminders. Installments are finite scheduled payments,
  not an automatically charging Stripe subscription.

### Sold-out waitlist

- The landing CTA becomes **Join the waitlist** when live inventory is exhausted.
  Sold-out checkout replaces booking controls with a waitlist link. Inventory
  errors return an error rather than pretending the expedition is sold out.
- `/expeditions/antarctica/waitlist` uses the purchaser's verified bitcoin++
  account email, records the Join action as availability-email consent and validates CSRF, and offers
  a leave action. It does not subscribe anyone to the newsletter or hold a berth.
- Migration `140_expedition_waitlist.sql` stores the request against the shared
  email contact. Duplicate joins preserve the notification key and sent state.
- The optional worker sends one availability alert per signup via the existing
  mailer, with a stable job key for retry deduplication. It checks remaining spots,
  category sales/cutoffs, active booking terms, checkout configuration and mail
  availability. Deleted/unverified email contacts and withdrawn requests are skipped.
- Enable `ANTARCTICA_WAITLIST_NOTIFICATIONS_ENABLED=true` with mail delivery when
  launching. No messages are sent by the local demo or automated tests. The worker
  checks once per minute; availability is not a guarantee or priority reservation.
- Local sold-out preview: `http://127.0.0.1:8110/?checkout=1&soldout=1`.

### Three-slide sponsorship deck

The sponsorship PDF and editable PowerPoint now include a landing-inspired cover,
a photo-led expedition slide, and placement pricing with sponsor@btcpp.dev.
The pricing preview on the site remains the third slide. All per-placement prices
and the approximately 22 parka count are preserved; no total fundraising figures.

Photo source: Adventure Life Antarctica Basecamp trip page:
https://www.adventure-life.com/antarctica/cruises/16115/antarctica-basecamp
The images are source-trip photography, not our participants; the photo slide
identifies this and credits Adventure Life. The cover uses the existing leading.png
concept illustration; the brand mark is preserved from the supplied source deck.

### Bitcoin-only launch checkout — October 23, 2026

The checkout UI offers Bitcoin only, for both installments and full payment.
Full payment in Bitcoin retains the 5% discount. Saved checkout selections
restore to Bitcoin. `ANTARCTICA_STRIPE_CHECKOUT_ENABLED` defaults to false;
leave it unset or false for launch. The server rejects Stripe booking requests,
blocks payment on existing Stripe expedition orders, and guards the Stripe
handoff itself. Verified settlement callbacks remain intact for reconciliation.
Stripe Tax is still required for billing-inclusive tax calculations, including
Bitcoin purchases; disabling card checkout does not remove that dependency.
This does not enable sales, alter other products, or expire previously issued
provider checkout sessions. Any already-issued Stripe sessions must be audited
and expired at the provider before a production cutover.

### Installment reminders and abandonment — October 23, 2026

Implemented locally; production activation is still off. Confirmed bookings that
remain unpaid become **abandoned / forfeited**, release their berths and retain all
payments. Guest-requested cancellation is a separate outcome. An initial checkout
hold that never receives its deposit expires; it is not an abandoned booking.

Deadlines use **America/Chicago, end of the due date**. Reminders run at 9am local
time 14 days before, 4 days before, on the due date, and every other day overdue
through day 28. Abandonment happens at the deadline plus 30 local calendar days.
For example, a January 1 installment is due through January 1 at 11:59:59pm CST;
abandonment becomes eligible February 1 at midnight CST. DST uses calendar days.
After downtime, only the latest applicable reminder is queued, avoiding a burst
of obsolete reminders. The worker runs once per minute, so these are eligibility
times, not guaranteed exact delivery times.

Migration `141_expedition_installment_lifecycle.sql` adds policy registration,
durable events, delivery jobs, provider observations and inventory release state.
No policy is registered automatically. Only bookings whose initially accepted
legal revision is explicitly registered in `expedition_lifecycle_policies` enter
this lifecycle. Do not register a revision that lacks the agreed rule or apply new
terms retroactively. Publish and review the final terms before activation, then
register that exact revision with `time_zone = 'America/Chicago'` and enable
`ANTARCTICA_INSTALLMENT_LIFECYCLE_ENABLED=true` with working transactional email.
The draft terms page is not a published shared-commerce legal revision.

Later installments open 14 days before their due date. The authenticated booking
page and reminder link lead to a fresh tax/address/terms review and Bitcoin payment.
Expired quotes can be replaced while preserving previous order/payment history;
active, paid or unresolved attempts prevent competing invoices. Installments are
finite payments, never automatic account debits.

Before reminders or forfeiture, the worker reconciles outstanding provider
attempts. Pending, partial, unknown and ambiguous payments block action for review.
Paid-but-not-yet-fulfilled orders also block forfeiture. Inventory release, the
audit event and both notification jobs commit together. Late payments received
against an already abandoned booking require review and never restore its berths.
A settlement recorded at or after the cutoff also requires review if the worker
has not yet run; those berths stay held pending reconciliation. A payment recorded
before the cutoff is not treated as late merely because fulfillment was delayed.
Customer and `inbox@btcpp.dev` notices retry independently with stable job keys;
a failed email cannot release inventory twice. After 12 delivery attempts the job
moves to review. Worker errors and delivery jobs in review require operator action.
Mail being disabled prevents lifecycle processing.

Global admins can inspect `/expeditions/admin/bookings/{booking-id}` for event
snapshots and delivery status. The staff email includes purchaser identity,
released allocations, retained payment evidence, policy version and audit link.
Inspect `expedition_lifecycle_mail` for pending/review jobs and worker logs for
unresolved provider reconciliation. Keep the activation flag off until production
migration, policy registration, mail delivery and provider checks are verified.
Voluntary cancellation refund rules still need a separate final decision.

Real email-template previews (no database or delivery):
`http://127.0.0.1:8110/expeditions/antarctica/installment-preview`.
Use the links above the preview for all reminder stages and both forfeiture notices.
This replaces the earlier static concept in `outputs/antarctica-reminder-preview`.

### New-email checkout handoff — October 23, 2026

A verified email session without a person record now goes to `/dashboard/profile`
with `/expeditions/antarctica/checkout` preserved as `next`. This uses the existing
validated-session-email check, matching OAuth onboarding, since `auth.Resolve`
returns nil before profile creation. Existing profile creation upgrades the session
and returns to checkout; form validation errors retain the destination. Anonymous
customers still go to login, and pending profiles cannot POST a purchase.
Targeted redirect tests and the isolated-Postgres pending-email onboarding test pass.
The existing profile requirements still apply; the demo checkout does not simulate
login, profile creation, or outgoing email.

### Launch rehearsal — October 23, 2026

Extended `TestAntarcticaCheckoutHTTPPostgres` to cover Bitcoin full payment and
Bitcoin installments with Stripe checkout disabled, retaining a separate card
regression case. The Bitcoin cases exercise real HTTP handlers, an isolated
PostgreSQL database and local fake OpenNode/Stripe APIs. Verified:

- One twin full-payment charge is $15,865 (5% off $16,700); installment signup is
  $3,340 with a five-payment schedule. Inclusive tax evidence is retained.
- Authentication, CSRF, accepted/current terms and server pricing checks reject
  invalid submissions before inventory is held. Order retries and checkout reloads
  resume the same purchase; repeated payment clicks create one provider invoice.
- Invalid callback signatures fail. A signed callback claiming paid does not
  confirm a booking when the authenticated provider lookup still reports unpaid.
- Repeated paid callbacks produce one payment event. Tax finalization and the
  shared fulfillment worker produce the confirmed booking and payment schedule.
- The authenticated booking page renders; the shared receipt job composes the
  expedition welcome and January 24 calendar attachment for the purchaser.
  The test captures the message instead of contacting a mail service.
- The separate `TestPendingEmailDashboardOnboardingPostgres` passes verified-email
  onboarding, profile setup and preserved return-to-checkout behavior.

All four affected package suites passed with the isolated database. This is an
HTTP/database integration rehearsal, not evidence of actual provider settlement,
email inbox delivery, or a browser walkthrough of provider-hosted checkout.
The current workspace has no `.env`/`.env.local` or inherited OpenNode/Stripe
credentials configured for that external rehearsal. Obtain a non-production
configuration before testing those external boundaries.

Deployment must enable `SERVICE_FULFILLMENT_ENABLED=true` and
`SERVICE_RECEIPTS_ENABLED=true` with working mail configuration, in addition to
checkout, lifecycle and waitlist settings. Payment callbacks alone do not confirm
expedition berths: the durable fulfillment worker performs that transition.

Read-only comparison with the alerts checkout found receipt branding commit
`f0c031ba` (shared receipt HTML, wordmark asset and printable invoice styling) not
integrated into this branch. Reconcile it before final receipt visual acceptance;
no changes were made to the alerts workspace or its uncommitted files.


### Master integration and one-cent beta — October 23, 2026

Merged `origin/master` through `fa09a7ab` into `antartica`, including the current
shared receipt/invoice branding, billing layouts and payment-status fixes. The
other alerts worktree's uncommitted changes were not imported or modified.

Expedition migrations are now 135–141. 132 belongs to master site features;
133–134 are left available for the in-progress alerts migrations. New migration
142 records the one-use beta claim. Test references were updated with filenames.
The real migration runner passed an empty-database master installation, expedition
upgrade (eight new migrations), and an idempotent second run. If a development
DB already applied the old expedition migration numbers, use a fresh development
DB for this branch; do not rewrite a live migration ledger or replay table creates.
No production database or migration ledger was changed.

A single $0.01 USD full-payment Bitcoin booking is automatically available to
an account with verified `nifty@btcpp.dev` email. No account-discount deployment
flag is required. The expedition is unlisted, not access-restricted: anyone with
the link may register and purchase at the normal fares.
All normal checkout gates, published terms, tax calculation, inventory allocation,
provider verification, fulfillment and receipts still apply. Other accounts keep
standard fares. Neither an email address nor a custom price is accepted from the
checkout request. The fixed email is checked again inside the reservation
transaction; a globally unique beta claim prevents concurrent or subsequent
second discounted bookings. Retries use the original reservation. The schedule
and payment description identify the private beta purchase, with no later balance.
The claim remains consumed after payment, refund or expiry; it is not a coupon
that can be reused. A failed/expired test requiring a new reservation needs an
explicit operator decision. No live payment has been made, and provider acceptance
of a one-cent charge is still to be rehearsed. Use Lightning for the tiny payment;
do not assume the existing on-chain refund workflow can conveniently return it.

Refund/cancellation inspection: `/expeditions/admin/bookings/{booking-id}` is
read-only. `/admin/service-refunds` reviews existing refund/access/tax records;
`/admin/service-closure` reviews and verifies manual on-chain wallet payouts for
prepaid-service closure. They are not an expedition cancellation/refund action.
There is currently no admin button that cancels a trip, releases its allocation,
and sends a Bitcoin refund. The existing verified payout recorder never initiates
wallet transfers, and Lightning refund recording is not implemented. A trip
cancellation and Lightning refund workflow remains necessary before rehearsing
the complete one-cent cancellation/refund journey. Do not mark a trip refunded
without verified payout evidence or treat abandonment/forfeiture as a refund.

### Approved guest cancellation policy — October 23, 2026

This decision supersedes earlier draft cancellation discussions in this document.

Cancellation requests received before 12:00 a.m. on October 1, 2027, in America/Chicago time are eligible for a refund of passage payments received, less the initial 20% deposit, which is non-refundable. No refund is available for guest cancellations received at or after that cutoff. Send cancellation requests to inbox@btcpp.dev. Passenger substitutions are not permitted.

Bitcoin payments are valued in USD at their value on the day each payment was received. Refund entitlement is calculated from those recorded USD amounts, less the non-refundable deposit. A refund paid in Bitcoin is converted from the refundable USD amount at the exchange rate when the refund is sent; the number of satoshis returned may differ from the number originally paid.

Final guest payment remains due at the end of October 1, 2027, Texas time; this is distinct from the start-of-day refund cutoff. These terms describe voluntary cancellation, not overdue abandonment. Customer policy activation and the administrative cancellation/refund execution remain separate deployment requirements.

### Admin-controlled launch and policy publication — October 24, 2026

New purchases are controlled by the Open/Pause Antarctica checkout button on
`/admin`, backed by `site_features.antarctica_checkout`. Missing state defaults to
paused. Changes apply without a redeploy. The previous checkout environment flag
has been removed. Pausing does not interrupt existing installments or bookings.

Use `/admin/antarctica` to review and publish a new immutable terms/privacy
version. Prepared defaults are embedded in `internal/expedition/policy/` and used
by the public policy page until a version is published. Publication activates the
shared checkout snapshot and its America/Chicago lifecycle registration in one
transaction. Earlier accepted versions remain unchanged. Reusing a version for
different text or moving the effective date backward is rejected.

The admin screen checks tax/provider configuration, transactional mail, worker
settings, inventory sales windows and policy registration before opening sales.
The production app specification enables shared fulfillment/reconciliation and
the expedition lifecycle/waitlist workers. Provider credentials and approved tax
configuration must still be present on the deployed service; no tax exemption is
assumed. Configured does not prove successful provider communication: verify a
real purchase and receipt before announcing the launch.

The inventory initialization button inserts only missing category rows (18/3/4),
with one-hour unpaid holds and a January 1, 2027 00:00 UTC signup cutoff. Existing
rows and the configured staff reserve are preserved. It does not reset bookings.

### Conference-ticket tax treatment — October 24, 2026

Per organizer instruction, new Antarctica passage payments use the existing
conference-ticket default classification (`types.StripeTaxCodeNontaxable`,
`txcd_00000000`). No sales tax is added to passage. Merchandise remains separate.
This replaces the earlier requirement to configure an expedition-specific tax
code/mode; both configuration fields have been removed. This records the chosen
checkout treatment, not a determination of statutory tax liability.

Shared billing still obtains and retains a Stripe Tax calculation for payment
and refund accounting. A response containing nonzero tax is rejected rather than
silently changing the approved fare. Shared Stripe credentials remain necessary,
even though checkout is Bitcoin-only. Historical tax evidence is not rewritten.

### Optional invoice details — October 24, 2026

Passage checkout and installment payments no longer collect a billing address.
The conference-ticket treatment is recorded as organizer policy evidence in
`tax_quotes` (`sales_tax_provider=conference_ticket`, zero sales tax, no destination
or external quote ID). No Stripe Tax API call or Stripe credential is needed for
Bitcoin passage. Shared tax validation limits this path to this expedition, zero
tax, exact policy evidence and the payment expiry. Existing Stripe tax records and
refund processing remain unchanged; new policy-only payments do not fabricate a
Stripe calculation or tax transaction.

Paid receipts link to the existing owner-only invoice workflow at
`/dashboard/orders/{public-order-id}/invoice`. Invoice creation is optional;
buyers who need one can supply company/name/address/tax ID there. The ordinary
receipt remains available without those details. Issued invoice snapshots do not
change the payment amount, tax treatment or account profile.

Pausing new sales no longer blocks payments toward confirmed installment plans.

### Card checkout restored — October 24, 2026

Bitcoin and Card via Stripe are available side by side when shared Stripe
credentials are configured. The former expedition-only Stripe environment flag
has been removed. Stripe uses the existing card Checkout adapter, durable payment
attempts and verified settlement. Full card payments and all installments use the
standard fare; full Bitcoin payments retain the 5% discount in the totals. The
Bitcoin choice itself says “Lightning or on-chain.” The one-cent test remains
Bitcoin-only. Later installment orders retain the booking's original provider.
ACH and other delayed payment methods remain deferred.

Migration 143 copies the currently active policy when it contains the exact
Bitcoin-only launch sentence, replacing only that sentence in a new immutable
revision. Privacy text, historical acceptances and the lifecycle timezone are
preserved. Installations with custom wording must review their active policy in
admin; the migration does not rewrite arbitrary custom terms.

### Both beta payment methods — October 24, 2026

The verified nifty@btcpp.dev beta checkout now offers Bitcoin ($0.01) and
Stripe card payment ($0.50, the USD card minimum). Prices are fixed server-side
and displayed per method before payment. The single-berth, paid-in-full and
one-lifetime-claim rules remain unchanged; ordinary purchasers cannot receive
either beta fare.

### Admin cancellation — October 24, 2026

`/admin/antarctica/bookings` lists confirmed and closed bookings and supports
exact purchaser-email search. Global admins can review a whole booking and
confirm cancellation with a reason and CSRF-protected POST. A preview hash
prevents confirming stale payment/refund totals. Migration 147 stores an immutable
cancellation audit with actor, reason, accepted policy and settlement snapshot.

Cancellation releases all booking berths, closes unpaid orders, suppresses queued
installment reminders, and prevents future installment collection. It is distinct
from abandonment and does not send money or manufacture a refund ledger entry.
Unsettled provider attempts, unapplied payments and custom terms require review.
Before October 1, 2027 in America/Chicago, the refund entitlement is payments
received less the first installment/deposit (20% of the contracted fare for full
payment, rounded up to a cent); prior recorded refunds are deducted. At/after the
cutoff the entitlement is zero. Existing refund ledger entries remain intact.

Admin and buyer booking views show refund still due. Stripe refunds are issued
through Stripe and synchronized by shared billing; Bitcoin refunds use the
existing manual/shared refund workflow and recorded USD payment values. No
cancellation email or test-allowance reset is performed by this action.
