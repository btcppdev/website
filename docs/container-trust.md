# Container CA trust policy

The October 22, 2026 container change requires Alpine's October 20 CA package
release or newer, removes affected root sources if present, regenerates the
system bundle/hash links, and verifies every certificate in `/etc/ssl/certs`.
The build fails if a blocked root remains or the resulting store is empty.

Blocked subject common names:

- `emSign ECC Root CA - G3`
- `emSign Root TLS CA - G3`
- `AAA Certificate Services` (the alternate cross-signing trust path)

This is a targeted root-store mitigation for clients without reliable intermediate
revocation enforcement. It can reject legitimate services still chaining to these
roots; those services must migrate their certificate chains. It does not disable
TLS verification or change btcpp.dev's public Let's Encrypt certificate.

Sources:

- https://groups.google.com/a/mozilla.org/g/dev-security-policy/c/0R3XLJL6FuE
- https://access.redhat.com/security/vulnerabilities/RHSB-2026-012
- https://linux.oracle.com/errata/ELSA-2026-500449.html

The trust guard is copied before the package-install layer, invalidating the old
cached layer for this release. Future CA updates still require a fresh package
layer or a no-cache rebuild; `apk --no-cache` alone does not bypass Docker caching.

Validation: `go test ./cmd/container-trust` covers blocked roots, concatenated
bundles, hashed certificate symlinks, empty stores, and preservation of unrelated
roots. The guard also accepted all 118 certificates in Alpine v3.22's published
`ca-certificates-bundle-20261020-r0` package during local verification.

After deployment, use the App Platform console to confirm:

```
apk info -v | grep -E '^ca-certificates(-bundle)?-[0-9]'
GO111MODULE=off GOTOOLCHAIN=local go run /tmp/container-trust.go verify /etc/ssl/certs
```

The successful image build runs the same check. Updating the system bundle does
not claim to update independent browser/application trust stores such as
Chromium's own root store. Chromium is refreshed by this build's package upgrade,
but the trust guard specifically verifies the system store used by Go HTTPS clients.

The final build repeats the verification after application compilation and emits
`/static/security/container-trust.txt` with the policy date, successful check and
package versions. This public, non-secret build receipt lets us distinguish a
completed rollout from a push that has only queued a deployment.
