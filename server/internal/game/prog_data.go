package game

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"log/slog"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Progression data (main task overrides 7..29, side/clan tasks, badges,
// achievements, dragon balls). Data comes from prog_content.json plus the
// legacy template tables imported by nro-migrate.

//go:embed prog_content.json
var progContentJSON []byte

// progSub is an extended subtask override. Kinds talk/kill/map/collect are
// handled by the core listeners in tasks.go; p_* kinds are handled here.
type progSub struct {
	Kind       string   `json:"kind"`
	Src        string   `json:"src"`
	Name       string   `json:"name"`
	Max        int      `json:"max"`
	Npc        *int     `json:"npc"`
	Npcs       []int    `json:"npcs"`
	PerGender  bool     `json:"per_gender"`
	OnMap      *int     `json:"on_map"`
	Maps       []int    `json:"maps"`
	Mobs       []int    `json:"mobs"`
	GMobs      [][]int  `json:"gender_mobs"`
	Item       int      `json:"item"`
	Value      int64    `json:"value"`
	Bosses     []string `json:"bosses"`
	ClanOnly   bool     `json:"clan_only"`
	ClanDouble int      `json:"clan_double"`
	Drop       int      `json:"drop"`
}

type progReward struct {
	Item     int   `json:"item"`
	ByGender []int `json:"by_gender"`
	Qty      int   `json:"qty"`
}

type progContent struct {
	Tasks struct {
		Overrides  map[string]*progSub     `json:"overrides"`
		SubRewards map[string][]progReward `json:"sub_rewards"`
		LegacyGold struct {
			From int   `json:"from"`
			To   int   `json:"to"`
			Gold int64 `json:"gold"`
		} `json:"legacy_gold"`
		TaskItems []struct {
			Task   int     `json:"task"`
			Sub    int     `json:"sub"`
			Item   int     `json:"item"`
			Chance float64 `json:"chance"`
			Mobs   []int   `json:"mobs"`
		} `json:"task_items"`
	} `json:"tasks"`
	Side struct {
		Npc           int      `json:"npc"`
		Maps          []int    `json:"maps"`
		MaxPerDay     int      `json:"max_per_day"`
		OfferLevels   []int    `json:"offer_levels"`
		LevelNames    []string `json:"level_names"`
		Gold          []int64  `json:"gold"`
		Items         []int    `json:"items"`
		ItemOptions   [][2]int `json:"item_options"`
		HellBonusItem int      `json:"hell_bonus_item"`
		Mobs          []int    `json:"mobs"`
		GoldTemplate  int      `json:"gold_template"`
	} `json:"side"`
	ClanTask struct {
		Npcs []struct {
			Npc int `json:"npc"`
			Map int `json:"map"`
		} `json:"npcs"`
		MaxPerDay      int   `json:"max_per_day"`
		Levels         int   `json:"levels"`
		PointsPerLevel int64 `json:"points_per_level"`
	} `json:"clan_task"`
	Badges struct {
		Days  int                      `json:"days"`
		Tasks map[string]progBadgeHook `json:"tasks"`
	} `json:"badges"`
	Achievements map[string]progAchSpec `json:"achievements"`
	Dragon       struct {
		Maps      []int `json:"maps"`
		WaitS     int   `json:"wait_s"`
		CooldownS int   `json:"cooldown_s"`
		Namek     struct {
			Map       int   `json:"map"`
			Npc       int   `json:"npc"`
			Balls     []int `json:"balls"`
			HourFrom  int   `json:"hour_from"`
			HourTo    int   `json:"hour_to"`
			CooldownS int   `json:"cooldown_s"`
		} `json:"namek"`
	} `json:"dragon"`
}

type progBadgeHook struct {
	On     string   `json:"on"` // boss, pickup, side_pay, dragon
	Bosses []string `json:"bosses"`
}

type progAchSpec struct {
	On    string `json:"on"` // power, tree, kill_type, kill_mob, play_hours, pickup_item, kill_boss
	Value int    `json:"value"`
}

// Template rows from the legacy tables.
type progTemplate struct {
	ID       int
	Name     string
	Min, Max []int // per difficulty 0..4
}

type progBadge struct {
	ID, Effect, Item int
	Name             string
	Options          [][2]int
}

type progBadgeTask struct {
	ID, Max, Reward int
	Name            string
}

type progAch struct {
	Idx, ID      int
	Info1, Info2 string
	Money        int64
	Max          int64
}

type progData struct {
	C          *progContent
	Side, Clan []progTemplate
	Badges     map[int]*progBadge // by effect id
	BadgeTasks []progBadgeTask
	Achs       []progAch
	subs       map[*TaskSub]*progSub
}

var (
	progReg     sync.Map // *Data -> *progData
	progDefault *progContent
)

func progParse() *progContent {
	c := &progContent{}
	if err := json.Unmarshal(progContentJSON, c); err != nil {
		panic("prog_content.json: " + err.Error())
	}
	return c
}

func progOf(d *Data) *progData {
	if d == nil {
		return nil
	}
	if v, ok := progReg.Load(d); ok {
		return v.(*progData)
	}
	return nil
}

func init() {
	progDefault = progParse()
	dataHooks = append(dataHooks, func(ctx context.Context, pool *pgxpool.Pool, d *Data) error {
		return progInit(d, func(pd *progData) error { return pd.load(ctx, pool) })
	})
	worldStartHooks = append(worldStartHooks, func(w *World) { w.progInstall() })
}

// progInit builds the progression data for d; load fills the template tables.
func progInit(d *Data, load func(pd *progData) error) error {
	pd := &progData{C: progParse(), Badges: map[int]*progBadge{}, subs: map[*TaskSub]*progSub{}}
	if load != nil {
		if err := load(pd); err != nil {
			return err
		}
	}
	pd.applyTasks(d)
	if d.Content != nil {
		// The achievement_template table replaces the 2 sample achievements of content.json.
		d.Content.Achievements = nil
		for _, ti := range pd.C.Tasks.TaskItems {
			d.Content.Drops.TaskItems = append(d.Content.Drops.TaskItems, struct {
				Task   int     `json:"task"`
				Sub    int     `json:"sub"`
				Item   int     `json:"item"`
				Chance float64 `json:"chance"`
				Mobs   []int   `json:"mobs"`
			}{ti.Task, ti.Sub, ti.Item, ti.Chance, ti.Mobs})
		}
	}
	progReg.Store(d, pd)
	return nil
}

// applyTasks has the semantics of Data.applyTaskOverrides, plus custom kinds.
func (pd *progData) applyTasks(d *Data) {
	byID := map[int]*TaskMain{}
	for _, t := range d.Tasks {
		byID[t.ID] = t
	}
	for key, o := range pd.C.Tasks.Overrides {
		ti, si, ok := strings.Cut(key, ".")
		if !ok {
			continue
		}
		tid, _ := strconv.Atoi(ti)
		s, _ := strconv.Atoi(si)
		t := byID[tid]
		if t == nil || s < 0 || s >= len(t.Subs) {
			continue
		}
		sub := t.Subs[s]
		sub.Kind = o.Kind
		if o.Name != "" {
			sub.Name = o.Name
		}
		if o.Max > 0 {
			sub.MaxCount = o.Max
		}
		if o.Maps != nil {
			sub.Maps = o.Maps
		}
		if o.Npc != nil {
			sub.Npc = *o.Npc
		}
		if o.Mobs != nil {
			sub.Mobs = o.Mobs
		}
		if o.Item != 0 {
			sub.Item = o.Item
		}
		if strings.HasPrefix(o.Kind, "p_") {
			pd.subs[sub] = o
		}
	}
}

func (pd *progData) load(ctx context.Context, pool *pgxpool.Pool) error {
	if pool == nil {
		return nil
	}
	var err error
	if pd.Side, err = loadProgTemplates(ctx, pool, "side_task_template"); err != nil {
		slog.Warn("side_task_template", "err", err)
	}
	if pd.Clan, err = loadProgTemplates(ctx, pool, "clan_task_template"); err != nil {
		slog.Warn("clan_task_template", "err", err)
	}
	rows, err := pool.Query(ctx, `SELECT id, effect_id, item_id, name, options FROM badge ORDER BY id`)
	if err == nil {
		for rows.Next() {
			b := &progBadge{}
			var raw []byte
			if err := rows.Scan(&b.ID, &b.Effect, &b.Item, &b.Name, &raw); err != nil {
				return fmt.Errorf("badge: %w", err)
			}
			var opts []struct{ ID, Param int }
			json.Unmarshal(raw, &opts)
			for _, o := range opts {
				b.Options = append(b.Options, [2]int{o.ID, o.Param})
			}
			pd.Badges[b.Effect] = b
		}
	}
	rows, err = pool.Query(ctx, `SELECT id, name, max_count, reward_effect_id FROM task_badge_template ORDER BY id`)
	if err == nil {
		for rows.Next() {
			var t progBadgeTask
			if err := rows.Scan(&t.ID, &t.Name, &t.Max, &t.Reward); err != nil {
				return fmt.Errorf("task_badge_template: %w", err)
			}
			pd.BadgeTasks = append(pd.BadgeTasks, t)
		}
	}
	rows, err = pool.Query(ctx, `SELECT idx, id, info1, info2, money, max_count FROM achievement_template ORDER BY idx`)
	if err == nil {
		for rows.Next() {
			var a progAch
			if err := rows.Scan(&a.Idx, &a.ID, &a.Info1, &a.Info2, &a.Money, &a.Max); err != nil {
				return fmt.Errorf("achievement_template: %w", err)
			}
			pd.Achs = append(pd.Achs, a)
		}
	}
	return nil
}

func loadProgTemplates(ctx context.Context, pool *pgxpool.Pool, table string) ([]progTemplate, error) {
	rows, err := pool.Query(ctx, `SELECT id, name, count_min, count_max FROM `+table+` ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []progTemplate
	for rows.Next() {
		var t progTemplate
		var mn, mx []int32
		if err := rows.Scan(&t.ID, &t.Name, &mn, &mx); err != nil {
			return nil, err
		}
		for i := range mn {
			t.Min = append(t.Min, int(mn[i]))
		}
		for i := range mx {
			t.Max = append(t.Max, int(mx[i]))
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// badgeForReward resolves task_badge_template.reward_effect_id, which holds a
// badge effect id (or, for a few rows, an item id).
func (pd *progData) badgeForReward(id int) *progBadge {
	if b := pd.Badges[id]; b != nil {
		return b
	}
	for _, b := range pd.Badges {
		if b.Item == id {
			return b
		}
	}
	return nil
}

// --- shared helpers ---

var progFoldMap = func() map[rune]rune {
	m := map[rune]rune{'đ': 'd'}
	for base, vs := range map[rune]string{
		'a': "àáạảãâầấậẩẫăằắặẳẵ", 'e': "èéẹẻẽêềếệểễ", 'i': "ìíịỉĩ",
		'o': "òóọỏõôồốộổỗơờớợởỡ", 'u': "ùúụủũưừứựửữ", 'y': "ỳýỵỷỹ",
	} {
		for _, r := range vs {
			m[r] = base
		}
	}
	return m
}()

// progNorm folds Vietnamese diacritics and keeps only [a-z0-9].
func progNorm(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if f, ok := progFoldMap[r]; ok {
			r = f
		}
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

var progLevelSuffix = regexp.MustCompile(`^(cap|lan|level|lv)?\d*$`)

// progBossMatch reports whether the killed boss (template id, plus its name if
// it is a world boss) is one of aliases. A level suffix ("fide2", "xencon_3")
// still matches.
func (w *World) progBossMatch(boss string, aliases []string) bool {
	names := []string{progNorm(boss)}
	if t := w.bossTemplate(boss); t != nil {
		names = append(names, progNorm(t.Name))
	}
	for _, a := range aliases {
		na := progNorm(a)
		for _, n := range names {
			if na != "" && strings.HasPrefix(n, na) && progLevelSuffix.MatchString(n[len(na):]) {
				return true
			}
		}
	}
	return false
}

func progToday() string { return time.Now().Format("2006-01-02") }

func progFreeSlots(p *Player) int {
	n := 0
	for i := range p.size(1) { // 1 = bag
		if p.item(1, i) == nil {
			n++
		}
	}
	return n
}

func progItemName(d *Data, id int) string {
	if t := d.ItemsFull[id]; t != nil {
		return t.Name
	}
	return "vật phẩm"
}

// progNear reports whether p stands near an NPC of one of ids in this zone.
func progNear(z *Zone, p *Player, ids ...int) bool {
	for _, n := range z.Map.Npcs {
		if containsInt(ids, n.TemplateID) && dist(p.Char.X, p.Char.Y, n.X, n.Y) <= npcRange*2 {
			return true
		}
	}
	return false
}

// progPeek returns feature state only if the player already has it, so hooks
// that run for everyone do not create empty entries.
func progPeek[T any](p *Player, key string) *T {
	if _, ok := p.persist[key]; ok {
		return State[T](p, key)
	}
	if _, ok := p.Char.Extra.Feat[key]; ok {
		return State[T](p, key)
	}
	return nil
}
