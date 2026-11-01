package game

import (
	"context"
	_ "embed"
	"encoding/json"
	"log/slog"
	"math/rand/v2"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Item economy (crafting, usable items, drops, lucky wheel, consignment,
// power limit). Rates and costs live in items2_content.json.

//go:embed items2_content.json
var i2Raw []byte

type i2Opt struct {
	ID     int     `json:"id"`
	Min    int     `json:"min"`
	Max    int     `json:"max"`
	Chance float64 `json:"chance"` // percent; 0 = always
	Else   []int   `json:"else"`   // [id, param] added when Chance fails
}

type i2Pick struct {
	IDs []int `json:"ids"`
	Min int   `json:"min"`
	Max int   `json:"max"`
}

// i2Reward is one entry of a weighted reward pool (boxes, lucky wheel).
type i2Reward struct {
	Items  []int    `json:"items"`
	Min    int      `json:"min"`
	Max    int      `json:"max"`
	Weight float64  `json:"weight"`
	Opts   []i2Opt  `json:"opts"`
	Pick   []i2Pick `json:"pick"`
}

// i2Stack is an item outside the inventory (lucky box, consignment).
type i2Stack struct {
	TID  int      `json:"tid"`
	Qty  int      `json:"qty"`
	Opts [][2]int `json:"opts"`
}

type i2Input struct {
	IDs []int `json:"ids"`
	Qty int   `json:"qty"`
}

type i2Recipe struct {
	ID      string    `json:"id"`
	Name    string    `json:"name"`
	Inputs  []i2Input `json:"inputs"`
	Gold    int64     `json:"gold"`
	Gem     int64     `json:"gem"`
	Rate    float64   `json:"rate"`
	Outputs []int     `json:"outputs"`
}

type i2Earring struct {
	From     int     `json:"from"`
	To       int     `json:"to"`
	Level    int     `json:"level"`
	Fragment int     `json:"fragment"`
	Need     int     `json:"need"`
	FailCost int     `json:"fail_cost"`
	Gold     int64   `json:"gold"`
	Gem      int64   `json:"gem"`
	Rate     float64 `json:"rate"`
}

type i2EarStat struct {
	Item  int     `json:"item"`
	Level int     `json:"level"`
	Soul  int     `json:"soul"`
	Need  int     `json:"need"`
	Stone int     `json:"stone"`
	Gem   int64   `json:"gem"`
	Rate  float64 `json:"rate"`
	Count int     `json:"count"`
}

type i2DropGroup struct {
	Src    string     `json:"src"`
	Maps   [][2]int   `json:"maps"`
	Except []int      `json:"except"`
	Rules  []DropRule `json:"rules"`
}

type i2Config struct {
	CraftNpc int `json:"craft_npc"`
	Upgrade  struct {
		MaxLevel    int            `json:"max_level"`
		StoneByType map[string]int `json:"stone_by_type"`
		Protect     int            `json:"protect_item"`
		Stones      []int          `json:"stones"`
		Gold        []int64        `json:"gold"`
		Rate        []float64      `json:"rate"`
		Downgrade   []int          `json:"downgrade_levels"`
		MainOpts    []int          `json:"main_options"`
		SideOpts    []int          `json:"side_options"`
		LevelOpt    int            `json:"level_option"`
		CounterOpt  int            `json:"downgrade_counter_option"`
	} `json:"upgrade"`
	Crystal struct {
		Types      []int     `json:"types"`
		MaxStar    int       `json:"max_star"`
		Gold       []int64   `json:"gold"`
		Gem        []int64   `json:"gem"`
		Rate       []float64 `json:"rate"`
		CooldownMs int       `json:"cooldown_ms"`
		MaxBatch   int       `json:"max_batch"`
		SlotOpt    int       `json:"slot_option"`
	} `json:"crystallize"`
	Socket struct {
		Gem       int64             `json:"gem"`
		StoneType int               `json:"stone_type"`
		Stones    map[string][2]int `json:"stones"`
		StarOpt   int               `json:"star_option"`
		SlotOpt   int               `json:"slot_option"`
		MaxSlots  int               `json:"max_slots"`
		SepFrom   int               `json:"separate_from_star"`
		UnlockOpt int               `json:"unlock_option"`
		UnlockAt  int               `json:"unlock_from_slot"`
	} `json:"socket"`
	Earrings     []i2Earring `json:"earring_upgrade"`
	EarringStats []i2EarStat `json:"earring_stats"`
	EarringOpts  []int       `json:"earring_options"`
	EarringParam [2]int      `json:"earring_param"`
	FragOpt      int         `json:"fragment_count_option"`
	Recipes      []i2Recipe  `json:"recipes"`
	Disassemble  struct {
		Gold    int64 `json:"gold"`
		Options []int `json:"options"`
		Output  int   `json:"output"`
	} `json:"disassemble"`
	Kit struct {
		Capsule   int              `json:"capsule"`
		Items     [][][]int        `json:"items"` // [gender][type] -> ids
		TypeNames []string         `json:"type_names"`
		Sets      [][]int          `json:"sets"` // [gender] -> set option ids
		Subs      map[string][]int `json:"subs"`
		NoTrade   int              `json:"no_trade_option"`
		Drop      struct {
			Maps        []int          `json:"maps"`
			Chance      float64        `json:"chance"`
			Items       [][][]int      `json:"items"` // [type][gender] -> ids
			TypeWeights []float64      `json:"type_weights"`
			SetWeights  [][][2]float64 `json:"set_weights"` // [gender] -> (set id, weight)
		} `json:"drop"`
	} `json:"kit"`
	Use struct {
		GoldBar      int                   `json:"gold_bar"`
		GoldBarValue int64                 `json:"gold_bar_value"`
		Notice       map[string]string     `json:"notice"`
		EquipTypes   map[string]int        `json:"equip_types"`
		Boxes        map[string][]i2Reward `json:"boxes"`
	} `json:"use"`
	Drops []i2DropGroup `json:"drops"`
	Lucky struct {
		Npc       int        `json:"npc"`
		PriceGold int64      `json:"price_gold"`
		PriceGem  int64      `json:"price_gem"`
		Ticket    int        `json:"ticket"`
		BoxMax    int        `json:"box_max"`
		MaxSpin   int        `json:"max_spin"`
		Pool      []i2Reward `json:"pool"`
	} `json:"lucky"`
	Consign struct {
		Npc          int      `json:"npc"`
		GoldBar      int      `json:"gold_bar"`
		FeeBars      int      `json:"fee_bars"`
		UpTopBars    int      `json:"uptop_bars"`
		TaxPct       int      `json:"tax_pct"`
		MaxQty       int      `json:"max_qty"`
		MaxBars      int      `json:"max_price_bars"`
		MaxGem       int      `json:"max_price_gem"`
		BuyMinPower  int64    `json:"buy_min_power"`
		PageSize     int      `json:"page_size"`
		AllowedTypes []int    `json:"allowed_types"`
		AllowedIDs   []int    `json:"allowed_ids"`
		AllowedOpts  []int    `json:"allowed_options"`
		Forbidden    []int    `json:"forbidden_options"`
		TabNames     []string `json:"tab_names"`
	} `json:"consign"`
	Limit struct {
		BasicSeconds int64             `json:"basic_seconds"`
		SpeedGold    int64             `json:"speed_gold"`
		Max          int               `json:"max"`
		Npcs         map[string][2]int `json:"npcs"`
	} `json:"limit"`
}

var i2cfg = func() *i2Config {
	c := &i2Config{}
	if err := json.Unmarshal(i2Raw, c); err != nil {
		panic("items2_content.json: " + err.Error())
	}
	return c
}()

func init() {
	dataHooks = append(dataHooks, func(ctx context.Context, pool *pgxpool.Pool, d *Data) error {
		i2MergeDrops(d)
		if pool != nil {
			i2LoadFlagBags(ctx, pool)
		}
		return nil
	})
}

// --- option helpers ---

func i2OptIdx(opts [][2]int, id int) int {
	for i, o := range opts {
		if o[0] == id {
			return i
		}
	}
	return -1
}

func i2OptVal(opts [][2]int, id int) int {
	if i := i2OptIdx(opts, id); i >= 0 {
		return opts[i][1]
	}
	return 0
}

// i2SetOpt sets option id on it (appending if absent). The slice is copied
// first so options shared with templates or other items are never mutated.
func i2SetOpt(it *store.Item, id, v int) {
	it.Options = append([][2]int(nil), it.Options...)
	if i := i2OptIdx(it.Options, id); i >= 0 {
		it.Options[i][1] = v
		return
	}
	it.Options = append(it.Options, [2]int{id, v})
}

func i2Rand(lo, hi int) int {
	if hi <= lo {
		return lo
	}
	return lo + rand.IntN(hi-lo+1)
}

func i2Roll(rate float64) bool { return rand.Float64()*100 < rate }

func i2Has(list []int, v int) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}

// --- reward pools ---

func i2PickPool(pool []i2Reward) *i2Reward {
	total := 0.0
	for i := range pool {
		total += pool[i].Weight
	}
	if total <= 0 {
		return nil
	}
	r := rand.Float64() * total
	for i := range pool {
		if r < pool[i].Weight {
			return &pool[i]
		}
		r -= pool[i].Weight
	}
	return &pool[len(pool)-1]
}

// roll materializes the reward; ids missing from the templates are skipped.
func (r *i2Reward) roll(d *Data) i2Stack {
	var ids []int
	for _, id := range r.Items {
		if d.ItemsFull[id] != nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return i2Stack{TID: 189, Qty: 5000} // gold fallback
	}
	s := i2Stack{TID: ids[rand.IntN(len(ids))], Qty: i2Rand(max(r.Min, 1), max(r.Max, r.Min, 1))}
	for _, o := range r.Opts {
		if o.Chance > 0 && !i2Roll(o.Chance) {
			if len(o.Else) == 2 {
				s.Opts = append(s.Opts, [2]int{o.Else[0], o.Else[1]})
			}
			continue
		}
		s.Opts = append(s.Opts, [2]int{o.ID, i2Rand(o.Min, o.Max)})
	}
	for _, pk := range r.Pick {
		if len(pk.IDs) > 0 {
			s.Opts = append(s.Opts, [2]int{pk.IDs[rand.IntN(len(pk.IDs))], i2Rand(pk.Min, pk.Max)})
		}
	}
	return s
}

// i2CanAdd reports whether AddItem would succeed for tid without changing anything.
func (p *Player) i2CanAdd(d *Data, tid int, opts [][2]int) bool {
	t := d.ItemsFull[tid]
	if t == nil {
		return false
	}
	if t.Type == ItemGold || t.Type == ItemGem || p.freeSlot(store.ContainerBag) >= 0 {
		return true
	}
	if t.Stackable {
		for _, it := range p.Char.Items {
			if it.Container == store.ContainerBag && it.TemplateID == tid && sameOptions(it.Options, opts) {
				return true
			}
		}
	}
	return false
}

// i2ShopOpts returns the options a shop sells tid with (ItemService.getListOptionItemShop).
func i2ShopOpts(d *Data, tid int) [][2]int {
	best := -1
	var out [][2]int
	for id, s := range d.Shops {
		for _, it := range s.items {
			if it.TemplateID == tid && (best < 0 || id < best) {
				best = id
				out = append([][2]int(nil), it.Options...)
			}
		}
	}
	return out
}

func i2Notice(p *Player, s string) { p.Send("notice", map[string]string{"text": s}) }

// i2NearNpc reports whether an NPC template stands near p on this map.
func i2NearNpc(z *Zone, p *Player, npc int) bool {
	for _, n := range z.Map.Npcs {
		if n.TemplateID == npc && dist(p.Char.X, p.Char.Y, n.X, n.Y) <= npcRange {
			return true
		}
	}
	return false
}

// --- drops (Mob.getItemMobReward, mob/Mob.java:603-1072) ---

func (g *i2DropGroup) has(mapID int) bool {
	if i2Has(g.Except, mapID) {
		return false
	}
	for _, r := range g.Maps {
		if mapID >= r[0] && mapID <= r[1] {
			return true
		}
	}
	return false
}

// i2MergeDrops installs the legacy per-map rules. dropsFor uses a map's list
// instead of the default one, so each affected map gets default + extras.
func i2MergeDrops(d *Data) {
	c := d.Content
	if c == nil {
		return
	}
	if c.Drops.Maps == nil {
		c.Drops.Maps = map[string][]DropRule{}
	}
	for id := range d.Maps {
		var extra []DropRule
		for gi := range i2cfg.Drops {
			g := &i2cfg.Drops[gi]
			if !g.has(id) {
				continue
			}
			for _, r := range g.Rules {
				if d.ItemsFull[r.Item] != nil {
					extra = append(extra, r)
				}
			}
		}
		if len(extra) == 0 {
			continue
		}
		key := strconv.Itoa(id)
		base, ok := c.Drops.Maps[key]
		if !ok {
			base = c.Drops.Default
		}
		c.Drops.Maps[key] = append(append([]DropRule(nil), base...), extra...)
	}
}

// --- flag bags (flag_bag table): part id -> icons, for the client ---

var i2FlagBags = map[int][]int{}

func i2LoadFlagBags(ctx context.Context, pool *pgxpool.Pool) {
	rows, err := pool.Query(ctx, `SELECT id, icons FROM flag_bag`)
	if err != nil {
		slog.Warn("flag_bag", "err", err)
		return
	}
	defer rows.Close()
	for rows.Next() {
		var id int
		var icons []int32
		if rows.Scan(&id, &icons) == nil {
			out := make([]int, len(icons))
			for i, v := range icons {
				out[i] = int(v)
			}
			i2FlagBags[id] = out
		}
	}
}
