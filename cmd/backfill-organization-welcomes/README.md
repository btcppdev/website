# Organization welcome notices: launch backfill

Run this from the repository root **after deploying the organization welcome email and organization pages**, against the production database and mailer. This is a required launch step; it is not run on application startup or by database migrations.

1. Record the deployment time in UTC as the launch cutoff. Keep that same cutoff for review, sending, and retries.
2. Review a dry run (the default):

   ```sh
   go run ./cmd/backfill-organization-welcomes -env .env.prod -before '<LAUNCH_UTC_RFC3339>'
   ```

3. Send the launch notices:

   ```sh
   MAILER_OFF=false go run ./cmd/backfill-organization-welcomes -env .env.prod -before '<LAUNCH_UTC_RFC3339>' -send
   ```

4. Confirm `missing-email=0 failed=0`, and verify delivery in the mailer. Resolve any reported missing email addresses or send failures, then rerun the same command with the same cutoff. Record completion with the launch notes.

The command covers active members, managers, and owners, including organizations hidden from the directory. Removed memberships and pending invitations are excluded. A person in multiple organizations receives one welcome per organization. Current membership, role, and primary email are checked again before queuing each notice.

The normal add flow and backfill share the same email and mailer job key, based on organization, person, and membership creation time. Repeated runs and subsequent role changes reuse that key, so the mailer's idempotent queue does not create duplicate notices. `queued` counts successful queue requests, including requests for an existing job; it is not an inbox-delivery count. Mailer delivery records must be retained through the rollout and retries.

The dry run does not send mail or modify the database. Missing email addresses and failures produce a nonzero exit status; other eligible memberships are still processed in send mode. Process environment variables take precedence over the selected environment file.
