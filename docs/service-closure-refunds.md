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

## Small-output top-ups

bitcoin++ funds any top-up needed to make a small on-chain refund payable. The
admin destination approval now records the top-up in whole satoshis, its reason
and the total recipient output before payment. Choose the amount using the sending
wallet's output requirements and recipient address; the application does not assume
one universal dust threshold. Zero means no top-up. A positive refund remains
required; zero-refund plans do not become payout obligations.

The saved approval displays refund owed plus top-up as the exact output to send.
Wallet transaction fees are additional bitcoin++ expenses, never deducted from the
recipient output. Verification requires that exact approved total. The immutable
payout record stores the top-up separately; the refund ledger and USD/tax allocation
contain only the refund owed. Top-ups can exceed the original customer payment
without exceeding that payment's refundable balance, because they are not refunds
of customer funds. Changed approval replays are rejected. The recorded output,
including its top-up, remains subject to six-confirmation and periodic/new-block
verification. This is still manual wallet payment; no funds are sent by the app.

This schema change is part of unshipped migration 128; use a fresh development
acceptance database rather than applying it to an already-migrated local fixture.

October 1 verification: the full shared acceptance suite passed with race detection
on a fresh PostgreSQL schema, including both products and existing zero-top-up
flows. Connected admin HTTP tests additionally passed with top-up approval,
negative/fractional/duplicate-field rejection, payout recording and new-block
reconciliation. Local logs: `/private/tmp/service-closure-topups-20261001.log` and
`/private/tmp/service-closure-topup-http-20261001.log`. No real payout was sent.

## Stop new sales before closure

`service-sales -service alerts` (or `insider`) inspects the recorded sales-stop
state without changing it. The first operational closure step is an explicit,
permanent stop authorized by an active global admin:

```sh
service-sales -service alerts -actor ADMIN_UUID -reason 'Approved permanent closure' -confirm-stop-sales
```

Run this only after a reviewed closure decision. The record is immutable, with
actor, reason and stop time; matching retries return the same decision. There is
no automatic reopen. New prepaid purchases, payment attempts and both services'
Stripe checkout reservations are blocked under a shared database lock. Checkout
openers recheck after provider I/O and persist any returned provider reference for
reconciliation before withholding the link. Provider webhook settlement, refund
recording and operational recovery remain enabled.

This is the first closure step, not complete shutdown orchestration. Existing
payment links may still work at the provider, and an already-started provider
request can race the local stop. Inventory and explicitly expire/cancel those
provider sessions, stop recurring renewals, reconcile in-flight payments, then
rerun the Bitcoin obligation inventory. Coordinate the product's access cutoff
separately. No closure date or provider cancellation is implied by this command.


October 1 sales-stop verification: the full shared race acceptance suite passed
on fresh schema 128. After final changes, getter and handler race suites passed
again, including service isolation, active-admin checks, immutable/idempotent
closure decisions, blocked checkout paths, and settlement of an in-flight payment
after sales stopped. The operator binary builds; its read-only status command was
checked against the local fixture. Logs: `/private/tmp/service-sales-closure-20261001.log`
and `/private/tmp/service-sales-final-20261001.log`. No service was closed outside
the disposable test database; no provider sessions were changed.

The sales stop also covers existing Alerts Stripe subscriptions' purchase-increase
path: preview creation, durable customer approval, new submission claims and
hosted invoice payment-link handoffs. These cannot be used to bypass a stopped
checkout. Read-only invoice recovery and recording an already-paid upgrade remain
active; cancellation and decrease paths are unchanged. A provider request already
in flight at the stop still requires reconciliation and provider-side cleanup.
The getter race suite passes, including stopped-sales upgrade rejection followed
by recovery/accounting of an already-paid invoice. Verification log:
`/private/tmp/service-sales-upgrades-final-20261001.log`.


### Expire unpaid Stripe subscription checkouts

With `SERVICE_STRIPE_RECONCILIATION_ENABLED=true`, the existing recovery worker
now expires unbound, unpaid subscription checkout sessions for a service whose
sales have stopped. It verifies provider ownership/product metadata and expiration
before recording local closure. A lost expiration response leaves the local row
open until a later read confirms expiration; the stable provider idempotency key
is reused. A completed session falls through to normal settlement recovery, and
an already-bound subscription is never expired by this path.

Alerts retains its existing lost-session-reference discovery. Stopped Insider
sales now also make missing-reference reservations eligible for discovery through
a bounded authenticated Stripe listing. No match remains retryable, because an
in-flight creation may still finish. Discovery never creates a replacement session.
The shared worker is wired with retrieval, listing and expiration capabilities.

This runs on the existing reconciliation schedule, not synchronously in the
sales-stop command. It covers the Alerts and Insider recurring-checkout tables;
one-off Insider purchase sessions, OpenNode payment links, upgrade invoices and
recurring subscription cancellation still require closure handling. Follow-up must
confirm every affected provider object, not infer completion from the sales flag.
Stripe permits expiration of open sessions; see
https://docs.stripe.com/api/checkout/sessions/expire.

October 1 verification: PostgreSQL cases for both services cover lost creation
references, lost expiration responses, ownership mismatch and completed-session
handoff. After fixing fixture cleanup, the full race acceptance suite passed on
a fresh database: `/private/tmp/service-sales-expiration-acceptance-20261001.log`.
No Stripe account or live session was contacted by these fixture tests.


### Stop recurring renewals

For a stopped service, the Stripe reconciliation worker now refreshes verified
subscription state and reuses the shared durable cancellation flow to schedule
cancellation at the current period end. Existing paid periods remain unchanged.
The command itself does not contact Stripe, but its persisted sales-stop decision
now authorizes this background operation when reconciliation is enabled.

A deterministic intent ID binds each closure cancellation to its product, checkout
and period end. Existing pending cancellation instructions are reconciled first.
An ambiguous response remains retryable and a later verified cancellation confirms
it without repeating the provider mutation. Provider changes discovered after a
confirmed cancellation, expired/mismatched period instructions and unresolved
review-state instructions require operator review; they do not silently authorize
another mutation. Pending upgrade/decrease conflicts retain their existing guards
and also need resolution. The worker reports failures; complete closure still
requires checking the affected subscriptions and resolving every exception.

This prevents subsequent renewal for successfully confirmed cancellations. It does
not shorten access to an earlier service shutdown date, void outstanding upgrade
invoices or cover one-off provider sessions/OpenNode links. Those remain separate
closure steps. Do not treat the local sales-stop timestamp as proof that all
provider billing has stopped.

October 1 renewal-shutdown verification: both products' existing PostgreSQL/Stripe
fixture flows now exercise sales-stop cancellation, a lost provider response,
retry confirmation without duplicate mutation, unchanged paid entitlements and
operator-review rejection after provider-side reactivation. The full shared race
acceptance suite passed on a fresh database:
`/private/tmp/service-closure-renewals-acceptance-20261001.log`.
No live subscription was changed.

### Known one-off Stripe checkout sessions

The shared Stripe worker also inspects unsettled one-off service payment attempts
with a known `cs_` session reference after sales stop. It expires only a matching
payment-mode session with the saved attempt/order metadata, exact original amount,
USD currency, expected live/test mode, and unpaid status without a payment intent,
invoice or subscription. A verified expiration creates immutable normalized
`service_payment_checkout_expirations` evidence. It does not settle, refund or
cancel the accounting order, and later verified payment evidence remains recordable.

A lost expiration response leaves no closure record; retry reads Stripe before
considering another expiration. Completed/paid sessions or sessions with payment
activity are left for settlement/review, not cancelled. Missing session references
still require discovery/operator recovery. The worker claims bounded batches on a
15-minute durable retry schedule and skips attempts with recorded expiration.
This does not expire OpenNode payment addresses or void upgrade invoices.

October 1 one-off checkout verification: the full shared race acceptance suite
passed on a fresh database (`/private/tmp/service-oneoff-closure-20261001.log`).
After adding the original quote/order-total consistency check, the targeted
PostgreSQL race test passed again
(`/private/tmp/service-oneoff-closure-final-20261001.log`). Tests cover lost-response
recovery, immutable evidence, paid-session preservation and unchanged accounting
status. These use local provider fixtures; live Stripe behavior remains a launch
rehearsal requirement.

### Outstanding OpenNode charges

OpenNode's published [create-charge API](https://developers.opennode.com/reference/create-charge)
supports a checkout lifetime of 10 to 4,320 minutes. Our service adapter already
requests `ttl: 10`. The published API index reviewed on October 1, 2026 did not
expose a charge-cancellation operation; do not invent a cancellation call or mark
an issued charge cancelled locally. This is a documentation finding, not provider
confirmation that previously issued payment destinations can be disabled.

Sales stop blocks new charges and further checkout-link access through bitcoin++.
It cannot retract a hosted link or on-chain address the customer already has.
Continue authenticated charge reconciliation and webhook handling after sales
stop, including after an `expired` observation. The [webhook documentation](https://developers.opennode.com/docs/charges-webhooks)
distinguishes expiration from paid settlement; elapsed TTL is not evidence that
no money was received. An authenticated later paid observation must still preserve
the original Bitcoin amount and enter the refund inventory where applicable.

Sales stop is separate from the actual access cutoff: existing fulfillment still
records the purchased term. Coordinate that cutoff before permanent closure.
Provider confirmation about old payment destinations, unresolved charge creation,
underpayments and late payments remains an operational closure requirement. Do
not declare the refund inventory final merely because ten minutes have elapsed.

Local verification: `TestOpenNodeServiceLookupPostgres` now runs both active-sales
and stopped-sales flows. It verifies checkout access is blocked after sales stop,
non-final statuses do not settle, and a later paid observation records one payment
and the original purchased term despite concurrent retries. The PostgreSQL race
test passed (`/private/tmp/service-opennode-closure-20261001.log`). Provider responses
are local HTTP fixtures, not a live OpenNode cancellation or settlement rehearsal.

### Unsettled payment inventory

Run `service-sales -service alerts -unsettled-payments` (or `-service insider`)
to list up to 100 unresolved prepaid/one-off payment attempts from local accounting.
Pass the returned `next` token as `-after TOKEN` until it is absent. The token is
bound to the selected service. This mode is read-only and cannot be combined with
`-confirm-stop-sales`.

Each item includes the attempt ID, public order ID, product, provider, saved
reference when present, creation time and a review reason. Missing references
remain visible. Settled attempts and attempts with recorded verified Stripe
checkout expiration are excluded. No expiry is inferred from elapsed time.

The report does not query providers or cover subscription checkout intents,
upgrade invoices, renewal cancellations or refund obligations. An empty page
therefore does not establish that closure is complete. Pagination is a changing
local view; restart from the beginning after concurrent settlements or new
attempts. Use the separate Bitcoin closure report for settled refund obligations.

The PostgreSQL race test covers pagination, cross-service cursor rejection,
missing references, known Stripe/OpenNode attempts and exclusion of settled or
verified-expired attempts. It passed along with the CLI package check; log:
`/private/tmp/service-unsettled-report-20261001.log`.

## Recover missing one-off Stripe checkout references

The stopped-service Stripe worker now includes attempts whose provider reference
was lost before it could be saved. It lists authenticated Stripe Checkout sessions
within the saved attempt/quote time window, using at most 100 pages of 100 sessions
and a 30-second deadline. It accepts only a complete scan with one matching attempt,
order, payment mode, currency, total and live/test environment. It saves the
reference using the existing conflict-checked binding, then applies the existing
provider retrieval and unpaid-session expiration checks.

An empty, truncated, failed or ambiguous scan does not mark an attempt closed and
does not create a replacement checkout. A recovered paid session stays available
for settlement/review; it is never expired by this path. The durable scheduling
continues retrying unresolved attempts, and they remain in the unsettled inventory.
This does not complete closure orchestration or prove that all provider activity
has stopped. Access cutoff, payment-bearing sessions, upgrade invoices and OpenNode
handling still need their separate closure steps.

Local validation: the servicestripe race suite, PostgreSQL stopped-one-off tests
(including missing-reference worker selection, lost expiration responses and
paid settlement), and shared worker dispatch tests pass. Logs:
`/private/tmp/service-oneoff-lookup-unit.log`,
`/private/tmp/service-oneoff-lookup-worker.log`, and
`/private/tmp/service-oneoff-lookup-dispatch.log`. These use provider fixtures;
no live Stripe session was queried or expired. No migration was added.

## Cancel unpaid pending Alerts upgrades during closure

After an audited Alerts sales stop, upgrade recovery can now void the reviewed
open invoice for an attempted pending upgrade. It requires the saved invoice
identity, original subscription quantity/period, exact pending quantity change,
latest-invoice match and absence of a conflicting schedule. It uses a stable
quote-based Stripe idempotency key and refuses paid or partially paid invoices.
The existing account lock and submission-lease check fence concurrent local work.

After the void action it reads the invoice and subscription again. The existing
immutable expiration evidence and local expired state are recorded only after
Stripe reports the invoice void, the original quantity retained, and no pending
update or schedule. A lost response leaves the local instruction pending; retry
observes the completed provider action without another void. Late successful
payments remain eligible for normal settlement and refund review.

This follows Stripe's documented pending-update cancellation:
https://docs.stripe.com/billing/subscriptions/pending-updates
Voiding requires the audited service sales stop. Background execution uses the
existing Stripe worker opt-in; authenticated recovery uses the same coordinator.
Unsubmitted reservations, ambiguous provider state, conflicting schedules, and
final access cutoff remain separate closure work.

Adapter race tests and PostgreSQL upgrade reservation/payment tests pass, including
no action during active sales or a live submission lease, paid/partial/conflicting
provider state, lost responses, durable retry and late settlement. Shared handler
recovery-dispatch checks also pass. Logs:
`/private/tmp/service-closure-upgrade-unit.log` and
`/private/tmp/service-closure-upgrade-postgres.log`. No migration was added and no
real invoice was voided.
