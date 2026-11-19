ALTER TABLE service_legal_revisions DROP CONSTRAINT service_legal_revisions_product_code_check;
ALTER TABLE service_legal_revisions ADD CONSTRAINT service_legal_revisions_product_code_check
CHECK(product_code IN ('alerts.repository-slots','insider.membership','insider.article',
 'insider.membership.monthly','insider.membership.annual','expedition.antarctica2028.passage',
 'insider.article.gifts'));
