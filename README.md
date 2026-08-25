This is the code for the [bitcoin++ website](https://btcpp.dev). (Conference [X Account](https://x.com/btcplusplus))

Configuration is loaded from environment variables. For local development,
the app also reads `.env` from the repo root without overwriting variables
already exported in the shell.


## Setup Dependencies

We use nix for this. Installs go + tailwindcss + air dependencies for Makefile.

```
	nix develop
```


## Local Dev Harness

From a clean checkout, enter the Nix shell and start the full local harness:

```
	nix develop
	just dev-up
```

When the server reports that it is listening, open:

```
	http://localhost:8888
```

`just dev-up` bootstraps `.env`, starts local Postgres, applies migrations,
seeds realistic local fixture data, prints a local admin login URL, and runs
the app in the foreground. Stop the foreground process with `Ctrl-C`.

To stop local harness services:

```
	just dev-down
```

`just dev-down` stops the local Postgres service. For lower-level app-only live
reload, use `make dev-run`.

If local Postgres fails to start because the dev data directory is stale or
corrupt, explicitly rebuild the local dev database with:

```
	just dev-reset-db
```

This moves the previous local data directory aside with a `.reset-<timestamp>`
suffix before creating a fresh one.


## To build

```
  make build
```


This will put all the files necessary to serve the site into `target/`


## Recording autopublisher

The per-event recordings dashboard lives at `/{conf}/admin/recordings` and can auto-publish recording rows that have `FileURI` and `PublishAt` set. Enable the background worker with:

```
RECORDINGS_AUTOPUBLISH_ENABLED=true
RECORDINGS_AUTOPUBLISH_POLL_SEC=60
RECORDINGS_NOTIFY_EMAIL=nifty@btcpp.dev
SOCIAL_STATE_KEY=<base64-encoded 32-byte key>
YOUTUBE_UPDATES_ENABLED=true
```

`YOUTUBE_UPDATES_ENABLED` gates every write to YouTube, including video
uploads, scheduling changes, playlist creation/assignment, and thumbnails.
It defaults to `false` when `PROD=false` and `true` when `PROD=true`; YouTube
status and playlist reads remain available while updates are disabled.

For safe local delivery testing, set `PROD=false`, `MAILER_OFF=false`,
`MAILER_JOB_ENABLED=false`, and `DEV_EMAIL_OVERRIDE=you@example.com`. In
non-production environments the override redirects every outgoing email to
that address and namespaces its mailer job key so it cannot collide with
production delivery. `MAILER_JOB_ENABLED` controls only the periodic database
mailer scan; request-driven email remains available while it is false.

YouTube OAuth tokens are encrypted into Spaces because DigitalOcean App Platform does not persist local disk across deploys. The default object key is:

```
YOUTUBE_TOKEN_OBJECT=private/social/youtube-token.json.enc
```

X Studio broadcast scheduling uses the unsupported private endpoints observed in Studio. Configure the integration with `X_STUDIO_ENABLED`, `X_STUDIO_COOKIE`, `X_STUDIO_USER_AGENT`, and `X_STUDIO_INGEST_ID`. The cookie and ingest ID are server-side secrets. Scheduling persists each returned identifier before moving to poster upload and finalization, allowing an interrupted operation to resume without creating another broadcast.

The recording admin's X action now creates a broadcast rather than uploading a video into an X post. Its public URL is copied into `recording_broadcasts.x_broadcast_url`, alongside the durable control-plane record in `recording_x_broadcasts`. The recordings autoschedule review also offers an explicit, opt-in batch action for poster-ready rows; it saves all publish times first and creates or updates X broadcasts sequentially. streamctl can incrementally poll `GET /api/v1/recording-broadcast-plans?updated_after=…` with a global-admin machine token carrying `recordings:write`; the response contains the source object key, canonical X schedule and URL, destinations, and a server-provided next cursor. streamctl synchronization is the next integration step: it will reconcile those plans into local one-shot streams and drive the existing HLS live/ended callback. Buffer remains responsible for announcement posts; btcpp-web no longer drives X.com through a Chrome profile.

YouTube OAuth uses the current event-scoped callback URL, for example `https://btcpp.dev/berlin26/admin/recordings/oauth/youtube/callback`. Register the event callback URL in the Google OAuth client before authorizing YouTube from that event dashboard.

Note that the Github actions deployer uses Docker and isn't nix-aware, so for now you *must* make and check-in any CSS changes before deploying.

CSS updates are made automatically by `dev-run`, so this shouldn't be too hard.


## Connected-account sign-in

OAuth providers are optional and stay hidden on the login page until both
values for that provider are configured:

```
AUTH_GITHUB_CLIENT_ID=
AUTH_GITHUB_CLIENT_SECRET=
AUTH_DISCORD_CLIENT_ID=
AUTH_DISCORD_CLIENT_SECRET=
AUTH_GITLAB_CLIENT_ID=
AUTH_GITLAB_CLIENT_SECRET=
AUTH_MLH_CLIENT_ID=
AUTH_MLH_CLIENT_SECRET=
```

Use a dedicated OAuth App for each environment. Register the local callback as
`http://localhost:8888/auth/oauth/{provider}/callback`, where provider is
`github`, `discord`, `gitlab`, or `mlh`; production derives the equivalent
callback from `HOST`. GitHub, Discord, and GitLab use S256 PKCE. MLH uses its
documented confidential authorization-code flow. The app requests only basic
identity/email scopes and never retains provider access or refresh tokens.

Password, passkey, and Nostr sign-in need no provider configuration. Passwords
are hashed with Argon2id; reset links are one-use and expire after 30 minutes.
Email magic links are also stored as hashed, one-use tokens and expire after 72
hours; the redirect destination is stored with the token rather than trusted
from the clicked URL. Opening a link only renders a confirmation page; a
CSRF-protected confirmation consumes it, preventing email scanners from using
the credential. Expired and consumed links can be reissued to their original
mailbox without revealing the address.
Passkeys use discoverable WebAuthn credentials, require user verification, and
are bound to the exact public origin derived from `HOST`/`PORT`. The complete
WebAuthn credential record is encrypted at rest while its credential ID remains
indexed for usernameless login.

Nostr uses a one-time, five-minute NIP-98-style challenge signed by a NIP-07
browser extension. Users link keys from Account settings by signing a challenge;
editing the public Nostr profile field does not create a login credential.
Legacy profile keys do not grant sign-in access; users must explicitly prove and
link a key from Account settings after signing in another way.

Account settings are at `/dashboard/settings`. Adding, changing, or removing a
credential requires authentication completed within the previous 15 minutes.
Adding a new sign-in method preserves existing browser sessions. Replacing or
removing a credential, changing recovery-email authority, and password resets
revoke other browser sessions. Credential changes send a security notice with
the event time to the account's primary email. OAuth identities are
matched only by the provider's immutable subject; matching provider email
addresses are never linked automatically. The first OAuth sign-in with a
verified, unused email continues into profile creation. If that verified email
already belongs to a profile, the site sends a magic link to that fixed address
before offering to link the provider identity to the existing profile.

Personal API tokens are also managed from Account settings. Tokens use the
versioned `btcpp_v1.<selector>.<secret>` format, are displayed only once, and
are stored as SHA-256 digests. Each token has explicit scopes and an expiry;
revocation is independent from browser-session logout and password resets.
Future API handlers should accept them only in the `Authorization: Bearer`
header, call `auth.AuthenticateAPIToken`, enforce the endpoint's required
scope, and load current person roles rather than copying roles into tokens.

## Deploy Testing

Currently, we deploy the app using Digital Ocean, using the `Dockerfile`. Sometimes it's useful to test building changes locally. For this, I'd recommend using the `doctl` app.

Instructions [here](https://docs.digitalocean.com/products/app-platform/how-to/build-locally/), but in brief.

```
doctl app dev build
```

Then follow the instructions to run.

The Docker image uses environment variables in App Platform. For local
build testing, make sure the needed values are present in `.env` or exported
in your shell.
