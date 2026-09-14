# CLN payment provider architecture

Architecture proposal, 2026-09-14. Repository review only; no node connectivity,
production configuration, or payment execution was tested.

## Recommendation

Launch community hackathon prize pools first as the CLN proof of concept.
Initial pool funding is Lightning-only through CLN: BOLT12 offers and LNURL-pay
BOLT11 invoices, with BIP353 address discovery. Pools have no OpenNode fallback
and accept neither card nor on-chain funding at initial launch.

The launch foundation is pool provisioning, stable payment attribution, a durable
receipt/contribution ledger, reconnecting monitoring, and public confirmed totals.
Use actual contributed amounts rather than fiat-pegged shop quotes. Manual audited
payouts remain the proposed initial approach, pending agreement on award policy.

Commerce integration is a later, separate phase. The checkout/provider abstraction,
OpenNode fallback policy, shop pricing changes, and POS migration documented below
are future design notes, not prerequisites for the prize-pool proof of concept.

Reuse the Go BOLT-8/Commando transport maintained in `../accts/internal/lnsocket`,
after the cancellation and I/O hardening below. Authenticate the node by its public
key and authorize RPCs with a dedicated restricted rune. This route does not require
CLNRest or a separate HTTPS endpoint. CLNRest remains an optional future transport
behind the same RPC interface; no extra gateway service is required initially.

## Reusing the accts transport and handling disconnects

Local review of `../accts` at `4f5beae` found `internal/lnsocket` (the adapter referred
to as lnconnect), copied from `github.com/niftynei/lnsocket/go` at `a24b3399ff83` with
MIT attribution and subsequent local lifecycle fixes. `internal/syncer/syncer.go`
creates a new client for each RPC, connects/initializes, executes a method, and
closes it. There is no automatic reconnect/replay supervisor in the transport.

Existing code/tests cover handshake deadlines, cancellation during handshake,
reader shutdown before reuse, interrupted RPCs, concurrent response correlation,
and BOLT-8 vectors. The accts README reports race-test validation; this architecture
pass inspected code and tests but did not rerun them or connect to the live node.

Reuse the locally fixed version, not just the older upstream revision. Go's
`internal` import restriction prevents importing this package from btcpp-web.
Preferred ownership is a small shared versioned module used by both projects;
a pinned attributed copy with its tests is an interim option. Neither a sibling
directory `replace` nor importing the whole accounting service is appropriate for
the production build. Update Go dependency pins and the Nix dependency hash when
the module is introduced.

Before checkout use, harden and test:

- `RpcContext` writes before selecting on cancellation, with no RPC write deadline.
  A stalled peer can therefore exceed the requested timeout. Enforce bounded I/O
  and close the dedicated connection on cancellation across init/write/read.
- Bound aggregate response buffers and pending calls, clean partial response state
  after cancellation, and validate incoming init/ping messages. The accts transport
  review identifies these as remaining hardening work.
- Return typed failure stages: no invoice request sent, definitive RPC rejection,
  or outcome unknown after a possible write. Partial writes and response decode
  failures are ambiguous. Context cancellation does not undo node-side execution.
- Use separate short-lived checkout connections and a supervised payment-listener
  connection. Serialize connection lifecycle changes, drain readers on close, and
  reconnect the listener with capped exponential backoff and jitter from its durable
  cursor. Do not replay invoice creation simply because a connection dropped.

Proposed commerce policy: **OpenNode** or **Prefer CLN, fall back to OpenNode**.
Use a short configurable end-to-end CLN creation budget (initial target 3–5 seconds,
to be measured) and a circuit breaker with bounded recovery probes. A known outage
routes new attempts directly to OpenNode rather than making every buyer wait.

| Failure point | Behavior |
| --- | --- |
| Circuit open, dial/handshake/init fails before invoice RPC is sent | Persist the CLN failure and automatically create one OpenNode attempt |
| Definitive invoice RPC rejection with no created invoice | Record the failure; fall back; alert on rune/configuration errors |
| Request may have reached CLN, but response is lost | Mark outcome unknown; recover by deterministic label before creating another invoice |
| Recovery finds matching unpaid invoice | Save and use that CLN invoice |
| Recovery finds paid invoice | Settle the original payment; never ask the buyer to pay again |
| Recovery cannot establish the outcome | Show a bounded processing/review state for this attempt; route other new checkouts to OpenNode |
| Buyer already received a CLN invoice | Continue reconciling it; do not silently issue an OpenNode replacement |
| OpenNode also fails | Keep the cart and recoverable attempt state; report checkout unavailable |

A missing label immediately after a timeout is not by itself proof that the old RPC
cannot still execute. Switching an ambiguous attempt requires proving the prior
request has finished without an invoice, or confirming the invoice is unpayable
and unpaid under a reviewed cancellation/expiry procedure. Keep all attempts and
references, even those superseded. Guard fallback with a database transition so
concurrent retries cannot create multiple OpenNode charges. Never log rune secrets.

Automatic fallback applies to commerce invoice creation. A published BOLT12 offer
remains bound to its CLN node and cannot transparently fail over to OpenNode. LNURL
pool fallback would need separate provider/invoice and pool-ledger support; leave
it explicit rather than redirecting a wallet to hosted checkout. Continue receiving
and reconciling pool contributions when commerce has fallen back to OpenNode.

## Current integration map

| Area | Current behavior | Required change |
| --- | --- | --- |
| Tickets and ticket/merch bundles | `internal/handlers/handlers.go` calls `OpenNodeInit`; checkout metadata is sent to OpenNode and recovered in `OpenNodeCallback` | Persist checkout intent locally before calling a provider; extract shared ticket/bundle settlement |
| Shop | `internal/handlers/merch.go:ShopOpenNodeInit` redirects to OpenNode hosted checkout | Provider selection and a local CLN invoice page |
| Paid merchandise | `external/getters/merch.go:MarkShopOrderPaid` records provider/ID, converts inventory, and queues merch-admin notices | Reuse the transaction; validate payment against the stored attempt first |
| Conference POS | `internal/handlers/pos.go` and `external/getters/pos_opennode.go` already use local invoice UI, integer sats, and reconciliation | Adapt creation/status to provider interface; persist provider on sales or their attempts; replace OpenNode-only FX dependency |
| Pricing | `internal/handlers/merch_price.go` rounds display to thousands; OpenNode shop request still sends fiat total | Persist the actual sats quote and make the invoice and confirmation page agree |
| Refunds | `adminRefundShopOrder` automates Stripe; other providers use recorded manual refunds | Keep manual CLN refunds initially; record sats/payment reference alongside existing fiat accounting |
| Configuration | `internal/envconfig/envconfig.go` loads OpenNode endpoint/key | Add CLN connection secrets, node identity/network validation, and a persisted provider selector |

The existing callback verifies the OpenNode signature and fetches the charge to
confirm paid status. Keep that endpoint and its credentials available after CLN
is selected. Do not route old callbacks through whichever provider is currently
selected.

## Provider boundary and data

Introduce a small internal payment package with provider-neutral create/lookup
operations, with optional offer-management capabilities. A result contains the provider reference, status, expected and received
amounts in msat, expiry, and either a hosted checkout URL or BOLT11 invoice.
The CLN adapter owns RPC/REST behavior; the OpenNode adapter owns its API and
callback authentication. Neither adapter issues tickets or sends receipts.

Persist payment attempts before external calls, including:

- Payment purpose: ticket, shop, mixed bundle, POS, or prize contribution; associated
  checkout/order/sale or pool. A contribution need not have a cart or buyer account.
- Immutable buyer, ticket quantity/type, discounts, affiliate basis, newsletter
  consent, and accounting totals needed for fulfillment.
- Provider and provider-account/node identity, unique attempt ID and CLN label,
  provider reference/payment hash, invoice/hosted URL, and expiry.
- Base currency/amount, quoted integer sats, FX source/time/rate, rounding
  allocation, received msat, and payment time. Do not store sats in cents fields.
- Creation/reconciliation state, fulfillment state, timestamps, and last error.

Use unique constraints for provider account/reference and attempt labels. Snapshot
the selected provider when creating an attempt. Prevent multiple active attempts
for a checkout. Multiple payments for one checkout must be recorded and sent to
review without issuing a second set of goods.

For CLN, use a deterministic opaque label such as `btcpp:<attempt UUID>`. A timed-out
creation request is ambiguous: query that label before retrying. A duplicate label
requires verifying the existing invoice hash/amount/expiry against the stored
attempt, not blindly accepting it. Do not switch providers automatically after
an ambiguous request or free its reserved stock.

## Receiving and fulfilling payments

1. Persist intent and quote; create the invoice with explicit amount and expiry.
2. Save its reference and show the buyer the invoice page.
3. A background worker calls `waitanyinvoice` with a persisted pay-index cursor.
   Use a bounded long-poll timeout and reconnect/backoff; this is not per-browser
   polling of the node.
4. Match the event to the stored node, label, and payment hash; verify paid status
   and sufficient received msat. Record a durable payment event and advance the
   cursor in the same database transaction. Persist unrelated-node-invoice skips
   too, so they do not stall the stream. Never advance past an unrecorded event.
   For native offer payments, resolve `local_offer_id` from the invoice lookup to
   its registered pool and ingest the contribution even without a preexisting
   website attempt. CLN can issue those invoices without a website request.
5. Process settlement idempotently from that durable record. Use a transactional
   outbox for receipts, ticket mail/subscriptions, and other retryable side effects.
   Existing merch-admin notices already have an atomic queue to reuse.
6. Reconcile outstanding attempts by label on startup and periodically to repair
   missed creation responses and detect expired invoices. Coordinate workers using
   a database lease; scope cursors by node/account and handle identity changes.

Extract OpenNode's existing ticket, discount, affiliate, tax, and receipt behavior
into this shared settlement path with regression tests. Currently some email and
subscription work follows ticket creation with a replay guard; copying that code
alone would not make failures in those later effects recoverable.

Expiry needs particular attention: `ExpirePendingShopOrders` currently cancels
orders by wall clock, while `MarkShopOrderPaid` rejects cancelled orders. A payment
received before invoice expiry but observed after cleanup must remain recorded
and recoverable. Reconcile before releasing stock; unresolved node outages should
enter review with bounded operational handling. If stock was already released,
record the payment and resolve fulfillment/refund explicitly rather than lose the
payment or silently oversell.

## Buyer experience and pricing

The CLN page should use the site's current styling with a scannable QR, exact sats,
copy invoice, open-wallet link, expiry countdown, and paid/expired/review states.
Use a session-bound or unguessable capability URL for guest buyers; status routes
must not expose buyer details. Stream app-side status with SSE backed by durable
state and reconnect recovery. Never expose the rune or call CLN from the browser.

Refresh rates when moving the cart to checkout, then freeze the final quote for
the invoice lifetime. For merch, keep the requested nearest-1,000-sat display and
charge consistent: derive unit prices from that quote, add quantities, convert
shipping/tax with a documented allocation rule, and display the exact payable
total before invoice creation. Keep fiat accounting and any rounding difference
explicit. Enforce a positive minimum for nonzero charges. Ticket currencies and
POS's existing integer-sat prices need explicit unit handling.

OpenNode currently chooses the final Bitcoin conversion from the fiat amount.
Exact shared sats across providers would require changing its web checkout requests
to explicit sats, as POS already does, and checking that hosted-checkout, tax, and
accounting behavior remains correct. Do this as a separately tested pricing change.

Initial CLN checkout is Lightning-only. On-chain payment requires a separate
address/confirmation/underpayment/late-payment design; it should not be implied
by reusing OpenNode's existing Bitcoin button. BOLT12 prize-pool support is a
part of the first prize-pool phase; checkout integration follows separately.

## Community hackathon prize pools

### Existing tracker and clnurl integration (local review)

The user clarified that the reference is Afterglow, located in `../bolt12-tracker`.
Its offer-ID matching and persisted payment-hash deduplication informed the
PostgreSQL receipt/credit ledger used here. Address domain: `{event}@zap.btcplusplus.dev`.
Nostr zaps are separate from dedicated prize-pool payments.

Use the existing modified `../clnurl` for LNURL discovery and invoice creation,
rather than building a second LNURL implementation in btcpp-web. Its dynamic
`clnurl-add/update/remove/list` RPCs persist endpoint descriptions in CLN datastore.
Provision a pool's endpoint through a separate admin capability and route the
well-known address URL to clnurl with a controlled canonical host.

Use clnurl unchanged for the proof of concept. It already lets us configure each
endpoint description. For ordinary LNURL payments, CLN stores the advertised JSON
metadata as the invoice description, including `text/plain` and the structured
`text/identifier` value such as `berlin26@zap.btcplusplus.dev`.

The monitor should parse that JSON and map the exact full identifier to a registered
pool on this node. A human-readable description may end with the address, but no
new clnurl label format or configuration field is required. Match registered
description snapshots as supporting evidence, retaining prior versions for invoices
created before description changes. Ambiguous or conflicting metadata goes to review.
This is a routing convention for verified received funds, not proof of donor identity
or immutable endpoint provenance: the public callback accepts supplied metadata.

Attribution priority: registered node/offer ID for BOLT12, exact registered LNURL
identifier from invoice metadata for ordinary clnurl payments, then an explicitly
reviewed historical-import rule. Keep retired mappings so endpoint renames and offer
rotation cannot move old credits. Do not attribute by a loose free-text suffix alone.

Zap invoices contain a Nostr request instead of ordinary LNURL metadata. Do not
claim that the configured description appears in every zap invoice. Zap attribution
is not required for the ordinary LNURL/BOLT12 proof of concept; any such receipts
remain unallocated for review unless a separately verified mapping is available.
No clnurl extension is a prerequisite for launch.

### BIP353 registration through the existing Go library

Local `../bip353-config` has module and Git remote
`github.com/btcppdev/bip353-config` (MIT, authored by niftynei). Reuse its
`Manager.Put/Get/Delete` and Cloudflare provider for publishing pool addresses.
It manages DNS TXT records; it does not create CLN offers or clnurl endpoints.
`Put` is idempotent for content/TTL, preserves unrelated TXT records, rejects
ambiguous multiple payment records, and checks Cloudflare's active DNSSEC status.
That API status check does not independently validate the registrar DS chain;
verify public DNSSEC resolution as a separate deployment/readiness check.

Pool provisioning should persist a stable pool ID first, create/recover its CLN
offer and clnurl endpoint, persist their mappings, then publish
`bitcoin:?lno=<offer>` with this library. Track each provisioning step durably and
retry toward the same desired state. Show the address as ready only after its
DNS and HTTPS paths have been verified. Serialize registration across app replicas
with a database lease: the library's mutex protects only one Manager instance.

Keep the Cloudflare token zone-scoped in the provisioning worker. CLN runes cannot
authorize DNS changes. Terraform owns zone/delegation and stable infrastructure;
this library exclusively owns the dynamic BIP353 TXT records. Confirm the chosen
payment domain has a working DNSSEC chain and is in the configured Cloudflare zone
before rollout. The precise address domain remains a deployment choice.

### Monitoring credentials and receipt transactions

Use one dedicated observer rune for the node's incoming payment stream. Its initial
method allowlist can be expressed as:

```json
[["method=getinfo", "method=waitanyinvoice", "method=listinvoices"]]
```

Validate it with `checkrune` and live read-only calls on the deployed CLN version.
Add `listoffers`/`decode` only if the monitor actually needs discovery/verification;
prefer a database destination registry. `readonly` shorthand alone does not cover
`waitanyinvoice`. Keep endpoint/offer provisioning, invoice creation, and payouts
under separate credentials. No observer access to `pay`, `withdraw`, or general
datastore mutation is needed; its cursor lives in PostgreSQL.

This observer can see invoices across the node, including unrelated descriptions
and payment preimages in RPC responses. Persist only necessary accounting fields
and do not log raw responses. A rune filters allowed RPCs/parameters, not returned
rows by description. If node-wide visibility is unacceptable, use scoped per-offer
lookups and a purpose-built filtered clnurl monitoring RPC; description-based rune
restrictions cannot implement that boundary.

Persist immutable receipt facts with `UNIQUE(node_id, payment_hash)`, and give each
receipt at most one pool-credit allocation (unique receipt ID). In one transaction:
insert/upsert the paid receipt without changing established facts, create a credit
only if it is new, update any cached pool total only from that inserted credit,
enqueue any new-credit event, and advance the stream cursor. Use integer msat.
Mismatched repeated facts go to review rather than overwriting credited amounts.
Prefer deriving totals from the ledger until a cached total is actually necessary.

The cursor is an efficiency aid, not the deduplication key. A crash before commit
replays the event; a crash after commit finds the receipt/credit already present.
Worker leases plus database constraints cover concurrent monitors. Retain paid but
unattributed receipts for later mapping, so advancing the cursor cannot discard a
contribution whose endpoint registration arrived late. Correct allocations through
audited ledger adjustments rather than editing history.

On first setup or recovery, backfill retained paid invoices through the same ingest
path and resume with an overlap/catch-up pass; never initialize to the latest cursor
and skip history. Replaying the entire available history must leave totals unchanged.
Register historical offers and endpoint mappings before importing. Invoice deletion
on CLN limits recoverable history, so retain/back up the application ledger and
coordinate invoice retention with the node operator. Full node/database restore
requires reconciliation before assuming cursor continuity.

Tests must cover repeat events, restart on either side of commit, two workers,
backfill overlapping the live stream, multiple protocols hitting the same receipt,
conflicting metadata, unrecognized zap receipts, endpoint rename/removal, mapping added after
receipt ingestion, conflicting attribution, and replay without duplicate notices.

User requirement: each event can collect community contributions through a custom
Lightning address and a reusable BOLT12 offer. Suggested example:
`berlin26@zap.btcplusplus.dev`, with `₿berlin26@zap.btcplusplus.dev` for BIP353 display.
These are proposed names, not provisioned addresses.

Two discovery mechanisms can lead to the same pool:

- LNURL address: clnurl via HTTPS `/.well-known/lnurlp/berlin26` serves LNURL-pay
  metadata and a callback. The callback validates the requested integer msat
  amount and creates a fresh CLN BOLT11 invoice bound to the pool. Include the
  required identifier metadata and implement applicable invoice metadata binding;
  rate-limit public invoice creation and return protocol errors for closed pools.
- BIP353: a DNSSEC-signed TXT record at
  `berlin26.user._bitcoin-payment.zap.btcplusplus.dev` contains
  `bitcoin:?lno=<pool BOLT12 offer>`. Publish one logical TXT record, splitting
  long values into DNS character strings as needed. DNSSEC delegation and chain
  validation are deployment requirements, not just an extra TXT record.
- Direct BOLT12: the pool page also exposes the reusable offer. CLN's `offer`
  supports contributor-chosen amounts and automatic invoice issuance. Persist
  the node/offer ID mapping before publishing any address or QR.

BIP353 and LNURL are distinct protocols with similar address presentation. Support
both explicitly and test target wallets; do not assume every wallet supports both
or falls back between them. BIP353 DNS publication belongs in the infrastructure
workflow; the public web process should not need broad DNS credentials.

Add `prize_pools` linked to event/competition, a destination registry with immutable
pool/node/offer associations, and a contribution/disbursement ledger. Deduplicate
incoming funds by node plus payment hash across all entry points. Record actual
received msat; do not round community contributions to the shop's 1,000-sat price
increments. Public totals come from confirmed ledger entries, never the node's
overall balance or invoice creation count. Donor identity/display is optional and
explicitly opt-in; a payment or payer note alone does not authenticate an account.

Existing hackathon schema already supports `pooled` prizes, `pool_percentage`,
`pool_url`, and prize distributions (`024_hackathon_schema.sql` and
`026_hackathon_operations.sql`). Link those prizes to a real pool with a foreign
key; keep external pool URLs supported. At finalization, snapshot the distributable
balance, winner allocations, team splits, fee policy, and rounding remainder.
Distinguish received, reserved for awards, paid out, refunded, and available funds.
These are accounting allocations on one operator-controlled node, not separate
wallets or trustless escrow.

Pool lifecycle: draft, open, closing, finalized. Stop new LNURL invoices and disable
the offer when closing, but continue reconciling already issued invoices. Account
for DNS caching and a declared treatment for contributions received after the award
cutoff. Keep address/offer history permanently associated with its original pool;
never reuse an old event address for a new event. An address does not itself enforce
the pool's payout or closing policy.

The checkout provider toggle does not disable CLN prize pools: pools remain pinned
to their CLN node independently of whether commerce uses OpenNode. Offer management
needs its own restricted capability (`offer`, offer lookup, enable/disable as needed).
Winning teams can initially be paid manually with audited payment references. Any
later automatic payout worker requires separate spending authorization, approval
records, fee limits, and idempotent payment reconciliation; the public receiving
service should retain no spending permission.

Prize-pool launch tests include both protocols crediting one pool; native BOLT12 payment
while the website is offline; repeated listener events; offer rotation; DNSSEC and
TXT splitting; closing with invoices outstanding; exact contribution totals and
award allocation; late contributions; switching commerce back to OpenNode while
pool contributions continue; and duplicate payout prevention.

## Toggle and production configuration

Proposed global-admin setting: `Bitcoin payments: OpenNode / Prefer CLN with OpenNode fallback`.
Show connection readiness and save an audit event. Keep private credentials in
deployment secrets. An environment default of `opennode` preserves existing installs;
the database setting allows changing providers without a deploy. Use an explicit
scope if POS is migrated later so the UI does not imply that it has switched too.

Proposed Commando deployment inputs: node host/port, dedicated rune, expected node
public key, expected network, and optionally a stable client identity if the rune
is restricted to one. Confirm the CLN version, Commando availability, reachability,
and rune restrictions. TLS CA configuration is only needed for an optional REST
transport; BOLT-8 authenticates against the pinned node key.
Use a receive-only application rune permitting just required methods such as
`invoice`, `listinvoices`, `waitanyinvoice`, and `getinfo`. Verify restrictions on
the deployed version; invoice-read access may cover other invoices on that node.
The website does not need `pay`, `withdraw`, channel management, or seed material.

Operational readiness includes private connectivity from the web deployment,
TLS verification, receive liquidity and routing, worker/node availability metrics,
oldest unreconciled payment, fulfillment queue lag, and a documented restore and
reconciliation procedure. Keep OpenNode configured for automatic safe fallback.
Switching back affects new attempts while CLN's listener continues draining old ones.

## Implementation sequence and validation

1. Confirm node/Commando/BOLT12 configuration and payment domain; reuse and harden
   the accts transport with dedicated monitoring/provisioning credentials.
2. Add pool/destination registry, unique paid-receipt records, transactional pool
   credits, and durable monitoring cursors. Link existing pooled hackathon prizes.
3. Configure existing clnurl endpoint descriptions and register exact LNURL identifiers
   for pool attribution. Provision reusable BOLT12 offers, then publish BIP353
   records using bip353-config. Persist steps so provisioning retries are safe.
4. Add reconnecting observation, historical backfill, and reconciliation for both
   offer payments and clnurl invoices, including those created outside btcpp-web.
5. Add event pool pages with funding instructions, confirmed totals, last successful
   synchronization, and clear funding/monitoring status. Show received funds and
   available prize balance distinctly when allocations/payouts begin.
6. Validate regtest flows and failure/restart scenarios, then conduct an explicitly
   authorized small live pool contribution. Reconcile it against the node before
   opening the proof of concept publicly.

During a monitoring disconnect, retain the last confirmed total and show delayed
updates. A monitor outage does not necessarily stop CLN/clnurl from accepting funds;
catch up on reconnect. If invoice issuance/node availability fails, show funding
temporarily unavailable and let wallets retry CLN. Never redirect pool funding to
OpenNode or another rail. A stale monitor alone must not imply the node is offline.

Pool launch gates: no duplicate credits on repeated full-history replay; crash on
either side of receipt/cursor commit; concurrent monitors; CLN disconnect/recovery;
native BOLT12 payment with btcpp-web offline; correct ordinary LNURL attribution;
conflicting or unknown metadata routed to review; DNSSEC and mobile wallet verification;
accurate msat totals; and explicit closing/late-contribution handling.

Later commerce phase: shared checkout settlement and attempts, provider selection,
safe OpenNode fallback, exact sats quotes, local invoice checkout, and POS migration.
The following commerce tests apply to that later phase:

Required failure tests: process restart before/after invoice creation; duplicate
callbacks/events; two app instances; switch with unpaid and paid-but-unprocessed
invoices; node outage; stale FX; expired invoice; paid-before-expiry observed after
cleanup; duplicate payment attempts; partial bundle settlement; failed mail/tax work;
wrong node/network; amount mismatch; guest status access; mobile QR usability.
Also test stalled init and writes, partial writes, response loss after successful
invoice creation, failure before send, concurrent fallback requests, circuit opening
and recovery, OpenNode outage during fallback, and listener reconnection while new
checkouts are using OpenNode. Verify restricted live Commando compatibility before
enabling CLN; accounting Bookkeeper access alone does not establish invoice access.

For the first phase, the main work is reliable attribution, durable accumulation,
provisioning, and recovery. Node access/version, address domain, and pool award and
closing policies remain inputs. On-chain support is explicitly outside initial scope.

## Upstream references

- [CLNRest configuration and authentication](https://docs.corelightning.org/docs/rest)
- [Invoice creation, labels, amounts, and expiry](https://docs.corelightning.org/reference/invoice)
- [Resumable paid-invoice waiting](https://docs.corelightning.org/reference/waitanyinvoice)
- [Invoice status lookup](https://docs.corelightning.org/reference/listinvoices)
- [Restricted runes](https://docs.corelightning.org/reference/createrune)
- [BOLT12 offers in CLN](https://docs.corelightning.org/reference/offer)
- [BIP353 DNS payment instructions](https://github.com/bitcoin/bips/blob/master/bip-0353.mediawiki)
- [LNURL-pay](https://github.com/lnurl/luds/blob/luds/06.md)
- [Lightning address discovery](https://github.com/lnurl/luds/blob/luds/16.md)
