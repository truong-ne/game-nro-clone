// Package importer loads game template data from the legacy MariaDB dump and
// legacy data files into the Postgres schema.
package importer

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/truong-ne/game-nro-clone/server/internal/legacy"
	"github.com/truong-ne/game-nro-clone/server/internal/sqldump"
)

// Template tables in dependency order; they are truncated before import.
var templateTables = []string{
	"part", "item_option_template", "item_template", "mob_template", "npc_template",
	"tileset_flag", "map_template", "map_waypoint", "map_mob", "map_npc",
	"bg_item_template", "map_bg_item", "skill_template", "skill_level",
	"head_avatar", "img_by_name", "flag_bag", "intrinsic", "task_main", "task_sub",
	"side_task_template", "clan_task_template", "achievement_template",
	"shop", "shop_tab", "shop_item", "shop_item_option", "radar", "badge", "task_badge_template",
}

type Importer struct {
	Dump    map[string]*sqldump.Table
	DataDir string // legacy SRC/data
	tx      pgx.Tx
	ctx     context.Context
	mapIDs  map[int64]bool
}

func Run(ctx context.Context, pool *pgxpool.Pool, dumpPath, dataDir string) error {
	dump, err := sqldump.ParseFile(dumpPath)
	if err != nil {
		return fmt.Errorf("parse dump: %w", err)
	}
	tx, err := pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, "TRUNCATE "+strings.Join(templateTables, ", ")+" CASCADE"); err != nil {
		return err
	}
	im := &Importer{Dump: dump, DataDir: dataDir, tx: tx, ctx: ctx, mapIDs: map[int64]bool{}}
	steps := []struct {
		name string
		fn   func() error
	}{
		{"part", im.parts}, {"item options", im.itemOptions}, {"items", im.items},
		{"mobs", im.mobs}, {"npcs", im.npcs}, {"tilesets", im.tilesets},
		{"maps", im.maps}, {"bg items", im.bgItems}, {"skills", im.skills},
		{"misc", im.misc}, {"tasks", im.tasks}, {"shops", im.shops},
		{"radar", im.radar}, {"badges", im.badges},
	}
	for _, s := range steps {
		if err := s.fn(); err != nil {
			return fmt.Errorf("import %s: %w", s.name, err)
		}
	}
	return tx.Commit(ctx)
}

func (im *Importer) table(name string) (*sqldump.Table, error) {
	t := im.Dump[name]
	if t == nil {
		return nil, fmt.Errorf("table %s not found in dump", name)
	}
	return t, nil
}

func (im *Importer) copy(table string, cols []string, rows [][]any) error {
	n, err := im.tx.CopyFrom(im.ctx, pgx.Identifier{table}, cols, pgx.CopyFromRows(rows))
	if err != nil {
		return fmt.Errorf("copy %s: %w", table, err)
	}
	slog.Info("imported", "table", table, "rows", n)
	return nil
}

// each runs fn over every row of a dump table and copies the produced rows.
func (im *Importer) each(src, dst string, cols []string, fn func(r sqldump.Row) ([]any, error)) error {
	t, err := im.table(src)
	if err != nil {
		return err
	}
	var rows [][]any
	err = t.Each(func(r sqldump.Row) error {
		v, err := fn(r)
		if err != nil {
			return fmt.Errorf("%s row %s: %w", src, r.Str(t.Columns[0]), err)
		}
		if v != nil {
			rows = append(rows, v)
		}
		return nil
	})
	if err != nil {
		return err
	}
	return im.copy(dst, cols, rows)
}

// parts keys parts by row position: the legacy client indexed the part file
// by position, and the dump's id column has duplicates.
func (im *Importer) parts() error {
	n := 0
	return im.each("part", "part", []string{"id", "kind", "frames"}, func(r sqldump.Row) ([]any, error) {
		frames, err := legacy.IntRows(strings.ReplaceAll(r.Str("DATA"), `"`, ""))
		if err != nil {
			return nil, err
		}
		b, _ := json.Marshal(frames)
		n++
		return []any{n - 1, r.Int("TYPE"), string(b)}, nil
	})
}

func (im *Importer) itemOptions() error {
	return im.each("item_option_template", "item_option_template", []string{"id", "name"}, func(r sqldump.Row) ([]any, error) {
		return []any{r.Int("id"), r.Str("NAME")}, nil
	})
}

func (im *Importer) items() error {
	cols := []string{"id", "type", "gender", "name", "description", "level", "icon_id", "part",
		"stackable", "power_require", "gold", "gem", "head", "body", "leg"}
	return im.each("item_template", "item_template", cols, func(r sqldump.Row) ([]any, error) {
		return []any{r.Int("id"), r.Int("TYPE"), r.Int("gender"), r.Str("NAME"), r.Str("description"),
			r.Int("level"), r.Int("icon_id"), r.Int("part"), r.Int("is_up_to_up") != 0,
			r.Int("power_require"), r.Int("gold"), r.Int("gem"), r.Int("head"), r.Int("body"), r.Int("leg")}, nil
	})
}

func (im *Importer) mobs() error {
	cols := []string{"id", "type", "name", "hp", "range_move", "speed", "dart_type", "percent_dame", "percent_potential"}
	return im.each("mob_template", "mob_template", cols, func(r sqldump.Row) ([]any, error) {
		return []any{r.Int("id"), r.Int("TYPE"), r.Str("NAME"), r.Int("hp"), r.Int("range_move"),
			r.Int("speed"), r.Int("dart_Type"), r.Int("percent_dame"), r.Int("percent_tiem_nang")}, nil
	})
}

func (im *Importer) npcs() error {
	return im.each("npc_template", "npc_template", []string{"id", "name", "head", "body", "leg", "avatar"}, func(r sqldump.Row) ([]any, error) {
		return []any{r.Int("id"), r.Str("NAME"), r.Int("head"), r.Int("body"), r.Int("leg"), r.Int("avatar")}, nil
	})
}

// tilesets reads data/map/tile_set_Info:
// u8 count, then per tileset: u8 nTypes, then nTypes × {i32 flag, u8 n, n × u8 tile}.
func (im *Importer) tilesets() error {
	b, err := os.ReadFile(filepath.Join(im.DataDir, "map", "tile_set_Info"))
	if err != nil {
		return err
	}
	var rows [][]any
	p := 0
	count := int(b[p])
	p++
	for ts := 1; ts <= count; ts++ {
		nTypes := int(b[p])
		p++
		for range nTypes {
			flag := int32(binary.BigEndian.Uint32(b[p:]))
			n := int(b[p+4])
			p += 5
			tiles := make([]int16, n)
			for i := range n {
				tiles[i] = int16(b[p+i])
			}
			p += n
			rows = append(rows, []any{ts, flag, tiles})
		}
	}
	return im.copy("tileset_flag", []string{"tileset_id", "flag", "tiles"}, rows)
}

func (im *Importer) maps() error {
	t, err := im.table("map_template")
	if err != nil {
		return err
	}
	var maps, wps, mobs, npcs [][]any
	err = t.Each(func(r sqldump.Row) error {
		id := r.Int("id")
		im.mapIDs[id] = true
		w, h, tiles := 0, 0, []byte{}
		if b, err := os.ReadFile(filepath.Join(im.DataDir, "map", "tile_map_data", strconv.FormatInt(id, 10))); err == nil && len(b) >= 2 {
			w, h = int(b[0]), int(b[1])
			if len(b) >= 2+w*h {
				tiles = b[2 : 2+w*h]
			} else {
				slog.Warn("short tile map", "map", id)
				w, h = 0, 0
			}
		} else {
			slog.Warn("missing tile map", "map", id)
		}
		maps = append(maps, []any{id, r.Str("NAME"), r.Int("zones"), r.Int("max_player"), r.Int("type"),
			r.Int("planet_id"), r.Int("bg_type"), r.Int("tile_id"), r.Int("bg_id"), w, h, tiles})
		for i, wp := range legacy.Waypoints(r.Str("waypoints")) {
			wps = append(wps, []any{id, i, wp.Name, wp.MinX, wp.MinY, wp.MaxX, wp.MaxY,
				wp.IsEnter, wp.IsOffline, wp.GoMap, wp.GoX, wp.GoY})
		}
		mobRows, err := legacy.IntRows(strings.ReplaceAll(r.Str("mobs"), `"`, ""))
		if err != nil {
			return fmt.Errorf("map %d mobs: %w", id, err)
		}
		for i, m := range mobRows {
			if len(m) >= 5 {
				mobs = append(mobs, []any{id, i, m[0], m[1], m[2], m[3], m[4]})
			}
		}
		npcRows, err := legacy.IntRows(strings.ReplaceAll(r.Str("npcs"), `"`, ""))
		if err != nil {
			return fmt.Errorf("map %d npcs: %w", id, err)
		}
		for i, n := range npcRows {
			if len(n) >= 3 {
				npcs = append(npcs, []any{id, i, n[0], n[1], n[2]})
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := im.copy("map_template", []string{"id", "name", "zones", "max_player", "type", "planet_id",
		"bg_type", "tile_id", "bg_id", "width", "height", "tiles"}, maps); err != nil {
		return err
	}
	if err := im.copy("map_waypoint", []string{"map_id", "idx", "name", "min_x", "min_y", "max_x", "max_y",
		"is_enter", "is_offline", "go_map_id", "go_x", "go_y"}, wps); err != nil {
		return err
	}
	if err := im.copy("map_mob", []string{"map_id", "idx", "mob_template_id", "level", "hp", "x", "y"}, mobs); err != nil {
		return err
	}
	return im.copy("map_npc", []string{"map_id", "idx", "npc_template_id", "x", "y"}, npcs)
}

// bgItems imports bg_item_template and data/map/item_bg_map_data/<map>:
// i16 count, count × {i16 bgItemId, i16 x, i16 y}.
func (im *Importer) bgItems() error {
	err := im.each("bg_item_template", "bg_item_template", []string{"id", "image_id", "layer", "dx", "dy"}, func(r sqldump.Row) ([]any, error) {
		return []any{r.Int("id"), r.Int("image_id"), r.Int("layer"), r.Int("dx"), r.Int("dy")}, nil
	})
	if err != nil {
		return err
	}
	var rows [][]any
	for id := range im.mapIDs {
		b, err := os.ReadFile(filepath.Join(im.DataDir, "map", "item_bg_map_data", strconv.FormatInt(id, 10)))
		if err != nil || len(b) < 2 {
			continue
		}
		n := int(int16(binary.BigEndian.Uint16(b)))
		for i := 0; i < n && 2+i*6+6 <= len(b); i++ {
			o := 2 + i*6
			rows = append(rows, []any{id, i,
				int16(binary.BigEndian.Uint16(b[o:])), int16(binary.BigEndian.Uint16(b[o+2:])), int16(binary.BigEndian.Uint16(b[o+4:]))})
		}
	}
	return im.copy("map_bg_item", []string{"map_id", "idx", "bg_item_id", "x", "y"}, rows)
}

type skillLevel struct {
	ID           int64  `json:"id"`
	Point        int64  `json:"point"`
	PowerRequire int64  `json:"power_require"`
	Damage       int64  `json:"damage"`
	Dx           int64  `json:"dx"`
	Dy           int64  `json:"dy"`
	Price        int64  `json:"price"`
	MaxFight     int64  `json:"max_fight"`
	ManaUse      int64  `json:"mana_use"`
	CoolDown     int64  `json:"cool_down"`
	Info         string `json:"info"`
}

func (im *Importer) skills() error {
	t, err := im.table("skill_template")
	if err != nil {
		return err
	}
	var tmpl, levels [][]any
	err = t.Each(func(r sqldump.Row) error {
		nclass, id := r.Int("nclass_id"), r.Int("id")
		tmpl = append(tmpl, []any{nclass, id, r.Str("NAME"), r.Int("max_point"), r.Int("mana_use_type"),
			r.Int("TYPE"), r.Int("icon_id"), r.Str("dam_info"), r.Int("slot")})
		var ls []skillLevel
		if err := legacy.Decode(r.Str("skills"), &ls); err != nil {
			return fmt.Errorf("skill %d/%d: %w", nclass, id, err)
		}
		for _, l := range ls {
			levels = append(levels, []any{l.ID, nclass, id, l.Point, l.PowerRequire, l.Damage, l.Dx, l.Dy,
				l.Price, l.MaxFight, l.ManaUse, l.CoolDown, l.Info})
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := im.copy("skill_template", []string{"nclass", "id", "name", "max_point", "mana_use_type",
		"type", "icon_id", "dam_info", "slot"}, tmpl); err != nil {
		return err
	}
	return im.copy("skill_level", []string{"id", "nclass", "template_id", "point", "power_require", "damage",
		"dx", "dy", "price", "max_fight", "mana_use", "cool_down", "info"}, levels)
}

func (im *Importer) misc() error {
	if err := im.each("head_avatar", "head_avatar", []string{"head_id", "avatar_id"}, func(r sqldump.Row) ([]any, error) {
		return []any{r.Int("head_id"), r.Int("avatar_id")}, nil
	}); err != nil {
		return err
	}
	if err := im.each("img_by_name", "img_by_name", []string{"name", "n_frame"}, func(r sqldump.Row) ([]any, error) {
		return []any{r.Str("NAME"), r.Int("n_frame")}, nil
	}); err != nil {
		return err
	}
	if err := im.each("flag_bag", "flag_bag", []string{"id", "name", "gold", "gem", "icon_id", "icons"}, func(r sqldump.Row) ([]any, error) {
		return []any{r.Int("id"), r.Str("NAME"), r.Int("gold"), r.Int("gem"), r.Int("icon_id"), legacy.CSVInts(r.Str("icon_data"))}, nil
	}); err != nil {
		return err
	}
	// intrinsic rows are duplicated 8× in the dump; keep the first of each (id, gender).
	seen := map[[2]int64]bool{}
	return im.each("intrinsic", "intrinsic", []string{"id", "gender", "name", "p1_min", "p1_max", "p2_min", "p2_max", "icon"}, func(r sqldump.Row) ([]any, error) {
		k := [2]int64{r.Int("id"), r.Int("gender")}
		if seen[k] {
			return nil, nil
		}
		seen[k] = true
		return []any{k[0], k[1], r.Str("NAME"), r.Int("param_from_1"), r.Int("param_to_1"),
			r.Int("param_from_2"), r.Int("param_to_2"), r.Int("icon")}, nil
	})
}

func (im *Importer) tasks() error {
	if err := im.each("task_main_template", "task_main", []string{"id", "name", "detail"}, func(r sqldump.Row) ([]any, error) {
		return []any{r.Int("id"), r.Str("NAME"), r.Str("detail")}, nil
	}); err != nil {
		return err
	}
	// Subtasks are positional per main task, ordered by the auto-increment column.
	idx := map[int64]int{}
	if err := im.each("task_sub_template", "task_sub", []string{"task_main_id", "idx", "name", "max_count", "notify", "npc_id", "map_id"}, func(r sqldump.Row) ([]any, error) {
		m := r.Int("task_main_id")
		i := idx[m]
		idx[m]++
		return []any{m, i, r.Str("NAME"), r.Int("max_count"), r.Str("notify"), r.Int("npc_id"), r.Int("map")}, nil
	}); err != nil {
		return err
	}
	ranged := func(r sqldump.Row) ([]any, error) {
		var lo, hi []int32
		for lv := 1; lv <= 5; lv++ {
			a, b := legacy.Range(r.Str(fmt.Sprintf("max_count_lv%d", lv)))
			lo, hi = append(lo, int32(a)), append(hi, int32(b))
		}
		return []any{r.Int("id"), r.Str("NAME"), lo, hi}, nil
	}
	cols := []string{"id", "name", "count_min", "count_max"}
	if err := im.each("side_task_template", "side_task_template", cols, ranged); err != nil {
		return err
	}
	if err := im.each("clan_task_template", "clan_task_template", cols, ranged); err != nil {
		return err
	}
	n := 0
	return im.each("achievement_template", "achievement_template", []string{"idx", "id", "info1", "info2", "money", "max_count"}, func(r sqldump.Row) ([]any, error) {
		n++
		return []any{n - 1, r.Int("id"), r.Str("info1"), r.Str("info2"), r.Int("money"), r.Int("max_count")}, nil
	})
}

func (im *Importer) shops() error {
	if err := im.each("shop", "shop", []string{"id", "npc_id", "tag_name", "type_shop"}, func(r sqldump.Row) ([]any, error) {
		return []any{r.Int("id"), r.Int("npc_id"), r.Str("tag_name"), r.Int("type_shop")}, nil
	}); err != nil {
		return err
	}
	tabs := map[int64]bool{}
	if err := im.each("tab_shop", "shop_tab", []string{"id", "shop_id", "name"}, func(r sqldump.Row) ([]any, error) {
		tabs[r.Int("id")] = true
		return []any{r.Int("id"), r.Int("shop_id"), strings.ReplaceAll(r.Str("NAME"), "<>", "\n")}, nil
	}); err != nil {
		return err
	}
	sold := map[int64]bool{}
	n, orphans := 0, 0
	defer func() {
		if orphans > 0 {
			slog.Warn("skipped shop items with unknown tab or item", "count", orphans)
		}
	}()
	if err := im.each("item_shop", "shop_item", []string{"id", "tab_id", "template_id", "is_new", "currency", "cost", "icon_spec", "sort_order"}, func(r sqldump.Row) ([]any, error) {
		if r.Int("is_sell") != 1 {
			return nil, nil
		}
		if !tabs[r.Int("tab_id")] || r.Int("temp_id") < 0 || r.Int("temp_id") >= 2000 {
			orphans++
			return nil, nil
		}
		sold[r.Int("id")] = true
		n++
		return []any{r.Int("id"), r.Int("tab_id"), r.Int("temp_id"), r.Int("is_new") != 0, r.Int("type_sell"),
			r.Int("cost"), r.Int("icon_spec"), n}, nil
	}); err != nil {
		return err
	}
	return im.each("item_shop_option", "shop_item_option", []string{"shop_item_id", "option_id", "param"}, func(r sqldump.Row) ([]any, error) {
		if !sold[r.Int("item_shop_id")] {
			return nil, nil
		}
		return []any{r.Int("item_shop_id"), r.Int("option_id"), r.Int("param")}, nil
	})
}

func jsonCol(s string) string {
	var v any
	if err := legacy.Decode(s, &v); err != nil {
		return "[]"
	}
	b, _ := json.Marshal(v)
	return string(b)
}

func (im *Importer) radar() error {
	cols := []string{"id", "icon_id", "rank", "max", "type", "mob_id", "body", "name", "info", "options", "require", "require_level", "aura_id"}
	return im.each("radar", "radar", cols, func(r sqldump.Row) ([]any, error) {
		return []any{r.Int("id"), r.Int("iconId"), r.Int("rank"), r.Int("max"), r.Int("type"), r.Int("mob_id"),
			jsonCol(r.Str("body")), r.Str("name"), r.Str("info"), jsonCol(r.Str("options")), r.Int("require"),
			r.Int("require_level"), r.Int("aura_id")}, nil
	})
}

func (im *Importer) badges() error {
	if err := im.each("data_badges", "badge", []string{"id", "effect_id", "item_id", "name", "options"}, func(r sqldump.Row) ([]any, error) {
		return []any{r.Int("id"), r.Int("idEffect"), r.Int("idItem"), r.Str("NAME"), jsonCol(r.Str("Options"))}, nil
	}); err != nil {
		return err
	}
	return im.each("task_badges_template", "task_badge_template", []string{"id", "name", "max_count", "reward_effect_id"}, func(r sqldump.Row) ([]any, error) {
		return []any{r.Int("id"), r.Str("NAME"), r.Int("maxCount"), r.Int("idBadgesReward")}, nil
	})
}
