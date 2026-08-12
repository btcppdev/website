# Provision conference missives

Creates the reusable one-shot missives used as source templates for event email campaigns. Existing templates are never overwritten.

Preview the changes against the database configured in an environment file:

```sh
nix develop -c go run ./cmd/provision-conference-missives -env .env.prod
```

Apply them after reviewing the output:

```sh
nix develop -c go run ./cmd/provision-conference-missives -env .env.prod -apply
```

You can also pass `-database-url`. The command defaults to a rolled-back dry run; only `-apply` commits rows.

After provisioning, the templates appear under the **One-shots** tab in `/admin/missives`. Editing a source template affects campaigns created for future events. Existing event campaigns keep their event-specific copy.
