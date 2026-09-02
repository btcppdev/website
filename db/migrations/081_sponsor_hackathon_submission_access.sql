ALTER TABLE sponsorship_entitlements
ADD COLUMN all_hackathon_submissions_access boolean NOT NULL DEFAULT false,
ADD COLUMN automatic_submission_contact_access boolean NOT NULL DEFAULT false;

-- Hackathon and headline sponsorships include full submission visibility by
-- default. Keep the copied entitlement explicit so later tier renames do not
-- silently change an existing agreement.
UPDATE sponsorship_entitlements entitlements
SET all_hackathon_submissions_access = true,
    automatic_submission_contact_access = true,
    participant_contact_access = true,
    participant_contact_export = true
FROM sponsorships
WHERE sponsorships.id = entitlements.sponsorship_id
  AND lower(sponsorships.level) IN ('hackathon', 'headline');
