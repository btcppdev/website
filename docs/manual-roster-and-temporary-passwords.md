# Manual roster entries and temporary passwords

## Volunteer coordinators

Open `/{event}/volcoord` and expand **Add to roster**. Enter the person's
name and email and confirm that the address belongs to them. This is an
organizer-confirmed identity entry, not an email-verification message.

An existing email alias links the entry to its account without changing its
profile or staff/admin roles. A new address creates a profile. Duplicate
person/event entries are rejected, including when another alias is used.

The new entry starts at **Pending Shifts**, ready for assignments. Adding it
does not send email, subscribe it to newsletters, or issue tickets. Use the
existing scheduling/onboarding actions when ready.

## Global admins

Open `/admin/people`, then **Issue a temporary password**. You must have
signed in within the last 15 minutes. Enter an existing account's email.
Supply a password of 12–128 characters, or leave it blank to generate one.

The result page displays the password for you to share directly. It is not
emailed automatically or stored in plaintext. Issuing it replaces the old
password, revokes browser sessions and unused password-reset links, and
expires after 72 hours.

When the recipient signs in at `/login` with that password, they receive a
restricted session with no account access. They have 15 minutes to choose
a different password. Successful replacement establishes a normal login.
Reissuing or resetting the password invalidates earlier replacement proofs.
Other independent sign-in methods, such as a verified email link or passkey,
remain available. Normal password recovery clears the temporary restriction.

Deploy database migration `151_temporary_passwords.sql` before the new code.
