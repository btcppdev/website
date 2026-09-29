# Public newsletter signup

All public newsletter forms submit `newsletter-email` to `POST /newsletter/subscribe`, both through HTMX and native form submission. Responses are shown in a dedicated result area so errors leave the form available for retry. The shared mailing-list form submits on the form rather than a button click, so browser email validation and Enter-key submission work consistently.

On 29 September 2026 the production homepage was observed posting `email` to `/subscribe`; an empty POST to that route returned HTTP 405. This route/field mismatch prevented the confirmation-email handler from running. The local fix aligns the homepage with the existing handler.

Signup sends a signed confirmation link; it does not create a subscriber row yet. `/confirm/{token}` validates the link and writes the subscription. Therefore an unconfirmed signup will not appear in the subscriber list.

`TestPublicNewsletterSignupForms` extracts the submission path and email field from each of the three public templates, posts through the actual newsletter router, and checks a local mock mailer for the recipient and valid confirmation token. It also exercises invalid input and mailer failure. This test never sends real email and does not establish production inbox deliverability.

## Full subscription audit (29 September 2026)

| Pathway | Verification |
| --- | --- |
| Homepage, event page, shared mailing-list form | Actual form fields/routes posted through newsletter router; confirmation recipient and signed token checked with mock mailer. |
| Public confirmation, unsubscribe, resubscribe | Local PostgreSQL integration tests through HTTP routes, including first-time follow-up mail, repeated confirmation, malformed token, mailer failure and cancellation retry. |
| Sponsor inquiry checkbox | Actual POST handler tested with opt-in checked/unchecked and mailer failure. Opt-in sends a confirmation link; inquiry success survives confirmation failure. |
| Speaker applications (public and invited), volunteer applications | Handler consent plumbing reviewed; actual checkbox decoding and shared application subscription persistence tested, opted in and out. Volunteer subscription occurs after application confirmation. |
| Account creation | Authenticated-email subscription code reviewed; existing account/template tests cover presence of signup opt-in and its absence on normal profile edits. Shared subscription persistence exercised by lifecycle tests. |
| Stripe and OpenNode checkout | Consent propagation from form to provider metadata/callback reviewed; Stripe metadata parsing and shared ticket subscription persistence tested with consent on/off. No live paid checkout performed. |
| Complimentary, speaker, sponsor and volunteer tickets | Call sites reviewed: issuing a ticket adds operational lists, without forcing general-newsletter consent. Volunteer scheduling now follows this rule too. |
| Newsletter email subscribe links | Signed links use the same tested confirmation endpoint. |

Additional fixes:

- `Subscriber.AddSublist` short-circuited after the first new membership. New subscribers now receive every requested list, including the optional general newsletter.
- Sponsor inquiries previously ignored their newsletter checkbox.
- Scheduling a volunteer previously forced general-newsletter subscription, overriding consent.
- First-time confirmation previously skipped scheduling follow-up messages. Membership is now saved before scheduling; failures show a retryable error. Repeated confirmation reuses the mailer's stable job key.
- Unsubscribe previously hid database/mailer errors and never retried cancellation after membership removal. It now reports failures and retries cancellation on another visit.
- Malformed signed-link timestamps no longer panic.

These changes do not backfill previously dropped memberships. Historical consent must be established before any recovery. Local tests do not establish production inbox deliverability or live payment-provider behavior; production deployment and a controlled real-inbox confirmation check remain necessary. No real email was sent during this audit.

Validation result: the targeted subscription regressions passed across `internal/missives`, `internal/handlers`, and `internal/mtypes`, using the isolated local PostgreSQL database and mock mailer. A broader `TestLoadTemplates` run parsed the templates but stopped on an unrelated managed-signer wording assertion (`exact connection request hash`); that test and signer template were not changed by this work.
