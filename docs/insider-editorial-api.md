# Insider API identity contract

Insider owns publication and subscriber data and its `/api/v1` endpoints. Bitcoin++
owns personal API tokens, OAuth grants, and current editorial roles. Deploy this
companion change before the Insider API release.

Additive token/OAuth scopes are `insider:read`, `insider:write`, `insider:review`,
`insider:schedule`, and `insider:analytics`. They appear in the account settings
token and OAuth-client forms, consent descriptions, discovery and OpenAPI.
Existing grants are not upgraded automatically; users must explicitly grant
the new permissions. No browser-login scope or shared service secret changes.

Subscriber self-service adds `insider:subscriber:read` for the token owner's
subscription, email preferences and receipts, plus `insider:subscriber:write`
for renewal cancellation and email choices. These scopes also appear in the
token, OAuth consent and discovery controls. They do not require editorial roles
and do not grant editorial access. Editorial scopes do not grant subscriber access.

`GET /api/v1/me/insider?scope=insider:read` accepts the existing bearer token
formats and verifies the exact requested scope with the standard token
authenticator. Only one of the seven Insider scope names is accepted. Responses
are private and uncached; the ordinary token rate limiter applies.

The response data has `id`, `name`, `roles`, `scope` (the verified query value)
and `scopes` (the granted scope list). Roles are loaded from the current person
record on each request. It returns no email, membership or subscriber data.
An identity-only token receives 403. A missing, expired or revoked token receives
401. An invalid scope query receives 400.

This endpoint attests identity and scope; it does not grant a role. Insider
independently requires a current reporter/editor/admin role for editorial routes and enforces
publication ownership, version checks, workflow rules and account deletion.
Reporter tokens cannot schedule another person's post. Client scopes never
override those rules. Contributors cannot use editorial endpoints. Subscriber
routes use the authenticated person's ID exclusively, including for editors.

The standalone Insider repository documents client endpoints and workflows in
`docs/editorial-api.md` and `docs/subscriber-api.md`, and serves its combined
OpenAPI contract at `/api/v1/openapi.json`. The companion branch is
`codex/insider-editorial-api`; keep it separate from unrelated bitcoin++ work.
