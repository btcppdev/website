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

| Berth | Total | Each 20% payment | 75% bitcoin contribution, USD value |
| --- | ---: | ---: | ---: |
| Shared | $16,429 | $3,285.80 | $12,321.75 |
| Three-person bunk | $15,599 | $3,119.80 | $11,699.25 |
| Four-person bunk | $14,429 | $2,885.80 | $10,821.75 |

The payment plan is five equal payments: 20% on reservation, then January 1,
April 1, July 1, and October 1, 2027. The bitcoin contribution deadline is
December 1, 2026 (updated from November 15). Settlement is October 1, 2027;
any shortfall is due October 15, 2027. Failure to pay forfeits the spot.

Prices are currently draft constants in `internal/handlers/antarctica.go`, calculated
in integer cents. They are not ticket records and do not enable checkout.

## Booking work still required

The upfront reservation amount is 25% of the full ticket price; the remaining
75% of the fare is reconciled October 1, 2027. The draft assumes the upfront
reservation cost comes out of the initial 75% contribution, leaving bitcoin
initially worth 50% of the fare. For a shared berth: $12,321.75 received,
$4,107.25 used upfront, $8,214.50 initially retained.

The intended tax deduction relates to bitcoin appreciation from receipt to
settlement. Before implementing it, confirm the receiving entity’s tax jurisdiction
and classification, ownership/custody, whether settlement entails a sale, the basis
of the actual coins disposed of, valuation source/time, and refund method. Do not calculate tax as a percentage of
bitcoin basis or promise a particular return.

Actual booking needs persisted berth inventory and reservations, installment
obligations, payment reconciliation, reminders, settlement records, and an explicit
forfeiture workflow. The existing single-payment checkout does not implement this
plan. This draft intentionally creates no invoices or custody transactions.

Confirm departure port, route, fare inclusions, final parka artwork,
patch dimensions and prices. The interactive parka is a concept illustration with
four proposed placements; it is not live sponsor inventory. Sponsor inquiries
currently link to the existing `/sponsor` page.

## Checks

`go test ./internal/handlers -run '^TestAntarctica(Fares|Template)$' -count=1`

Browser checks cover responsive layout, local font/image loading, fare selection,
installment amount updates, and parka front/back and patch selection.

## Parka sponsorship pricing proposal — September 30, 2026

Organizer estimates approximately 30 attendees/parkas, with the garments featured
in expedition photos. Route, garment design, and whether attendees
keep the parkas remain unconfirmed. These are proposed asking prices, not approved
offers, guaranteed audience valuations, or live inventory.

| Placement across all expedition parkas | Proposed USD price | Slots |
| --- | ---: | ---: |
| Left sleeve | $2,500 | 1 |
| Right sleeve | $2,500 | 1 |
| Front chest | $5,000 | 1 |
| Back panel | $7,500 | 1 |

Total potential sponsorship revenue: $17,500, before garment and fulfillment costs.
Reserve the other chest position for bitcoin++. The current illustration has two
chest sponsor concepts and one sleeve concept; revise it to match the final inventory
when the pricing/layout is selected. Do not present the existing mockup as final.

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

## Receiving entity and tax modeling

Organizer confirms the receiving business is a single-member Texas LLC with US-tax-resident
ownership, currently not an S corporation, with an S election planned for 2027.
Assume default disregarded-entity federal treatment only if no corporate election
is already in place. The effective S election date, owner marginal tax rate and
applicable state tax residency remain to be confirmed. Do not apply the illustrative
C-corporation 21% rate.

An S corporation generally passes gains to its shareholders rather than paying a
flat federal corporate income tax on them. The customer terms must explicitly address
whether any deduction includes owner-level taxes and how that amount is calculated;
do not describe an arbitrary reserve as actual business tax owed. The accountant
should review the 2026 receipt/2027 sale and election transition, opening basis,
advance-payment income timing, refunds, and any state or other applicable taxes.

Sources: [IRS LLC classifications](https://www.irs.gov/faqs/small-business-self-employed-other-business/entities),
[IRS S corporations](https://www.irs.gov/businesses/small-businesses-self-employed/s-corporations),
[IRS digital asset transactions](https://www.irs.gov/individuals/international-taxpayers/frequently-asked-questions-on-digital-asset-transactions),
[Texas franchise tax](https://comptroller.texas.gov/taxes/franchise/).
