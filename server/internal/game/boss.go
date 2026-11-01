package game

import (
	"encoding/json"
	"math/rand/v2"
	"sync"
	"time"
)

const (
	bossSight     = 400
	bossReach     = 70
	bossAttackCD  = 1500 * time.Millisecond
	bossLeashTime = 10 * time.Minute // leaves if nobody kills it
)

// BossTemplate is one configurable boss (content.json "bosses").
type BossTemplate struct {
	ID   string `json:"id"`
	Name string `json:"name"`
	Mob  int    `json:"mob"` // mob template whose sprite the boss uses
	// Parts [head, body, leg] draw the boss as a character (legacy outfits);
	// when empty the mob sprite is used.
	Parts     []int    `json:"parts,omitempty"`
	HP        int64    `json:"hp"`
	Dmg       int64    `json:"dmg"`
	Def       int64    `json:"def"`
	Maps      []int    `json:"maps"`
	RespawnS  int      `json:"respawn_s"`
	RestS     int      `json:"rest_s"`
	Speed     int      `json:"speed"`
	ChatStart string   `json:"chat_start"`
	ChatDie   string   `json:"chat_die"`
	Rewards   []Reward `json:"rewards"`
}

func (t *BossTemplate) normalize() {
	t.Speed = max(t.Speed, 40)
	t.RespawnS = max(t.RespawnS, 30)
}

// Boss is a live boss in one zone, owned by that zone's goroutine.
type Boss struct {
	T        *BossTemplate
	Key      string
	HP       int64
	X, Y     int
	Dir      int
	spawned  time.Time
	attackAt time.Time
	onDeath  func(killer *Player) // notifies the scheduler or dungeon
}

type BossView struct {
	Key   string `json:"key"`
	Name  string `json:"name"`
	Mob   int    `json:"mob"`
	HP    int64  `json:"hp"`
	MaxHP int64  `json:"max_hp"`
	X     int    `json:"x"`
	Y     int    `json:"y"`
	Parts []int  `json:"parts,omitempty"`
}

func (b *Boss) View() BossView {
	return BossView{b.Key, b.T.Name, b.T.Mob, b.HP, b.T.HP, b.X, b.Y, b.T.Parts}
}

func (z *Zone) bossesView() []BossView {
	out := make([]BossView, 0, len(z.bosses))
	for _, b := range z.bosses {
		out = append(out, b.View())
	}
	return out
}

func (z *Zone) spawnBoss(b *Boss) {
	b.X = rand.IntN(max(z.Map.PixelWidth()-200, 1)) + 100
	b.Y = z.World.Data.GroundY(z.Map, b.X, 0)
	b.spawned = time.Now()
	b.Dir = 1
	z.bosses = append(z.bosses, b)
	z.broadcast(Encode("boss_spawn", b.View()), 0)
	if b.T.ChatStart != "" {
		z.broadcast(Encode("boss_chat", map[string]string{"key": b.Key, "text": b.T.ChatStart}), 0)
	}
}

func (z *Zone) removeBoss(b *Boss) {
	for i, x := range z.bosses {
		if x == b {
			z.bosses = append(z.bosses[:i], z.bosses[i+1:]...)
			break
		}
	}
}

// updateBosses: chase the nearest living player in sight and hit them.
func (z *Zone) updateBosses(now time.Time) {
	for _, b := range z.bosses {
		if z.instance.id == "" && now.Sub(b.spawned) > bossLeashTime {
			z.removeBoss(b)
			z.broadcast(Encode("boss_leave", map[string]string{"key": b.Key}), 0)
			if b.onDeath != nil {
				b.onDeath(nil)
			}
			return // slice changed; continue next tick
		}
		var target *Player
		best := bossSight + 1
		for _, p := range z.players {
			if d := dist(b.X, b.Y, p.Char.X, p.Char.Y); !p.Dead && d < best {
				target, best = p, d
			}
		}
		if target == nil {
			continue
		}
		if best > bossReach {
			step := int(float64(b.T.Speed) * tickInterval.Seconds())
			b.X += clamp(target.Char.X-b.X, -step, step)
			b.Y += clamp(target.Char.Y-b.Y, -step, step)
			b.Dir = sign(target.Char.X - b.X)
			z.broadcast(Encode("boss_move", map[string]any{"key": b.Key, "x": b.X, "y": b.Y, "dir": b.Dir}), 0)
			continue
		}
		if now.After(b.attackAt) {
			b.attackAt = now.Add(bossAttackCD)
			dmg := target.takeDamageFrom(b.T.Dmg+b.T.Dmg*int64(rand.IntN(21)-10)/100, 0, DamageSrc{Boss: b})
			z.broadcast(Encode("boss_atk", map[string]any{"key": b.Key, "id": target.ID(), "dmg": dmg, "hp": target.Char.HP}), 0)
			if target.Char.HP <= 0 {
				z.playerDie(target, nil)
			}
		}
	}
}

// hitBoss: bosses ignore armor/dodge but subtract flat defense (Boss.injured).
func (z *Zone) hitBoss(p *Player, b *Boss, h hitResult) {
	dmg := max(h.Dmg-b.T.Def, 1)
	b.HP -= dmg
	p.gainPower(dmg / 10)
	z.broadcast(Encode("boss_hit", map[string]any{"key": b.Key, "hp": max(b.HP, 0), "dmg": dmg, "crit": h.Crit, "by": p.ID()}), 0)
	if b.HP > 0 {
		return
	}
	z.removeBoss(b)
	if b.T.ChatDie != "" {
		z.broadcast(Encode("boss_chat", map[string]string{"key": b.Key, "text": b.T.ChatDie}), 0)
	}
	z.broadcast(Encode("boss_die", map[string]any{"key": b.Key, "by": p.ID()}), 0)
	for _, r := range b.T.Rewards {
		if rand.Float64()*100 < r.Chance {
			qty := r.Min
			if r.Max > r.Min {
				qty += rand.IntN(r.Max - r.Min + 1)
			}
			z.dropItem(r.Item, max(qty, 1), nil, b.X, b.Y, p.ID())
		}
	}
	z.World.Events.Emit(Event{Kind: EvKillBoss, Player: p, Zone: z, Boss: b.T.ID, BossRef: b})
	z.World.Broadcast(Encode("notice", map[string]string{"text": p.Char.Name + " đã tiêu diệt " + b.T.Name + "!"}))
	if b.onDeath != nil {
		b.onDeath(p)
	}
}

func (z *Zone) bossByKey(key string) *Boss {
	for _, b := range z.bosses {
		if b.Key == key {
			return b
		}
	}
	return nil
}

// bossScheduler spawns world bosses on random configured maps and respawns
// them after death (replaces the ~15 per-family BossManager threads).
type bossScheduler struct {
	mu    sync.Mutex
	next  map[string]time.Time // template id -> next spawn; zero while alive
	alive map[string]bool
}

func (w *World) runBosses(stop <-chan struct{}) {
	s := &bossScheduler{next: map[string]time.Time{}, alive: map[string]bool{}}
	w.bossSched = s
	for _, t := range w.Data.Content.Bosses {
		s.next[t.ID] = time.Now().Add(time.Duration(t.RestS) * time.Second)
	}
	tick := time.NewTicker(time.Second)
	defer tick.Stop()
	for {
		select {
		case <-stop:
			return
		case now := <-tick.C:
			for _, t := range w.Data.Content.Bosses {
				s.mu.Lock()
				due := !s.alive[t.ID] && now.After(s.next[t.ID])
				if due {
					s.alive[t.ID] = true
				}
				s.mu.Unlock()
				if due {
					w.SpawnWorldBoss(t)
				}
			}
		}
	}
}

// SpawnWorldBoss places a boss on one of its maps (also used by /boss).
func (w *World) SpawnWorldBoss(t *BossTemplate) bool {
	var candidates []*Zone
	for _, m := range t.Maps {
		candidates = append(candidates, w.zones[m]...)
	}
	if len(candidates) == 0 {
		return false
	}
	z := candidates[rand.IntN(len(candidates))]
	b := &Boss{T: t, Key: t.ID, HP: t.HP}
	b.onDeath = func(*Player) {
		if s := w.bossSched; s != nil {
			s.mu.Lock()
			s.alive[t.ID] = false
			s.next[t.ID] = time.Now().Add(time.Duration(t.RespawnS) * time.Second)
			s.mu.Unlock()
		}
	}
	z.Post(func() { z.spawnBoss(b) })
	w.Broadcast(Encode("notice", map[string]string{"text": "BOSS " + t.Name + " vừa xuất hiện tại " + z.Map.Name + " khu " + itoa(z.ID)}))
	return true
}

func (w *World) bossTemplate(id string) *BossTemplate {
	for _, t := range w.Data.Content.Bosses {
		if t.ID == id {
			return t
		}
	}
	return nil
}

func init() {
	register("boss_list", func(z *Zone, p *Player, _ json.RawMessage) {
		var out []string
		for _, t := range z.World.Data.Content.Bosses {
			out = append(out, t.Name)
		}
		p.Send("boss_list", out)
	})
}
