# Community prize pools

CLN-only proof of concept. No payment-provider fallback, on-chain funding, card
funding, automatic payouts, or zap attribution. Existing commerce is unchanged.

## Routes

- `/{event}/admin/hackathon/community-pool`: hackathon admin setup, opening, closing, and links
  to existing pooled hackathon prizes. Linked prizes retain their configured values
  and percentages; linking does not allocate or pay any money.
- `/{event}#hackathon`: community tracker styled to match the conference page.
- `/{event}/hackathon?prize=community#community-prize`: dedicated share link,
  community funding unfurl, contribution instructions, and community prize card.
- `/{event}/prize-pool`: redirects to the hackathon community prize.
- `/{event}/prize-pool/events`: SSE snapshots of public confirmed totals. A shared
  PostgreSQL listener invalidates shared snapshots after committed funding,
  lifecycle, monitor-health or conference-visibility changes.
- `/{event}/prize-pool/status`: polling fallback (20 seconds) when SSE is absent,
  disconnected or stalled. Neither endpoint makes node requests.

Streams send heartbeat events every 15 seconds and renew after 60 seconds.
Reconnects send absolute database totals, so dropped notifications cannot lose
funding or count it twice. Client streams pause when the tracker/tab is hidden;
failed streams are retried while polling continues. A failed database listener
explicitly closes streams into fallback. Shared snapshots coalesce concurrent
viewers, and public streams never include payment details or payer notes.
- `/{event}/prize-pool/qr`: public BOLT12 offer QR while funding is open.

Goals advance in 1M-sat steps through 10M; contributions remain open beyond 10M.
Confetti celebrates first viewing and newly reached milestones once per session,
respecting reduced-motion preferences. Linked pooled prizes are excluded from
aggregate prize totals so community funds count only once.

The hackathon setup form can enable a private draft, choose an address name, and
set the LNURL/BOLT12 description without requiring node or DNS credentials.
The Community pool admin tab handles registration and opening; its Successful
payments subtab lists 50 receipts per page with timestamps, exact sats, method,
invoice descriptions, optional BOLT12 payer notes, hashes and late-payment flags.
Totals always cover all receipts, independently of the selected page. Payment
notes remain admin-only and are HTML-escaped. CLN supplies optional payer notes
through `listinvoices.invreq_payer_note`; LNURL descriptions are not donor notes.

Addresses default to the conference tag,
for example `berlin26@btcplusplus.dev`. A pool keeps its original address mapping;
custom address names do not change public event routes. Renaming conference tags is not an address migration. Endpoint collisions with a
changed description and conflicting existing DNS records stop setup for review.

## Deployment secrets

```
PRIZE_CLN_HOST=node-host:9735
PRIZE_CLN_NODE_ID=<compressed public key>
PRIZE_CLN_NETWORK=bitcoin
PRIZE_CLN_MONITOR_RUNE=<observer rune>
PRIZE_CLN_PROVISION_RUNE=<provisioning rune>
PRIZE_ADDRESS_DOMAIN=btcplusplus.dev
PRIZE_CLOUDFLARE_TOKEN=<zone-scoped DNS edit token>
PRIZE_CLOUDFLARE_ZONE=<zone ID>
```

The monitor is disabled until host, node ID and observer rune are all supplied.
Apply migration 105 before enabling it. Confirm CLN supports Commando, BOLT12 and
this clnurl fork. Both client transports and DNS integration run in the backend;
secrets are never returned to the browser. The copied accts transport uses a fresh
BOLT-8 connection per RPC, with cancellation closing the dedicated socket across
init and writes. CLN's returned node identity/network must match the configuration.

Suggested observer rune method restriction (test with `checkrune` on your node):

```json
[["method=getinfo","method=waitanyinvoice","method=listinvoices"]]
```

The provisioning rune requires `getinfo`, `offer`, `disableoffer`, `clnurl-list`,
`clnurl-add`, and `clnurl-remove`. It needs no spending RPCs. Scope to a dedicated
node client/network boundary as appropriate; this initial client generates an
identity per connection, so do not pin the rune to a persistent client identity.
The observer sees node-wide invoices, but stores only hashes, payment indexes,
amounts, timestamps, offer IDs and parsed LNURL identifiers. It never stores
preimages or Nostr request bodies. No payment history is exposed publicly.

Cloudflare DNSSEC must be active, including registrar delegation. The Go DNS
library checks Cloudflare's status; an administrator must independently verify
public DNSSEC and the LNURL path before opening funding. Terraform owns stable
zone infrastructure; the library owns dynamic BIP353 records. Proxy
`/.well-known/lnurlp/{event}` to clnurl's `/lnurl/{event}` and preserve the canonical
Host header. Ensure the returned callback is reachable over HTTPS.

Setup actions create real endpoints, offers and DNS records when configured.
Each completed step is persisted. Retry setup to recover from interruption; setup
never silently replaces a conflicting payment destination. Offers/endpoints can
receive funds as soon as provisioned, before the public page is opened.

## Accounting and recovery

`community_payment_receipts` is unique on `(node_id, payment_hash)`. Credits share
that uniqueness and refer to exactly one pool. Receipt insertion, crediting, and
cursor advancement commit atomically. Totals are sums of ledger receipts, preserving
millisatoshi precision. A full-history replay cannot increment them twice.

First startup starts from pay index zero and backfills retained invoices. Each paid
event is fetched with `listinvoices` to recover the local offer ID. Existing BOLT12
offers map by offer ID; ordinary clnurl invoices map by exact `text/identifier` in
JSON description metadata. Zaps/free-text descriptions do not auto-credit. Unmatched
receipts stay available and are reconsidered when a pool is registered. Metadata is
an attribution convention for confirmed node receipts, not authenticated donor
identity. Conflicting evidence remains uncredited; conflicting replay facts stop
monitoring for review rather than change previously credited funds.

The last successful catch-up time is shown on the page. A monitor disconnect keeps
confirmed totals visible as stale. The node/clnurl may keep receiving funds while
the web monitor is offline. Recovery resumes at the committed cursor. For a deliberate
full rescan, stop the monitor, back up the database, reset this node's cursor to zero,
and restart; do not delete receipts or credits. Node invoice retention bounds how
much missing history can be recovered. Back up the application ledger too.

Closing fixes a cutoff before disabling the offer and removing the clnurl endpoint.
Retries preserve that cutoff. Already issued invoices may still settle; those paid
at or after cutoff are recorded as late for administrator review. DNS remains tied
to the original disabled offer, and retired addresses must never be reassigned.
Public totals include late contributions and explicitly describe gross receipts,
not available funds. Distribution/refund/fee policy and automated payouts are future
work; do not treat the receipt total as an allocated award balance.

## Validation

```
go test ./...
PRIZE_TEST_DATABASE_URL=<disposable PostgreSQL URL> go test -race ./internal/prizepool ./internal/lnsocket ./internal/bip353/...
PRIZE_POOL_PREVIEW=1 go test ./internal/handlers -run '^TestPrizePoolTemplates$' -v -timeout 0
```

The preview at `http://127.0.0.1:8103/` and `/admin` uses synthetic amounts, an
unpayable placeholder offer and no POST actions. A live deployment still requires
node/rune and DNS verification, a compatible-wallet check, and an explicitly
authorized small payment test. Unit/DB tests do not establish live connectivity.
