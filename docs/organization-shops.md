# Organization merchandise

Organization owners/managers and global administrators manage merchandise at
`/dashboard/orgs/{organization}/shop`. **Payouts & tax** stores the supplier's
legal recipient, tax residence, optional registration details, and preferred
bank-transfer, Bitcoin, or Lightning payment instructions. It is private,
requires the existing session/CSRF checks, and uses revision checks for edits.

Payment instructions and tax registrations are encrypted in the database using
the existing `TAX_FORM_ENCRYPTION_KEY`, with an organization-specific key context.
Configure and back up that key before accepting details. Missing keys prevent
saves; unreadable ciphertext is an error, never an empty replacement form.
Legacy tax IDs are prefilled from the shop profile and removed from its plaintext
JSON when the new encrypted profile is saved. Changing shipping settings preserves
the separate payout profile. The billing address remains in Shop profile.

These are supplier instructions, not verified payment destinations. Saving them
does not initiate a transfer, establish a payout schedule, validate a tax
registration, or certify completion of any required tax forms. Review destinations,
supplier invoices and applicable documents before paying. Cryptocurrency conversion
and settlement timing must be agreed separately.

## Price split

bitcoin++ keeps **15%** of the merchandise price **after discounts**, excluding
sales tax and shipping. The organization receives the remainder, normally 85%.
Fees are rounded half-up once per order line to the nearest minor unit (cent for
ordinary orders, satoshi for event-register sales). No additional processing-fee
deduction is included in this supplier split. A $40 item yields a $6 fee and $34
for the supplier. A $22.50 line yields $3.38 and $19.12 respectively.

The product editor estimates one item's proceeds, including variant adjustments.
The server independently applies the rate when an order is created. It stores the
rate, fee, and supplier amount on the order item, in the order's currency. Event
registers store the equivalent split in satoshis. Mixed orders calculate each
organization's item separately; bitcoin++'s own products do not receive this split.
Later price or rate changes must not recalculate historical snapshots.

These snapshots represent the original sale terms, not a paid or currently payable
balance. Canceled/unpaid sales are not earnings. Refunds and subsequent payments
must be reconciled before settlement; the existing refund records remain separate.
Older sales intentionally have null snapshots rather than retroactively assigning
them new terms. No automatic supplier payout or settlement ledger is introduced.

Apply migration `166_organization_shop_payouts.sql` before running the updated
shop handlers. The local preview accepts `TAX_FORM_ENCRYPTION_KEY` and uses only
the disposable database; keep real bank/tax data out of test fixtures.

Validation includes `TestOrganizationShopSplit`, `TestOrganizationShopPayoutValidation`,
`TestOrganizationShopPayoutStorage`, `TestOrganizationShopMixedOrderLifecycle`,
`TestOrganizationShopAccess`, and the shared template suite. Database tests require
`BTCPP_POSTGRES_SMOKE=1` / `BTCPP_EVENT_WIZARD_TEST=1` and a disposable migrated database.
