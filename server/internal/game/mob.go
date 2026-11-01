package game

import (
	"math/rand/v2"
	"time"
)

const (
	mobRespawn      = 3 * time.Second
	mobRegenEvery   = 30 * time.Second
	mobAggroRange   = 300
	mobAggressRange = 100
	mobAggroAttack  = time.Second
	mobAggressCD    = 2 * time.Second
	mobAggroMemory  = 15 * time.Second
	trainingDummy   = 0 // mob template "Mộc nhân": never dies from one hit, capped damage
	firstAggressive = 19
)

// Mob is a live monster in one zone; owned by the zone goroutine.
type Mob struct {
	Idx      int
	T        *MobTemplate
	Level    int
	MaxHP    int64
	HP       int64
	X, Y     int
	diedAt   time.Time
	regenAt  time.Time
	attackAt time.Time
	aggro    map[int64]time.Time // player id -> last hit by
}

type MobView struct {
	Idx   int   `json:"i"`
	TID   int   `json:"tid"`
	Level int   `json:"lv"`
	HP    int64 `json:"hp"`
	MaxHP int64 `json:"max_hp"`
	X     int   `json:"x"`
	Y     int   `json:"y"`
}

func (m *Mob) Alive() bool { return m.HP > 0 }

func (m *Mob) View() MobView {
	return MobView{m.Idx, m.T.ID, m.Level, m.HP, m.MaxHP, m.X, m.Y}
}

func newMobs(d *Data, mt *MapTemplate) []*Mob {
	out := make([]*Mob, 0, len(mt.Mobs))
	for i, mm := range mt.Mobs {
		t := d.Mobs[mm.TemplateID]
		if t == nil {
			continue
		}
		hp := mm.HP
		if hp <= 0 {
			hp = t.HP
		}
		out = append(out, &Mob{Idx: i, T: t, Level: mm.Level, MaxHP: hp, HP: hp, X: mm.X, Y: mm.Y,
			aggro: map[int64]time.Time{}, regenAt: time.Now().Add(mobRegenEvery)})
	}
	return out
}

func (z *Zone) mobsView() []MobView {
	out := make([]MobView, 0, len(z.mobs))
	for _, m := range z.mobs {
		out = append(out, m.View())
	}
	return out
}

// updateMobs runs mob respawn, regen and attacks; called every tick.
func (z *Zone) updateMobs(now time.Time) {
	for _, m := range z.mobs {
		if !m.Alive() {
			if !z.instance.noRespawn && now.Sub(m.diedAt) >= mobRespawn {
				m.HP = m.MaxHP
				clear(m.aggro)
				z.broadcast(Encode("mob_spawn", m.View()), 0)
			}
			continue
		}
		if now.After(m.regenAt) {
			m.regenAt = now.Add(mobRegenEvery)
			if m.HP < m.MaxHP {
				m.HP = min(m.MaxHP, m.HP+m.MaxHP/10)
				z.broadcast(Encode("mob_hp", map[string]int64{"i": int64(m.Idx), "hp": m.HP}), 0)
			}
		}
		if now.Before(m.attackAt) || m.T.ID == trainingDummy {
			continue
		}
		if p, cd := z.mobTarget(m, now); p != nil {
			m.attackAt = now.Add(cd)
			z.mobAttack(m, p)
		}
	}
}

// mobTarget picks whom to attack: recent attackers first (aggro), otherwise
// aggressive mobs attack the nearest player in short range.
func (z *Zone) mobTarget(m *Mob, now time.Time) (*Player, time.Duration) {
	var best *Player
	bestD := 1 << 30
	for id, at := range m.aggro {
		p := z.players[id]
		if p == nil || p.Dead || now.Sub(at) > mobAggroMemory {
			delete(m.aggro, id)
			continue
		}
		if d := dist(m.X, m.Y, p.Char.X, p.Char.Y); d <= mobAggroRange && d < bestD {
			best, bestD = p, d
		}
	}
	if best != nil {
		return best, mobAggroAttack
	}
	if m.T.ID < firstAggressive {
		return nil, 0
	}
	for _, p := range z.players {
		if p.Dead {
			continue
		}
		if d := dist(m.X, m.Y, p.Char.X, p.Char.Y); d <= mobAggressRange && d < bestD {
			best, bestD = p, d
		}
	}
	return best, mobAggressCD
}

// mobDamage is MobPoint.getDameAttack: a percent of the mob's max HP ± level×10.
func mobDamage(m *Mob) int64 {
	dmg := m.MaxHP * m.T.PctDmg / 100
	if m.Level > 0 {
		dmg += int64(rand.IntN(m.Level*20+1) - m.Level*10)
	}
	return max(dmg, 1)
}

func (z *Zone) mobAttack(m *Mob, p *Player) {
	dmg := p.takeDamageFrom(mobDamage(m), 0, DamageSrc{Mob: m})
	z.broadcast(Encode("mob_atk", map[string]int64{"i": int64(m.Idx), "id": p.ID(), "dmg": dmg, "hp": p.Char.HP}), 0)
	if p.Char.HP <= 0 {
		z.playerDie(p, nil)
	}
}

func dist(x1, y1, x2, y2 int) int {
	dx, dy := x1-x2, y1-y2
	if dx < 0 {
		dx = -dx
	}
	if dy < 0 {
		dy = -dy
	}
	return max(dx, dy)
}
