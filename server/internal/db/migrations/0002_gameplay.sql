-- M3-M8 gameplay state.

-- Small per-character state that is always loaded with the character
-- (magic tree, pet, achievements). Typed in Go as store.Extra.
ALTER TABLE character ADD COLUMN extra jsonb NOT NULL DEFAULT '{}';
ALTER TABLE character ADD COLUMN limit_level smallint NOT NULL DEFAULT 0;

CREATE TABLE clan (
    id         bigserial PRIMARY KEY,
    name       text NOT NULL,
    slogan     text NOT NULL DEFAULT '',
    leader_id  bigint NOT NULL REFERENCES character ON DELETE CASCADE,
    level      smallint NOT NULL DEFAULT 1,
    points     bigint NOT NULL DEFAULT 0,
    created_at timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX clan_name_lower ON clan (lower(name));

CREATE TABLE clan_member (
    clan_id      bigint NOT NULL REFERENCES clan ON DELETE CASCADE,
    character_id bigint NOT NULL UNIQUE REFERENCES character ON DELETE CASCADE,
    role         smallint NOT NULL DEFAULT 2, -- 0 leader, 1 deputy, 2 member
    donated      bigint NOT NULL DEFAULT 0,
    joined_at    timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (clan_id, character_id)
);

CREATE TABLE friend (
    character_id bigint NOT NULL REFERENCES character ON DELETE CASCADE,
    friend_id    bigint NOT NULL REFERENCES character ON DELETE CASCADE,
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (character_id, friend_id)
);

CREATE TABLE giftcode (
    code       text PRIMARY KEY,
    rewards    jsonb NOT NULL,          -- [{"item": id, "qty": n, "options": [[id, param]]}]
    max_uses   integer NOT NULL DEFAULT 1000000,
    used       integer NOT NULL DEFAULT 0,
    expires_at timestamptz
);

CREATE TABLE giftcode_use (
    code         text   NOT NULL REFERENCES giftcode ON DELETE CASCADE,
    character_id bigint NOT NULL REFERENCES character ON DELETE CASCADE,
    used_at      timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (code, character_id)
);

-- Append-only audit trail for admin actions and economy-sensitive events.
CREATE TABLE audit_log (
    id         bigserial PRIMARY KEY,
    at         timestamptz NOT NULL DEFAULT now(),
    actor      text NOT NULL,
    action     text NOT NULL,
    detail     jsonb NOT NULL DEFAULT '{}'
);
CREATE INDEX audit_log_at ON audit_log (at);
