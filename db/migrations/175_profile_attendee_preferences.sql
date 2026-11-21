ALTER TABLE people
  ADD COLUMN dietary_preference text NOT NULL DEFAULT '' CHECK (dietary_preference IN ('','none','vegetarian','vegan','other')),
  ADD COLUMN dietary_other text NOT NULL DEFAULT '' CHECK (length(dietary_other) <= 500),
  ADD COLUMN food_allergies text NOT NULL DEFAULT '' CHECK (length(food_allergies) <= 500);
