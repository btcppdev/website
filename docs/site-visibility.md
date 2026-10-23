# Insider launch visibility

Global administrators can open **Admin → Site visibility** (`/admin/site-features`)
and enable **Show Insider Edition** when the product is ready to launch.
Migration 132 initializes this switch to off. Changes persist in PostgreSQL with
the administrator ID and update time; no process restart is needed.

While off, Insider's dashboard tab and subscription card are absent, and
`/dashboard/insider` returns 404, including for administrators. Shared navigation,
homepage promotion, footer links, ticker and sponsor-brand promotion are hidden.
Missing flag configuration or database errors also leave the product hidden.

This is a presentation and discovery flag, not a billing kill switch: existing
purchase history, account records, entitlement APIs and service integrations
remain intact. Previously sent emails and financial records are not rewritten.
The switch does not make subscription navigation public; existing role gates
still apply when Insider visibility is enabled.

POST requires global-admin authorization and a session CSRF token. Enabling the
flag restores the configured Insider links, currently including the beta account
management URL. Review those destinations as part of the eventual launch.
