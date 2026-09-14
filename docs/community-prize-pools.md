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
for example `berlin26@zap.btcplusplus.dev`. A pool keeps its original address mapping;
custom address names do not change public event routes. Renaming conference tags is not an address migration. Endpoint collisions with a
changed description and conflicting existing DNS records stop setup for review.

## Node configuration

Apply migrations 105 and 106, then open `/admin/node-config`. Only an explicit
`accts-admin` identity can view or submit this page; global-admin and conference
admin alone do not grant access. Links appear in Account settings and (for users
who also have global-admin) the global admin tools. Event admins still manage
individual pools, but cannot change the shared connection or see credentials.

Enter the CLN host and Lightning port, compressed node public key, network,
monitoring and provisioning runes, address domain (default `zap.btcplusplus.dev`),
Cloudflare zone ID for `zap.btcplusplus.dev` and a DNS edit token scoped to that
zone. Enable monitoring explicitly.
There is no environment-variable fallback or automatic import of the old prize
connection variables. Existing installations start with monitoring disabled.

Settings are encrypted in PostgreSQL using AES-GCM with a domain-separated key
derived from the existing application `HMAC_SECRET`; no new deployment secret is
required. Keep that secret with database backups. Rotating it requires coordinated
re-encryption of node settings before switching; otherwise the settings cannot be
read. Credential fields are write-only: blank preserves, replacement rotates,
and an explicit remove checkbox clears. Audits contain actor, revision, activation
state and time, never credentials. Concurrent stale forms are rejected.

Each application instance reloads configuration every five seconds, cancels the
previous monitor on change or read/decryption failure, and starts the replacement
only when enabled. Disabling monitoring does not close offers or stop receipts at
the node. Resuming catches up from the durable cursor. Existing pool node/network
and domain bindings cannot be changed through this form; host/rune rotation is
allowed. Pool provisioning and creation hold a shared configuration lock so they
cannot race a destination change.

The **Test saved connection** action calls only `getinfo` with the saved monitoring
rune and checks node identity/network. It does not prove the remaining permissions,
provisioning, DNS or a wallet payment. Confirm CLN supports Commando, BOLT12 and
this clnurl fork. Both transports and DNS integration run in the backend. The
copied accts transport uses a fresh BOLT-8 connection per RPC, with cancellation
closing the dedicated socket across init and writes.

The **Create the service runes** helper on `/admin/node-config` provides copyable
commands to run locally on your CLN node. Paste each response's `rune` value into
the corresponding field; the website never receives your node's rune-creation
permission. Use your normal CLI connection/network options when running:

```sh
lightning-cli createrune -k restrictions='[["method=getinfo","method=waitanyinvoice","method=listinvoices"]]'
lightning-cli createrune -k restrictions='[["method=getinfo","method=offer","method=disableoffer","method=clnurl-list","method=clnurl-add","method=clnurl-remove"]]'
```

These use [CLN createrune](https://docs.corelightning.org/reference/createrune),
available since v23.08. Exact method alternatives belong in one inner array (OR),
not separate arrays (AND). No broad `readonly` or unrestricted rune is needed.

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
