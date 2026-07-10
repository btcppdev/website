ALTER TABLE conference_tickets
  ADD COLUMN IF NOT EXISTS base_price integer NOT NULL DEFAULT 0,
  ADD COLUMN IF NOT EXISTS card_surcharge_bps integer NOT NULL DEFAULT 1000;

UPDATE conference_tickets
SET base_price = CASE
    WHEN base_price > 0 THEN base_price
    WHEN btc_price > 0 THEN btc_price
    ELSE usd_price
  END,
  card_surcharge_bps = CASE
    WHEN card_surcharge_bps > 0 THEN card_surcharge_bps
    ELSE 1000
  END;

ALTER TABLE conference_tickets
  DROP CONSTRAINT IF EXISTS conference_tickets_base_price_check,
  DROP CONSTRAINT IF EXISTS conference_tickets_card_surcharge_bps_check;

ALTER TABLE conference_tickets
  ADD CONSTRAINT conference_tickets_base_price_check CHECK (base_price >= 0),
  ADD CONSTRAINT conference_tickets_card_surcharge_bps_check CHECK (card_surcharge_bps >= 0);
