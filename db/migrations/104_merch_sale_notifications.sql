-- Enqueue with the paid transition; existing sales are deliberately not backfilled.
CREATE TABLE merch_sale_notifications (
 order_id uuid PRIMARY KEY REFERENCES shop_orders(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 sent_at timestamptz
);
CREATE INDEX merch_sale_notifications_pending ON merch_sale_notifications(next_attempt_at) WHERE sent_at IS NULL;
