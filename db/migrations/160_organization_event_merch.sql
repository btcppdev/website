-- Preserve the responsible organization on event-register purchases as well.
ALTER TABLE conference_pos_sale_items ADD COLUMN fulfillment_organization_id uuid REFERENCES organizations(id) ON DELETE RESTRICT;
CREATE INDEX conference_pos_items_fulfillment_org_idx ON conference_pos_sale_items(fulfillment_organization_id);
UPDATE conference_pos_sale_items i SET fulfillment_organization_id=p.fulfillment_organization_id FROM merch_variants v JOIN merch_products p ON p.id=v.product_id WHERE v.id=i.variant_id AND p.fulfillment_organization_id IS NOT NULL;
