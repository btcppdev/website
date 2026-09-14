# Local CLN transport

Copied from `github.com/niftynei/lnsocket/go` at commit
`a24b3399ff83` (Go module version
`v0.0.0-20260904033035-a24b3399ff83`). The upstream MIT license is
retained in LICENSE. This package is maintained here so transport fixes and
regression tests run with bitcoin++'s normal backend checks.

Local changes:

- Keep the connection deadline in force throughout the BOLT-8 handshake;
  cancellation closes a stalled handshake connection.
- Wait for the background reader to exit before clearing connection state or
  reusing a client. A reader started after close and a pong attempted after
  close return safely instead of dereferencing a nil transport.
- Close the previous connection before a sequential reconnect. Connection setup,
  initialization, and reconnect must be serialized by the caller; concurrent
  RPCs on an initialized connection are supported. bitcoin++ uses a new client
  per RPC.

## Inherited accts security review (2026-09-13)

The old LND dependency was already removed upstream. The copied code uses btcec
and Go's crypto packages for authenticated BOLT-8 transport; this change does
not alter cryptographic primitives or the handshake/wire format. Upstream
handshake, encryption, key-rotation, initialization, and concurrent-response
correlation tests are retained. Lifecycle tests cover a stalled handshake with
a deadline or explicit cancellation, reader shutdown, client state reuse, and an interrupted connection with a pending RPC.

Review found and fixed a disconnect-time nil dereference and a handshake whose
deadline was cleared before it ran. Tests pass under Go's race detector. This
is a focused lifecycle review, not an independent cryptographic audit. Live
Bookkeeper/Commando compatibility with the production read-only rune still
needs verification after deployment. Init and RPC I/O cancellation, aggregate
response buffering under concurrent requests, and untrusted peer feature/ping
validation warrant further hardening; the lifecycle tests do not establish
coverage of those cases.

## bitcoin++ prize-pool integration

Copied from the locally maintained accts implementation, including its lifecycle
fixes and tests. Prize-pool Commando calls use a dedicated connection, closed on
context cancellation even during init or blocked RPC writes. This integration
also validates init vector lengths and ping payload lengths/response size.
A connection handles one RPC, bounding aggregate response state to one request.
