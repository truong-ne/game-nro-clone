package game

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// World owns every map's zones and the set of online players.
type World struct {
	Data    *Data
	Store   *store.Store
	zones   map[int][]*Zone
	mapJSON map[int]json.RawMessage
	mapVer  map[int]string
	stop    chan struct{}
	saves   sync.WaitGroup

	Events    *Bus
	clans     clanRegistry
	bossSched *bossScheduler
	instances instanceRegistry

	mu     sync.Mutex
	online map[int64]*session // by account id
	byChar map[int64]*Player  // by character id
}

// eventExpMult is the multiplier from currently active scheduled events.
var eventExpMult atomic.Int64

func init() { eventExpMult.Store(1) }

type session struct {
	p    *Player
	gone chan struct{} // closed after the final save
	once sync.Once
}

func NewWorld(d *Data, st *store.Store) *World {
	w := &World{Data: d, Store: st, zones: map[int][]*Zone{}, mapJSON: map[int]json.RawMessage{},
		mapVer: map[int]string{}, stop: make(chan struct{}), online: map[int64]*session{},
		byChar: map[int64]*Player{}, Events: NewBus(),
		clans:     clanRegistry{clans: map[int64]*store.Clan{}, invites: map[int64]int64{}},
		instances: instanceRegistry{byID: map[string]*instance{}}}
	w.installTaskListeners()
	for id, m := range d.Maps {
		if m.Width == 0 {
			continue // no tile data, not enterable
		}
		m.Waypoints = nonNil(m.Waypoints)
		m.Mobs = nonNil(m.Mobs)
		m.Npcs = nonNil(m.Npcs)
		m.BgItems = nonNil(m.BgItems)
		w.mapJSON[id], _ = json.Marshal(m)
		sum := sha256.Sum256(w.mapJSON[id])
		w.mapVer[id] = hex.EncodeToString(sum[:6])
		n := max(m.Zones, 1)
		for i := range n {
			z := newZone(w, m, i)
			w.zones[id] = append(w.zones[id], z)
			go z.run(w.stop)
		}
	}
	return w
}

// Start launches world-level loops (boss scheduler, timed events, instance cleanup).
func (w *World) Start() {
	go w.runBosses(w.stop)
	go w.runSchedules(w.stop)
	go w.runPersist(w.stop)
	for _, h := range worldStartHooks {
		h(w)
	}
}

// Stop returns a channel closed on shutdown (for feature goroutines).
func (w *World) Done() <-chan struct{} { return w.stop }

// EventSpec is a scheduled event (content.json "events").
type EventSpec struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Weekdays []int  `json:"weekdays"` // 0 Sunday .. 6 Saturday
	ExpMult  int64  `json:"exp_mult"`
}

func (w *World) runSchedules(stop <-chan struct{}) {
	tick := time.NewTicker(30 * time.Second)
	defer tick.Stop()
	active := map[string]bool{}
	for {
		mult := int64(1)
		now := time.Now()
		for _, e := range w.Data.Content.Events {
			on := containsInt(e.Weekdays, int(now.Weekday()))
			if on {
				mult *= max(e.ExpMult, 1)
			}
			if on != active[e.ID] {
				active[e.ID] = on
				state := map[bool]string{true: "bắt đầu", false: "kết thúc"}[on]
				w.Broadcast(Encode("notice", map[string]string{"text": "Sự kiện " + e.Name + " " + state}))
			}
		}
		eventExpMult.Store(mult)
		w.instances.mu.Lock()
		var expired []*instance
		for _, in := range w.instances.byID {
			if now.After(in.expires.Add(time.Minute)) {
				expired = append(expired, in)
			}
		}
		w.instances.mu.Unlock()
		for _, in := range expired {
			for _, z := range in.zones {
				z.Post(func() { z.instanceTick(time.Now()) })
			}
			w.closeInstance(in)
		}
		select {
		case <-stop:
			return
		case <-tick.C:
		}
	}
}

// Broadcast sends a message to every online player.
func (w *World) Broadcast(msg []byte) {
	w.mu.Lock()
	ps := make([]*Player, 0, len(w.byChar))
	for _, p := range w.byChar {
		ps = append(ps, p)
	}
	w.mu.Unlock()
	for _, p := range ps {
		p.SendRaw(msg)
	}
}

// ActiveZones counts zones that currently have players.
func (w *World) ActiveZones() int {
	n := 0
	for _, zs := range w.zones {
		for _, z := range zs {
			if z.count.Load() > 0 {
				n++
			}
		}
	}
	return n
}

func (w *World) playerByName(name string) *Player {
	w.mu.Lock()
	defer w.mu.Unlock()
	for _, p := range w.byChar {
		if strings.EqualFold(p.Char.Name, name) {
			return p
		}
	}
	return nil
}

// KickByName disconnects an online character (admin API).
func (w *World) KickByName(name string) bool {
	p := w.playerByName(name)
	if p != nil {
		p.Send("notice", map[string]string{"text": "Bạn bị ngắt kết nối bởi quản trị viên"})
		p.Kick("admin kick")
	}
	return p != nil
}

// GiveByName adds items to an online character's bag (admin API).
func (w *World) GiveByName(name string, item, qty int) bool {
	p := w.playerByName(name)
	if p == nil || w.Data.ItemsFull[item] == nil {
		return false
	}
	w.postToPlayer(p, func(*Zone) {
		if p.AddItem(w.Data, item, qty, nil) {
			p.Send("notice", map[string]string{"text": "Bạn nhận được quà từ quản trị viên"})
			p.sendInventory()
		}
	})
	return true
}

func (w *World) SpawnBossByID(id string) error {
	t := w.bossTemplate(id)
	if t == nil || !w.SpawnWorldBoss(t) {
		return fmt.Errorf("unknown boss %q", id)
	}
	return nil
}

// EnterZone queues p into a specific zone (dungeon instances).
func (w *World) EnterZone(p *Player, z *Zone, x, y int) {
	z.Post(func() {
		if w.isGone(p) {
			w.finalSave(p)
			return
		}
		z.add(p, x, y)
	})
}

// MapJSON returns the pre-rendered map template JSON, or nil if not enterable.
func (w *World) MapJSON(id int) []byte { return w.mapJSON[id] }

// MapVersion is a short content hash used to cache-bust /api/map.
func (w *World) MapVersion(id int) string { return w.mapVer[id] }

func (w *World) Online() int {
	w.mu.Lock()
	defer w.mu.Unlock()
	return len(w.online)
}

// pickZone returns the requested zone if valid, else the first non-full one,
// else the least loaded (the legacy server looped forever when all were full).
func (w *World) pickZone(mapID, zoneID int) *Zone {
	zones := w.zones[mapID]
	if len(zones) == 0 {
		return nil
	}
	if zoneID >= 0 && zoneID < len(zones) {
		return zones[zoneID]
	}
	best := zones[0]
	for _, z := range zones {
		if int(z.count.Load()) < z.Map.MaxPlayer {
			return z
		}
		if z.count.Load() < best.count.Load() {
			best = z
		}
	}
	return best
}

// Enter queues p into a zone of mapID; unknown maps send the player home.
func (w *World) Enter(p *Player, mapID, zoneID, x, y int) {
	z := w.pickZone(mapID, zoneID)
	if z == nil {
		z = w.pickZone(HomeMap(p.Char.Gender), -1)
		x, y = 300, 336
	}
	z.Post(func() {
		if w.isGone(p) {
			w.finalSave(p)
			return
		}
		z.add(p, x, y)
	})
}

// postToPlayer runs f on the goroutine of the zone p is in when f executes,
// following the player if they changed zones while the post was queued.
func (w *World) postToPlayer(p *Player, f func(z *Zone)) {
	z := p.Zone()
	if z == nil {
		return
	}
	if p.left.Load() {
		// Logged out: no zone owns the player any more, but its last zone's
		// goroutine still serializes this with the final save.
		z.Post(func() { f(z) })
		return
	}
	z.Post(func() {
		switch {
		case z.players[p.ID()] == p, p.left.Load():
			f(z)
		case p.Zone() != z:
			w.postToPlayer(p, f) // moved zones: follow
		default:
			// Between zones (removed here, not yet added elsewhere): retry shortly.
			time.AfterFunc(50*time.Millisecond, func() { w.postToPlayer(p, f) })
		}
	})
}

// TakeOver kicks any session already playing accountID and waits until its
// state is saved, so the new login never loads stale data.
func (w *World) TakeOver(accountID int64) {
	w.mu.Lock()
	s := w.online[accountID]
	w.mu.Unlock()
	if s == nil {
		return
	}
	s.p.Send("notice", map[string]string{"text": "Tài khoản đăng nhập ở nơi khác"})
	s.p.Kick("logged in elsewhere")
	select {
	case <-s.gone:
	case <-time.After(10 * time.Second):
		slog.Warn("timed out waiting for previous session", "account", accountID)
	}
}

// Join registers p as online and places it in the world.
func (w *World) Join(p *Player) {
	p.loadPet()
	for _, h := range joinHooks {
		h(w, p)
	}
	if p.Char.ClanID != 0 && w.clan(p.Char.ClanID) == nil {
		p.Char.ClanID = 0 // clan was disbanded while offline
	}
	w.mu.Lock()
	w.online[p.Char.AccountID] = &session{p: p, gone: make(chan struct{})}
	w.byChar[p.ID()] = p
	w.mu.Unlock()
	if p.Char.HP <= 0 { // logged out dead: wake up at home
		p.Char.HP, p.Char.MapID, p.Char.X, p.Char.Y = p.MaxHP, HomeMap(p.Char.Gender), 300, 336
	}
	w.Enter(p, p.Char.MapID, -1, p.Char.X, p.Char.Y)
}

// Leave is called once when the connection closes.
// If p is mid-transfer between zones, Enter sees p.left and saves instead.
func (w *World) Leave(p *Player) {
	p.left.Store(true)
	if z := p.Zone(); z != nil {
		z.Post(func() {
			if z.players[p.ID()] == p {
				z.remove(p)
				w.finalSave(p)
			}
		})
	}
}

func (w *World) isGone(p *Player) bool { return p.left.Load() }

func (w *World) finalSave(p *Player) {
	w.mu.Lock()
	s := w.online[p.Char.AccountID]
	w.mu.Unlock()
	if s == nil || s.p != p {
		return
	}
	if p.pending.Load() > 0 {
		return // an async callback still has to apply; it calls finalSave when done
	}
	s.once.Do(func() {
		snap := p.Snapshot()
		w.saves.Add(1)
		go func() {
			defer w.saves.Done()
			w.save(snap)
			w.mu.Lock()
			if w.online[snap.AccountID] == s {
				delete(w.online, snap.AccountID)
			}
			if w.byChar[snap.ID] == p {
				delete(w.byChar, snap.ID)
			}
			w.mu.Unlock()
			close(s.gone)
		}()
	})
}

func (w *World) saveAsync(c *store.Character) {
	w.saves.Add(1)
	go func() {
		defer w.saves.Done()
		w.save(c)
	}()
}

func (w *World) save(c *store.Character) {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := w.Store.SaveCharacter(ctx, c); err != nil {
		slog.Error("save character", "id", c.ID, "err", err)
	}
}

// Shutdown kicks everyone, waits for their saves and stops the zones.
func (w *World) Shutdown() {
	w.mu.Lock()
	var ps []*Player
	for _, s := range w.online {
		ps = append(ps, s.p)
	}
	w.mu.Unlock()
	for _, p := range ps {
		p.Send("notice", map[string]string{"text": "Máy chủ đang bảo trì"})
		p.Kick("shutdown")
	}
	deadline := time.Now().Add(15 * time.Second)
	for w.Online() > 0 && time.Now().Before(deadline) {
		time.Sleep(100 * time.Millisecond)
	}
	w.saves.Wait()
	w.saveServerState(map[string][]byte{})
	close(w.stop)
}

func clamp(v, lo, hi int) int { return max(lo, min(v, hi)) }

// nonNil makes empty slices encode as [] instead of null.
func nonNil[T any](s []T) []T {
	if s == nil {
		return []T{}
	}
	return s
}

func itoa(n int) string { return strconv.Itoa(n) }

func parseInt(s string, out *int) bool {
	n, err := strconv.Atoi(s)
	*out = n
	return err == nil
}
