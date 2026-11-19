# Merch sale notifications

Confirmed shop payments enqueue an operational notification in the same database
transaction as the paid state. This covers Stripe and OpenNode shop payments,
including merchandise purchased with tickets. Ticket-only orders and abandoned
carts do not enqueue notices. The separate conference POS register is not part
of this shop-order notification flow.

Recipients explicitly hold `people_roles(scope='merch', position='admin')`, shown
as `merch-admin` in role tags. Each person receives one message at their verified
primary email, or their earliest verified email if their primary is unverified.
Global admins keep merch management access, but are not implicitly subscribed.
No roles are assigned by this migration. Assign the role to the fulfillment team
through the existing role provisioning process; the email links to
`/admin/merch/orders/{order-id}`. Merch admins can manage the catalog and orders,
including fulfillment and refunds, but gain no global administration access.

A worker checks the queue at startup and each minute. Claims last ten minutes;
failed or interrupted sends retry after the claim expires. A stable mailer job
key per order and email deduplicates successful recipients on partial retries.
No eligible recipients leaves the notice pending, with an application error log.
`MAIL_OFF` leaves queued notices pending rather than marking them sent. Completion
means the mailer accepted every job, not that every mailbox has delivered it.

Migration 104 adds the queue without backfilling historical sales. Deploy the
application with the migration before processing new shop payments. The test
suite uses synthetic orders and a fake sender; it does not send operational mail.

To generate a synthetic HTML preview, run:

```sh
MERCH_NOTICE_PREVIEW=1 go test ./internal/handlers -run '^TestMerchSaleEmail$'
```

It writes `/private/tmp/merch-sale-notification.html`.

## Organization owners and managers

Migration 167 adds a separate organization notification queue so supplier mail
does not depend on having a bitcoin++ merch admin subscribed. Both confirmed shop
payments (Stripe/OpenNode, including merch bought with tickets) and confirmed
event-register purchases enqueue one notice per organization in the payment
transaction. Payment retries cannot enqueue another notice for the same purchase.
Old sales are not backfilled. Expired or canceled event payments that go into
manual review do not send a normal sale notice.

The worker checks at startup and every minute, with a ten-minute retry lease.
Every current active owner and manager with a verified account email receives
one message: primary address first, otherwise earliest verified address. Ordinary
members, pending invites and removed memberships are excluded. Recipients are
resolved again on retries. With no eligible recipients, the notice remains pending
and the worker logs the problem. MAIL_OFF leaves notices unclaimed.

Each message contains only that organization's purchased items, quantities,
fulfillment methods, and recorded supplier share after the platform fee. Shipping
and tax are excluded from the share; missing historical split data is not guessed.
Event-register proceeds are shown in sats. This is a sale notice, not confirmation
of a supplier payout; subsequent refunds can change the amount due.

The link opens `/dashboard/orgs/{organization-id}/shop?tab=orders`, which requires
the existing organization permissions. Customer addresses, other suppliers' items,
and the whole mixed-order total are not included in the email. A person managing
two participating organizations receives a separately scoped notice for each.
Stable mailer job keys per notice and email deduplicate successful recipients
when another recipient's delivery fails. Completion means mailer acceptance,
not confirmed inbox delivery. The existing bitcoin++ merch-admin notice continues.

`TestOrganizationSaleNotifications` exercises mixed orders, recipient selection,
duplicate payment callbacks, partial delivery failure, removed memberships and
MAIL_OFF using a fake sender. `TestOrganizationEventSaleNotifications` covers
confirmed, invalid and late event payments. `TestOrganizationSaleEmail` checks
rendering, escaping, sats and stable delivery keys. Set `BTCPP_EVENT_WIZARD_TEST=1`
and `BTCPP_POSTGRES_SMOKE=1` with a migrated disposable `DATABASE_URL` to run the
database cases. Adding `ORG_SALE_NOTICE_PREVIEW=1` writes a synthetic email to
`/private/tmp/organization-sale-email.html` without sending any mail.
