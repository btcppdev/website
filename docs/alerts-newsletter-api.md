# Alerts newsletter integration

Migration note: the incremental, unpublished Alerts migrations described in the
implementation history below are consolidated into `110_alerts_service_billing.sql`.
Deploy this migration after the existing bitcoin++ master migrations. Development
databases containing the earlier migration sequence must be recreated separately;
do not rewrite their recorded migration checksums.

The account API exposes `GET /api/v1/me/newsletters` with
`newsletters:self:read` and `PUT /api/v1/me/newsletters` with
`newsletters:self:write`. Register those scopes on the Alerts OAuth client before
enabling the integration. Apply migration 101 before serving these endpoints.
This worktree has not been deployed.

GET returns the standard private `data`/`meta` envelope. Data contains both
`newsletter` (Bitcoin++ news) and `insider` (Insider Edition) preferences for each
verified account email, including unsubscribed choices. Responses are not cached.

PUT accepts exactly `email`, `newsletter`, and boolean `subscribed`. The email
must belong to the authenticated account and be verified. There is no caller
supplied person ID. Send `Content-Type: application/json` and a fresh random
`Idempotency-Key` of 16–128 characters for each explicit user choice.

Only the named membership changes; other newsletters are preserved. Membership
and the consent record commit together. The record includes account, verified
email, newsletter, choice, authenticated client, request key, and server time.
Retry the exact request with the same key after a transport failure. A successful
replay acknowledges the original request without restoring that old preference
over a later choice. Reusing a key with different contents or client returns 409.
GET again to display current preferences after a retry.

Alerts must present an optional, unchecked newsletter choice separately from
release notifications. A footer invitation does not constitute consent. Do not
request write access or issue a subscription change merely because somebody
creates an Alerts account. The transient OAuth token lifecycle and restoration
of the user's saved choice must be handled by the Alerts integration before
this feature is enabled there.

Validation so far: API tests cover scope separation, explicit false versus a
missing choice, rejected account-ID injection, verified-email failures, and key
conflicts. The full migration chain through 096 applied to an isolated PostgreSQL
18.6 database. Race-enabled database tests passed for preserved memberships,
opt-out followed by an old opt-in retry, key/client conflicts, rejected unowned
email, rollback on consent-write failure, and eight concurrent retries producing
one consent record. Account-handler tests also pass. End-to-end Alerts consent
verification and the consent retention policy remain pending.

Migration 101 was originally numbered 096 during isolated development. It was
renumbered before deployment to follow Insider migrations 096–100. Do not deploy
both historical versions. Existing disposable test databases may retain the old
number; production has not received either version from this worktree.

The combined 89-migration sequence (Insider 096–100 plus Alerts 101) applied
successfully to a fresh isolated PostgreSQL 18.6 database. Newsletter transaction
and concurrent retry tests pass against that combined schema with the race detector.
This validates database compatibility, not a merge of the two API implementations.
