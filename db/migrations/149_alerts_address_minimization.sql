-- Alerts checkout does not retain billing addresses. Provider IDs, amounts,
-- expiry, payment evidence and separately requested invoices remain intact.
CREATE OR REPLACE FUNCTION alerts_private_tax_snapshot(value jsonb) RETURNS jsonb
LANGUAGE sql IMMUTABLE AS $$
 SELECT jsonb_build_object(
  'OrderID',coalesce(value->>'OrderID',''),
  'Provider',value->>'Provider', 'ProviderQuoteID',value->>'ProviderQuoteID',
  'SalesTaxAmountCents',value->'SalesTaxAmountCents', 'ExpiresAt',value->'ExpiresAt',
  'DestinationCountry','', 'DestinationRegion','', 'DestinationPostalCode','', 'RawResponse','{}')
$$;

-- The migration transaction holds the table lock while the immutable-quote
-- trigger is disabled. Only address-bearing tax evidence is removed.
ALTER TABLE service_checkout_quotes DISABLE TRIGGER service_checkout_quote_immutable;
UPDATE service_checkout_quotes SET tax_snapshot=alerts_private_tax_snapshot(tax_snapshot)
 WHERE quote_snapshot->>'product_code'='alerts.repository-slots';
ALTER TABLE service_checkout_quotes ENABLE TRIGGER service_checkout_quote_immutable;

UPDATE tax_quotes t SET destination_country='',destination_region='',destination_postal_code='',raw_tax_response='{}',raw_import_response='{}'
 WHERE EXISTS (SELECT 1 FROM service_purchase_requests r WHERE r.order_id=t.order_id AND r.quote_snapshot->>'product_code'='alerts.repository-slots');
UPDATE tax_transactions t SET raw_response='{}'
 WHERE EXISTS (SELECT 1 FROM service_purchase_requests r WHERE r.order_id=t.order_id AND r.quote_snapshot->>'product_code'='alerts.repository-slots');

-- Enforce minimization even for an older worker during a rolling deployment.
CREATE OR REPLACE FUNCTION minimize_alerts_checkout_address() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF NEW.quote_snapshot->>'product_code'='alerts.repository-slots' THEN
  NEW.tax_snapshot := alerts_private_tax_snapshot(NEW.tax_snapshot);
 END IF;
 RETURN NEW;
END;
$$;
DROP TRIGGER IF EXISTS alerts_checkout_address_minimization ON service_checkout_quotes;
CREATE TRIGGER alerts_checkout_address_minimization BEFORE INSERT ON service_checkout_quotes FOR EACH ROW EXECUTE FUNCTION minimize_alerts_checkout_address();

CREATE OR REPLACE FUNCTION minimize_alerts_tax_address() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF EXISTS(SELECT 1 FROM service_purchase_requests r WHERE r.order_id=NEW.order_id AND r.quote_snapshot->>'product_code'='alerts.repository-slots') THEN
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
