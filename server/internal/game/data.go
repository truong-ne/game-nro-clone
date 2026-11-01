package game

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/truong-ne/game-nro-clone/server/internal/anim"
)

const TileSize = 24

// Tile flag bits from tile_set_Info.
const TileTop = 2 // solid ground

type Waypoint struct {
	Name      string `json:"name"`
	MinX      int    `json:"min_x"`
	MinY      int    `json:"min_y"`
	MaxX      int    `json:"max_x"`
	MaxY      int    `json:"max_y"`
	IsEnter   bool   `json:"is_enter"`
	IsOffline bool   `json:"is_offline"`
	GoMap     int    `json:"-"`
	GoX       int    `json:"-"`
	GoY       int    `json:"-"`
}

func (w Waypoint) Contains(x, y int) bool {
	// Same slack as the legacy check: waypoints are thin strips at map edges.
	return x >= w.MinX-24 && x <= w.MaxX+24 && y >= w.MinY-48 && y <= w.MaxY+48
}

type MapMob struct {
	TemplateID int   `json:"template_id"`
	Level      int   `json:"level"`
	HP         int64 `json:"hp"`
	X          int   `json:"x"`
	Y          int   `json:"y"`
}

type MapNpc struct {
	TemplateID int `json:"template_id"`
	X          int `json:"x"`
	Y          int `json:"y"`
}

type BgItem struct {
	ID int `json:"id"`
	X  int `json:"x"`
	Y  int `json:"y"`
}

type MapTemplate struct {
	ID        int        `json:"id"`
	Name      string     `json:"name"`
	Zones     int        `json:"-"`
	MaxPlayer int        `json:"-"`
	Type      int        `json:"type"`
	PlanetID  int        `json:"planet_id"`
	BgType    int        `json:"bg_type"`
	TileID    int        `json:"tile_id"`
	BgID      int        `json:"bg_id"`
	Width     int        `json:"width"`
	Height    int        `json:"height"`
	Tiles     []byte     `json:"tiles"`
	Waypoints []Waypoint `json:"waypoints"`
	Mobs      []MapMob   `json:"mobs"`
	Npcs      []MapNpc   `json:"npcs"`
	BgItems   []BgItem   `json:"bg_items"`
}

func (m *MapTemplate) PixelWidth() int  { return m.Width * TileSize }
func (m *MapTemplate) PixelHeight() int { return m.Height * TileSize }

type ItemTemplate struct {
	ID     int    `json:"id"`
	Type   int    `json:"type"`
	Gender int    `json:"gender"`
	Name   string `json:"name"`
	IconID int    `json:"icon_id"`
	Part   int    `json:"part"`
}

// Data is the immutable template set shared by every zone.
type Data struct {
	Maps    map[int]*MapTemplate
	Items   map[int]*ItemTemplate
	TileTop map[int]map[byte]bool // tileset id -> tile values that are ground

	Skills    map[[2]int]*SkillTemplate // (nclass, template id)
	Mobs      map[int]*MobTemplate
	ItemsFull map[int]*ItemTemplateFull
	Shops     map[int]*Shop   // by shop id
	ShopByNpc map[int][]*Shop // npc template id -> shops
	Tasks     []*TaskMain
	Bosses    []*BossTemplate
	Content   *Content // data-driven game content (content.json)
	npcNames  map[int]string

	// ClientJSON is the pre-rendered template bundle served at /api/data.
	ClientJSON []byte
}

func (d *Data) Map(id int) *MapTemplate { return d.Maps[id] }

// GroundY returns the y of the first ground tile at or below (x, y), or y if none.
func (d *Data) GroundY(m *MapTemplate, x, y int) int {
	top := d.TileTop[m.TileID]
	col := x / TileSize
	if col < 0 || col >= m.Width || m.Width == 0 {
		return y
	}
	for row := max(y/TileSize, 0); row < m.Height; row++ {
		if v := m.Tiles[row*m.Width+col]; v != 0 && top[v] {
			return row * TileSize
		}
	}
	return y
}

func LoadData(ctx context.Context, pool *pgxpool.Pool) (*Data, error) {
	d := &Data{Maps: map[int]*MapTemplate{}, Items: map[int]*ItemTemplate{}, TileTop: map[int]map[byte]bool{}}

	rows, err := pool.Query(ctx, `SELECT id, name, zones, max_player, type, planet_id, bg_type, tile_id, bg_id, width, height, tiles FROM map_template`)
	if err != nil {
		return nil, err
	}
	for rows.Next() {
		m := &MapTemplate{}
		if err := rows.Scan(&m.ID, &m.Name, &m.Zones, &m.MaxPlayer, &m.Type, &m.PlanetID, &m.BgType,
			&m.TileID, &m.BgID, &m.Width, &m.Height, &m.Tiles); err != nil {
			return nil, err
		}
		d.Maps[m.ID] = m
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	rows, _ = pool.Query(ctx, `SELECT map_id, name, min_x, min_y, max_x, max_y, is_enter, is_offline, go_map_id, go_x, go_y FROM map_waypoint ORDER BY map_id, idx`)
	for rows.Next() {
		var id int
		var w Waypoint
		if err := rows.Scan(&id, &w.Name, &w.MinX, &w.MinY, &w.MaxX, &w.MaxY, &w.IsEnter, &w.IsOffline, &w.GoMap, &w.GoX, &w.GoY); err != nil {
			return nil, err
		}
		if m := d.Maps[id]; m != nil {
			m.Waypoints = append(m.Waypoints, w)
		}
	}
	rows, _ = pool.Query(ctx, `SELECT map_id, mob_template_id, level, hp, x, y FROM map_mob ORDER BY map_id, idx`)
	for rows.Next() {
		var id int
		var mb MapMob
		if err := rows.Scan(&id, &mb.TemplateID, &mb.Level, &mb.HP, &mb.X, &mb.Y); err != nil {
			return nil, err
		}
		if m := d.Maps[id]; m != nil {
			m.Mobs = append(m.Mobs, mb)
		}
	}
	rows, _ = pool.Query(ctx, `SELECT map_id, npc_template_id, x, y FROM map_npc ORDER BY map_id, idx`)
	for rows.Next() {
		var id int
		var n MapNpc
		if err := rows.Scan(&id, &n.TemplateID, &n.X, &n.Y); err != nil {
			return nil, err
		}
		if m := d.Maps[id]; m != nil {
			m.Npcs = append(m.Npcs, n)
		}
	}
	rows, _ = pool.Query(ctx, `SELECT map_id, bg_item_id, x, y FROM map_bg_item ORDER BY map_id, idx`)
	for rows.Next() {
		var id int
		var b BgItem
		if err := rows.Scan(&id, &b.ID, &b.X, &b.Y); err != nil {
			return nil, err
		}
		if m := d.Maps[id]; m != nil {
			m.BgItems = append(m.BgItems, b)
		}
	}
	rows, _ = pool.Query(ctx, `SELECT tileset_id, tiles FROM tileset_flag WHERE flag & $1 <> 0`, TileTop)
	for rows.Next() {
		var ts int
		var tiles []int16
		if err := rows.Scan(&ts, &tiles); err != nil {
			return nil, err
		}
		set := d.TileTop[ts]
		if set == nil {
			set = map[byte]bool{}
			d.TileTop[ts] = set
		}
		for _, t := range tiles {
			set[byte(t)] = true
		}
	}
	rows, _ = pool.Query(ctx, `SELECT id, type, gender, name, icon_id, part FROM item_template`)
	for rows.Next() {
		it := &ItemTemplate{}
		if err := rows.Scan(&it.ID, &it.Type, &it.Gender, &it.Name, &it.IconID, &it.Part); err != nil {
			return nil, err
		}
		d.Items[it.ID] = it
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}

	if err := loadMoreData(ctx, pool, d); err != nil {
		return nil, fmt.Errorf("skills/mobs/items: %w", err)
	}
	if err := loadShops(ctx, pool, d); err != nil {
		return nil, fmt.Errorf("shops: %w", err)
	}
	if err := loadTasks(ctx, pool, d); err != nil {
		return nil, fmt.Errorf("tasks: %w", err)
	}
	if err := loadContent(d); err != nil {
		return nil, fmt.Errorf("content: %w", err)
	}
	d.applyTaskOverrides()
	for _, h := range dataHooks {
		if err := h(ctx, pool, d); err != nil {
			return nil, err
		}
	}
	d.npcNames = map[int]string{}
	rows, _ = pool.Query(ctx, `SELECT id, name FROM npc_template`)
	for rows.Next() {
		var id int
		var name string
		if err := rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		d.npcNames[id] = name
	}

	cj, err := clientBundle(ctx, pool, d)
	if err != nil {
		return nil, fmt.Errorf("client bundle: %w", err)
	}
	d.ClientJSON = cj
	return d, nil
}

// clientBundle builds the static data the Godot client needs for rendering.
func clientBundle(ctx context.Context, pool *pgxpool.Pool, d *Data) ([]byte, error) {
	var b []byte
	err := pool.QueryRow(ctx, `SELECT json_build_object(
		'parts',      (SELECT json_object_agg(id, frames) FROM part),
		'npcs',       (SELECT json_object_agg(id, json_build_object('name', name, 'head', head, 'body', body, 'leg', leg, 'avatar', avatar)) FROM npc_template),
		'mobs',       (SELECT json_object_agg(id, json_build_object('name', name, 'type', type, 'hp', hp)) FROM mob_template),
		'bg_items',   (SELECT json_object_agg(id, json_build_object('image', image_id, 'layer', layer, 'dx', dx, 'dy', dy)) FROM bg_item_template),
		'head_avatar',(SELECT json_object_agg(head_id, avatar_id) FROM head_avatar),
		'tile_top',   (SELECT json_object_agg(tileset_id, tiles) FROM tileset_flag WHERE flag & 2 <> 0),
		'items',      (SELECT json_object_agg(id, json_build_object('name', name, 'icon', icon_id, 'type', type,
			'desc', description, 'gender', gender, 'power', power_require)) FROM item_template),
		'options',    (SELECT json_object_agg(id, name) FROM item_option_template),
		'skills',     (SELECT json_object_agg(nclass || '_' || id, json_build_object('name', name, 'icon', icon_id,
			'max', max_point, 'type', type)) FROM skill_template),
		'tile_flags', (SELECT json_object_agg(tileset_id, f) FROM
			(SELECT tileset_id, json_object_agg(flag, tiles) f FROM tileset_flag GROUP BY tileset_id) t)
	)`).Scan(&b)
	if err != nil {
		return nil, err
	}
	var bundle map[string]any
	if err := json.Unmarshal(b, &bundle); err != nil {
		return nil, fmt.Errorf("invalid bundle json: %w", err)
	}
	bundle["char_frames"] = anim.Frames
	bundle["char_anims"] = anim.Animations
	bundle["bg_parallax"] = anim.BgParallax
	return json.Marshal(bundle)
}
