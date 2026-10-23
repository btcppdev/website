-- Expedition-only availability requests; independent of newsletter consent.
CREATE TABLE expedition_waitlist (
    person_id uuid PRIMARY KEY REFERENCES people(id) ON DELETE CASCADE,
    contact_id uuid NOT NULL REFERENCES email_contacts(id) ON DELETE CASCADE,
    notification_key uuid NOT NULL DEFAULT gen_random_uuid(),
    subscribed boolean NOT NULL DEFAULT true,
    created_at timestamptz NOT NULL DEFAULT now(),
    notified_at timestamptz
);
CREATE INDEX expedition_waitlist_pending ON expedition_waitlist(created_at)
    WHERE subscribed AND notified_at IS NULL;
