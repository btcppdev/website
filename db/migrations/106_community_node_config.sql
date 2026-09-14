CREATE TABLE community_node_config (
 singleton boolean PRIMARY KEY DEFAULT true CHECK (singleton),
 revision bigint NOT NULL DEFAULT 0,
 encrypted_settings bytea,
 enabled boolean NOT NULL DEFAULT false,
 updated_at timestamptz NOT NULL DEFAULT now(),
 updated_by text NOT NULL DEFAULT ''
);
INSERT INTO community_node_config(singleton) VALUES(true);
CREATE TABLE community_node_config_audit (
 id bigint GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
 revision bigint NOT NULL,
 actor text NOT NULL,
 enabled boolean NOT NULL,
 created_at timestamptz NOT NULL DEFAULT now()
);

-- New pools use the dedicated payment zone; existing address bindings stay intact.
ALTER TABLE community_prize_pools ALTER COLUMN domain SET DEFAULT 'zap.btcplusplus.dev';
