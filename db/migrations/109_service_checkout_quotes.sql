-- Server-generated pricing and tax evidence reviewed before order confirmation.
-- The caller submits only this opaque identifier, never a price or tax snapshot.
CREATE TABLE service_checkout_quotes (
 id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
 person_id uuid REFERENCES people(id) ON DELETE SET NULL,
 quote_snapshot jsonb NOT NULL CHECK (jsonb_typeof(quote_snapshot)='object'),
 tax_snapshot jsonb NOT NULL CHECK (jsonb_typeof(tax_snapshot)='object'),
 expires_at timestamptz NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX service_checkout_quotes_person ON service_checkout_quotes(person_id,created_at);
CREATE FUNCTION protect_service_checkout_quote() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.id IS DISTINCT FROM OLD.id OR NEW.quote_snapshot IS DISTINCT FROM OLD.quote_snapshot
  OR NEW.tax_snapshot IS DISTINCT FROM OLD.tax_snapshot OR NEW.expires_at IS DISTINCT FROM OLD.expires_at
  OR NEW.created_at IS DISTINCT FROM OLD.created_at
  OR (NEW.person_id IS DISTINCT FROM OLD.person_id AND NEW.person_id IS NOT NULL)
 THEN
  RAISE EXCEPTION 'checkout quotes cannot be rewritten or reassigned' USING ERRCODE='23514';
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER service_checkout_quote_immutable BEFORE UPDATE ON service_checkout_quotes
FOR EACH ROW EXECUTE FUNCTION protect_service_checkout_quote();
