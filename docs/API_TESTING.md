# Local API and OAuth testing

Never start the application with `PROD=true`. Rebuild a disposable seeded
database with `just dev-reset`, then start it with `just dev-up`.

## Personal access token

1. Use the development login on `/login` as a seeded account.
2. Open `/dashboard/settings` and create a personal API token.
3. Copy the token immediately; only its hash is stored.
4. Call a private endpoint:

```sh
curl -H 'Authorization: Bearer btcpp_v1.…' \
  http://localhost:8888/api/v1/me
```

Public endpoints such as `/api/v1/conferences`, `/api/v1/people`, and
`/api/v1/recordings` do not require a token. List endpoints accept `limit` and
the opaque `cursor` returned as `meta.next_cursor`.

## OAuth authorization code with PKCE

Sign in as the seeded global admin and register a public application under
Account Settings → OAuth server. Use a loopback redirect such as
`http://127.0.0.1:8765/callback`, select the desired scopes, and copy the
client ID.

Generate a verifier and S256 challenge in the app under test, then open:

```text
http://localhost:8888/oauth/authorize?response_type=code&client_id=CLIENT_ID&redirect_uri=http%3A%2F%2F127.0.0.1%3A8765%2Fcallback&scope=profile%3Aself%3Aread%20offline_access&state=RANDOM_STATE&code_challenge=S256_CHALLENGE&code_challenge_method=S256
```

After signing in and approving consent, verify `state` and `iss` on the exact
registered redirect. Exchange the single-use code:

```sh
curl -X POST http://localhost:8888/oauth/token \
  -H 'Content-Type: application/x-www-form-urlencoded' \
  --data-urlencode 'grant_type=authorization_code' \
  --data-urlencode 'client_id=CLIENT_ID' \
  --data-urlencode 'redirect_uri=http://127.0.0.1:8765/callback' \
  --data-urlencode 'code=AUTHORIZATION_CODE' \
  --data-urlencode 'code_verifier=PKCE_VERIFIER'
```

Use the returned access token against `/api/v1/me`. If `offline_access` was
approved, exchange the refresh token once with `grant_type=refresh_token` and
replace both stored tokens with the response. Reusing an old refresh token
revokes the entire family.

The authorization-server metadata is at
`/.well-known/oauth-authorization-server`. Account Settings → Authorized apps
revokes a user's grant and all tokens. A global admin can revoke a registered
client, which revokes every token issued to it.

## Recording service credential

For `stream.btcpp.dev`, create a personal token while signed in as a
`global-admin`, selecting only `recordings:write`. Store it in a root-owned
`0400` file and inject it into the service; do not put it in Nix source,
Terraform state, command-line flags, or URLs. The service can discover work at
`GET /api/v1/conferences/{tag}/recording-candidates` and idempotently update a
record with `PUT /api/v1/conferences/{tag}/talks/{talk_id}/recording`.
