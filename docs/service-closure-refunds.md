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
durable allocation, ongoing reorganization monitoring and financial recording
remain to be implemented before this workflow is ready to use.

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
- Resolve refund timing and implement verified recording of admin wallet payouts;
  persist the confirmed rounding rule and
  closure timestamp used for an executable refund plan.
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
intervening refunds before authorizing payment; payout recording is unfinished.

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
USD/tax allocation for actual Bitcoin refunds still needs implementation before
payout execution. The shared ledger, refund worker and Insider receipts now allow
a zero USD-cent accounting amount when a positive Bitcoin refund is backed by known
original Bitcoin settlement evidence. Empty refunds and zero-cent card refunds
remain invalid. Both asset balances, immutable replays and per-order serialization
still apply. Counting prior refund records (rather than their USD sum) permits
multiple sub-cent refunds without treating the order as never refunded. Shared
order receipts also show the refunded satoshis when the USD amount is zero.
This does not calculate an exchange rate, refund tax or settle a wallet payout.

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
reserve an output or funds, record a refund, change access or send mail. The final
recording transaction must bind an approved recipient destination, revalidate the
review, enforce unique output allocation and update the shared financial ledger
atomically. USD/tax allocation (including sub-cent refunds), admin UI/commands for
destination approval, and reorg reconciliation after recording remain outstanding.

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
The website still needs its old campaign relay transport replaced with direct
mailer delivery, and verified manual payout recording remains unfinished.
No deployed integration or actual message/payment delivery is claimed.
