# Bitcoin++ API implementation plan

Status: canonical design for API v1

Base path: `/api/v1`

This document supersedes the exploratory API notes under `reqs/`. It records
the deliberately small first release and the authorization boundaries for the
authenticated work that follows it.

## Goals

- Give mobile applications, integrations, and agents a stable JSON API.
- Keep PostgreSQL and the existing domain operations authoritative.
- Expose published website data without requiring an API token.
- Use person-owned API tokens for private reads and mutations.
- Require both token scope and live ownership/role checks for privileged work.
- Keep API DTOs independent from database rows and template-facing Go types.

## API v1 release boundary

The public, read-only surface is:

```text
GET /api/v1/bootstrap
GET /api/v1/conferences
GET /api/v1/conferences/{tag}
GET /api/v1/conferences/{tag}/days
GET /api/v1/conferences/{tag}/agenda
GET /api/v1/conferences/{tag}/talks/{conf_talk_id}
GET /api/v1/conferences/{tag}/speakers
GET /api/v1/people
GET /api/v1/people/{person_id}
GET /api/v1/conferences/{tag}/sponsors
GET /api/v1/organizations/{organization_id}
GET /api/v1/recordings
GET /api/v1/recordings/{recording_id}
GET /api/v1/conferences/{tag}/hackathons
GET /api/v1/hackathons/{competition_id}
GET /api/v1/hackathons/{competition_id}/projects
GET /api/v1/hackathons/{competition_id}/projects/{project_id}
GET /api/v1/hackathons/{competition_id}/awards
GET /api/v1/hackathons/{competition_id}/results
```

Only published conferences, public agenda entries, published recordings,
submitted/public projects, confirmed teammates, and finalized/published award
results are anonymous. Draft, withdrawn, disqualified, or unfinalized data is
not part of the public representation.

Transcripts, captions, and SRT artifacts are not part of the initial API.
They will be designed and implemented in a dedicated feature sprint.

## Representations and privacy

Every response uses explicit request/response structs in `internal/api`. The
API must never JSON-marshal `internal/types` database or template values.

Public people fields may include:

- person ID, display name, public profile URL, avatar, biography;
- public organization, website, and social handles;
- public Nostr profile value (verified credentials still govern sign-in);
- published talks, conference appearances, and projects.

Public project teammates include person ID, display name, public profile URL,
avatar, and project role. Email addresses, invitations, tickets, contact
details, and payout information are never included.

Private profile data is served separately from `/me`; it is never inserted
conditionally into cacheable public profile responses. Each profile field is
classified as public, self-only, conference-role-only, global-admin-only, or
never exposed before its endpoint ships.

Fields never exposed through API v1 include password hashes, passkey material,
OAuth access tokens, session credentials, magic/reset tokens, API-token hashes,
tax documents, and internal person-merge state.

## Personal API tokens

Personal API tokens use the existing `btcpp_v1.<selector>.<secret>` format and
are accepted only in the HTTP `Authorization: Bearer` header. Tokens are never
accepted in URLs, query parameters, or cookies.

The scope vocabulary is:

```text
profile:self:read
profile:self:write
talks:read
talks:write
schedule:write
recordings:write
```

The legacy experimental `profile:read` scope is temporarily treated as
`profile:self:read`; new credentials cannot request it.

Authorization is evaluated as:

```text
resource visibility + token scope + live role + ownership
```

A token scope is consent, not a role grant. Roles are loaded from the token
owner on every privileged request. Removing a conference or global admin role
therefore removes API authority without requiring token revocation.

Protected endpoints are:

```text
GET   /api/v1/me
GET   /api/v1/me/talks
PATCH /api/v1/me

PATCH  /api/v1/conferences/{tag}/talks/{conf_talk_id}
PUT    /api/v1/conferences/{tag}/talks/{conf_talk_id}/schedule

GET /api/v1/conferences/{tag}/recording-candidates
GET /api/v1/recording-broadcast-plans?updated_after={rfc3339}
PUT /api/v1/conferences/{tag}/talks/{conf_talk_id}/recording
```

### Talk editing

`talks:write` permits a confirmed speaker attached to a talk to edit an
allowlist of shared content fields. A conference admin for `{tag}` or a global
admin may also edit them. Speaker-editable fields initially include title,
description, slides URL, and repository URL. Conference, schedule, venue,
acceptance/publication state, speaker membership, production notes, and
recording publication data are excluded.

Any confirmed co-speaker may edit shared content. Ownership is resolved by
canonical person relationships, never email.

### Scheduling

`schedule:write` additionally requires `{tag}-admin` or `global-admin`.
Scheduling validates conference ownership, RFC 3339 timestamps, start/end
order, conference dates, configured venues, room collisions, and speaker
collisions. Conflict validation and the update run in one transaction under a
conference-level advisory lock, so concurrent schedule requests cannot both
claim the same room or speaker time. Schedule writes produce audit records.

### Recordings

`recordings:write` additionally requires `{tag}-admin` or `global-admin`.
There is at most one recording per conference talk, so `PUT` is naturally
idempotent. The server defaults its title from the talk and verifies that the
talk belongs to the conference in the URL. Large file upload and presigned
Spaces upload URLs are deferred; the first mutation manages recording metadata.

## API conventions

- JSON keys use `snake_case`.
- Database-backed identifiers are UUID strings.
- Instants use RFC 3339 timestamps with offsets.
- Absent optional values are `null`, not magic empty strings.
- Collections use a top-level `data` array; objects use a top-level `data`
  object. `meta.request_id` is included on JSON responses.
- Errors use a stable code, human-readable message, and request ID.
- Unknown JSON request fields are rejected for mutation endpoints.
- Potentially growing collections accept `limit` (default 50, maximum 100) and
  an opaque `cursor`; `meta.next_cursor` is present when another page exists.

Status conventions:

```text
400 invalid_json / invalid_pagination
401 invalid_token
403 insufficient_scope / forbidden
404 not_found
409 conflict
415 unsupported_media_type
422 validation_error
429 rate_limited
500 internal_error
```

Public successful GETs use ETags and short public caching. Private reads and
all credential, ticket, and mutation responses use `private, no-store` or
`no-store`. Protected representations must never enter a public cache.

## Rate limits

Starting limits, subject to production measurement:

- public reads: 120 requests/minute/IP with a burst of 30;
- authenticated reads: 600 requests/minute/token with a burst of 100;
- ordinary mutations: 60 requests/minute/token;
- recording writes: 10 requests/minute/token and 30/hour/person;
- upload-URL creation, when added: 5 requests/10 minutes/person.

Anonymous high-volume limiting belongs at the edge so rejected traffic does
not create a PostgreSQL write. Sensitive application limits are keyed by both
trusted client IP and token/person as appropriate. `429` responses include
`Retry-After`. Trusted proxy configuration must be explicit before using
forwarded client-address headers.

Payments are not rate limits. L402, MPP, paid quotas, and entitlements are
deferred. A future paid credential may increase a commercial quota but may
never bypass absolute safety ceilings or grant admin authorization.

## Security and operations

- API bearer endpoints do not use browser cookie sessions or CSRF tokens.
- Browser credential-management flows remain web-only and require CSRF
  protection; sensitive client registration and revocation also require a
  recent authentication.
- API CORS is denied by default. OAuth token and revocation CORS is limited to
  web origins derived from the requesting client's registered redirect URIs.
- JSON mutation bodies have bounded limits and reject unknown fields.
- Logs include request ID, route template, status, latency, authenticated
  person ID, and token ID where appropriate.
- Logs never include bearer tokens, private profile fields, ticket authority,
  payout data, or payment credentials.
- Privileged mutations audit the person, token, credential kind, conference,
  resource, operation, remote address, and timestamp without logging bearer
  values or private field contents.

## Testing and compatibility

`internal/api/openapi-v1.json` is the embedded contract served publicly from
`/api/v1/openapi.json`. Tests cover:

- anonymous/publication filtering;
- stable DTO and error shapes;
- ETag and cache behavior;
- missing resources and unsupported methods;
- bearer scope failures and private projection behavior;
- pagination, rate-limit behavior, schedule validation helpers, OAuth PKCE,
  redirect origins, token parsing, and refresh-family replay revocation;
- absence of private fields in public responses.

Additive optional fields are compatible. Breaking field, meaning, or endpoint
changes require `/api/v2` or retirement of every affected client version.

## OAuth authorization server

Bitcoin++ also acts as an OAuth 2.1-style authorization server for native,
browser, and server-backed clients:

```text
GET  /.well-known/oauth-authorization-server
GET  /oauth/authorize
POST /oauth/authorize
POST /oauth/token
POST /oauth/revoke
```

Authorization code is the only interactive flow and S256 PKCE is mandatory
for every client. Public clients use `token_endpoint_auth_method=none`;
confidential clients use HTTP Basic authentication. Redirect URIs are exact
matches. Codes expire after ten minutes and are single-use. Access tokens
expire after one hour. `offline_access` enables 30-day rotating refresh tokens;
reuse revokes the refresh family and all access tokens derived from it.

Global admins register and revoke clients from Account Settings. Revoking a
client also revokes all of its access and refresh tokens. OAuth access tokens
enter the same API authorization path as personal tokens: requested scope plus
live person ownership/roles.

This is an OAuth authorization service, not yet an OpenID Connect provider.
Apps identify the resource owner through `GET /api/v1/me`; ID tokens, JWKS,
OIDC discovery, and federated logout are not advertised.

## Deferred features

- transcripts, captions, and SRT files;
- L402, MPP, payments, and paid API entitlements;
- OpenID Connect ID tokens and JWKS;
- tickets, QR/check-in authority, and Wallet passes;
- personal schedules and workshop registration;
- notifications and device registration;
- hackathon project mutations and judging;
- large recording uploads and social publishing through the API.
