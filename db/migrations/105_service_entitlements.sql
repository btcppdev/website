CREATE TABLE service_entitlements (
  id uuid PRIMARY KEY DEFAULT gen_random_uuid(),
  purchase_id uuid NOT NULL UNIQUE REFERENCES service_purchase_requests(id) ON DELETE RESTRICT,
  person_id uuid REFERENCES people(id) ON DELETE SET NULL,
  product_code text NOT NULL CHECK (product_code IN ('alerts.repository-slots','insider.membership','insider.article')),
  resource_id text NOT NULL DEFAULT '',
  quantity integer NOT NULL CHECK (quantity>0),
  starts_at timestamptz,
  ends_at timestamptz,
  created_at timestamptz NOT NULL DEFAULT now(),
  revoked_at timestamptz,
  revocation_actor text NOT NULL DEFAULT '',
  revocation_reason text NOT NULL DEFAULT '',
  CHECK ((product_code='insider.article' AND resource_id<>'' AND quantity=1 AND starts_at IS NULL AND ends_at IS NULL)
    OR (product_code<>'insider.article' AND starts_at IS NOT NULL AND ends_at IS NOT NULL AND ends_at>starts_at)),
  CHECK ((revoked_at IS NULL AND revocation_actor='' AND revocation_reason='')
    OR (revoked_at IS NOT NULL AND btrim(revocation_actor)<>'' AND btrim(revocation_reason)<>''))
);
CREATE INDEX service_entitlements_person_idx ON service_entitlements(person_id) WHERE revoked_at IS NULL;

CREATE FUNCTION protect_service_entitlement() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
  IF TG_OP='DELETE' THEN
    RAISE EXCEPTION 'service entitlement evidence cannot be deleted' USING ERRCODE='23514';
  END IF;
  IF NEW.id IS DISTINCT FROM OLD.id OR NEW.purchase_id IS DISTINCT FROM OLD.purchase_id
    OR NEW.product_code IS DISTINCT FROM OLD.product_code OR NEW.resource_id IS DISTINCT FROM OLD.resource_id
    OR NEW.quantity IS DISTINCT FROM OLD.quantity OR NEW.starts_at IS DISTINCT FROM OLD.starts_at
    OR NEW.ends_at IS DISTINCT FROM OLD.ends_at OR NEW.created_at IS DISTINCT FROM OLD.created_at
    OR (NEW.person_id IS DISTINCT FROM OLD.person_id AND NEW.person_id IS NOT NULL)
    OR (OLD.revoked_at IS NOT NULL AND (NEW.revoked_at IS DISTINCT FROM OLD.revoked_at
      OR NEW.revocation_actor IS DISTINCT FROM OLD.revocation_actor OR NEW.revocation_reason IS DISTINCT FROM OLD.revocation_reason))
  THEN
    RAISE EXCEPTION 'service entitlement cannot be rewritten or reactivated' USING ERRCODE='23514';
  END IF;
  RETURN NEW;
END;
$$;
CREATE TRIGGER service_entitlement_protected BEFORE UPDATE OR DELETE ON service_entitlements
FOR EACH ROW EXECUTE FUNCTION protect_service_entitlement();
