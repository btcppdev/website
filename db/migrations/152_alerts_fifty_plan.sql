BEGIN;
ALTER TABLE service_stripe_checkouts DROP CONSTRAINT service_stripe_checkouts_product_code_check;
ALTER TABLE service_stripe_checkouts ADD CONSTRAINT service_stripe_checkouts_product_code_check
 CHECK(product_code IN ('alerts.repository-slots','alerts.projects-50.annual','insider.membership.monthly','insider.membership.annual'));
ALTER TABLE service_stripe_checkouts ALTER COLUMN product_family SET EXPRESSION AS
 (CASE WHEN product_code IN ('alerts.repository-slots','alerts.projects-50.annual') THEN 'alerts' ELSE 'insider' END);
ALTER TABLE service_entitlements DROP CONSTRAINT service_entitlements_product_code_check;
ALTER TABLE service_entitlements ADD CONSTRAINT service_entitlements_product_code_check
 CHECK(product_code IN ('alerts.repository-slots','alerts.projects-50.annual','insider.membership','insider.article'));

-- Apply the existing address minimization to both Alerts plans.
CREATE OR REPLACE FUNCTION minimize_alerts_checkout_address() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.quote_snapshot->>'product_code' IN ('alerts.repository-slots','alerts.projects-50.annual') THEN
  NEW.tax_snapshot := alerts_private_tax_snapshot(NEW.tax_snapshot);
 END IF;
 RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS alerts_checkout_address_minimization ON service_checkout_quotes;
CREATE TRIGGER alerts_checkout_address_minimization BEFORE INSERT ON service_checkout_quotes FOR EACH ROW EXECUTE FUNCTION minimize_alerts_checkout_address();

CREATE OR REPLACE FUNCTION minimize_alerts_tax_address() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM service_purchase_requests r WHERE r.order_id=NEW.order_id AND r.quote_snapshot->>'product_code' IN ('alerts.repository-slots','alerts.projects-50.annual')) THEN
  IF TG_TABLE_NAME='tax_quotes' THEN
   NEW.destination_country := ''; NEW.destination_region := ''; NEW.destination_postal_code := '';
   NEW.raw_tax_response := '{}'; NEW.raw_import_response := '{}';
  ELSE
   NEW.raw_response := '{}';
  END IF;
 END IF;
 RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS alerts_tax_address_minimization ON tax_quotes;
CREATE TRIGGER alerts_tax_address_minimization BEFORE INSERT OR UPDATE ON tax_quotes FOR EACH ROW EXECUTE FUNCTION minimize_alerts_tax_address();
DROP TRIGGER IF EXISTS alerts_tax_address_minimization ON tax_transactions;
CREATE TRIGGER alerts_tax_address_minimization BEFORE INSERT OR UPDATE ON tax_transactions FOR EACH ROW EXECUTE FUNCTION minimize_alerts_tax_address();

COMMIT;
