CREATE TABLE alerts_stripe_subscription_state (
 checkout_id uuid PRIMARY KEY REFERENCES alerts_stripe_subscriptions(checkout_id),
 revision bigint NOT NULL CHECK(revision>0),
 snapshot jsonb NOT NULL CHECK(jsonb_typeof(snapshot)='object'),
 observed_at timestamptz NOT NULL DEFAULT now()
);
