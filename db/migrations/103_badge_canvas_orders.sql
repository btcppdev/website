-- Store the selected design with the sale, independently of later badge edits.
ALTER TABLE shop_order_items ADD COLUMN badge_canvas jsonb;
ALTER TABLE shop_order_items ADD CONSTRAINT shop_order_items_badge_canvas_object
  CHECK (badge_canvas IS NULL OR jsonb_typeof(badge_canvas) = 'object');

-- Prepare the product for merchant configuration; never start selling at a
-- placeholder price or with invented shipping dimensions.
WITH product AS (
  INSERT INTO merch_products(tag,slug,name,subtitle,description,status,product_type,base_price_cents,currency,requires_shipping,allow_event_pickup)
  VALUES ('badge-canvas','badge-canvas','Your badge, on canvas.','9×9-inch badge canvas','A 9×9-inch square canvas featuring a badge issued to your Bitcoin++ account. Choose your design before adding it to your cart.','draft','badge_canvas',6500,'USD',true,true)
  RETURNING id
), variant AS (
  INSERT INTO merch_variants(product_id,sku,label,inventory_policy,status)
  SELECT id,'BADGE-CANVAS-9X9','9×9-inch canvas','unlimited','active' FROM product
)
INSERT INTO merch_product_images(product_id,object_key,alt_text,is_primary)
SELECT id,'/static/img/merch/badge-canvas.svg','Illustration of a personalized square badge canvas',true FROM product;
