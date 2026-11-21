-- Standard tiers retain cumulative limits; evergreen tickets have their own caps.
ALTER TABLE conference_tickets
 ADD COLUMN evergreen boolean NOT NULL DEFAULT false,
 ADD COLUMN disabled boolean NOT NULL DEFAULT false,
 ADD COLUMN allow_discounts boolean NOT NULL DEFAULT true,
 ADD COLUMN ticket_kind text NOT NULL DEFAULT 'general',
 ADD COLUMN description text NOT NULL DEFAULT '';
ALTER TABLE registrations ADD COLUMN ticket_id uuid REFERENCES conference_tickets(id) ON DELETE SET NULL;
CREATE INDEX registrations_ticket_id ON registrations(ticket_id) WHERE NOT revoked;
ALTER TABLE discounts
 ADD COLUMN checkout_conference_id uuid REFERENCES conferences(id) ON DELETE CASCADE,
 ADD COLUMN checkout_label text NOT NULL DEFAULT '',
 ADD COLUMN checkout_enabled boolean NOT NULL DEFAULT false;
CREATE INDEX discounts_checkout_event ON discounts(checkout_conference_id);
CREATE TABLE ticket_reservations (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 conference_id uuid NOT NULL REFERENCES conferences(id) ON DELETE CASCADE,
 ticket_id uuid NOT NULL REFERENCES conference_tickets(id),
 quantity integer NOT NULL CHECK(quantity BETWEEN 1 AND 8),
 status text NOT NULL DEFAULT 'pending' CHECK(status IN ('pending','fulfilled','released')),
 provider text NOT NULL CHECK(provider IN ('stripe','opennode')),
 provider_id text NOT NULL DEFAULT '',
 email citext NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now(),
 updated_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX ticket_reservations_pending ON ticket_reservations(conference_id,ticket_id) WHERE status='pending';

-- Preserve existing local/student offers at the current price. Their former
-- event-wide limit becomes the initial per-type cap, editable in event setup.
WITH counts AS (SELECT conference_id,count(*) AS sold FROM registrations WHERE NOT revoked AND lower(type)<>'sponsored' GROUP BY conference_id),
chosen AS (
 SELECT DISTINCT ON (t.conference_id) t.*, max(t.max_count) OVER (PARTITION BY t.conference_id) AS event_cap
 FROM conference_tickets t LEFT JOIN counts c ON c.conference_id=t.conference_id
 ORDER BY t.conference_id,
 coalesce((t.sales_end_at>now() AND t.max_count>coalesce(c.sold,0)),false) DESC,
 CASE WHEN t.sales_end_at>now() AND t.max_count>coalesce(c.sold,0) THEN t.sales_end_at END ASC,
 t.sales_end_at DESC NULLS LAST,t.id
)
INSERT INTO conference_tickets(conference_id,ticket_key,tier,base_price,btc_price,usd_price,max_count,currency,symbol,post_symbol,stripe_tax_code,card_surcharge_bps,evergreen,ticket_kind,description)
SELECT t.conference_id,'evergreen-'||v.kind,initcap(v.kind),v.price,v.price,v.price,greatest(t.event_cap,1),t.currency,t.symbol,t.post_symbol,t.stripe_tax_code,t.card_surcharge_bps,true,v.kind,
 coalesce(CASE v.kind WHEN 'local' THEN c.local_ticket_body ELSE c.student_ticket_body END,'')
FROM chosen t JOIN conferences c ON c.id=t.conference_id
CROSS JOIN LATERAL (VALUES ('local',t.local_price),('student',t.student_price)) v(kind,price)
WHERE v.price>0;
UPDATE registrations r SET ticket_id=t.id FROM conference_tickets t
WHERE t.conference_id=r.conference_id AND t.evergreen AND t.ticket_kind=lower(r.type);
-- Old columns are retained for old paid-checkout compatibility, but new setup
-- and public cards use the independent ticket records above.
