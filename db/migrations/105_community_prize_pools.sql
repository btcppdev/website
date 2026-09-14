CREATE TABLE community_prize_pools (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 conference_id uuid NOT NULL UNIQUE REFERENCES conferences(id),
 slug text NOT NULL UNIQUE CHECK(slug ~ '^[a-z0-9][a-z0-9._-]{0,63}$'),
 domain text NOT NULL DEFAULT 'btcplusplus.dev',
 node_id text NOT NULL,
 description text NOT NULL,
 offer_id text NOT NULL DEFAULT '',
 offer text NOT NULL DEFAULT '',
 status text NOT NULL DEFAULT 'draft' CHECK(status IN ('draft','open','closing','closed')),
 dns_published boolean NOT NULL DEFAULT false,
 endpoint_created boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 closed_at timestamptz,
 UNIQUE(node_id,slug,domain)
);
CREATE UNIQUE INDEX community_pool_offer ON community_prize_pools(node_id,offer_id) WHERE offer_id <> '';
CREATE TABLE community_payment_receipts (
 node_id text NOT NULL,
 payment_hash text NOT NULL CHECK(payment_hash ~ '^[0-9a-f]{64}$'),
 pay_index bigint NOT NULL CHECK(pay_index > 0),
 received_msat bigint NOT NULL CHECK(received_msat > 0),
 paid_at timestamptz NOT NULL,
 offer_id text NOT NULL DEFAULT '',
 identifier text NOT NULL DEFAULT '',
 review_reason text NOT NULL DEFAULT '',
 description text NOT NULL DEFAULT '',
 payer_note text NOT NULL DEFAULT '',
 PRIMARY KEY(node_id,payment_hash)
);
CREATE TABLE community_pool_credits (
 node_id text NOT NULL,
 payment_hash text NOT NULL,
 pool_id uuid NOT NULL REFERENCES community_prize_pools(id),
 late boolean NOT NULL DEFAULT false,
 created_at timestamptz NOT NULL DEFAULT now(),
 PRIMARY KEY(node_id,payment_hash),
 FOREIGN KEY(node_id,payment_hash) REFERENCES community_payment_receipts(node_id,payment_hash)
);
CREATE INDEX community_credits_pool ON community_pool_credits(pool_id,node_id,payment_hash);
CREATE TABLE community_payment_cursors (
 node_id text PRIMARY KEY,
 pay_index bigint NOT NULL DEFAULT 0,
 synced_at timestamptz
);
CREATE TABLE community_pool_audit (
 id bigserial PRIMARY KEY,
 pool_id uuid NOT NULL REFERENCES community_prize_pools(id),
 actor text NOT NULL,
 action text NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
ALTER TABLE prizes ADD COLUMN community_pool_id uuid REFERENCES community_prize_pools(id);

-- PostgreSQL delivers these only after commit. No payment data is broadcast.
CREATE FUNCTION notify_community_pool_changes() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 PERFORM pg_notify('btcpp_community_pools','');
 RETURN NULL;
END;
$$;
CREATE TRIGGER community_pool_changed AFTER INSERT OR UPDATE OR DELETE ON community_prize_pools FOR EACH STATEMENT EXECUTE FUNCTION notify_community_pool_changes();
CREATE TRIGGER community_credits_changed AFTER INSERT OR UPDATE OR DELETE ON community_pool_credits FOR EACH STATEMENT EXECUTE FUNCTION notify_community_pool_changes();
CREATE TRIGGER community_monitor_changed AFTER INSERT OR UPDATE OR DELETE ON community_payment_cursors FOR EACH STATEMENT EXECUTE FUNCTION notify_community_pool_changes();
CREATE TRIGGER community_visibility_changed AFTER UPDATE OR DELETE ON conferences FOR EACH STATEMENT EXECUTE FUNCTION notify_community_pool_changes();
