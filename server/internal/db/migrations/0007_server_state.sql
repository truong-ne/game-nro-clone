-- Small server-wide state that must survive restarts (daily dungeon counters,
-- open minigame bets...). One JSON document per feature key.
CREATE TABLE IF NOT EXISTS server_state (
    key        text PRIMARY KEY,
    value      jsonb NOT NULL,
    updated_at timestamptz NOT NULL DEFAULT now()
);
