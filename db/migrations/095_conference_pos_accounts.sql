ALTER TABLE conference_pos_sales ADD COLUMN buyer_person_id uuid REFERENCES people(id) ON DELETE SET NULL;
CREATE INDEX conference_pos_sales_buyer ON conference_pos_sales(buyer_person_id,created_at DESC) WHERE buyer_person_id IS NOT NULL;
