-- Invoices preserve the seller, purchaser and original purchase amounts at issue.
CREATE TABLE purchase_invoices (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 order_id uuid NOT NULL UNIQUE REFERENCES shop_orders(id),
 person_id uuid REFERENCES people(id) ON DELETE SET NULL,
 issued_at timestamptz NOT NULL DEFAULT now(),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object')
);
CREATE INDEX purchase_invoices_person ON purchase_invoices(person_id,id);
CREATE FUNCTION purchase_invoice_immutable() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.id IS DISTINCT FROM OLD.id OR NEW.order_id IS DISTINCT FROM OLD.order_id
 OR NEW.issued_at IS DISTINCT FROM OLD.issued_at OR NEW.snapshot IS DISTINCT FROM OLD.snapshot THEN
  RAISE EXCEPTION 'Issued invoices cannot be rewritten';
 END IF;
 RETURN NEW;
END $$;
CREATE TRIGGER purchase_invoice_immutable BEFORE UPDATE ON purchase_invoices
FOR EACH ROW EXECUTE FUNCTION purchase_invoice_immutable();
