-- The initial broadcast cadence included legacy YouTube channel rows. Repair
-- only the untouched seed; an administrator's edited broadcast slots win.
WITH original AS (
 SELECT coalesce(min(timezone),'America/Chicago') AS timezone,
        coalesce(jsonb_agg(jsonb_build_object('day',weekday,'time',to_char(publish_time,'HH24:MI')) ORDER BY weekday,publish_time),'[]'::jsonb) AS slots
 FROM youtube_publish_slots WHERE active
), current_channel AS (
 SELECT coalesce(min(timezone),'America/Chicago') AS timezone,
        coalesce(jsonb_agg(jsonb_build_object('day',weekday,'time',to_char(publish_time,'HH24:MI')) ORDER BY weekday,publish_time),'[]'::jsonb) AS slots
 FROM youtube_publish_slots WHERE active AND channel='btcpp-youtube'
)
UPDATE publishing_channels c
SET timezone=current_channel.timezone, slots=current_channel.slots, updated_at=now()
FROM original,current_channel
WHERE c.id='broadcasts'
 AND c.timezone=original.timezone AND c.slots=original.slots
 AND c.updated_at <= (SELECT applied_at FROM schema_migrations WHERE version=156);
