# Conference merch register

Admin setup: `/{conf}/admin/merch-pos`, linked from the conference admin dashboard.
Mobile register: `/{conf}/merch/sell`.

## Set up an event

1. Create products and active size/color variants in the existing merch catalog.
2. Configure the event's ticket currency. The register uses that currency for
   secondary price estimates (EUR for Berlin). Conflicting ticket-tier currencies
   must be resolved before opening sales.
3. On the POS setup page, enable each variant, enter its final price in whole sats,
   and transfer physical stock to the event. Use **Save items** above or below
   the table to save all rows together. If any row fails validation, no changes
   are applied and entered values remain available to correct. Positive transfers deduct central
   online stock; negative transfers return unreserved event stock. Repeated form
   submissions do not repeat a transfer. Prices are final totals; this first
   version does not calculate tax or issue tax invoices.
4. Open the register using the event-level switch. Setup can happen before the
   conference begins. Closing sales prevents new carts from checking out but does
   not invalidate invoices already issued.
5. Share the register URL and the existing volunteer ticket check-in PIN. No
   customer account, email address, or shipping address is required.

## Taking a payment

Select quantities, review the sats total, and tap Charge. The register submits an
integer amount with **no currency field** to OpenNode. OpenNode's `/v1/rates`
provides the muted local estimate; it never changes the configured sats price.
Each sale snapshots the rate and event ticket currency. An unavailable rate
blocks new checkout instead of displaying an invented local price.

Only the Lightning invoice is presented, not the on-chain address or hosted
checkout. The requested charge TTL is ten minutes. Invoice expiration displayed
on the phone comes from OpenNode's Lightning invoice response.

Wait for **Payment received**, then tap **Items handed over · Next customer**.
Refreshing an invoice page retains the sale. Recent sales lets another volunteer
resume it. A shared PIN identifies a register session, not a named volunteer.
After payment, **Email receipt (optional)** sends an itemized sats receipt using the
online shop’s mail service, with the saved purchase-time local estimate. It remains
available after handover from the sale screen. No mailing-list signup is created. A receipt address matching an existing verified
account email links the paid purchase to that account, including secondary email
addresses. Purchases appear on the dashboard and `/dashboard/orders`, with an
account-only receipt view. Unknown addresses do not create accounts. A resend
cannot move an already-linked purchase to another account. Account matching happens
when the valid receipt action is submitted, even if mail delivery later fails.
The confirmation means queued for delivery, not confirmed inbox delivery.

Handover is recorded separately from payment so a paid order is not handed out
again by accident. Locking the register also clears that browser's check-in PIN.

## Inventory, records and recovery

Event allocations, pending reservations, sales, price snapshots, and handovers
are stored in the `conference_pos_*` tables. This is a sats-denominated ledger;
POS sales are available on the event setup/register pages, not in the fiat-cent
online-shop order list. Online products, variants and central stock are shared.
The admin page exposes the recent stock/price/payment audit history. Refunds,
discounts and additional payment methods are outside this version.

Each checkout request has a persistent unique key and reserves stock in a
transaction. Concurrent registers cannot reserve the same last item. Provider
creation is attempted once per sale: ambiguous errors leave it in `creating` and
retain stock. Do not make a second invoice for the same customer while resolving
this state. In the admin page, recover it by entering the charge ID found in
OpenNode; the server verifies its order ID and exact amount. If no charge exists,
an admin can explicitly attest to that check and cancel the unconfirmed sale
(after two minutes), releasing stock once.

A signed webhook at `/callback/opennode-pos/{sale-id}` triggers an authenticated
charge lookup. Order ID, charge ID and exact sats must match. Browser polling
and a separate maintenance worker also reconcile payments. The webhook body is
never accepted as proof of payment. Expiration returns stock only after provider
confirmation; closing a tab or the local countdown reaching zero does not release
it. A late paid notification after stock was released becomes `review`, requiring
admin/provider reconciliation, rather than silently overselling stock.

## Validation and rollout

Migrations: `094_conference_pos.sql` and `095_conference_pos_accounts.sql`. Apply it through the normal migration runner
before starting the new code. OpenNode uses the existing environment credentials.
No real charges or production inventory are needed for the automated tests.

Run targeted unit and integration tests with a **disposable migrated database**:

```sh
BTCPP_POSTGRES_SMOKE=1 DATABASE_URL=postgres://localhost/disposable_pos \
  go test ./internal/handlers ./external/getters -run TestPOS -count=1
```

Tests intentionally create fixture records and are not for production databases.
They cover exact-sats payloads, PIN/CSRF/admin gates, duplicate requests, concurrent
stock reservations, price tampering, signed callback verification, expiry,
cancellation, late payments and handover. Set `POS_BROWSER_PREVIEW=1` when running
`TestPOSFlow` to serve a local mock-payment preview on 127.0.0.1:19433; the test
prints its URL and PIN. Stable shortcuts are `/dev26/admin` for the conference dashboard,
`/dev26/admin/merch-pos` for
admin setup and `/dev26/merch/sell` for the register; they redirect to the current
isolated demo event. These aliases and the demo admin login exist only in the
test preview server, never in production. The demo includes Core, Libbit and LibreRelay hats with
repository product photos, event prices, and a sold-out variant. `/preview/pay` simulates payment in that test server only.

Before enabling a real event, verify its configured stock and final prices, then
complete a controlled Lightning payment and confirm the received amount and
handover record. This has not been performed by the automated preview.
