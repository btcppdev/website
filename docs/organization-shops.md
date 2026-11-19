# Organization merchandise

Organization owners/managers and global administrators manage merchandise at
`/dashboard/orgs/{organization}/shop`. **Payouts & tax** stores the supplier's
legal recipient, tax residence, optional registration details, and preferred
bank-transfer, Bitcoin, or Lightning payment instructions. It is private,
requires the existing session/CSRF checks, and uses revision checks for edits.

Shop administrators can edit any supplier product at `/admin/merch`, including
its shop category. Changing category preserves the fulfillment organization.
The organization editor includes all six storefront categories and rejects stale
edits after an administrator changes a product. Public product cards and detail
pages show the organization's current light-mode logo and name; without a logo,
the name still appears. Branding is loaded from the organization profile, with
no shipping, billing or payment details included in public catalog data.

Organization product forms use plain-language merchandise choices instead of raw
Stripe/Easyship codes. Everyday adult clothing and hats use the verified Stripe
categories and Easyship's `fashion` category; other physical merchandise uses
Stripe's general tangible-goods category and needs an existing shipping category
or a supplier-provided HS code before online publication. Storefront categories
do not change this classification. Existing admin classifications are preserved
on ordinary edits; changing the kind clears an unchanged old HS code for review.
The admin catalog flags incomplete setup without creating tasks or sending mail.

Manufacturing country uses a country-name selector with an unknown option. Drafts
can be saved without tax classification, manufacturing country or customs data.
Online publication requires a known manufacturing country and shipping setup;
event-only merchandise doesn't require shipping customs fields. No manufacturing
country or HS code is inferred from the organization's address or product title.
Supplier-provided customs codes are optional in a collapsed section of the form.

Classification references: [Stripe product tax codes](https://docs.stripe.com/tax/tax-codes),
[Easyship rates](https://developers.easyship.com/docs/how-to-get-shipping-rates), and
[Easyship country of origin](https://support.easyship.com/hc/en-us/articles/19245058359058-How-to-Change-Your-Product-s-Country-of-Origin).

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
