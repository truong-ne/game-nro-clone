package game

import (
	"encoding/json"
	"strconv"
	"sync"
	"time"
)

// DungeonSpec is a timed instanced map chain (content.json "dungeons").
type DungeonSpec struct {
	Name       string        `json:"name"`
	Maps       []int         `json:"maps"`
	Minutes    int           `json:"minutes"`
	MobHPMult  int64         `json:"mob_hp_mult"`
	Boss       *BossTemplate `json:"boss"`
	MinMembers int           `json:"min_members"`
}

// instanceInfo marks a zone as part of a dungeon instance.
type instanceInfo struct {
	id        string
	inst      *instance
	noRespawn bool
}

// instance is one running copy of a dungeon: private zones per map that are
// torn down when time runs out (legacy phó bản such as Doanh trại Độc Nhãn).
type instance struct {
	key     string
	spec    *DungeonSpec
	zones   map[int]*Zone // map id -> zone
	expires time.Time
	stop    chan struct{}
	once    sync.Once
}

type instanceRegistry struct {
	mu   sync.Mutex
	byID map[string]*instance // owner key (clan:<id> or player:<id>) -> instance
}

func (w *World) newInstance(key string, spec *DungeonSpec) *instance {
	in := &instance{key: key, spec: spec, zones: map[int]*Zone{}, stop: make(chan struct{}),
		expires: time.Now().Add(time.Duration(spec.Minutes) * time.Minute)}
	for i, mid := range spec.Maps {
		m := w.Data.Map(mid)
		if m == nil || m.Width == 0 {
			continue
		}
		z := newZone(w, m, 0)
		z.instance = instanceInfo{id: key, inst: in, noRespawn: true}
		for _, mob := range z.mobs {
			mob.MaxHP *= max(spec.MobHPMult, 1)
			mob.HP = mob.MaxHP
		}
		if i == len(spec.Maps)-1 && spec.Boss != nil {
			bt := spec.Boss
			z.bosses = nil
			b := &Boss{T: bt, Key: bt.ID, HP: bt.HP}
			z.Post(func() { z.spawnBoss(b) })
		}
		in.zones[mid] = z
		go z.run(in.stop)
	}
	return in
}

func (z *Zone) openDungeon(p *Player, id string) {
	w := z.World
	spec := w.Data.Content.Dungeons[id]
	if spec == nil || len(spec.Maps) == 0 {
		return
	}
	key := "player:" + strconv.FormatInt(p.ID(), 10)
	if p.Char.ClanID != 0 {
		key = "clan:" + strconv.FormatInt(p.Char.ClanID, 10)
	}
	key = id + "/" + key
	w.instances.mu.Lock()
	in := w.instances.byID[key]
	if in == nil {
		in = w.newInstance(key, spec)
		w.instances.byID[key] = in
	}
	w.instances.mu.Unlock()
	first := in.zones[spec.Maps[0]]
	if first == nil {
		notice(p, "Phó bản chưa mở")
		return
	}
	left := time.Until(in.expires).Round(time.Minute)
	notice(p, "Vào "+spec.Name+", còn "+left.String())
	z.remove(p)
	p.dirty = true
	w.EnterZone(p, first, SpawnX, 0)
}

// instanceWaypoint keeps players inside the instance when the target map is
// part of it. Returns false to fall back to the normal world waypoint.
func (z *Zone) instanceWaypoint(p *Player, w Waypoint) bool {
	in := z.instance.inst
	if in == nil {
		return false
	}
	if t := in.zones[w.GoMap]; t != nil {
		z.remove(p)
		z.World.EnterZone(p, t, w.GoX, w.GoY)
		return true
	}
	return false
}

// instanceTick (once a second, per zone) expels everyone when time is up.
func (z *Zone) instanceTick(now time.Time) {
	in := z.instance.inst
	if in == nil || now.Before(in.expires) {
		return
	}
	for _, p := range z.players {
		notice(p, "Đã hết thời gian phó bản")
		z.leaveTo(p, HomeMap(p.Char.Gender), -1, 300, 336)
	}
	z.World.closeInstance(in)
}

func (w *World) closeInstance(in *instance) {
	in.once.Do(func() {
		w.instances.mu.Lock()
		delete(w.instances.byID, in.key)
		w.instances.mu.Unlock()
		// Let each zone flush its expel posts before stopping.
		time.AfterFunc(2*time.Second, func() { close(in.stop) })
	})
}

// --- PvP: duels (matches/PVP) ---

func init() {
	register("duel_req", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ ID int64 }
		if json.Unmarshal(d, &r) != nil {
			return
		}
		t := z.players[r.ID]
		if t == nil || t == p || t.duel != 0 || p.duel != 0 {
			return
		}
		p.duelInvite = t.ID()
		t.Send("duel_invite", map[string]any{"id": p.ID(), "name": p.Char.Name})
	})
	register("duel_accept", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ ID int64 }
		if json.Unmarshal(d, &r) != nil {
			return
		}
		a := z.players[r.ID]
		if a == nil || a.duelInvite != p.ID() || a.duel != 0 || p.duel != 0 {
			return
		}
		a.duelInvite = 0
		a.duel, p.duel = p.ID(), a.ID()
		msg := Encode("duel_start", map[string]int64{"a": a.ID(), "b": p.ID()})
		a.SendRaw(msg)
		p.SendRaw(msg)
	})
}

func (z *Zone) canPvP(a, b *Player) bool {
	if a.duel == b.ID() && b.duel == a.ID() {
		return true
	}
	return containsInt(z.World.Data.Content.PvpMaps, z.Map.ID)
}

// endDuel is called when loser dies; the duel ends without a real death penalty.
func (z *Zone) endDuel(loser, winner *Player) {
	if loser.duel != winner.ID() {
		return
	}
	loser.duel, winner.duel = 0, 0
	z.broadcast(Encode("duel_end", map[string]int64{"winner": winner.ID(), "loser": loser.ID()}), 0)
}
