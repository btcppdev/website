# Permanent-closure prepaid refunds

Confirmed product rule: refund the unused proportion of the Bitcoin originally
paid, without converting the original USD price at the refund-date exchange rate.
This applies to permanent closure; ordinary cancellation does not issue a refund.
Confirmed rounding: round down to the nearest whole satoshi, capped at the
original payment. Use `FloorSatoshis` from the calculator for this rule.
Confirmed payout flow: an admin reviews the amounts and pays from our wallet;
shared Bitcoin++ commerce records verified payout evidence. V1 does not initiate
OpenNode withdrawals. This recording flow must be shared with Insider.
Confirmed v1 payout type: on-chain Bitcoin. An admin sends the payment from our
wallet; the shared system verifies and records the transaction output (transaction
ID and output index), destination, and satoshi amount against the reviewed refund.
Lightning payouts are outside v1. Output allocation must prevent the same payout
from being credited twice. Initial confirmation verification is implemented below;
durable output allocation and financial recording are now implemented in the shared
backend. The operator interface, closure orchestration, tax reporting and ongoing
reorganization monitoring remain before the workflow is ready to use.

`servicebilling.EstimateBitcoinClosureRefund` computes the unused proportion for
one funded service period. It returns both floor and ceiling satoshi amounts,
plus the original amount and timestamps. It does not select a rounding policy,
create a refund record, change access, or contact a provider. Rounding down is
the confirmed product rule. Timestamp precision is retained exactly so a later
whole-second policy can be applied explicitly rather than hidden in arithmetic.

The calculation uses actual elapsed time between the purchased start/end dates.
It caps the estimate at the original payment before/start of the period and returns
zero at/after expiry. Each separately paid upgrade must use its own funded window
and original payment. Never reuse the base period start for an upgrade refund.
No provider/network fee deduction is made by the calculator.

Tests cover 12/24/120 months, half-used and expired periods, leap-day boundaries,
mid-term upgrades, a fractional final satoshi, timezone equivalence, invalid inputs
and large-value/date overflow. The servicebilling race suite passed on September 25:
`/private/tmp/closure-refund-calculation-20260925.log`.

## Settlement evidence

The authenticated OpenNode `/v2/charge/{id}` lookup documents `price` in satoshis
and `fee` separately. The service adapter now freezes that price and any reported
fee, missing amount and overpayment in `service_payment_events.opennode_bitcoin`
within the settlement transaction. It preserves omitted optional fields as unknown.
USD order accounting remains separate. Paid observations with missing/invalid
Bitcoin prices or a nonzero missing amount cannot settle. Replays must match the
stored evidence; changes require reconciliation rather than overwriting it.
See the provider's [charge-info schema](https://developers.opennode.com/reference/charge-info).

This establishes the purchased charge price, not permission to pay out an amount.
Overpayments and prior refunds still require explicit review. Existing events
without Bitcoin evidence must not use a reconstructed exchange rate or silently
acquire a new refund basis. The existing immutable-event trigger protects this
new field as well.

Verification (September 25): the full fresh-database acceptance suite passed with
race detection and randomized order across `./cmd/... ./internal/... ./external/...`.
It used the current Alerts executable. The connected OpenNode test verifies missing
price rejection before settlement, concurrent settlement deduplication, persisted
price/fee/overpayment, conflicting replay rejection and database immutability.
Log: `/private/tmp/opennode-bitcoin-evidence-20260925.log`.
Database: `commerce_account_deletion_acceptance_20260925165525_46486`.
Provider calls used local fixtures; no real payment or refund was made.

## Outstanding execution requirements

- Rehearse captured Bitcoin evidence against a configured provider test charge,
  including overpayments, partial payments, provider fees and previous refunds.
  OpenNode documents separate overpayment
  fields in its [charge webhook guide](https://developers.opennode.com/docs/charges-webhooks);
  the authenticated [charge lookup](https://developers.opennode.com/reference/charge-info)
  must establish the refund basis. A callback alone is insufficient evidence.
- Resolve refund timing and expose the shared verified wallet-payout recorder
  through an authenticated operator flow.
- Verify the original-asset refund ledger and closure preview against provider
  refund evidence before issuing payouts. Preview output is not a payout instruction.
- Provide durable per-order approval/allocation, stop renewals, terminate access
  at closure, verify admin wallet payout evidence without duplicate allocation,
  reconcile ambiguous/manual payments and issue receipts.
- Rehearse provider-backed completion before treating closure refunds as shipped.

## Prior refunds and operator preview

`ReviewBitcoinClosureRefund` adds an immutable admin review of the calculated
amount, closure date, original payment basis and prior refunds. It recalculates
under the same order lock used by settlement and refunds, rejects stale expected
amounts, and records the authenticated global admin and reason. A review neither
reserves funds nor records a payout. Execution must revalidate the review against
intervening refunds before authorizing payment; recording now does so under lock.

September 30: the full fresh-database acceptance suite passes with race detection
and randomized test order, including admin review tests for Alerts and Insider,
immutable audit records, matching/conflicting replays and intervening refunds.
The person-merge manifest treats reviewer attribution as immutable. Log:
`/private/tmp/shared-closure-review-acceptance-20260930.log`.

`VerifiedServiceRefund.AmountSatoshis` records a verified Bitcoin refund alongside
its USD accounting amount. Unknown amounts remain null. The ledger serializes on
the order/payment lock, checks both asset balances, and rejects conflicting replays
or concurrent refunds that would exceed the original Bitcoin price. Missing or
conflicting original Bitcoin evidence, or an earlier refund with unknown satoshis,
prevents recording a further Bitcoin-denominated refund. No exchange rate fills
these gaps. The immutable refund trigger also protects the satoshi amount.

Inspect one prepaid order without sending a payment:

```sh
service-refunds -attempt PAYMENT_ATTEMPT_UUID -closure-at 2030-01-01T00:00:00Z
```

This reads the order's actual funded period and original charge price in a single
database snapshot. `remaining_refund_satoshis` uses the floor estimate minus earlier
verified refunds marked `permanent_closure`, never less than zero. Earlier goodwill
refunds need allocation review. Unknown Bitcoin amounts and absent/nonzero
overpayment evidence also produce review reasons and a null remaining amount.
Ordinary cancellation still does not issue refunds.

The preview neither reserves funds nor changes access, queues delivery, or calls
a payment provider. The manual payout workflow must revalidate and reserve under
the same order lock, freeze the reviewed closure time, and reconcile payout
evidence before recording completion. A transaction ID alone is not sufficient
to establish the paid amount and its allocation to an order.
The preview now includes `usd_allocation`, based on the original tax-inclusive
USD total and original Bitcoin payment. It floors the cumulative refunded share
of that USD total, then subtracts previously recorded refund cents. This preserves
sub-cent amounts across split payouts and allocates the exact original USD total
when all original satoshis have been returned. Arithmetic uses integers, with no
current exchange rate. The policy identifier is `original_usd_cumulative_floor_v1`.
Earlier USD amounts must match this cumulative allocation; otherwise the preview
flags the payment for review and cannot authorize a new refund. A changed order
total/tax amount that no longer matches the immutable quote also blocks the plan.
Admin reviews freeze this allocation as part of their complete basis.

September 30 verification: integer/race tests cover sub-cent credits, crossing a
cent boundary, full repayment, split-payment invariance, invalid prior allocations
and multiplication overflow. The full fresh-database acceptance suite passes with
race detection and randomized test order for both products:
`/private/tmp/bitcoin-refund-usd-allocation-20260930.log`.
The actual `service-refunds` binary was run against synthetic Insider evidence:
original payment 1,001 sats / $10.00, prior refund 200 sats / $1.99, next closure
refund 300 sats / $3.00, cumulative refund $4.99. No provider operation was issued.

`original_tax_cents` is the original included tax, not a completed tax credit.
Provider tax reporting still needs an idempotent reversal of the original Stripe
Tax transaction using the allocated gross refund amount. Stripe supports a
[tax-inclusive flat reversal amount](https://docs.stripe.com/api/tax/transactions/create_reversal).
Do not label the local allocation as a verified provider tax reversal or recompute
tax using current rates. Provider tax reversal remains unfinished. Recovery of the
original tax transaction now includes partially and fully refunded orders.

The recovery scheduler and operator review keep missing original tax transactions
visible after a refund. Finalization verifies the settled provider/reference and
records the original sale using the original quote and payment date, with the same
per-order idempotency key. Refunded orders skip access fulfillment. Paid Insider
orders use their existing fulfillment outbox, rather than the Alerts grant function;
once tax recording finishes, the absence of a local Alerts grant no longer causes
endless Insider polling. Existing refund statuses are preserved. Recording the
original sale is distinct from recording its later tax reversal.

October 1 verification: the full fresh-database acceptance suite passes with race
detection and randomized test order. New tests exercise paid, partially refunded
and fully refunded Alerts and Insider purchases through the actual recovery
selection query and finalization code with local Stripe responses. They verify
original totals/date/idempotency, one tax record, no new access after a refund,
unchanged order status, and no repeated selection after completed refund recovery.
Log: `/private/tmp/service-tax-refund-recovery-20261001.log`.

The shared ledger, refund worker and Insider receipts now allow
a zero USD-cent accounting amount when a positive Bitcoin refund is backed by known
original Bitcoin settlement evidence. Empty refunds and zero-cent card refunds
remain invalid. Both asset balances, immutable replays and per-order serialization
still apply. Counting prior refund records (rather than their USD sum) permits
multiple sub-cent refunds without treating the order as never refunded. Shared
order receipts also show the refunded satoshis when the USD amount is zero.
This does not refund tax or settle a wallet payout.

September 30: the complete fresh-database shared acceptance suite passes with
race detection and randomized order. Tests cover successive zero-cent Bitcoin
refunds, replay deduplication, satoshi over-refund rejection, database rejection of
empty refunds, worker payloads and visible satoshi amounts on shared receipts.
Log: `/private/tmp/shared-subcent-refund-acceptance-20260930.log`. Insider's full
race suite also passes with fresh PostgreSQL and the queue-only mailer fixture:
`/private/tmp/insider-subcent-refunds-20260930.log`.

Verification (September 25): the complete fresh-database acceptance suite passed
with race detection and randomized order after these changes. Tests cover
concurrent Bitcoin balance exhaustion even when USD capacity remains, replay
identity, immutable satoshi amounts, unknown prior amounts, overpayment review,
floor rounding, previous closure refunds and expired periods. Log:
`/private/tmp/bitcoin-refund-planning-20260925.log`; database:
`commerce_account_deletion_acceptance_20260925170252_47672`.
The actual CLI was also run against its synthetic 1,001-satoshi payment at the
period midpoint: 500 satoshis unused, minus 200 already refunded, leaves 300.
No real payment, refund, access change or message was issued.

## Read-only on-chain verification

`internal/bitcoinpayout` checks a payout through an explicitly configured, trusted
Bitcoin Core node. It verifies the network, initial-sync status, header/block
agreement and tip freshness; checks the address script and exact integer satoshi
amount at the specified output; then requires six confirmations at an unchanged
node tip. Six confirmations is the initial operational setting, not a claim of
irreversibility. A changing tip fails the observation and requires a retry.
Already-spent outputs remain verifiable. Coinbase outputs are rejected.

The RPC contract follows Core's
[getrawtransaction](https://bitcoincore.org/en/doc/30.0.0/rpc/rawtransactions/getrawtransaction/),
[validateaddress](https://bitcoincore.org/en/doc/30.0.0/rpc/util/validateaddress/),
[getblockheader](https://bitcoincore.org/en/doc/30.0.0/rpc/blockchain/getblockheader/)
and [getblockchaininfo](https://bitcoincore.org/en/doc/30.0.0/rpc/blockchain/getblockchaininfo/).
The containing block is required, so a transaction index is unnecessary; the node
must still have that block available (a pruned-away block cannot be verified).
RPC access should be restricted to these read-only methods. Use a local connection
or authenticated TLS; never configure a user-provided endpoint. Credentials come
from `BITCOIN_RPC_USER` and `BITCOIN_RPC_PASSWORD`, with `BITCOIN_RPC_URL` for the
endpoint. Redirects are refused and remote error text/credentials are not logged.

Build with `make build-ops`, then inspect an existing immutable review:

```sh
target/service-bitcoin-verify -review REVIEW_UUID -txid TRANSACTION_ID \
  -block CONTAINING_BLOCK_HASH -vout OUTPUT_INDEX
```

The command uses the shared commerce database and defaults to Bitcoin mainnet.
It rejects a review whose calculation has changed, uses its saved satoshi amount,
and prints the review, destination approval and node evidence with `recorded: false`.
The destination comes exclusively from the immutable approval; the command cannot
override it. This command does not
reserve an output or funds, record a refund, change access or send mail. The
recording function below binds the approved recipient, revalidates the review,
enforces unique output allocation and updates the shared financial ledger atomically.
The operator interface, provider tax reversals, small-payout policy and reorg
reconciliation after recording remain outstanding.

`ApproveBitcoinClosureDestination` freezes the address, node-validated script and
network, authenticated global admin, reason and timestamp against one immutable
review. The admin must confirm recipient ownership separately; an on-chain address
check cannot establish that. Production rejects test-network destinations. The
function checks authorization before RPC and again under lock, then recalculates
the review under the same order lock as settlements/refunds. A stale amount cannot
receive a new approval. Matching retries return the original audit record; a
changed address, admin or reason is rejected. A new address needs a new review.
An existing approval is historical evidence and must be revalidated before payout
recording; it does not reserve funds or make a stale review executable. Account
merges cannot reassign its approver. No public write route is enabled yet.

The September 30 destination-approval acceptance run passed the complete race
suite with fresh PostgreSQL and a real regtest node enabled. Both Alerts and
Insider tests cover unauthorized approval, invalid addresses, production/testnet
separation, competing destination approvals, immutable matching/conflicting
replays and stale reviews after intervening refunds. Log:
`/private/tmp/bitcoin-destination-acceptance-20260930.log`.

Small payout policy is pending: normal wallet/relay rules impose dust thresholds
depending on the output script and configured fee policy (see
[Core's dust calculation](https://github.com/bitcoin/bitcoin/blob/v31.1/src/policy/policy.cpp)).
A floor-rounded obligation may fall below that threshold. Do not silently waive
it, deduct fees from it, or change its rounded amount. Whether bitcoin++ adds a
separately recorded top-up or holds it for combination needs a product decision.

September 30 verification: the race suite passes against an isolated Bitcoin Core
31.1 regtest node, with no external peers or real coins. It tests an actual payment,
one-versus-six confirmations, incorrect amount/destination, spending the payout,
invalidating its block and reconsidering it. HTTP fixtures also cover stale/wrong
network nodes, missing fields, tip changes, exact decimal arithmetic and failure
redaction. Log: `/private/tmp/bitcoin-payout-verification-20260930.log`.
Run it with `BITCOINPAYOUT_REGTEST_BINARY=/path/to/bitcoind go test -race
./internal/bitcoinpayout`; without that variable the real-node test is skipped.

The full fresh-database acceptance suite also passes with race detection,
randomized test order and the regtest binary enabled:
`/private/tmp/bitcoin-payout-acceptance-20260930.log`. The operator binary builds
and its `-help` command runs without opening the database or contacting a node.

## Atomic wallet-payout recording

`RecordBitcoinClosurePayout` receives authenticated admin identity and a review ID,
transaction ID, output index and containing block. It takes the recipient and
amount from the saved review/approval, verifies through the configured Core node,
then locks the order/payment and rechecks admin authority and the complete review
basis. Node evidence older than one minute is rejected before a new record commits.
The network must match the approval; production cannot record test-network payouts.

The transaction inserts the normal shared refund and delivery job together with
immutable node evidence and an output allocation. `(chain, txid, vout)` is unique
across services, and each review can record one exact payout. Other outputs in the
same transaction can fund other reviewed refunds. Every error rolls back the
allocation, ledger, job and order status together. Matching retries return the
original record even if node RPC is temporarily unavailable; changed replay inputs
are rejected. A replay is historical evidence, not a fresh confirmation check.

The refund reference is `onchain:{chain}:{txid}:{vout}`. Its `opennode` provider
links to the original payment; it does not claim OpenNode sent the wallet refund.
The refund timestamp records when payout verification succeeded. The complete node
observation, including containing block and observed tip, remains in the audit.
No signing, broadcast or withdrawal method exists in this path. The queued refund
still needs the reviewed product-access decision. Service closure/renewal shutdown,
provider rehearsal and ongoing reorg handling remain unfinished operations.
The authenticated admin route now exposes destination approval and verified payout recording, as described below.

Verification: the full fresh-database race suite passes, with the isolated Core
regtest verifier enabled: `/private/tmp/bitcoin-payout-booking-acceptance-20260930.log`.
PostgreSQL tests cover both products, matching concurrent retries, stale reviews
and removed admin authority during RPC, immutable evidence, cross-service output
reuse and injected failure after ledger insertion. Additional race tests prove one
winner when different orders compete for an output and two successful allocations
for distinct outputs of one transaction:
`/private/tmp/bitcoin-payout-booking-concurrency-20260930.log`.
Booking tests use local RPC fixtures; the independent node verifier uses actual
regtest transactions. No real payment, provider reversal or message was sent.

## Reuse with Insider

Alerts and Insider use the same central service orders, settlement evidence,
refund ledger and closure-preview function. A PostgreSQL race test runs the same
floor-rounding/prior-refund/expiry cases for `alerts.repository-slots` and
`insider.membership.annual`; both passed on September 25
(`/private/tmp/shared-insider-closure-20260925.log`). Product access handling and
receipt branding remain separate from these shared financial facts.

September 29 integration: shared dispatch now carries
`adjustment_amount_satoshis` and original settlement satoshis from the verified
OpenNode evidence. Insider's `codex/shared-refund-receipts` branch stores those
adjustments, rejects changed replays and renders the amount in email/account
receipts. It also now honors the existing shared `access_action` decision for
membership/article refunds; the prior website checkout rejected that field.
Receipt storage is presentation data, not a second provider refund ledger.

The shared getters, handlers and servicebilling race suites pass. Insider's
focused memory/PostgreSQL/HTTP receipt tests pass, including retain/revoke,
Bitcoin balance limits, email output, account receipt output and replay conflicts.
Logs: `/private/tmp/shared-bitcoin-receipts-20260929.log` and
`/private/tmp/insider-bitcoin-receipts-focused-20260929.log`.
The initial full Insider race run hit a pointer-check panic in its Nostr signing
dependency. Pinning upstream `v0.0.0-20260928115942-58e4c715304e` resolved it; the
full Insider race suite now passes with PostgreSQL and pointer checks enabled
(`/private/tmp/insider-full-race-20260929.log`). The local Nix package build also
passes. Insider website `main` now contains this integration at `d73c83b`, brought
in via rebase. The `codex/insider-commerce-converged` backend worktree was also
rebased onto this shared implementation on September 29. Its full fresh-database
acceptance suite passes with race detection and randomized order:
`/private/tmp/insider-backend-convergence-20260929.log`. The original `a1669b33`
tip remains on `archive/insider-commerce-before-shared-20260929`.
At that checkpoint, the website still needed its old campaign relay replaced with direct
mailer delivery at that checkpoint. Direct mailer delivery and shared payout
recording have since been implemented; operator and provider launch work remains.
No deployed integration or actual message/payment delivery is claimed.

## Bitcoin refund tax recording

Verified OpenNode-funded refunds enqueue a shared Stripe Tax adjustment in the
same transaction as the refund ledger entry. Alerts and Insider use this queue.
The existing `SERVICE_PAYMENT_RECONCILIATION_ENABLED` worker processes it when a
Stripe key is configured. It waits for the original tax transaction, preserves
refund order and submits a partial, tax-inclusive flat-amount reversal. A refund
with zero allocated USD cents is recorded locally without a Stripe request.

Retries retain the same reference, idempotency key and first-submission timestamp.
After 23 hours of unresolved submission ambiguity, the job requires review rather
than resubmission. Mismatched provider responses also require review. Completed
records are immutable and retain financial evidence without customer addresses.
`service-refunds` exposes tax state, review reason and reversal reference; its
unscoped view includes unfinished tax jobs even after receipt delivery completes.

This is tax reporting for an already recorded refund, not a provider payout.
Stripe test-mode rehearsal remains required before launch. No live reversal
or customer payment was executed during development.

Validation: the fresh-database acceptance suite passed with race detection and
randomized test order (`/private/tmp/service-refund-tax-acceptance-fixed-20261001.log`).
An additional PostgreSQL race test verifies ordered cumulative reversals
(`/private/tmp/service-refund-tax-order-20261001.log`). Provider responses are
local fixtures; these checks do not substitute for the Stripe test-mode rehearsal.

## Admin recovery of an existing tax reversal

`/admin/service-refunds`, linked from Service payments, lists unresolved refund
and tax jobs. An attempt filter includes completed records. For a tax job in
review, a global admin enters an existing Stripe Tax reversal ID and a reason.
The server retrieves that transaction using its own Stripe credentials and
reuses the worker's original-reference, amount, currency, tax and cumulative
balance validations. This flow never creates a new provider reversal or payout.

Authorization is checked before retrieval and locked/rechecked before writing.
A successful recovery atomically records the tax adjustment and an immutable
admin audit, preserving the original submission clock. A mismatch or database
failure leaves the review unchanged. Exact repeated submissions are accepted;
changed audit data is rejected. The form requires a session CSRF token.

The fresh-database race acceptance suite passed:
`/private/tmp/service-refund-tax-review-20261001.log`. Tests cover both products,
lost provider acknowledgment, revoked authority during retrieval, changed replays,
wrong amounts and rollback after a staged financial record. Handler tests check
anonymous/scoped-role denial, missing/wrong/duplicate CSRF tokens and escaped
provider references (`/private/tmp/service-refund-review-handlers-20261001.log`).
Provider responses remain fixtures; real Stripe test-mode verification and a
browser/operator rehearsal are still launch work.

Refund review now has cursor pagination in both the admin page and operator CLI.
The PostgreSQL test walks 102 refunds, verifies a scope-bound cursor and confirms
that resolving earlier jobs cannot skip later records. Getters and handler race
suites pass (`/private/tmp/service-refund-pagination-getters-20261001.log` and
`/private/tmp/service-refund-pagination-handlers-20261001.log`). Browser execution
was unavailable in this session; visual/operator rehearsal remains outstanding.

## Operational monitoring

The shared collector exports `btcpp_web_service_refund_tax_jobs` with only the
bounded `state` label (`pending`, `accepted`, `review`) and
`btcpp_web_service_refund_tax_oldest_pending_timestamp_seconds` (zero when empty).
These cover both Alerts and Insider. No account, refund, provider transaction or
address identifiers appear in these metrics.

Load `ops/service-refund-tax-alerts.yml` into the monitoring deployment alongside
existing refund-delivery rules. It warns after ten minutes in review, or when a
pending adjustment is older than fifteen minutes for five minutes. Both require
a successful metrics refresh within the last five minutes. A stalled job can be
waiting for original tax recording or an earlier refund; inspect
`/admin/service-refunds` before taking action. Review recovery retrieves an
existing Stripe transaction and must not be replaced by blind resubmission.

PostgreSQL aggregate and collector privacy race tests pass
(`/private/tmp/service-refund-tax-metrics-20261001.log`), as do
`promtool test rules ops/service-refund-tax-alerts.test.yml` checks for firing,
resolution, empty queues, stale data and failed collection. Rules have not been
installed on metrics.btcpp.dev or routed to an operator destination yet.

## Calculation review page

Global admins can open `/admin/service-closure` from Service payments or from an
OpenNode refund's review row. Enter a settled payment attempt and the permanent
closure timestamp, including its timezone. The preview displays the product,
order, previous Bitcoin refunds, remaining satoshis and original USD accounting
adjustment. Unknown or inconsistent evidence appears as blocking review reasons.

For a positive, unblocked amount, the admin confirms the preview and supplies a
reason. The POST uses the authenticated admin identity, session CSRF token,
expected satoshi amount and a stable review UUID. The shared getter recalculates
under the order lock before inserting the immutable review. Successful submission
redirects to the saved review, which remains read-only and explicitly requires
revalidation before later destination approval and payout recording.

Handler and template race checks pass in
`/private/tmp/service-closure-admin-20261001.log` and
`/private/tmp/service-closure-admin-template-20261001.log`. Tests cover role and
CSRF rejection, UTC normalization, unsupported timestamp precision, blocked and
saved states, amount rendering and escaped admin notes. Destination approval and payout-recording forms have since been added; visual/browser rehearsal remains open.

## Destination approval and payout recording UI

A saved `/admin/service-closure?review=...` page now loads the immutable address
approval and payout record. Before offering another action, it compares the saved
basis with the current plan. Stale/unavailable plans and missing node configuration
hide write forms; the shared getters repeat all authoritative checks on POST.

The destination form requires recipient-ownership confirmation and an approval
reason. Its address is validated through the operator-configured Bitcoin Core
connection. The payout form accepts only transaction ID, containing block and
output index, with explicit confirmation. Address and amount come exclusively from
the saved approval and reviewed basis. Successful verification records the payout
and shared refund atomically, then redirects to immutable evidence with a link to
refund delivery/tax status. It does not send funds or decide product access.

Configure `BITCOIN_RPC_URL`, `BITCOIN_RPC_USER` and `BITCOIN_RPC_PASSWORD` in the
web service environment. `BITCOIN_RPC_CHAIN` defaults to `main`; other networks are
accepted only with an explicitly non-production application configuration. RPC
credentials and network are never accepted from request fields. POST verification
has a 45-second overall timeout. Use a trusted node with the necessary blocks;
verification requires six confirmations and a synced chain.

Handler race tests pass in `/private/tmp/service-closure-payout-ui-final-20261001.log`.
They cover stale/missing-config/recorded UI states, escaped addresses, confirmation
requirements, production network enforcement and credential requirements. Existing
shared getter tests cover concurrency, stale basis and atomic rollback. No live
payout, network verification or provider operation was performed in this UI pass.
Browser/operator rehearsal, ongoing reorg reconciliation and closure orchestration
remain outstanding.

## Connected admin HTTP verification

`TestClosureAdminHTTPPostgres` exercises the full HTTP flow for both Alerts and
Insider with real session cookies, the admin authorization wrapper, rendered form
fields and PostgreSQL. It starts with an authenticated settled Bitcoin purchase,
previews the floor-rounded amount, records the calculation review, approves the
recipient address, verifies the payout through a local Bitcoin Core RPC fixture,
and checks the saved financial record. Repeated payout submission returns the
same record without another node call or a duplicate refund. The recorded USD
allocation and satoshi amount are checked against the original purchase.

The focused test and randomized full handler race suite pass:
`/private/tmp/service-closure-admin-http-20261001.log` and
`/private/tmp/service-closure-admin-http-handlers-20261001.log`. Navigation chrome
is stubbed in this HTTP fixture; separate template tests load the real navigation.
This proves HTTP/session/form/database wiring, not browser rendering or a live
payment. Node responses are local fixtures, complementing the separate real
Bitcoin Core regtest verifier tests. Browser/operator rehearsal remains open.

## Refund access decision UI

`/admin/service-refunds` now exposes the existing shared retain/revoke decision
for a recorded refund that has no decision yet. There is no preselected action:
the global admin chooses whether to retain the original paid access or end access
funded by that purchase, enters a reason and confirms. This neither initiates a
refund nor cancels future renewals. Permanent-closure access changes still need to
be coordinated with the closure date and renewal shutdown.

The server uses authenticated identity rather than form-supplied actor data,
locks and verifies a non-deleted global admin, and preserves the existing immutable
and idempotent decision semantics. Saved decisions show their actor and reason
instead of another form. Tax recovery has a separate action and retains its
existing provider verification. Both actions require session CSRF protection.

The connected closure HTTP test now follows the recorded payout into refund review,
records retain for Alerts and revoke for Insider, checks matching retries and
rejects a changed decision. It also verifies that a forged actor field cannot
change attribution. The shared getter rejects deleted administrators before
looking up a refund. Browser rendering and product-receiver deployment checks
remain separate launch work.

Verification: the connected HTTP and handler tests passed in the fresh-database
acceptance run `/private/tmp/service-refund-access-ui-acceptance-20261001.log`.
That run exposed a test-fixture ordering error (granting a role after deletion),
which was corrected to grant first, then delete. The full randomized getters race
suite then passed against that acceptance database in
`/private/tmp/service-refund-access-ui-getters-fixed-20261001.log`. Explicit-choice
form tests also pass. Run database-sharing packages sequentially via the acceptance
script; an earlier concurrent run had service-terms fixture interference.

## Recorded-payout follow-up

With `SERVICE_PAYMENT_RECONCILIATION_ENABLED=true` and valid Bitcoin Core RPC
configuration, the shared worker rechecks recorded payout outputs. It checks up
to 20 due records per batch, using leased jobs so multiple replicas can cooperate.
Successful checks are due again in six hours; failures become `unverified` and are
retried after fifteen minutes. A reclaimed lease prevents an old worker from
replacing a newer observation. Existing payouts are included automatically.

The check repeats verification of the saved transaction, original containing
block, exact recipient output, amount and required confirmations. The closure
review page displays the latest result and its time separately from immutable
original payout evidence. An unavailable/pruned node, changed chain tip, missing
block or reorg can all prevent verification. `unverified` is an investigation
signal, not a finding that the recipient was unpaid. Neither the ledger nor
product access changes, and the worker never creates a replacement payment.
If a transaction moves to another block, the admin reconciliation workflow below
verifies and approves its new inclusion while retaining original evidence.

Metrics include `btcpp_web_bitcoin_payout_checks{state="pending|confirmed|unverified"}`
and `btcpp_web_bitcoin_payout_oldest_due_timestamp_seconds`. Missing check jobs
count as pending, so a disabled worker is visible. Load
`ops/bitcoin-payout-alerts.yml` for sustained-unverified and overdue-check warnings;
these require fresh, successful metric collection and export no payout identifiers.

The fresh-database race acceptance suite passed in
`/private/tmp/bitcoin-payout-followup-20261001.log`. Follow-up tests exercise the
worker with local RPC fixtures, expired/replaced leases, failed verification,
recovery and immutable financial evidence. Promtool rule tests pass for warning
thresholds, resolution, stale data and failed collection. Installation on the
monitoring service and live-node/operator rehearsal remain deployment work.

## Reconcile a different containing block

The saved payout page accepts a new containing block hash, explicit confirmation
and an admin reason. Its transaction ID, output index, recipient script and amount
are fixed by the existing payout. Bitcoin Core must verify that exact output in
the proposed block with at least six confirmations on the configured chain.

A successful reconciliation inserts an immutable record linking the previous and
new block, evidence, actor and reason. Only the follow-up target is updated; the
original payout proof and refund remain unchanged. Subsequent checks use the newly
approved block. The page displays the monitored block and latest admin attribution.
An existing worker lease is invalidated so a late result for the old block cannot
replace the accepted observation. A concurrent reconciliation that changed the
baseline requires fresh review. Matching request IDs replay without new RPC;
changed requests conflict. Admin authority is checked before RPC and locked and
checked again before recording the result.

This operation cannot change the transaction/output or create a second refund.
It does not resolve a permanently missing payment by issuing a replacement.
Node failures remain `unverified` and require investigation, not automatic payment.

Verification: the fresh-database race acceptance suite passed in
`/private/tmp/bitcoin-payout-reconcile-20261001.log`. Connected HTTP/database
checks and follow-up target tests passed in
`/private/tmp/bitcoin-payout-reconcile-connected-20261001.log`, including both
products, idempotent replay, changed replay rejection, stale-worker rejection,
revoked authority during RPC and preserving one original refund. UI state tests
passed in `/private/tmp/bitcoin-payout-reconcile-ui-20261001.log`. RPC responses
are local fixtures; live-node/operator and browser rehearsal remain open.

## Service-wide Bitcoin inventory

The read-only operator command can enumerate settled Bitcoin payment attempts for
one service without requiring each payment ID up front:

```sh
service-refunds -closure-service alerts -closure-at 2030-01-01T00:00:00Z
service-refunds -closure-service insider -closure-at 2030-01-01T00:00:00Z
# Continue with the same service and closure timestamp:
service-refunds -closure-service alerts -closure-at 2030-01-01T00:00:00Z -after NEXT_CURSOR
```

Each page contains at most 100 plans and an optional `next` cursor. This inventory
includes expired and already-refunded payments (which may owe zero), prepaid
upgrades, and review-required payments. Missing Bitcoin evidence and inconsistent
settlement/order records remain visible with review reasons. Calculation/database
errors fail the page rather than silently omitting a payment. Product scoping uses
its stored `alerts.` or `insider.` prefix. Stripe payments are not included.

Every page reads a consistent, read-only database snapshot; pagination uses stable
payment IDs and binds its cursor to the service and closure timestamp. Pages are
not one cross-page snapshot. Stop new purchases, resolve in-flight settlements and
rerun the complete inventory before treating it as the final list; concurrent
settlements or refunds can change the result. This command does not stop checkout,
cancel renewals, end access, create reviews, reserve amounts or send funds. Each
positive obligation still needs the existing admin review, approved destination
and verified on-chain payout flow. Unknown/review-required amounts must be resolved
before totaling obligations; a missing amount is not zero.

PostgreSQL tests cover Alerts and Insider inclusion, bounded pagination, cursor
scope/date rejection, missing-evidence visibility and absence of review creation.
Production service-closure orchestration remains separate outstanding work.
