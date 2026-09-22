# Speaker application deadlines

In `/admin`, open the event's **Event details** page (`/{tag}/admin/details`). Set **Speaker applications close** and save event details. Dates and times use the event timezone, not the administrator's browser timezone. Invalid dates or timezone names are rejected.

For Seoul applications through Friday, September 25, 2026, use timezone `Asia/Seoul` and set the close time to **September 26, 2026 at 00:00**. That is September 25 at 15:00 UTC. The change takes effect when saved, including reopening applications whose previous deadline passed. Applications close at the exact saved instant.

Clear the field to restore the default: 45 days before the event starts, or 35 days for Nairobi. Draft, inactive, and ended events remain closed regardless of the deadline. Existing submissions are retained.

The deadline is stored in nullable `conferences.speaker_applications_close` (`timestamptz`), added by migration 113. The application checks, application-page deadline labels, and automatically generated important date use the same effective deadline. Hand-written campaign copy and custom important-date entries are not rewritten.

The web application runs migrations on startup. Deploy the change before using this setting in production. This implementation does not set a production deadline automatically.
