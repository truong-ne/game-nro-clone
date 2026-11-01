package game

import (
	"encoding/json"
	"math/rand/v2"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/anim"
)

const (
	meleeRange     = 100
	rangeSlack     = 40
	reviveGemCost  = 1
	regenEvery     = 1 // seconds
	combatRegenOff = 5 * time.Second
	reviveImmunity = 1500 * time.Millisecond
)

// ExpRate multiplies power/potential gains (server.expserver in the legacy config).
var ExpRate int64 = 1

type hitResult struct {
	Dmg  int64
	Crit bool
	Miss bool
}

// attackDamage is NPoint.getDameAttack, trimmed to the stats ported so far:
// damage × skill%, crit ×2, ±5% roll.
func (p *Player) attackDamage(lv *SkillLevel) hitResult {
	dmg := p.Dmg * lv.Damage / 100
	r := hitResult{}
	if rand.IntN(100) < p.Crit {
		dmg *= 2
		r.Crit = true
	}
	dmg += dmg * int64(rand.IntN(11)-5) / 100
	r.Dmg = max(dmg, 1)
	return r
}

// attack is attackDamage plus outgoing damage hooks (buffs, intrinsics...).
func (p *Player) attack(a *AttackCtx) hitResult {
	h := p.attackDamage(a.Level)
	for _, f := range outDamageHooks {
		h.Dmg = max(f(p, h.Dmg, a), 0)
	}
	return h
}

// takeDamage applies dodge, armor and flat defense (Player.injured) and
// returns the HP actually lost. pierce is the attacker's armor pierce %.
func (p *Player) takeDamage(dmg int64, pierce int) int64 {
	return p.takeDamageFrom(dmg, pierce, DamageSrc{})
}

// takeDamageFrom is takeDamage with the damage source passed to defensive hooks.
func (p *Player) takeDamageFrom(dmg int64, pierce int, src DamageSrc) int64 {
	if p.Dead || time.Since(p.lastHit) < 0 {
		return 0
	}
	if rand.IntN(100) < p.Mods.Dodge {
		return 0
	}
	armor := max(p.Mods.Armor-pierce, 0)
	dmg -= dmg * int64(armor) / 100
	dmg = max(dmg-p.Def, 1)
	for _, f := range inDamageHooks {
		dmg = max(f(p, dmg, src), 0)
	}
	dmg = min(dmg, p.Char.HP)
	p.Char.HP -= dmg
	p.lastHit = time.Now()
	p.dirty = true
	return dmg
}

// potentialFor is Mob.getTiemNangForPlayer without the level difference
// divisor (player levels are not ported yet): dmg + 0.05% of mob max HP.
func potentialFor(m *Mob, dmg int64) int64 {
	tn := dmg + m.MaxHP/2000
	tn = tn * 100 / 125 * max(m.T.PctPotent, 1) / 100
	return max(tn, 1)
}

func (p *Player) gainPower(n int64) {
	n = n * ExpRate * eventExpMult.Load() * int64(100+p.Mods.XPPct) / 100
	for _, f := range powerHooks {
		n = f(p, n)
	}
	lim := powerLimits[p.LimitLevel()].Power
	if p.Char.Power >= lim {
		n = min(n, 10) // at the cap only a trickle, as in the legacy server
	}
	p.Char.Power += n
	p.Char.Potential += n
	p.dirty = true
}

type skillReq struct {
	ID     int    `json:"id"`     // skill template id
	Mob    *int   `json:"mob"`    // target mob index
	Player int64  `json:"player"` // target player id (PvP, buffs)
	Boss   string `json:"boss"`   // target boss key
}

// useSkill validates and executes a skill (SkillService.useSkill).
func (z *Zone) useSkill(p *Player, raw json.RawMessage) {
	var req skillReq
	if json.Unmarshal(raw, &req) != nil || p.Dead || !canAct(p) {
		return
	}
	var learned int
	for _, s := range p.Char.Skills {
		if s.TemplateID == req.ID {
			learned = s.Level
		}
	}
	tmpl := z.World.Data.Skills[[2]int{p.Char.Gender, req.ID}]
	if tmpl == nil || learned == 0 {
		return
	}
	lv := tmpl.Level(learned)
	if lv == nil {
		return
	}
	now := time.Now()
	if last, ok := p.skillUsed[req.ID]; ok && now.Sub(last) < time.Duration(lv.CoolDown-50)*time.Millisecond {
		return
	}
	cost := lv.ManaUse
	switch tmpl.ManaUseType {
	case 1:
		cost = p.MaxMP * lv.ManaUse / 100
	case 2:
		cost = p.Char.MP
	}
	if p.Char.MP < cost || (tmpl.ManaUseType == 2 && cost == 0) {
		p.Send("notice", map[string]string{"text": "Không đủ KI"})
		return
	}
	p.Char.MP -= cost
	p.skillUsed[req.ID] = now
	p.Anim = anim.StateAttack
	z.moved[p.ID()] = p
	p.Send("mp", map[string]int64{"mp": p.Char.MP})

	a := &AttackCtx{Tmpl: tmpl, Level: lv}
	if req.Mob != nil && *req.Mob >= 0 && *req.Mob < len(z.mobs) {
		a.Mob = z.mobs[*req.Mob]
	}
	if req.Player != 0 {
		a.Target = z.players[req.Player]
	}
	if req.Boss != "" {
		a.Boss = z.bossByKey(req.Boss)
	}
	for _, h := range skillHooks {
		if h(z, p, a) {
			return // special skill handled (buff, stun, transform...)
		}
	}
	reach := max(lv.Dx, meleeRange) + rangeSlack
	switch {
	case req.Mob != nil:
		z.skillOnMobs(p, a, *req.Mob, reach)
	case req.Player != 0:
		z.skillOnPlayer(p, a, req.Player, reach)
	case a.Boss != nil:
		if dist(p.Char.X, p.Char.Y, a.Boss.X, a.Boss.Y) <= reach+30 {
			z.hitBoss(p, a.Boss, p.attack(a))
		}
	}
}

// skillOnMobs hits the target mob and, for area skills (max_fight > 1),
// other live mobs near it.
func (z *Zone) skillOnMobs(p *Player, a *AttackCtx, idx, reach int) {
	lv := a.Level
	if idx < 0 || idx >= len(z.mobs) {
		return
	}
	target := z.mobs[idx]
	if !target.Alive() || dist(p.Char.X, p.Char.Y, target.X, target.Y) > reach {
		return
	}
	hits := []*Mob{target}
	for _, m := range z.mobs {
		if len(hits) >= max(lv.MaxFight, 1) {
			break
		}
		if m != target && m.Alive() && dist(m.X, m.Y, target.X, target.Y) <= max(lv.Dx, 60) {
			hits = append(hits, m)
		}
	}
	for _, m := range hits {
		z.hitMob(p, m, p.attack(a))
	}
}

func (z *Zone) hitMob(p *Player, m *Mob, h hitResult) {
	dmg := h.Dmg
	if m.T.ID == trainingDummy {
		dmg = min(dmg, max(m.MaxHP/10, 1))
	}
	if m.HP == m.MaxHP && dmg >= m.HP {
		dmg = m.HP - 1 // a full-HP mob is never one-shot (Mob.injured)
	}
	dmg = max(dmg, 0)
	m.HP -= dmg
	m.aggro[p.ID()] = time.Now()
	if p.Mods.LeechHP > 0 {
		p.Char.HP = min(p.MaxHP, p.Char.HP+dmg*int64(p.Mods.LeechHP)/100)
	}
	p.gainPower(potentialFor(m, dmg))
	z.broadcast(Encode("mob_hit", map[string]any{"i": m.Idx, "hp": m.HP, "dmg": dmg, "crit": h.Crit, "by": p.ID()}), 0)
	p.Send("exp", map[string]int64{"power": p.Char.Power, "potential": p.Char.Potential, "hp": p.Char.HP})
	if m.HP <= 0 {
		m.diedAt = time.Now()
		z.broadcast(Encode("mob_die", map[string]any{"i": m.Idx, "by": p.ID()}), 0)
		z.onMobKilled(p, m)
	}
}

// skillOnPlayer is PvP: only allowed between duelists (M8) or in PK maps.
func (z *Zone) skillOnPlayer(p *Player, a *AttackCtx, id int64, reach int) {
	t := z.players[id]
	if t == nil || t.Dead || t == p || !z.canPvP(p, t) {
		return
	}
	if dist(p.Char.X, p.Char.Y, t.Char.X, t.Char.Y) > reach {
		return
	}
	h := p.attack(a)
	dmg := t.takeDamageFrom(h.Dmg, p.Mods.Pierce, DamageSrc{Player: p})
	if t.Mods.Reflect > 0 {
		p.takeDamageFrom(dmg*int64(t.Mods.Reflect)/100, 0, DamageSrc{Player: t})
	}
	z.broadcast(Encode("player_hit", map[string]any{"id": t.ID(), "hp": t.Char.HP, "dmg": dmg, "crit": h.Crit, "by": p.ID()}), 0)
	if t.Char.HP <= 0 {
		z.playerDie(t, p)
	}
}

// playerDie marks the player dead; they choose to revive in place (gems) or go home.
func (z *Zone) playerDie(p *Player, killer *Player) {
	p.Dead = true
	p.Char.HP = 0
	p.dirty = true
	var by int64
	if killer != nil {
		by = killer.ID()
		z.endDuel(p, killer)
	}
	for _, m := range z.mobs {
		delete(m.aggro, p.ID())
	}
	z.broadcast(Encode("die", map[string]int64{"id": p.ID(), "by": by}), 0)
	for _, h := range deathHooks {
		h(z, p, killer)
	}
}

// revive handles {"home": bool}: in place costs gems (PlayerService.hoiSinh),
// home is free and restores HP/KI.
func (z *Zone) revive(p *Player, raw json.RawMessage) {
	var req struct{ Home bool }
	json.Unmarshal(raw, &req)
	if !p.Dead {
		return
	}
	if !req.Home {
		if p.Char.Gem < reviveGemCost {
			p.Send("notice", map[string]string{"text": "Không đủ ngọc để hồi sinh"})
			return
		}
		p.Char.Gem -= reviveGemCost
	}
	p.Dead = false
	p.Char.HP, p.Char.MP = p.MaxHP, p.MaxMP
	p.lastHit = time.Now().Add(reviveImmunity) // brief invulnerability
	p.dirty = true
	if req.Home {
		z.leaveTo(p, HomeMap(p.Char.Gender), -1, 300, 336)
		return
	}
	z.broadcast(Encode("revive", map[string]int64{"id": p.ID(), "hp": p.Char.HP}), 0)
	p.Send("self", p.Self())
}

// addPoint spends potential on base stats ({"stat": 0-4, "n": count}).
func (z *Zone) addPoint(p *Player, raw json.RawMessage) {
	var req struct{ Stat, N int64 }
	if json.Unmarshal(raw, &req) != nil || req.N <= 0 || req.N > 1000 {
		return
	}
	c := p.Char
	cost, val, ok := PointCost(c, int(req.Stat), req.N, p.LimitLevel())
	if !ok {
		p.Send("notice", map[string]string{"text": "Vui lòng mở giới hạn sức mạnh"})
		return
	}
	if c.Potential < cost {
		p.Send("notice", map[string]string{"text": "Không đủ tiềm năng"})
		return
	}
	c.Potential -= cost
	switch req.Stat {
	case StatHP:
		c.HPBase = val
	case StatMP:
		c.MPBase = val
	case StatDmg:
		c.DmgBase = val
	case StatDef:
		c.DefBase = val
	case StatCrit:
		c.CritBase = int(val)
	}
	p.Recalc(z.World.Data)
	p.dirty = true
	p.Send("self", p.Self())
	z.World.Events.Emit(Event{Kind: EvAddPoint, Player: p, Zone: z})
}

// LimitLevel is the opened power-limit tier.
func (p *Player) LimitLevel() int { return min(max(p.Char.Limit, 0), len(powerLimits)-1) }

// regen restores 1% HP/KI per second out of combat.
func (z *Zone) regen(now time.Time) {
	for _, p := range z.players {
		if p.Dead || now.Sub(p.lastHit) < combatRegenOff || p.Anim == anim.StateFly {
			continue
		}
		c := p.Char
		if c.HP >= p.MaxHP && c.MP >= p.MaxMP {
			continue
		}
		c.HP = min(p.MaxHP, c.HP+max(p.MaxHP/100, 1))
		c.MP = min(p.MaxMP, c.MP+max(p.MaxMP/100, 1))
		p.Send("hpmp", map[string]int64{"hp": c.HP, "mp": c.MP})
	}
}
