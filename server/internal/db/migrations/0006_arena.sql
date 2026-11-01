-- Arena features (wip_arena): Siêu hạng ladder, Ngọc Rồng Sao Đen rewards for
-- offline clan members, clan dungeon records for the leaderboards.

-- One stored copy per character (legacy table super_rank): the fighter other
-- players challenge is built from these stats.
CREATE TABLE IF NOT EXISTS arena_super_rank (
    character_id bigint PRIMARY KEY REFERENCES character ON DELETE CASCADE,
    name         text   NOT NULL,
    rank         integer NOT NULL,
    hp           bigint NOT NULL DEFAULT 0,
    dmg          bigint NOT NULL DEFAULT 0,
    def          bigint NOT NULL DEFAULT 0,
    win          integer NOT NULL DEFAULT 0,
    lose         integer NOT NULL DEFAULT 0,
    history      jsonb  NOT NULL DEFAULT '[]',
    updated_at   timestamptz NOT NULL DEFAULT now()
);
CREATE INDEX IF NOT EXISTS arena_super_rank_rank ON arena_super_rank (rank);

CREATE TABLE IF NOT EXISTS arena_blackball_reward (
    character_id bigint   NOT NULL REFERENCES character ON DELETE CASCADE,
    star         smallint NOT NULL,
    expires      bigint   NOT NULL, -- unix seconds
    PRIMARY KEY (character_id, star)
);

-- Best result per clan and dungeon (legacy clan.levelDoneBanDoKhoBau/thoiGianHoanThanhBDKB...).
CREATE TABLE IF NOT EXISTS arena_dungeon_record (
    clan_id    bigint  NOT NULL REFERENCES clan ON DELETE CASCADE,
    dungeon    text    NOT NULL,
    level      integer NOT NULL,
    ms         bigint  NOT NULL,
    at         timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (clan_id, dungeon)
);
