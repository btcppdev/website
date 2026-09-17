ALTER TABLE service_receipt_jobs ADD COLUMN retry_after_attempt bigint NOT NULL DEFAULT 0
 CHECK(retry_after_attempt>=0 AND retry_after_attempt<=attempts);

CREATE TABLE service_receipt_recoveries (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 order_id uuid NOT NULL REFERENCES service_receipt_jobs(order_id) ON DELETE CASCADE,
 after_attempt bigint NOT NULL,
 actor text NOT NULL CHECK(length(actor) BETWEEN 1 AND 200),
 reason text NOT NULL CHECK(length(reason) BETWEEN 1 AND 1000),
 created_at timestamptz NOT NULL DEFAULT now(),
 UNIQUE(order_id,after_attempt)
);
