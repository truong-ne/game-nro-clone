//go:build wip_pets

package game

import (
	_ "embed"
	"encoding/json"
	"strconv"
)

// Pets, fusion, set kích hoạt, the crafts items2 skipped, stamina, enemies
// and bag expansion. Numbers live in pg_content.json; each rule cites the
// legacy Java source (SRC/src/nro/models/...) where it is applied.

//go:embed pg_content.json
var pgRaw []byte

type pgWeighted [][2]int // [value, weight]

type pgConfig struct {
	Fusion struct {
		TimeMs      int64          `json:"time_ms"`
		WaitMs      int64          `json:"wait_after_unfuse_ms"`
		PorataItems map[string]int `json:"porata_items"`
		Outfits     [][3]int       `json:"outfits"`
	} `json:"fusion"`
	Pet struct {
		StaminaMax      int            `json:"stamina_max"`
		Skill1          []int          `json:"skill1"`
		SkillPower      []int64        `json:"skill_power"`
		Skill2          pgWeighted     `json:"skill2"`
		Skill3          pgWeighted     `json:"skill3"`
		Skill4          pgWeighted     `json:"skill4"`
		Skill5          []int          `json:"skill5"`
		Skill5Types     []int          `json:"skill5_types"`
		Skill2CdMs      int            `json:"skill2_cooldown_ms"`
		DefBase         [2]int         `json:"def_base"`
		CritBase        [2]int         `json:"crit_base"`
		MeleeRange      int            `json:"melee_range"`
		SkillRange      int            `json:"skill_range"`
		PointsPerSecond int            `json:"points_per_second"`
		AskPeaMs        int64          `json:"ask_pea_ms"`
		PeaStaminaLv    int            `json:"pea_stamina_per_level"`
		EquipMinPower   int64          `json:"equip_min_power"`
		EquipTypes      map[string]int `json:"equip_types"`
		IdleMs          [2]int64       `json:"idle_ms"`
		AtkPerStamina   int            `json:"attacks_per_stamina"`
		AtkPerStaminaC  int            `json:"attacks_per_stamina_charm"`
	} `json:"pet"`
	Stamina struct {
		Max          int   `json:"max"`
		AtkPerPoint  int   `json:"attacks_per_point"`
		RegenMs      int64 `json:"regen_ms"`
		GrapeFull    int   `json:"grape_full"`
		GrapePart    int   `json:"grape_part"`
		GrapePartPct int   `json:"grape_part_pct"`
		GrapeMaxPct  int   `json:"grape_max_pct"`
	} `json:"stamina"`
	OaiHung struct {
		LeaderMobs []int `json:"leader_mobs"`
	} `json:"oai_hung"`
	Bag struct {
		Item int `json:"item"`
		Max  int `json:"max"`
	} `json:"bag"`
	Revenge struct {
		Gem        int64 `json:"gem"`
		WindowMs   int64 `json:"window_ms"`
		DelayMs    int64 `json:"start_delay_ms"`
		MaxEnemies int   `json:"max_enemies"`
	} `json:"revenge"`
	Sets struct {
		Options  map[string]string `json:"options"`
		Worldcup []int             `json:"worldcup_items"`
	} `json:"sets"`
	Craft struct {
		Hole struct {
			Gold     int64   `json:"gold"`
			Hematite int     `json:"hematite"`
			Punch    int     `json:"punch"`
			SlotOpt  int     `json:"slot_option"`
			StarOpt  int     `json:"star_option"`
			Unlock   int     `json:"unlock_option"`
			Mark     int     `json:"mark_option"`
			Rate9    float64 `json:"rate_9"`
		} `json:"hole"`
		StarUp struct {
			Gold     int64   `json:"gold"`
			Gem      int64   `json:"gem"`
			Rate     float64 `json:"rate"`
			Hematite int     `json:"hematite"`
			From     [2]int  `json:"from"`
			ToBase   int     `json:"to_base"`
		} `json:"star_up"`
		StarPolish struct {
			Gold   int64   `json:"gold"`
			Rate   float64 `json:"rate"`
			Stone  int     `json:"stone"`
			From   [2]int  `json:"from"`
			ToBase int     `json:"to_base"`
			Need   int     `json:"need"`
		} `json:"star_polish"`
		Angel struct {
			Gold      int64  `json:"gold"`
			Base      [2]int `json:"base"`
			Piece     [2]int `json:"piece"`
			PieceMin  int    `json:"piece_min"`
			PieceTake int    `json:"piece_take"`
		} `json:"angel"`
		AngelVip struct {
			Gold      int64   `json:"gold"`
			Recipes   []int   `json:"recipe_ids"`
			PieceNeed int     `json:"piece_need"`
			Upgrade   [2]int  `json:"upgrade"`
			Lucky     [2]int  `json:"lucky"`
			BaseRate  int     `json:"base_rate"`
			LuckyBase int     `json:"lucky_base"`
			Bonus     []int   `json:"bonus_options"`
		} `json:"angel_vip"`
		AngelItems [][]int        `json:"angel_items"`
		PieceType  map[string]int `json:"piece_type"`
		Convert    struct {
			Gold      int64  `json:"gold"`
			Gem       int64  `json:"gem"`
			MinLevel  int    `json:"min_level"`
			BaseIDs   []int  `json:"base_ids"`
			God       [2]int `json:"god"`
			GodGem    [2]int `json:"god_gem"`
			BaseStats []int  `json:"base_stats"`
		} `json:"convert"`
	} `json:"craft"`
	GodBox struct {
		Item  int                 `json:"item"`
		Items [][]int             `json:"items"`
		Stats map[string][][2]int `json:"stats"`
	} `json:"god_box"`
}

var pgCfg = func() *pgConfig {
	c := &pgConfig{}
	if err := json.Unmarshal(pgRaw, c); err != nil {
		panic("pg_content.json: " + err.Error())
	}
	return c
}()

func pgIn(v int, r [2]int) bool { return v >= r[0] && v <= r[1] }

func pgPick(w pgWeighted) int {
	total := 0
	for _, x := range w {
		total += x[1]
	}
	if total <= 0 {
		return -1
	}
	r := i2Rand(1, total) // Util.nextInt(1, totalTiLe), Pet.java:1009
	for _, x := range w {
		r -= x[1]
		if r <= 0 {
			return x[0]
		}
	}
	return w[len(w)-1][0]
}

func pgKey(n int) string { return strconv.Itoa(n) }

// pgSkillTmpl finds a skill template by id in any class (SkillUtil.createSkill
// looks skills up by id only; pets use skills of every race).
func pgSkillTmpl(d *Data, id int, prefer int) *SkillTemplate {
	if t := d.Skills[[2]int{prefer, id}]; t != nil {
		return t
	}
	for g := range 3 {
		if t := d.Skills[[2]int{g, id}]; t != nil {
			return t
		}
	}
	return nil
}

func pgBroadcastLook(z *Zone, id int64, head, body, leg int) {
	z.broadcast(Encode("look", map[string]any{"id": id, "head": head, "body": body, "leg": leg}), 0)
}
