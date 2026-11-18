# Insider editorial API identity contract

Insider owns publication data and its `/api/v1` editorial endpoints. Bitcoin++
owns personal API tokens, OAuth grants, and current editorial roles. Deploy this
companion change before the Insider editorial API release.

Additive token/OAuth scopes are `insider:read`, `insider:write`, `insider:review`,
`insider:schedule`, and `insider:analytics`. They appear in the account settings
token and OAuth-client forms, consent descriptions, discovery and OpenAPI.
Existing grants are not upgraded automatically; users must explicitly grant
the new permissions. No browser-login scope or shared service secret changes.

`GET /api/v1/me/insider?scope=insider:read` accepts the existing bearer token
formats and verifies the exact requested scope with the standard token
authenticator. Only one of the five Insider scope names is accepted. Responses
are private and uncached; the ordinary token rate limiter applies.

The response data has `id`, `name`, `roles`, `scope` (the verified query value)
and `scopes` (the granted scope list). Roles are loaded from the current person
record on each request. It returns no email, membership or subscriber data.
An identity-only token receives 403. A missing, expired or revoked token receives
401. An invalid scope query receives 400.

This endpoint attests identity and scope; it does not grant a role. Insider
independently requires a current reporter/editor/admin role and enforces
publication ownership, version checks, workflow rules and account deletion.
Reporter tokens cannot schedule another person's post. Client scopes never
override those rules. Contributors are excluded from Insider's first API version.

The standalone Insider repository documents client endpoints and workflows in
`docs/editorial-api.md` and serves its OpenAPI contract at `/api/v1/openapi.json`.
The companion branch is `codex/insider-editorial-api`; keep this small change
separate from unrelated Bitcoin++ work when preparing a release.
