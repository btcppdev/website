# Invite speakers to an existing talk by email

Open the talk editor, then **Speakers → Invite speaker by email**. The invitation form preselects the existing talk. Enter the email address; the name is optional. Send one invitation per recipient. The form also remains available from `/{event}/admin/invite-speaker`, where any non-declined talk in that event can be selected.

Existing accounts are reused by email. New email-only accounts receive the temporary display name “Invited speaker”; the invitation form asks for their name and replaces the placeholder when they save. An invitation links the person to the existing proposal without replacing its title, description, or schedule. Co-speaker submissions cannot overwrite existing talk content.

Direct invitation links bind the proposal token and recipient's person ID with a purpose-scoped signature. Each recipient opens their own profile, independent of the order other invitees open their links. A signed form cannot submit a different recipient email. Generic share links still permit joining, but do not prefill an existing co-speaker's personal data. Previously issued generic invitations may need to be resent to receive a personalized link when the person is already attached.

Accepted talks allow joining; declined/rejected talks and the existing event invitation cutoff remain protected. Revoking the proposal token invalidates its recipient links too. Invitation confirmation pages retain the exact recipient in the URL.

Only the selected recipient receives the direct invitation email. Development tests use an isolated local database and do not send mail.
