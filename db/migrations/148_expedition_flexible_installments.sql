-- Allow equal installments over remaining dates without changing accepted contracts.
-- Copy only the current expedition revision containing the original five-payment schedule.
SELECT pg_advisory_xact_lock(hashtextextended('service-policy:expedition.antarctica2028.passage',0));
DO $$
DECLARE
 old_policy service_legal_revisions%ROWTYPE;
 new_terms text;
 encoded text;
 new_version text := 'antarctica-2026-10-24-flexible-installments';
BEGIN
 SELECT r.* INTO old_policy FROM service_legal_current c
 JOIN service_legal_revisions r USING(product_code,version)
 WHERE c.product_code='expedition.antarctica2028.passage';
 IF NOT FOUND OR position('Choose full payment at signup or five installments: at booking, January 1, April 1, July 1 and October 1, 2027.' in old_policy.terms_text)=0 THEN RETURN; END IF;
 new_terms := replace(old_policy.terms_text,'Choose full payment at signup or five installments: at booking, January 1, April 1, July 1 and October 1, 2027.',
   'Choose full payment at signup or equal installments across signup and the remaining scheduled dates: January 1, April 1, July 1 and October 1, 2027. Dates on or before the signup date (America/Chicago) are omitted. Any rounding difference is included in the final payment. Your payment schedule is fixed when you book. If no scheduled dates remain, payment in full is required. The non-refundable portion remains 20% of the passage fare even if the signup payment is larger.');
 -- Match encoding/json.Marshal([]string{terms, privacy}), including HTML escaping.
 encoded := '[' || to_json(new_terms)::text || ',' || to_json(old_policy.privacy_text)::text || ']';
 encoded := replace(replace(replace(replace(replace(encoded,
   '&', E'\\u0026'), '<', E'\\u003c'), '>', E'\\u003e'),
   chr(8232), E'\\u2028'), chr(8233), E'\\u2029');
 INSERT INTO service_legal_revisions(product_code,version,terms_text,privacy_text,content_sha256,effective_at)
 VALUES(old_policy.product_code,new_version,new_terms,old_policy.privacy_text,
   encode(sha256(convert_to(encoded,'UTF8')),'hex'), greatest(now(),old_policy.effective_at));
 INSERT INTO expedition_lifecycle_policies(product_code,version,time_zone)
 SELECT product_code,new_version,time_zone FROM expedition_lifecycle_policies WHERE version=old_policy.version;
 UPDATE service_legal_current SET version=new_version WHERE product_code=old_policy.product_code;
END $$;

-- Extend only the original default cutoff; preserve customized inventory settings.
UPDATE expedition_inventory SET booking_closes_at='2027-10-02T05:00:00Z'
WHERE booking_closes_at='2027-01-01T00:00:00Z';
