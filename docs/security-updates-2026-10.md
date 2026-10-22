# October 2026 dependency refresh

This release upgrades the Go runtime from 1.25.10 to 1.27.3 and the container
base to `golang:1.27.3-alpine3.24`. The package layer runs `apk upgrade` before
installing Chromium, FFmpeg, and the CA packages. The eMudhra trust guard remains
in place and fails the build if blocked certificates remain.

Go dependencies were updated within their existing major-version module paths.
Stripe stays on its preview SDK (`v86.5.0-beta.1`) because the application's FX
quote functionality requires preview endpoints. Browser rendering and passkey
registration were adapted to the updated chromedp and WebAuthn APIs.

Validation before deployment:

- Full `go test ./...`, with local report database integration tests enabled.
- Actual Chrome PNG and PDF rendering, including output dimensions and file format.
- Existing social-card rendering, payment, authentication, and trust-guard tests.
- `go mod verify` and Linux/amd64 production build.
- `govulncheck` v1.9.0 rebuilt with Go 1.27.3, scanning `./...` for Linux/amd64:
  no affected imported packages or reachable symbols. The only module-level
  finding is GO-2026-5932, for unmaintained `golang.org/x/crypto/openpgp`, which
  is not imported. The application uses `github.com/ProtonMail/go-crypto/openpgp`.
- Trivy v0.76.1 scan of the upstream base image (Alpine 3.24.3): no known
  vulnerabilities. This base scan excludes packages installed by our Dockerfile.

The build publishes non-secret deployment receipts under `/static/security/`:

- `container-trust.txt`: certificate guard results and CA package versions.
- `runtime-versions.txt`: Go, Alpine, and installed package versions.
- `apk-installed.txt`: package metadata (not configuration or file contents) for
  scanning the actual installed OS dependency set after deployment.

For an OS-only post-deployment scan, save the APK receipt as
`ROOT/lib/apk/db/installed` and the Alpine version from the runtime receipt as
`ROOT/etc/alpine-release`, then run:

```
trivy rootfs --scanners vuln --pkg-types os ROOT
```

These checks cover known published advisories; they are not a guarantee that
all application security issues have been found.
