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
