# Purchase invoices

Authenticated purchasers can issue and download invoices from `/dashboard/orders` for confirmed shop and service orders. The seller is btcplusplus LLC, 900 Olive St, Austin TX 78702, USA; EIN 93-4644668.

The purchaser supplies their billing name, address and optional tax ID. These do not update the account profile or recompute checkout tax. Amounts, line items and recorded taxes come exclusively from the owned order. Tax-inclusive service lines are converted to amounts before tax. Unsupported currencies or inconsistent totals require support rather than estimated tax details. Older standalone tickets and point-of-sale purchases link to support for manual invoices.

Migration 131 creates invoice records. Issuance locks the order, assigns a unique sequential invoice number and saves an immutable snapshot. Concurrent retries return the same invoice. Refunds do not rewrite the original invoice; billing corrections and credit notes require support. Account merges transfer invoice access without rewriting the snapshot. Deleted accounts cannot access invoices; financial snapshots remain retained.

PDF downloads require the purchaser's account and use private/no-store responses. The renderer uses an escaped local template without external resources; temporary HTML is removed after rendering. No invoice endpoint is public, and issuing an invoice sends no email or payment request.

Verification covers ownership, deleted accounts, CSRF, concurrent issuance, immutable snapshots, mismatched totals, inclusive service tax and escaped HTML. `INVOICE_PDF_TEST=1` additionally exercises the Chrome PDF renderer in `TestPurchaseInvoiceHTTPPostgres` with synthetic purchase data.
