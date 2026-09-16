CREATE TABLE alerts_stripe_decrease_transitions (
 quote_id uuid PRIMARY KEY REFERENCES alerts_stripe_decrease_schedules(quote_id),
 checkout_id uuid NOT NULL,
 binding jsonb NOT NULL CHECK(jsonb_typeof(binding)='object'),
 transition jsonb NOT NULL CHECK(jsonb_typeof(transition)='object'),
 effective_at timestamptz NOT NULL,
 observed_at timestamptz NOT NULL DEFAULT now(),
 FOREIGN KEY(quote_id,checkout_id) REFERENCES alerts_stripe_decrease_quotes(id,checkout_id)
);
CREATE TRIGGER alerts_stripe_decrease_transition_protected BEFORE UPDATE OR DELETE ON alerts_stripe_decrease_transitions
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_evidence();

-- One ordering for both paid upgrades and verified renewal decreases. Account
-- locks serialize writers; old checkout bindings and invoice facts stay intact.
CREATE TABLE alerts_stripe_quantity_history (
 sequence bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 checkout_id uuid NOT NULL REFERENCES alerts_stripe_subscriptions(checkout_id),
 binding jsonb NOT NULL CHECK(jsonb_typeof(binding)='object'),
 increase_quote_id uuid UNIQUE REFERENCES alerts_stripe_increase_payments(quote_id),
 decrease_quote_id uuid UNIQUE REFERENCES alerts_stripe_decrease_transitions(quote_id),
 CHECK(num_nonnulls(increase_quote_id,decrease_quote_id)=1)
);
CREATE INDEX alerts_stripe_quantity_latest ON alerts_stripe_quantity_history(checkout_id,sequence DESC);
INSERT INTO alerts_stripe_quantity_history(checkout_id,binding,increase_quote_id)
 SELECT checkout_id,binding,quote_id FROM alerts_stripe_increase_payments ORDER BY sequence;
CREATE TRIGGER alerts_stripe_quantity_history_protected BEFORE UPDATE OR DELETE ON alerts_stripe_quantity_history
FOR EACH ROW EXECUTE FUNCTION protect_alerts_stripe_evidence();
CREATE FUNCTION append_alerts_stripe_quantity_history() RETURNS trigger LANGUAGE plpgsql AS $$
BEGIN
 IF TG_TABLE_NAME='alerts_stripe_increase_payments' THEN
 INSERT INTO alerts_stripe_quantity_history(checkout_id,binding,increase_quote_id) VALUES(NEW.checkout_id,NEW.binding,NEW.quote_id);
 ELSE
 INSERT INTO alerts_stripe_quantity_history(checkout_id,binding,decrease_quote_id) VALUES(NEW.checkout_id,NEW.binding,NEW.quote_id);
 END IF;
 RETURN NEW;
END;
$$;
CREATE TRIGGER alerts_stripe_increase_quantity AFTER INSERT ON alerts_stripe_increase_payments
FOR EACH ROW EXECUTE FUNCTION append_alerts_stripe_quantity_history();
CREATE TRIGGER alerts_stripe_decrease_quantity AFTER INSERT ON alerts_stripe_decrease_transitions
FOR EACH ROW EXECUTE FUNCTION append_alerts_stripe_quantity_history();
CREATE OR REPLACE VIEW alerts_stripe_current_subscriptions AS
SELECT s.checkout_id,s.subscription_id,s.created_at,
 COALESCE((SELECT h.binding FROM alerts_stripe_quantity_history h WHERE h.checkout_id=s.checkout_id ORDER BY h.sequence DESC LIMIT 1),s.binding) AS binding
FROM alerts_stripe_subscriptions s;
