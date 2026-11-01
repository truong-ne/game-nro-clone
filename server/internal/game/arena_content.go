package game

// Arena feature set: rich dungeons (phó bản), Ngọc Rồng Sao Đen, Mabư 14h,
// the legacy world-boss roster, Đại hội võ thuật, Siêu hạng, leaderboards and
// minigames. Everything is data-driven from arena_content.json and plugs into
// the core through hooks.go only. Legacy sources are cited as
// SRC/src/nro/models/<path>:<line>.

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"log/slog"
	"strconv"
	"sync"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed arena_content.json
var arenaContentJSON []byte

type arenaStage struct {
	Name      string `json:"name"`
	HP        int64  `json:"hp"`
	Dmg       int64  `json:"dmg"`
	Src       string `json:"src"`
	ChatStart string `json:"chat_start"`
	ChatDie   string `json:"chat_die"`
}

// arenaBossSpec is one legacy boss family (boss/*, BossesData.java). Multi-form
// bosses (Fide, Xên, Cooler...) list one stage per form; killing a stage spawns
// the next one in the same zone, as Boss.java cycled through its BossData[].
type arenaBossSpec struct {
	ID       string       `json:"id"`
	Src      string       `json:"src"`
	Mob      int          `json:"mob"`
	Maps     []int        `json:"maps"`
	RespawnS int          `json:"respawn_s"`
	RestS    int          `json:"rest_s"`
	Rewards  string       `json:"rewards"`
	Stages   []arenaStage `json:"stages"`

	templates []*BossTemplate
}

// arenaFloorBoss is a dungeon boss placed on one floor; hp/dmg scale with level
// (hp_lv × level) or, for Doanh trại, with the clan's strength (mult_pct).
type arenaFloorBoss struct {
	Map        int             `json:"map"`
	Name       string          `json:"name"`
	Mob        int             `json:"mob"`
	HPLv       int64           `json:"hp_lv"`
	DmgLv      int64           `json:"dmg_lv"`
	MultPct    int64           `json:"mult_pct"`
	Count      int             `json:"count"`
	Win        bool            `json:"win"`
	AfterClear bool            `json:"after_clear"`
	Src        string          `json:"src"`
	Then       *arenaFloorBoss `json:"then"`
}

type arenaDungeon struct {
	ID            string           `json:"-"`
	Name          string           `json:"name"`
	Src           string           `json:"src"`
	Npc           int              `json:"npc"`
	Maps          []int            `json:"maps"`
	Minutes       int              `json:"minutes"`
	ClanOnly      bool             `json:"clan_only"`
	LeaderOnly    bool             `json:"leader_only"`
	MinPower      int64            `json:"min_power"`
	Item          int              `json:"item"`
	MinLevel      int              `json:"min_level"`
	MaxLevel      int              `json:"max_level"`
	PlayerDaily   int              `json:"player_daily"`
	PlayerWeekly  int              `json:"player_weekly"`
	ClanDaily     int              `json:"clan_daily"`
	MobHPK        int64            `json:"mob_hp_k"`
	MobHPKMaps    map[string]int64 `json:"mob_hp_k_maps"`
	EliteIndex    *int             `json:"elite_index"`
	EliteK        int64            `json:"elite_k"`
	ScaleByClan   bool             `json:"scale_by_clan"`
	PickMinutes   int              `json:"pick_minutes"`
	Bosses        []arenaFloorBoss `json:"bosses"`
	WinWhen       string           `json:"win_when"` // "bosses" (win-flagged bosses dead) or "all"
	KickAfterWinS int              `json:"kick_after_win_s"`
	HomeMap       int              `json:"home_map"` // -1: the player's home planet
}

func (dg *arenaDungeon) leveled() bool { return dg.MaxLevel > 0 }

type arenaBuff struct {
	Star    int   `json:"star"`
	DmgPct  int64 `json:"dmg_pct"`
	HPPct   int64 `json:"hp_pct"`
	MPPct   int64 `json:"mp_pct"`
	LeechHP int   `json:"leech_hp"`
	Reflect int   `json:"reflect"`
	Dodge   int   `json:"dodge"`
}

type arenaCup struct {
	Name     string `json:"name"`
	Hours    []int  `json:"hours"`
	Gem      int64  `json:"gem"`
	Gold     int64  `json:"gold"`
	MinPower int64  `json:"min_power"`
	MaxPower int64  `json:"max_power"`
}

type arenaLucky struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Src    string `json:"src"`
	Item   int    `json:"item"` // 0: pay and win gems
	Cost   int    `json:"cost"`
	Reward int    `json:"reward"`
}

type arenaChoose struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Src       string `json:"src"`
	Gem       bool   `json:"gem"`
	Normal    int64  `json:"normal"`
	Vip       int64  `json:"vip"`
	NormalPct int64  `json:"normal_pct"`
	VipPct    int64  `json:"vip_pct"`
}

type arenaConfig struct {
	Rewards     map[string][]Reward      `json:"rewards"`
	WorldBosses []*arenaBossSpec         `json:"world_bosses"`
	Dungeons    map[string]*arenaDungeon `json:"dungeons"`
	Mabu14H     struct {
		Npc        int            `json:"npc"`
		HourOpen   int            `json:"hour_open"`
		HourClose  int            `json:"hour_close"`
		GateMap    int            `json:"gate_map"`
		BossMap    int            `json:"boss_map"`
		MaxPerZone int            `json:"max_per_zone"`
		Boss       *arenaBossSpec `json:"boss"`
	} `json:"mabu14h"`
	BlackBall struct {
		Npc         int         `json:"npc"`
		Maps        []int       `json:"maps"`
		FirstBall   int         `json:"first_ball"`
		HourOpen    int         `json:"hour_open"`
		PickMinute  int         `json:"pick_minute"`
		HourClose   int         `json:"hour_close"`
		HoldS       int         `json:"hold_s"`
		RewardHours int         `json:"reward_hours"`
		RepickS     int         `json:"repick_delay_s"`
		Buffs       []arenaBuff `json:"buffs"`
	} `json:"black_ball_war"`
	Tournament struct {
		Npc           int        `json:"npc"`
		LobbyMap      int        `json:"lobby_map"`
		ArenaMap      int        `json:"arena_map"`
		RegUntil      int        `json:"reg_until_minute"`
		StartMinute   int        `json:"start_minute"`
		EndMinute     int        `json:"end_minute"`
		MatchS        int        `json:"match_s"`
		RoundGapS     int        `json:"round_gap_s"`
		Cups          []arenaCup `json:"cups"`
		ChampionGems  int64      `json:"champion_gems"`
		ChampionItems []int      `json:"champion_items"`
		WinGoldMin    int        `json:"win_gold_min"`
		WinGoldMax    int        `json:"win_gold_max"`
	} `json:"tournament"`
	SuperRank struct {
		Npc         int   `json:"npc"`
		Map         int   `json:"map"`
		MatchS      int   `json:"match_s"`
		FreeTickets int   `json:"free_tickets"`
		GemCost     int64 `json:"gem_cost"`
		RivalMob    int   `json:"rival_mob"`
		DailyGems   []struct {
			MaxRank int   `json:"max_rank"`
			Gems    int64 `json:"gems"`
		} `json:"daily_gems"`
	} `json:"super_rank"`
	Minigames struct {
		Lucky      []*arenaLucky  `json:"lucky"`
		LuckyBetS  int            `json:"lucky_bet_s"`
		LuckyWaitS int            `json:"lucky_wait_s"`
		LuckyMaxN  int            `json:"lucky_max_numbers"`
		LuckyMax   int            `json:"lucky_max"`
		Choose     []*arenaChoose `json:"choose"`
		ChooseS    int            `json:"choose_round_s"`
		ChooseTop  int            `json:"choose_top"`
		Npc        int            `json:"npc"`
	} `json:"minigames"`
	TopRefreshS int `json:"top_refresh_s"`

	stageByID map[string]*arenaBossSpec // any stage template id -> family
}

var arena = mustLoadArena(arenaContentJSON)

func mustLoadArena(b []byte) *arenaConfig {
	c := &arenaConfig{}
	if err := json.Unmarshal(bytes.TrimPrefix(b, []byte("\xef\xbb\xbf")), c); err != nil {
		panic("arena_content.json: " + err.Error())
	}
	c.stageByID = map[string]*arenaBossSpec{}
	specs := append([]*arenaBossSpec{}, c.WorldBosses...)
	if c.Mabu14H.Boss != nil {
		specs = append(specs, c.Mabu14H.Boss)
	}
	for _, s := range specs {
		s.build(c.Rewards[s.Rewards])
		for _, t := range s.templates {
			c.stageByID[t.ID] = s
		}
	}
	for id, dg := range c.Dungeons {
		dg.ID = id
	}
	return c
}

// build turns the stages into boss templates; stage i>0 gets id "<id>_s<i>".
func (s *arenaBossSpec) build(rewards []Reward) {
	s.templates = nil
	for i, st := range s.Stages {
		id := s.ID
		if i > 0 {
			id += "_s" + strconv.Itoa(i)
		}
		t := &BossTemplate{ID: id, Name: st.Name, Mob: s.Mob, HP: max(st.HP, 1), Dmg: st.Dmg, Maps: s.Maps,
			RespawnS: s.RespawnS, RestS: s.RestS, ChatStart: st.ChatStart, ChatDie: st.ChatDie}
		if i == len(s.Stages)-1 {
			t.Rewards = rewards // only the last form drops (Boss.reward runs on final death)
		}
		t.normalize()
		s.templates = append(s.templates, t)
	}
}

// stageIndex returns the stage of template id inside its family, or -1.
func (s *arenaBossSpec) stageIndex(id string) int {
	for i, t := range s.templates {
		if t.ID == id {
			return i
		}
	}
	return -1
}

func init() {
	dataHooks = append(dataHooks, func(_ context.Context, _ *pgxpool.Pool, d *Data) error {
		arenaInstallData(d)
		return nil
	})
}

// arenaInstallData registers the boss roster's first forms with the core boss
// scheduler and exposes each dungeon as a plain DungeonSpec too.
func arenaInstallData(d *Data) {
	if d.Content == nil {
		return
	}
	if d.Content.Dungeons == nil {
		d.Content.Dungeons = map[string]*DungeonSpec{}
	}
	for id, dg := range arena.Dungeons {
		d.Content.Dungeons[id] = &DungeonSpec{Name: dg.Name, Maps: dg.Maps, Minutes: dg.Minutes, MobHPMult: 1, MinMembers: 1}
	}
	have := map[string]bool{}
	for _, t := range d.Content.Bosses {
		have[t.ID] = true
	}
	n := 0
	for _, s := range arena.WorldBosses {
		if len(s.templates) == 0 || have[s.ID] {
			continue
		}
		d.Content.Bosses = append(d.Content.Bosses, s.templates[0])
		n++
	}
	slog.Info("arena content", "bosses", n, "dungeons", len(arena.Dungeons))
}

// --- shared helpers ---

// vnZone is the legacy server clock (TimeUtil uses Asia/Ho_Chi_Minh); a fixed
// offset avoids depending on tzdata in the container.
var vnZone = time.FixedZone("ICT", 7*3600)

// arenaNow is the clock used for scheduled events; tests replace it.
var arenaNow = func() time.Time { return time.Now().In(vnZone) }

func arenaDay(t time.Time) string { return t.In(vnZone).Format("2006-01-02") }

// arenaPlayer is the per-character arena state saved in character.extra.
type arenaPlayer struct {
	Day       string           `json:"day"`
	Entries   map[string]int   `json:"entries,omitempty"`  // dungeon id -> entries today
	LastRun   map[string]int64 `json:"last_run,omitempty"` // dungeon id -> opened unix of the run last counted
	WeekStart map[string]int64 `json:"week,omitempty"`     // dungeon id -> unix of the counted weekly entry
	BlackBall map[int]int64    `json:"bb,omitempty"`       // star -> buff expiry unix
	Tickets   int              `json:"tickets"`
	RankClaim string           `json:"rank_claim,omitempty"`
}

func arenaState(p *Player) *arenaPlayer {
	s := State[arenaPlayer](p, "arena")
	if today := arenaDay(arenaNow()); s.Day != today {
		s.Day, s.Entries, s.Tickets = today, map[string]int{}, arena.SuperRank.FreeTickets
	}
	if s.Entries == nil {
		s.Entries = map[string]int{}
	}
	if s.LastRun == nil {
		s.LastRun = map[string]int64{}
	}
	if s.WeekStart == nil {
		s.WeekStart = map[string]int64{}
	}
	if s.BlackBall == nil {
		s.BlackBall = map[int]int64{}
	}
	return s
}

// arenaNearNpc reports whether p stands next to an NPC of template npc.
func arenaNearNpc(z *Zone, p *Player, npc int) bool {
	if p.IsAdmin {
		return true
	}
	for _, n := range z.Map.Npcs {
		if n.TemplateID == npc && dist(p.Char.X, p.Char.Y, n.X, n.Y) <= npcRange+40 {
			return true
		}
	}
	return false
}

// arenaMove sends p (on z's goroutine) into a specific zone.
func arenaMove(z *Zone, p *Player, to *Zone, x, y int) {
	z.remove(p)
	p.dirty = true
	z.World.EnterZone(p, to, x, y)
}

// arenaRevive brings a duel/arena loser back without the death penalty.
func arenaRevive(z *Zone, p *Player) {
	if !p.Dead {
		return
	}
	p.Dead = false
	p.Char.HP, p.Char.MP = p.MaxHP, p.MaxMP
	p.dirty = true
	z.broadcast(Encode("revive", map[string]int64{"id": p.ID(), "hp": p.Char.HP}), 0)
}

// arenaZoneRegistry maps zones to feature state for code running on zone
// goroutines (second hooks) and on world goroutines.
type arenaZoneRegistry[T any] struct {
	mu sync.Mutex
	m  map[*Zone]T
}

func (r *arenaZoneRegistry[T]) get(z *Zone) (T, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	v, ok := r.m[z]
	return v, ok
}

func (r *arenaZoneRegistry[T]) set(z *Zone, v T) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.m == nil {
		r.m = map[*Zone]T{}
	}
	r.m[z] = v
}

func (r *arenaZoneRegistry[T]) del(z *Zone) {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.m, z)
}

func arenaCap(v, c int64) int64 { return max(min(v, c), 1) }
