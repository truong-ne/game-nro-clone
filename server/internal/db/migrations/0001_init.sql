-- Game templates (static data, imported from the legacy dump by nro-migrate).

CREATE TABLE part (
    id     integer PRIMARY KEY,
    kind   smallint NOT NULL,          -- 0 head, 1 body, 2 leg
    frames jsonb    NOT NULL           -- [[iconId, dx, dy], ...]
);

CREATE TABLE item_option_template (
    id   integer PRIMARY KEY,
    name text NOT NULL                 -- '#' is replaced by the param
);

CREATE TABLE item_template (
    id            integer PRIMARY KEY,
    type          smallint NOT NULL,
    gender        smallint NOT NULL,   -- 3 = any
    name          text     NOT NULL,
    description   text     NOT NULL,
    level         smallint NOT NULL,
    icon_id       integer  NOT NULL,
    part          integer  NOT NULL,
    stackable     boolean  NOT NULL,
    power_require bigint   NOT NULL,
    gold          bigint   NOT NULL,
    gem           integer  NOT NULL,
    head          integer  NOT NULL,
    body          integer  NOT NULL,
    leg           integer  NOT NULL
);

CREATE TABLE mob_template (
    id                integer PRIMARY KEY,
    type              smallint NOT NULL,
    name              text     NOT NULL,
    hp                bigint   NOT NULL,
    range_move        integer  NOT NULL,
    speed             integer  NOT NULL,
    dart_type         integer  NOT NULL,
    percent_dame      integer  NOT NULL,
    percent_potential integer  NOT NULL
);

CREATE TABLE npc_template (
    id     integer PRIMARY KEY,
    name   text    NOT NULL,
    head   integer NOT NULL,
    body   integer NOT NULL,
    leg    integer NOT NULL,
    avatar integer NOT NULL
);

CREATE TABLE tileset_flag (
    tileset_id smallint NOT NULL,       -- 1-based, referenced by map_template.tile_id
    flag       integer  NOT NULL,       -- bit flags, 2 = ground (TOP)
    tiles      smallint[] NOT NULL,
    PRIMARY KEY (tileset_id, flag)
);

CREATE TABLE map_template (
    id         integer PRIMARY KEY,
    name       text     NOT NULL,
    zones      smallint NOT NULL,
    max_player smallint NOT NULL,
    type       smallint NOT NULL,
    planet_id  smallint NOT NULL,
    bg_type    smallint NOT NULL,
    tile_id    smallint NOT NULL,
    bg_id      smallint NOT NULL,
    width      smallint NOT NULL DEFAULT 0, -- in 24px tiles
    height     smallint NOT NULL DEFAULT 0,
    tiles      bytea    NOT NULL DEFAULT ''  -- row-major, 0 = empty, else 1-based tile index
);

CREATE TABLE map_waypoint (
    map_id     integer  NOT NULL REFERENCES map_template ON DELETE CASCADE,
    idx        smallint NOT NULL,
    name       text     NOT NULL,
    min_x      integer  NOT NULL,
    min_y      integer  NOT NULL,
    max_x      integer  NOT NULL,
    max_y      integer  NOT NULL,
    is_enter   boolean  NOT NULL,
    is_offline boolean  NOT NULL,
    go_map_id  integer  NOT NULL,
    go_x       integer  NOT NULL,
    go_y       integer  NOT NULL,
    PRIMARY KEY (map_id, idx)
);

CREATE TABLE map_mob (
    map_id          integer  NOT NULL REFERENCES map_template ON DELETE CASCADE,
    idx             smallint NOT NULL,
    mob_template_id integer  NOT NULL REFERENCES mob_template,
    level           smallint NOT NULL,
    hp              bigint   NOT NULL,
    x               integer  NOT NULL,
    y               integer  NOT NULL,
    PRIMARY KEY (map_id, idx)
);

CREATE TABLE map_npc (
    map_id          integer  NOT NULL REFERENCES map_template ON DELETE CASCADE,
    idx             smallint NOT NULL,
    npc_template_id integer  NOT NULL REFERENCES npc_template,
    x               integer  NOT NULL,
    y               integer  NOT NULL,
    PRIMARY KEY (map_id, idx)
);

CREATE TABLE bg_item_template (
    id       integer PRIMARY KEY,
    image_id integer  NOT NULL,
    layer    smallint NOT NULL,
    dx       integer  NOT NULL,
    dy       integer  NOT NULL
);

CREATE TABLE map_bg_item (
    map_id     integer  NOT NULL REFERENCES map_template ON DELETE CASCADE,
    idx        smallint NOT NULL,
    bg_item_id integer  NOT NULL,
    x          integer  NOT NULL,
    y          integer  NOT NULL,
    PRIMARY KEY (map_id, idx)
);

CREATE TABLE skill_template (
    nclass        smallint NOT NULL,     -- 0 Earth, 1 Namek, 2 Saiyan
    id            smallint NOT NULL,
    name          text     NOT NULL,
    max_point     smallint NOT NULL,
    mana_use_type smallint NOT NULL,     -- 0 flat, 1 % of max MP, 2 all MP
    type          smallint NOT NULL,     -- 1 attack, 2 buff other, 3 self, 4 special
    icon_id       integer  NOT NULL,
    dam_info      text     NOT NULL,
    slot          smallint NOT NULL,
    PRIMARY KEY (nclass, id)
);

CREATE TABLE skill_level (
    id            integer  NOT NULL,     -- legacy skill id, shared skills repeat per class
    nclass        smallint NOT NULL,
    template_id   smallint NOT NULL,
    point         smallint NOT NULL,
    power_require bigint   NOT NULL,
    damage        integer  NOT NULL,
    dx            integer  NOT NULL,
    dy            integer  NOT NULL,
    price         bigint   NOT NULL,
    max_fight     integer  NOT NULL,
    mana_use      integer  NOT NULL,
    cool_down     integer  NOT NULL,
    info          text     NOT NULL,
    PRIMARY KEY (nclass, template_id, point),
    FOREIGN KEY (nclass, template_id) REFERENCES skill_template
);

CREATE TABLE head_avatar (
    head_id   integer PRIMARY KEY,
    avatar_id integer NOT NULL
);

CREATE TABLE img_by_name (
    name    text PRIMARY KEY,
    n_frame smallint NOT NULL
);

CREATE TABLE flag_bag (
    id      integer PRIMARY KEY,
    name    text    NOT NULL,
    gold    bigint  NOT NULL,
    gem     integer NOT NULL,             -- -1 = not for sale
    icon_id integer NOT NULL,
    icons   integer[] NOT NULL
);

CREATE TABLE intrinsic (
    id     integer  NOT NULL,
    gender smallint NOT NULL,             -- 3 = all
    name   text     NOT NULL,
    p1_min integer  NOT NULL,
    p1_max integer  NOT NULL,
    p2_min integer  NOT NULL,
    p2_max integer  NOT NULL,
    icon   integer  NOT NULL,
    PRIMARY KEY (id, gender)
);

CREATE TABLE task_main (
    id     integer PRIMARY KEY,
    name   text NOT NULL,
    detail text NOT NULL
);

CREATE TABLE task_sub (
    task_main_id integer  NOT NULL REFERENCES task_main ON DELETE CASCADE,
    idx          smallint NOT NULL,
    name         text     NOT NULL,
    max_count    integer  NOT NULL,
    notify       text     NOT NULL,
    npc_id       integer  NOT NULL,       -- negative = special/gender-relative
    map_id       integer  NOT NULL,
    PRIMARY KEY (task_main_id, idx)
);

CREATE TABLE side_task_template (
    id        integer PRIMARY KEY,
    name      text      NOT NULL,
    count_min integer[] NOT NULL,         -- per difficulty level 1..5
    count_max integer[] NOT NULL
);

CREATE TABLE clan_task_template (LIKE side_task_template INCLUDING ALL);

CREATE TABLE achievement_template (
    idx       integer PRIMARY KEY,        -- order the player data refers to
    id        integer NOT NULL,
    info1     text    NOT NULL,
    info2     text    NOT NULL,
    money     integer NOT NULL,
    max_count bigint  NOT NULL
);

CREATE TABLE shop (
    id        integer PRIMARY KEY,
    npc_id    integer  NOT NULL REFERENCES npc_template,
    tag_name  text     NOT NULL,
    type_shop smallint NOT NULL
);

CREATE TABLE shop_tab (
    id      integer PRIMARY KEY,
    shop_id integer NOT NULL REFERENCES shop ON DELETE CASCADE,
    name    text    NOT NULL
);

CREATE TABLE shop_item (
    id          integer PRIMARY KEY,
    tab_id      integer  NOT NULL REFERENCES shop_tab ON DELETE CASCADE,
    template_id integer  NOT NULL REFERENCES item_template,
    is_new      boolean  NOT NULL,
    currency    smallint NOT NULL,        -- 0 gold, 1 gem, 3 ruby, 4 coupon
    cost        bigint   NOT NULL,
    icon_spec   integer  NOT NULL,
    sort_order  integer  NOT NULL
);

CREATE TABLE shop_item_option (
    shop_item_id integer NOT NULL REFERENCES shop_item ON DELETE CASCADE,
    option_id    integer NOT NULL REFERENCES item_option_template,
    param        integer NOT NULL
);

CREATE TABLE radar (
    id            integer PRIMARY KEY,
    icon_id       integer  NOT NULL,
    rank          smallint NOT NULL,
    max           smallint NOT NULL,
    type          smallint NOT NULL,
    mob_id        integer  NOT NULL,
    body          jsonb    NOT NULL,
    name          text     NOT NULL,
    info          text     NOT NULL,
    options       jsonb    NOT NULL,
    require       integer  NOT NULL,
    require_level smallint NOT NULL,
    aura_id       integer  NOT NULL
);

CREATE TABLE badge (
    id        integer PRIMARY KEY,
    effect_id integer NOT NULL,
    item_id   integer NOT NULL,
    name      text    NOT NULL,
    options   jsonb   NOT NULL
);

CREATE TABLE task_badge_template (
    id               integer PRIMARY KEY,
    name             text    NOT NULL,
    max_count        bigint  NOT NULL,
    reward_effect_id integer NOT NULL
);

-- Accounts and characters (fresh start, no legacy player data).

CREATE TABLE account (
    id            bigserial PRIMARY KEY,
    username      text NOT NULL UNIQUE,
    password_hash text NOT NULL,          -- bcrypt
    is_admin      boolean NOT NULL DEFAULT false,
    banned        boolean NOT NULL DEFAULT false,
    created_at    timestamptz NOT NULL DEFAULT now(),
    last_login_at timestamptz,
    last_ip       text
);

CREATE TABLE character (
    id          bigserial PRIMARY KEY,
    account_id  bigint NOT NULL UNIQUE REFERENCES account ON DELETE CASCADE,
    name        text   NOT NULL,
    gender      smallint NOT NULL,        -- 0 Earth, 1 Namek, 2 Saiyan
    head        integer  NOT NULL,
    map_id      integer  NOT NULL,
    x           integer  NOT NULL,
    y           integer  NOT NULL,
    power       bigint   NOT NULL,
    potential   bigint   NOT NULL,
    hp_base     bigint   NOT NULL,
    mp_base     bigint   NOT NULL,
    dmg_base    bigint   NOT NULL,
    def_base    bigint   NOT NULL,
    crit_base   smallint NOT NULL,
    stamina     integer  NOT NULL,
    hp          bigint   NOT NULL,
    mp          bigint   NOT NULL,
    gold        bigint   NOT NULL,
    gem         bigint   NOT NULL,
    ruby        bigint   NOT NULL,
    task_id     integer  NOT NULL DEFAULT 0,
    task_index  integer  NOT NULL DEFAULT 0,
    task_count  integer  NOT NULL DEFAULT 0,
    created_at  timestamptz NOT NULL DEFAULT now(),
    updated_at  timestamptz NOT NULL DEFAULT now()
);
CREATE UNIQUE INDEX character_name_lower ON character (lower(name));

CREATE TABLE character_item (
    character_id bigint   NOT NULL REFERENCES character ON DELETE CASCADE,
    container    smallint NOT NULL,       -- 0 body, 1 bag, 2 box
    slot         smallint NOT NULL,
    template_id  integer  NOT NULL REFERENCES item_template,
    quantity     integer  NOT NULL,
    options      jsonb    NOT NULL DEFAULT '[]', -- [[optionId, param], ...]
    created_at   timestamptz NOT NULL DEFAULT now(),
    PRIMARY KEY (character_id, container, slot)
);

CREATE TABLE character_skill (
    character_id bigint   NOT NULL REFERENCES character ON DELETE CASCADE,
    template_id  smallint NOT NULL,
    level        smallint NOT NULL,       -- 0 = not learned
    shortcut     smallint,                -- 0..9 hotbar slot
    PRIMARY KEY (character_id, template_id)
);
