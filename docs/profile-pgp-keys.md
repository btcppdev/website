# Profile PGP keys

People manage multiple public OpenPGP certificates at `/dashboard/profile/keys`, linked from profile settings. There is no fixed key-count limit. Each upload accepts one ASCII-armored public certificate, up to 128 KiB; private key packets are rejected even inside a public-key armor block. The maintained ProtonMail `go-crypto/openpgp/v2` library handles verification, including v4/v6 certificates and signing subkeys.

## Verification and publication

1. Paste an exported public key. It remains private and unverified.
2. Generate and download a challenge. It includes the account UUID, full fingerprint, a random 256-bit nonce, expiry and explicit publication authorization.
3. Sign the exact downloaded bytes locally with `gpg --armor --local-user FULL_FINGERPRINT --detach-sign btcpp-pgp-challenge.txt`.
4. Paste the detached armored signature. Only successful verification publishes the key.

Challenges expire after 30 minutes. Generating another invalidates the previous challenge. Verification locks the owned database row and consumes the challenge atomically. All mutations require a signed-in person and session CSRF token. Removing a key immediately removes it from API responses and downloads; it does not revoke the key elsewhere. Re-adding a removed key requires another proof. Adding the same fingerprint again is idempotent and does not replace an existing certificate or reset its proof; remove and re-add it to publish an updated certificate.

Verification means signing control, not a legal identity or validation of every user ID. The whole public certificate, including its user IDs and email addresses, becomes public. The UI explains this before publication. Current validity is checked against the uploaded certificate; the website does not fetch revocations or refreshed certificates from external keyservers. Expired/revoked uploaded certificates and certificates without a usable signing key are excluded from public output.

Account merges transfer keys and preserve verified status. If both accounts have the same fingerprint, the canonical account's record wins; the original source record remains in the existing merge/undo archive. Pending challenges cannot be used after transfer because they bind to the original account ID. Generate a new challenge on the resulting account. Merge undo restores original key records.

## Public downloads

Existing public `/whois` eligibility and stable profile slugs still apply; adding a key does not create a new directory listing.

- `/whois/{person}/key.asc`: all currently valid verified keys, in one ASCII-armored block.
- `/whois/{person}/key.gpg`: the same bundle in binary OpenPGP format.
- `/whois/{person}/keys/{FULL_FINGERPRINT}.asc` or `.gpg`: a single verified key.

Missing profiles, unknown fingerprints and profiles without verified keys return 404. Downloads use `application/pgp-keys`, attachment filenames and `Cache-Control: no-store`. Profile links use the 16-hex-character (8-byte) OpenPGP key ID as a display label. Their destinations and accessible names use the full fingerprint. Never use the short label as a unique security identifier. Key-ID calculation follows the certificate version (v4 trailing 64 bits, v6 leading 64 bits).

## API

`GET /api/v1/people/{person_uuid}` now includes `pgp_keys`, an array containing only verified, currently valid uploaded certificates:

```json
{
  "fingerprint": "FULL_UPPERCASE_FINGERPRINT",
  "key_id": "16_HEX_CHARACTERS",
  "public_key": "-----BEGIN PGP PUBLIC KEY BLOCK-----\n...",
  "verified_at": "2026-09-14T12:00:00Z"
}
```

Profiles without keys return `"pgp_keys": []`. Challenges and unverified keys are never included. The public person response remains unauthenticated but now uses `private, no-store` so caches cannot retain a removed key. Search/directory summaries retain their existing shape. OpenAPI describes the new field and certificate object.

## Deployment and verification

Apply `102_person_pgp_keys.sql` before running the updated website. The integrated release also includes judging migrations 100–101; this feature does not depend on those tables. No environment variables or third-party services are required.

Run the ordinary affected packages:

```sh
go test ./internal/pgpkeys ./internal/handlers ./internal/api ./external/getters
```

With a **disposable, migrated** PostgreSQL database:

```sh
BTCPP_POSTGRES_SMOKE=1 DATABASE_URL=... go test -race ./internal/pgpkeys ./external/getters -run 'TestProof|TestRejectPrivate|TestGnuPG|TestPersonPGP' -v
BTCPP_POSTGRES_SMOKE=1 DATABASE_URL=... go test ./internal/handlers -run '^TestProfilePGPHTTPFlow$' -v
```

The GnuPG compatibility test uses a generated fixture key and a disposable keyring, and skips when GnuPG is absent. Coverage includes wrong signatures, signing subkeys, expired/revoked/private keys, challenge expiry/replacement/replay, ownership and CSRF checks, verified-only downloads/API, multiple-key bundles, deletion and account merge/undo.

A synthetic local preview is available through the HTTP integration fixture:

```sh
BTCPP_POSTGRES_SMOKE=1 PGP_BROWSER_PREVIEW=1 DATABASE_URL=... go test ./internal/handlers -run '^TestProfilePGPHTTPFlow$' -v -timeout 0
```

Open `http://127.0.0.1:8095/preview`. The fixture logs its public profile URL and includes verified and pending keys. It uses the real templates and key handlers. Unrelated navigation/account-status endpoints are not mounted in this fixture. Desktop (1440px), mobile (390px) and narrow mobile (320px) layouts were checked, including challenge generation through the browser.

References: [OpenPGP RFC 9580](https://www.rfc-editor.org/rfc/rfc9580.html), [ProtonMail go-crypto](https://github.com/ProtonMail/go-crypto).
