package game

import (
	_ "embed"
	"encoding/json"
	"time"
)

// Shared state and config for skill effects, timed item buffs, charms,
// intrinsics and radar cards (buffs_*.go). Everything here runs on the zone
// goroutine that owns the player.

//go:embed buffs_content.json
var buffsContentJSON []byte

type buffItemSpec struct {
	ID         int    `json:"id"`
	Key        string `json:"key"`
	Name       string `json:"name"`
	Minutes    int    `json:"minutes"`
	MaxMinutes int    `json:"max_minutes"` // >0: stacks up to this (ẩn danh); 0: reuse resets the timer
	Group      string `json:"group"`       // items of one group exclude each other
	HPPct      int64  `json:"hp_pct"`
	MPPct      int64  `json:"mp_pct"`
	DmgPct     int64  `json:"dmg_pct"`
	TakenPct   int64  `json:"taken_pct"`   // damage taken becomes this % (giáp xên)
	PowerExtra int64  `json:"power_extra"` // extra multiples of power gained
}

type buffConfig struct {
	Items []*buffItemSpec `json:"items"`
	MayDo struct {
		MobFrom int     `json:"mob_from"`
		MobTo   int     `json:"mob_to"`
		Chance  float64 `json:"chance"`
		Item    int     `json:"item"`
	} `json:"may_do"`
	Charms         map[int]string `json:"charms"`
	CharmShopMin   map[string]int `json:"charm_shop_minutes"`
	CharmDefaultMn int            `json:"charm_default_minutes"`
	Intrinsic      struct {
		Npcs     []int   `json:"npcs"`
		CostOpen []int64 `json:"cost_open_million"`
		VipGem   int64   `json:"vip_gem"`
		MinPower int64   `json:"min_power"`
	} `json:"intrinsic"`
	Radar struct {
		DropChance float64 `json:"drop_chance"`
	} `json:"radar"`

	byID  map[int]*buffItemSpec
	byKey map[string]*buffItemSpec
}

var buffCfg = loadBuffConfig()

func loadBuffConfig() *buffConfig {
	c := &buffConfig{}
	if err := json.Unmarshal(buffsContentJSON, c); err != nil {
		panic("buffs_content.json: " + err.Error())
	}
	c.byID, c.byKey = map[int]*buffItemSpec{}, map[string]*buffItemSpec{}
	for _, s := range c.Items {
		c.byID[s.ID] = s
		if c.byKey[s.Key] == nil {
			c.byKey[s.Key] = s
		}
	}
	return c
}

// Runtime (not saved) skill-effect state, see buffs_effects.go.
type buffFxState struct {
	until map[string]time.Time // effect kind -> end

	monkeyLv  int
	monkeyAt  time.Time // transform finishes (1.5s lock, EffectSkillService.startUseSkillMonkey)
	huytSao   int64     // +HP% from Huýt sáo
	nextDmg   int64     // intrinsic "sát thương đòn kế" (NPoint.dameAfter)
	charge    int       // remaining Tái tạo năng lượng ticks
	chargePct int64
	eggDmg    int64
	eggUntil  time.Time
	lowHPCrit bool // intrinsic 25 state last applied by Recalc
	pending   []buffPending
}

type buffPending struct {
	due  time.Time
	zone *Zone
	fn   func(z *Zone)
}

func buffFx(p *Player) *buffFxState {
	if p.Tmp == nil {
		p.Tmp = map[string]any{}
	}
	if v, ok := p.Tmp["buffs.fx"].(*buffFxState); ok {
		return v
	}
	v := &buffFxState{until: map[string]time.Time{}}
	p.Tmp["buffs.fx"] = v
	return v
}

func (f *buffFxState) active(kind string, now time.Time) bool { return now.Before(f.until[kind]) }

// Persistent timed-item state ("buffs"): expiries in unix ms; Seen lets
// joinHooks pause timers while offline (legacy saved remaining time, PlayerDAO.java:603).
type buffItemState struct {
	Until map[string]int64 `json:"until"`
	Seen  int64            `json:"seen"`
}

func buffItems(p *Player) *buffItemState {
	s := State[buffItemState](p, "buffs")
	if s.Until == nil {
		s.Until = map[string]int64{}
	}
	return s
}

func (s *buffItemState) on(key string, now time.Time) bool { return s.Until[key] > now.UnixMilli() }

// Charms are absolute expiries in unix ms, like Charms.td* in the legacy server.
type buffCharmState struct {
	Until map[string]int64 `json:"until"`
}

func buffCharms(p *Player) *buffCharmState {
	s := State[buffCharmState](p, "charms")
	if s.Until == nil {
		s.Until = map[string]int64{}
	}
	return s
}

func buffCharm(p *Player, key string) bool {
	return buffCharms(p).Until[key] > time.Now().UnixMilli()
}

// buffRefresh recomputes stats and pushes them to the client.
func buffRefresh(z *Zone, p *Player) {
	p.Recalc(z.World.Data)
	p.dirty = true
	p.Send("self", p.Self())
}

// buffEff tells the zone a player effect started (ms>0) or ended (ms=0).
func buffEff(z *Zone, p *Player, kind string, ms int64) {
	z.broadcast(Encode("eff", map[string]any{"id": p.ID(), "kind": kind, "ms": ms}), 0)
}

func buffMobEff(z *Zone, m *Mob, kind string, ms int64) {
	z.broadcast(Encode("eff", map[string]any{"mob": m.Idx, "kind": kind, "ms": ms}), 0)
}

func buffBossEff(z *Zone, b *Boss, kind string, ms int64) {
	z.broadcast(Encode("eff", map[string]any{"boss": b.Key, "kind": kind, "ms": ms}), 0)
}

// buffTimers sends the self buff panel: item buffs, charms and effects with time left.
func buffTimers(p *Player) {
	now := time.Now()
	type row struct {
		Key  string `json:"key"`
		Name string `json:"name"`
		Ms   int64  `json:"ms"`
		Kind string `json:"kind"`
	}
	list := []row{}
	for k, u := range buffItems(p).Until {
		if ms := u - now.UnixMilli(); ms > 0 {
			name := k
			if s := buffCfg.byKey[k]; s != nil {
				name = s.Name
			}
			list = append(list, row{k, name, ms, "item"})
		}
	}
	for k, u := range buffCharms(p).Until {
		if ms := u - now.UnixMilli(); ms > 0 {
			list = append(list, row{k, buffCharmNames[k], ms, "charm"})
		}
	}
	fx := buffFx(p)
	for k, u := range fx.until {
		if ms := u.Sub(now).Milliseconds(); ms > 0 {
			list = append(list, row{k, k, ms, "effect"})
		}
	}
	p.Send("buff_timers", map[string]any{"list": list})
}

var buffCharmNames = map[string]string{
	"tri_tue": "Bùa Trí Tuệ", "manh_me": "Bùa Mạnh Mẽ", "da_trau": "Bùa Da Trâu", "oai_hung": "Bùa Oai Hùng",
	"bat_tu": "Bùa Bất Tử", "deo_dai": "Bùa Dẻo Dai", "thu_hut": "Bùa Thu Hút", "de_tu": "Bùa Đệ Tử",
	"tri_tue3": "Bùa Trí Tuệ x3", "tri_tue4": "Bùa Trí Tuệ x4",
}

func init() {
	register("buff_timers", func(z *Zone, p *Player, _ json.RawMessage) { buffTimers(p) })
	enterHooks = append(enterHooks, func(z *Zone, p *Player) {
		buffTimers(p)
		buffSendIntrinsic(p)
	})
	// Pause item timers while offline (ItemTime counts only online time).
	joinHooks = append(joinHooks, func(w *World, p *Player) {
		s := buffItems(p)
		now := time.Now().UnixMilli()
		if s.Seen > 0 && now > s.Seen {
			off := now - s.Seen
			for k, u := range s.Until {
				if u > s.Seen {
					s.Until[k] = u + off
				} else {
					delete(s.Until, k)
				}
			}
		}
		s.Seen = now
		p.Recalc(w.Data)
	})
}
