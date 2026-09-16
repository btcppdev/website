-- Subscription identity is immutable. Paid quantity changes append evidence;
-- operational readers use its latest binding while original checkout replay
-- continues comparing against alerts_stripe_subscriptions itself.
CREATE VIEW alerts_stripe_current_subscriptions AS
SELECT s.checkout_id,s.subscription_id,s.created_at,
 COALESCE((SELECT p.binding FROM alerts_stripe_increase_payments p
           WHERE p.checkout_id=s.checkout_id ORDER BY p.sequence DESC LIMIT 1),s.binding) AS binding
FROM alerts_stripe_subscriptions s;
