# Account screen review

The gallery renders the real account templates with sample identities and invalid tokens. It covers sign-in, OAuth consent, email verification, password recovery, account linking, reauthentication and managed signer approvals, including expired links and errors.

From the repository root:

```sh
BTCPP_ACCOUNT_PREVIEW=1 go test ./internal/handlers -run '^TestAccountFlowGallery$' -timeout 0 -v
```

Open http://127.0.0.1:8138/preview/account. Select a state in the sidebar, switch between desktop and mobile, or open a full page. Use **Refresh preview** after editing a template or stylesheet. Templates reload on every request; fixture or gallery HTML changes require restarting the process.

This test-only server listens on loopback. It has no database or real auth handlers, rejects POST requests, blocks form submissions, and pauses the signer's automatic return. It does not send mail, authorize applications, or change accounts. It is not registered in production.

Render all sample states without starting a server:

```sh
go test ./internal/handlers -run '^TestAccountFlowPagesRender$'
```
