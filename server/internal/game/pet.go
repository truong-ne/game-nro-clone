package game

import (
	"encoding/json"
	"math/rand/v2"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/anim"
	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Pet statuses (PetService: đi theo / bảo vệ / tấn công / về nhà).
const (
	PetFollow = iota
	PetProtect
	PetAttack
	PetHome
)

const petSpeed = 220.0 // px/s

// Pet is the disciple following its owner; it lives in the owner's zone.
type Pet struct {
	Owner    *Player
	Data     *store.PetData
	X, Y     int
	HP       int64
	MaxHP    int64
	Dmg      int64
	Dir      int
	Anim     int
	attackAt time.Time
	target   *Mob
}

func (pt *Pet) ID() int64 { return -pt.Owner.ID() } // negative ids are pets

func (pt *Pet) View(d *Data) View {
	parts := d.Content.Pets.Parts
	var pp [3]int
	if pt.Data.Gender < len(parts) {
		pp = parts[pt.Data.Gender]
	}
	return View{ID: pt.ID(), Name: pt.Data.Name, Gender: pt.Data.Gender, Head: pp[0], Body: pp[1], Leg: pp[2],
		X: pt.X, Y: pt.Y, HP: pt.HP, MaxHP: pt.MaxHP, Dir: pt.Dir, Anim: pt.Anim}
}

// NewPetData is what an owner receives (admin /pet, dragon wish, eggs...).
func NewPetData(d *Data, gender int) *store.PetData {
	b := d.Content.Pets.Base
	return &store.PetData{Name: d.Content.Pets.Name, Gender: gender, Power: 2000, HPBase: b.HP, MPBase: b.MP, DmgBase: b.Dmg}
}

func (p *Player) loadPet() {
	if p.Char.Extra.Pet == nil {
		p.Pet = nil
		return
	}
	pd := p.Char.Extra.Pet
	p.Pet = &Pet{Owner: p, Data: pd, X: p.Char.X, Y: p.Char.Y, HP: pd.HPBase, MaxHP: pd.HPBase, Dmg: pd.DmgBase, Dir: 1}
}

func (z *Zone) addPet(p *Player) {
	pt := p.Pet
	if pt.Data.Status == PetHome {
		return
	}
	pt.X, pt.Y = p.Char.X-30, p.Char.Y
	z.broadcast(Encode("join", pt.View(z.World.Data)), 0)
}

func (z *Zone) removePet(p *Player) {
	if p.Pet != nil {
		z.broadcast(Encode("leave", map[string]int64{"id": p.Pet.ID()}), 0)
	}
}

func init() {
	register("pet", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ Status int }
		if p.Pet == nil || json.Unmarshal(d, &r) != nil || r.Status < PetFollow || r.Status > PetHome {
			return
		}
		was := p.Pet.Data.Status
		p.Pet.Data.Status = r.Status
		p.dirty = true
		switch {
		case r.Status == PetHome && was != PetHome:
			z.removePet(p)
		case r.Status != PetHome && was == PetHome:
			z.addPet(p)
		}
		p.Send("pet_info", p.Pet.View(z.World.Data))
	})
}

// updatePets moves pets toward their owner or target and attacks mobs.
func (z *Zone) updatePets(now time.Time) {
	dt := tickInterval.Seconds()
	for _, p := range z.players {
		pt := p.Pet
		if pt == nil || pt.Data.Status == PetHome || p.Dead {
			continue
		}
		cfg := z.World.Data.Content.Pets
		tx, ty := p.Char.X-p.Dir*cfg.FollowDistance/2, p.Char.Y
		if pt.Data.Status != PetFollow {
			if pt.target == nil || !pt.target.Alive() {
				pt.target = z.petPickTarget(p, cfg.AttackRange)
			}
			if t := pt.target; t != nil {
				tx, ty = t.X-pt.Dir*20, t.Y
				if dist(pt.X, pt.Y, t.X, t.Y) <= 40 && now.After(pt.attackAt) {
					pt.attackAt = now.Add(time.Duration(cfg.AttackEveryMs) * time.Millisecond)
					z.petAttack(pt, t)
				}
			}
		}
		ox, oy := pt.X, pt.Y
		step := int(petSpeed * dt)
		if abs(tx-pt.X) > 4 {
			pt.X += clamp(tx-pt.X, -step, step)
			pt.Dir = sign(tx - ox)
		}
		pt.Y += clamp(ty-pt.Y, -step, step)
		state := anim.StateStand
		if pt.X != ox {
			state = anim.StateRun
		}
		if pt.X != ox || pt.Y != oy || pt.Anim != state {
			pt.Anim = state
			z.petMoved = append(z.petMoved, pt)
		}
	}
}

// petPickTarget: protect only fights mobs that hit the owner; attack picks the
// nearest live mob around the owner.
func (z *Zone) petPickTarget(p *Player, rng int) *Mob {
	var best *Mob
	bestD := rng + 1
	for _, m := range z.mobs {
		if !m.Alive() {
			continue
		}
		if p.Pet.Data.Status == PetProtect {
			if _, hit := m.aggro[p.ID()]; !hit {
				continue
			}
		}
		if d := dist(m.X, m.Y, p.Char.X, p.Char.Y); d < bestD {
			best, bestD = m, d
		}
	}
	return best
}

func (z *Zone) petAttack(pt *Pet, m *Mob) {
	dmg := pt.Dmg + pt.Dmg*int64(rand.IntN(11)-5)/100
	owner := pt.Owner
	before := m.HP
	z.hitMob(owner, m, hitResult{Dmg: max(dmg, 1)})
	pt.Data.Power += max(before-m.HP, 1) // the disciple grows with what it deals
	pt.Anim = anim.StateAttack
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

func sign(v int) int {
	if v < 0 {
		return -1
	}
	return 1
}
