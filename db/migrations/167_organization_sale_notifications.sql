-- One notice per organization and confirmed purchase. Do not backfill old sales.
CREATE TABLE organization_sale_notifications (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 organization_id uuid NOT NULL REFERENCES organizations(id) ON DELETE CASCADE,
 order_id uuid REFERENCES shop_orders(id) ON DELETE CASCADE,
 pos_sale_id uuid REFERENCES conference_pos_sales(id) ON DELETE CASCADE,
 created_at timestamptz NOT NULL DEFAULT now(),
 next_attempt_at timestamptz NOT NULL DEFAULT now(),
 sent_at timestamptz,
 CHECK (num_nonnulls(order_id, pos_sale_id) = 1),
 UNIQUE (organization_id, order_id),
 UNIQUE (organization_id, pos_sale_id)
);
CREATE INDEX organization_sale_notifications_pending
 ON organization_sale_notifications(next_attempt_at, id) WHERE sent_at IS NULL;
