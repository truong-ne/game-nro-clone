package game

import (
	"encoding/json"
	"log/slog"
	"strings"
	"sync"
	"sync/atomic"
	"time"
	"unicode/utf8"

	"github.com/truong-ne/game-nro-clone/server/internal/anim"
)

const (
	tickInterval  = 100 * time.Millisecond
	ticksPerSec   = int(time.Second / tickInterval)
	autosaveTicks = 600 // 60s
	maxSpeedPxSec = 600 // generous: walking/flying plus client jitter
	moveBurstPx   = 120
	chatCooldown  = 700 * time.Millisecond
	zoneCooldown  = 5 * time.Second
	maxChatRunes  = 100
)

// Zone is one channel of a map. All of its state is owned by its goroutine;
// everything else talks to it through Post.
type Zone struct {
	World   *World
	Map     *MapTemplate
	ID      int
	inbox   chan func()
	players map[int64]*Player
	count   atomic.Int32
	ticks   int
	moved   map[int64]*Player // players whose position changed this tick
	dead    atomic.Bool       // goroutine stopped (instance closed)
	deadMu  sync.Mutex

	petMoved []*Pet
	mobs     []*Mob
	ground   map[int]*GroundItem
	nextItem int
	bosses   []*Boss
	instance instanceInfo // set for dungeon instances
}

func newZone(w *World, m *MapTemplate, id int) *Zone {
	z := &Zone{World: w, Map: m, ID: id, inbox: make(chan func(), 1024),
		players: map[int64]*Player{}, moved: map[int64]*Player{}, ground: map[int]*GroundItem{}}
	if w.Data != nil && w.Data.Mobs != nil {
		z.mobs = newMobs(w.Data, m)
	}
	return z
}

// Post queues f on the zone goroutine. Once the zone has stopped (closed
// dungeon instance), f runs immediately: nothing else owns the zone then.
func (z *Zone) Post(f func()) {
	if z.dead.Load() {
		z.deadMu.Lock()
		defer z.deadMu.Unlock()
		z.safe(f)
		return
	}
	z.inbox <- f
}

// run processes commands; the ticker only runs while the zone has players,
// so the ~1,700 idle zones cost nothing.
func (z *Zone) run(stop <-chan struct{}) {
	var ticker *time.Ticker
	var tickC <-chan time.Time
	defer func() {
		if ticker != nil {
			ticker.Stop()
		}
	}()
	for {
		select {
		case f := <-z.inbox:
			z.safe(f)
		case <-tickC:
			z.safe(z.tick)
		case <-stop:
			z.deadMu.Lock()
			z.dead.Store(true)
			for len(z.inbox) > 0 { // run what was already queued
				z.safe(<-z.inbox)
			}
			z.deadMu.Unlock()
			return
		}
		switch {
		case len(z.players) > 0 && ticker == nil:
			ticker = time.NewTicker(tickInterval)
			tickC = ticker.C
		case len(z.players) == 0 && ticker != nil:
			ticker.Stop()
			ticker, tickC = nil, nil
		}
	}
}

func (z *Zone) tick() {
	z.ticks++
	now := time.Now()
	z.updateMobs(now)
	z.updateBosses(now)
	z.updatePets(now)
	z.expireGround(now)
	for _, h := range tickHooks {
		h(z, now)
	}
	z.flushMoves()
	if z.ticks%ticksPerSec == 0 {
		for _, p := range z.players {
			z.drainFlight(p)
		}
		z.regen(now)
		z.instanceTick(now)
		for _, h := range secondHooks {
			h(z, now)
		}
	}
	if z.ticks%autosaveTicks == 0 {
		for _, p := range z.players {
			if p.dirty {
				p.dirty = false
				z.World.saveAsync(p.Snapshot())
			}
		}
	}
	// Mob AI, regen and buffs plug in here (milestone 3).
}

// flushMoves sends all position changes of this tick as one message, instead
// of one message per move per viewer.
func (z *Zone) flushMoves() {
	if len(z.moved) == 0 && len(z.petMoved) == 0 {
		return
	}
	batch := make([][5]int64, 0, len(z.moved)+len(z.petMoved))
	for id, p := range z.moved {
		c := p.Char
		batch = append(batch, [5]int64{id, int64(c.X), int64(c.Y), int64(p.Dir), int64(p.Anim)})
		delete(z.moved, id)
	}
	for _, pt := range z.petMoved {
		batch = append(batch, [5]int64{pt.ID(), int64(pt.X), int64(pt.Y), int64(pt.Dir), int64(pt.Anim)})
	}
	z.petMoved = z.petMoved[:0]
	z.broadcast(Encode("moves", batch), 0)
}

// drainFlight charges KI for flying: 1% of max KI per second (min 1).
func (z *Zone) drainFlight(p *Player) {
	if p.Anim != anim.StateFly {
		return
	}
	cost := max(p.MaxMP/100, 1)
	p.Char.MP = max(p.Char.MP-cost, 0)
	p.dirty = true
	p.Send("mp", map[string]int64{"mp": p.Char.MP})
}

// safe keeps one bad message from killing the whole zone.
func (z *Zone) safe(f func()) {
	defer func() {
		if r := recover(); r != nil {
			slog.Error("zone panic", "map", z.Map.ID, "zone", z.ID, "panic", r)
		}
	}()
	f()
}

func (z *Zone) broadcast(msg []byte, except int64) {
	for id, p := range z.players {
		if id != except {
			p.SendRaw(msg)
		}
	}
}

// mapEnter carries only the map id and version; the client fetches the map
// itself from /api/map/{id}?v=... (HTTP-cached) instead of over the socket.
type mapEnter struct {
	MapID   int          `json:"map_id"`
	Version string       `json:"version"`
	Zone    int          `json:"zone"`
	Zones   int          `json:"zones"`
	X       int          `json:"x"`
	Y       int          `json:"y"`
	Players []View       `json:"players"`
	Mobs    []MobView    `json:"mobs"`
	Items   []GroundView `json:"items"`
	Bosses  []BossView   `json:"bosses"`
}

// add must run on the zone goroutine.
func (z *Zone) add(p *Player, x, y int) {
	c := p.Char
	c.MapID = z.Map.ID
	c.X, c.Y = clamp(x, 0, max(z.Map.PixelWidth()-1, 0)), y
	c.Y = z.World.Data.GroundY(z.Map, c.X, c.Y)
	p.zone.Store(z)
	p.lastMove = time.Now()
	others := make([]View, 0, len(z.players))
	for _, o := range z.players {
		others = append(others, o.View())
	}
	z.players[p.ID()] = p
	z.count.Add(1)
	p.Send("self", p.Self())
	p.Anim = anim.StateStand
	p.Send("map", mapEnter{z.Map.ID, z.World.MapVersion(z.Map.ID), z.ID, len(z.World.zones[z.Map.ID]), c.X, c.Y, others,
		z.mobsView(), z.itemsView(), z.bossesView()})
	for _, o := range z.players {
		if o != p && o.Pet != nil && o.Pet.Data.Status != PetHome {
			p.Send("join", o.Pet.View(z.World.Data))
		}
	}
	if p.Pet != nil {
		z.addPet(p)
	}
	z.World.Events.Emit(Event{Kind: EvEnterMap, Player: p, Zone: z, Map: z.Map.ID})
	for _, h := range enterHooks {
		h(z, p)
	}
	z.broadcast(Encode("join", p.View()), p.ID())
}

// remove must run on the zone goroutine.
func (z *Zone) remove(p *Player) {
	if _, ok := z.players[p.ID()]; !ok {
		return
	}
	delete(z.players, p.ID())
	delete(z.moved, p.ID())
	z.count.Add(-1)
	z.cancelTrade(p)
	z.removePet(p)
	z.broadcast(Encode("leave", map[string]int64{"id": p.ID()}), 0)
	z.World.Events.Emit(Event{Kind: EvLeaveMap, Player: p, Zone: z})
}

// handler processes one client command on the zone goroutine.
type handler func(z *Zone, p *Player, d json.RawMessage)

// handlers is the command registry; each feature file registers its commands
// in init(), replacing the legacy ~97-case Controller switch.
var handlers = map[string]handler{}

// deadAllowed lists commands a dead player may still send.
var deadAllowed = map[string]bool{"revive": true, "chat": true, "world_chat": true, "zone": false}

func register(name string, h handler) { handlers[name] = h }

func init() {
	register("move", func(z *Zone, p *Player, d json.RawMessage) {
		var m struct{ X, Y, D, A int }
		if json.Unmarshal(d, &m) == nil {
			z.move(p, m.X, m.Y, m.D, m.A)
		}
	})
	register("chat", func(z *Zone, p *Player, d json.RawMessage) {
		var m struct{ Text string }
		if json.Unmarshal(d, &m) == nil {
			z.chat(p, m.Text)
		}
	})
	register("waypoint", func(z *Zone, p *Player, _ json.RawMessage) { z.useWaypoint(p) })
	register("zone", func(z *Zone, p *Player, d json.RawMessage) {
		var m struct{ Zone int }
		if json.Unmarshal(d, &m) == nil {
			z.changeZone(p, m.Zone)
		}
	})
	register("skill", func(z *Zone, p *Player, d json.RawMessage) { z.useSkill(p, d) })
	register("revive", func(z *Zone, p *Player, d json.RawMessage) { z.revive(p, d) })
	register("add_point", func(z *Zone, p *Player, d json.RawMessage) { z.addPoint(p, d) })
}

// Handle dispatches one client command for a player in this zone.
func (z *Zone) Handle(p *Player, t string, d json.RawMessage) {
	if z.players[p.ID()] != p {
		return // player already left this zone; drop stale command
	}
	if p.Dead && !deadAllowed[t] {
		return
	}
	if h := handlers[t]; h != nil {
		h(z, p, d)
	}
}

// move trusts the client's position only within a speed budget, unlike the
// legacy server which accepted any coordinates.
func (z *Zone) move(p *Player, x, y, dir, state int) {
	c := p.Char
	if !canAct(p) { // stunned, asleep, held...
		if x != c.X || y != c.Y {
			p.Send("pos", map[string]int{"x": c.X, "y": c.Y})
		}
		return
	}
	if state < 0 || state >= anim.NumStates {
		state = anim.StateStand
	}
	if state == anim.StateFly && c.MP <= 0 {
		state = anim.StateFall
	}
	if dir >= 0 {
		dir = 1
	} else {
		dir = -1
	}
	if dir != p.Dir || state != p.Anim {
		p.Dir, p.Anim = dir, state
		z.moved[p.ID()] = p
	}
	now := time.Now()
	dt := now.Sub(p.lastMove).Seconds()
	budget := maxSpeedPxSec*dt + moveBurstPx
	x = clamp(x, 0, max(z.Map.PixelWidth()-1, 0))
	y = clamp(y, -200, z.Map.PixelHeight())
	dx, dy := float64(x-c.X), float64(y-c.Y)
	if dx*dx+dy*dy > budget*budget {
		p.Send("pos", map[string]int{"x": c.X, "y": c.Y})
		return
	}
	p.lastMove = now
	if x == c.X && y == c.Y {
		return
	}
	c.X, c.Y = x, y
	p.dirty = true
	z.moved[p.ID()] = p
	if c.TaskID == 0 && c.TaskIndex == 0 { // only the very first task listens to moves
		z.World.Events.Emit(Event{Kind: EvMove, Player: p, Zone: z})
	}
}

func (z *Zone) chat(p *Player, text string) {
	text = strings.TrimSpace(text)
	if p.IsAdmin && strings.HasPrefix(text, "/") {
		z.adminCommand(p, text) // not rate limited like chat
		return
	}
	if text == "" || time.Since(p.lastChat) < chatCooldown {
		return
	}
	if utf8.RuneCountInString(text) > maxChatRunes {
		text = string([]rune(text)[:maxChatRunes])
	}
	p.lastChat = time.Now()
	z.broadcast(Encode("chat", map[string]any{"id": p.ID(), "name": p.Char.Name, "text": text}), 0)
}

func (z *Zone) useWaypoint(p *Player) {
	c := p.Char
	for _, w := range z.Map.Waypoints {
		if w.Contains(c.X, c.Y) {
			if z.instanceWaypoint(p, w) {
				return
			}
			if z.World.Data.Map(w.GoMap) == nil {
				p.Send("notice", map[string]string{"text": "Khu vá»±c nÃ y chÆ°a má»Ÿ"})
				return
			}
			z.leaveTo(p, w.GoMap, -1, w.GoX, w.GoY)
			return
		}
	}
	p.Send("pos", map[string]int{"x": c.X, "y": c.Y})
}

func (z *Zone) changeZone(p *Player, id int) {
	zones := z.World.zones[z.Map.ID]
	if id == z.ID || id < 0 || id >= len(zones) {
		return
	}
	if !p.IsAdmin {
		if time.Since(p.lastZone) < zoneCooldown {
			p.Send("notice", map[string]string{"text": "Vui lÃ²ng Ä‘á»£i Ä‘á»ƒ Ä‘á»•i khu"})
			return
		}
		if int(zones[id].count.Load()) >= z.Map.MaxPlayer {
			p.Send("notice", map[string]string{"text": "Khu vá»±c Ä‘Ã£ Ä‘áº§y"})
			return
		}
	}
	p.lastZone = time.Now()
	z.leaveTo(p, z.Map.ID, id, p.Char.X, p.Char.Y)
}

// leaveTo moves a player to another map/zone. zoneID < 0 picks one.
func (z *Zone) leaveTo(p *Player, mapID, zoneID, x, y int) {
	z.remove(p)
	p.dirty = true
	z.World.Enter(p, mapID, zoneID, x, y)
}

// adminCommand handles exact-match admin commands (the legacy server matched
// by prefix, so ordinary chat could trigger them).
func (z *Zone) adminCommand(p *Player, text string) {
	f := strings.Fields(text)
	slog.Info("admin command", "player", p.Char.Name, "cmd", text)
	switch f[0] {
	case "/map":
		var id int
		if len(f) == 2 && parseInt(f[1], &id) && z.World.Data.Map(id) != nil {
			z.leaveTo(p, id, -1, SpawnX, 0)
		}
	case "/pos":
		p.Send("notice", map[string]string{"text": z.Map.Name + " " + itoa(p.Char.X) + "," + itoa(p.Char.Y)})
	case "/online":
		p.Send("notice", map[string]string{"text": "Online: " + itoa(z.World.Online())})
	case "/item":
		var id, qty int
		if len(f) >= 2 && parseInt(f[1], &id) {
			if len(f) < 3 || !parseInt(f[2], &qty) {
				qty = 1
			}
			if p.AddItem(z.World.Data, id, max(qty, 1), nil) {
				p.sendInventory()
			}
		}
	case "/gold", "/gem", "/power":
		var n int
		if len(f) == 2 && parseInt(f[1], &n) {
			switch f[0] {
			case "/gold":
				p.Char.Gold = min(p.Char.Gold+int64(n), maxGold)
			case "/gem":
				p.Char.Gem += int64(n)
			case "/power":
				p.Char.Power += int64(n)
				p.Char.Potential += int64(n)
			}
			p.dirty = true
			p.Send("self", p.Self())
		}
	case "/pet":
		if p.Char.Extra.Pet == nil {
			p.Char.Extra.Pet = NewPetData(z.World.Data, p.Char.Gender)
			p.loadPet()
			z.addPet(p)
			p.dirty = true
		}
	case "/boss":
		if len(f) == 2 {
			if t := z.World.bossTemplate(f[1]); t != nil {
				z.nextItem++ // also used as a unique suffix for GM-spawned bosses
				b := &Boss{T: t, Key: t.ID + "_gm" + itoa(z.nextItem), HP: t.HP}
				z.spawnBoss(b)
			}
		}
	case "/learn": // teach every skill of the race at level 1
		for i := range p.Char.Skills {
			p.Char.Skills[i].Level = max(p.Char.Skills[i].Level, 1)
		}
		p.Send("self", p.Self())
	default:
		p.Send("notice", map[string]string{"text": "Lá»‡nh: /map id, /pos, /online, /item id [sl], /gold n, /gem n, /power n, /pet, /boss id, /learn"})
	}
}
