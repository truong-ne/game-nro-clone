package game

import (
	"math/rand/v2"
	"time"
)

// Special skill effects, ported from SkillService.useSkillAttack/useSkillAlone/
// useSkillBuffToPlayer/updateSkillSpecial, EffectSkillService and SkillUtil.
// Skill template ids are global (skill/Skill.java).
const (
	skTDHS        = 6  // Thái Dương Hạ San
	skTriThuong   = 7  // Trị thương
	skTTNL        = 8  // Tái tạo năng lượng
	skQCKK        = 10 // Quả cầu kênh khí
	skMakanko     = 11 // Makankosappo
	skDeTrung     = 12 // Đẻ trứng
	skBienKhi     = 13 // Biến hình (khỉ)
	skTuSat       = 14 // Tự phát nổ
	skSocola      = 18 // Biến Sôcôla
	skKhien       = 19 // Khiên năng lượng
	skDCTT        = 20 // Dịch chuyển tức thời
	skHuytSao     = 21 // Huýt sáo
	skThoiMien    = 22 // Thôi miên
	skTroi        = 23 // Trói
	skSuperKame   = 24
	skLienHoanCh  = 25 // Cađíc liên hoàn chưởng
	skMaPhongBa   = 26
	buffTimeGong  = 2000 * time.Millisecond // NewSkill.TIME_GONG
	buffBinhTime  = 11000                   // NewSkill.TIME_END_26 / finishUseMafuba
	buffMafubaRng = 500                     // SkillService.updateSkillSpecial:176
)

// Effect kinds that stop moving/attacking (EffectSkill.isHaveEffectSkill:252).
var buffLockKinds = []string{"stun", "sleep", "hold", "blind", "binh", "transform"}

// SkillUtil.java formulas (level = skill point).
func buffTimeStun(lv int) int64     { return int64(lv+2) * 1000 }                                        // :105
func buffRangeStun(lv int) int      { return 120 + lv*30 }                                               // :129
func buffTimeShield(lv int) int64   { return int64(lv+2) * 5000 }                                        // :113
func buffTimeTroi(lv int) int64     { return int64(lv) * 5000 }                                          // :117
func buffTimeDCTT(lv int) int64     { return int64(lv+1) * 500 }                                         // :121
func buffTimeThoiMien(lv int) int64 { return int64(lv+4) * 1000 }                                        // :125
func buffTimeMonkey(lv int) int64   { return int64(lv+5) * 10000 }                                       // :93
func buffPctHPMonkey(lv int) int64  { return int64(lv+3) * 10 }                                          // :97
func buffPctDmgMonkey(lv int) int64 { return int64(lv + 3) }                                             // :101
func buffRangeQCKK(lv int) int      { return 350 + lv*30 }                                               // :137
func buffRangeBom(lv int) int       { return 400 + lv*30 }                                               // :133
func buffPctHuytSao(lv int) int64   { return int64(lv+3) * 10 }                                          // :141
func buffPctTriThuong(lv int) int64 { return int64(lv+9) * 5 }                                           // :145
func buffPctCharge(lv int) int64    { return int64(lv + 3) }                                             // :149
func buffTimeSocola() int64         { return 30000 }                                                     // :109
func buffEggPct(lv int) int64       { return []int64{30, 40, 50, 60, 70, 80, 90}[min(max(lv, 1), 7)-1] } // :162

func init() {
	skillHooks = append(skillHooks, buffSkillHook)
	canActHooks = append(canActHooks, func(p *Player) bool {
		fx := buffFx(p)
		now := time.Now()
		for _, k := range buffLockKinds {
			if fx.active(k, now) {
				return false
			}
		}
		return true
	})
	outDamageHooks = append(outDamageHooks, buffFxOutDamage)
	inDamageHooks = append(inDamageHooks, func(p *Player, dmg int64, _ DamageSrc) int64 {
		fx := buffFx(p)
		if fx.active("shield", time.Now()) { // Player.injured:1172
			if dmg > p.MaxHP { // breakShield
				delete(fx.until, "shield")
				if z := p.Zone(); z != nil {
					buffEff(z, p, "shield", 0)
				}
			}
			return 1
		}
		return dmg
	})
	recalcHooks = append(recalcHooks, func(p *Player, d *Data, m *Mods) {
		fx := buffFx(p)
		if fx.monkeyLv > 0 { // NPoint.setHpMax:819, setDame:1175, setCrit:1230
			m.HPPct += buffPctHPMonkey(fx.monkeyLv)
			m.DmgPct += buffPctDmgMonkey(fx.monkeyLv)
			p.Crit = max(p.Crit, 100)
		}
		if fx.huytSao > 0 { // NPoint.setHpMax:892
			m.HPPct += fx.huytSao
		}
	})
	tickHooks = append(tickHooks, buffFxTick)
	secondHooks = append(secondHooks, buffFxSecond)
}

// buffLock applies a movement/attack lock to a player.
func buffLock(z *Zone, t *Player, kind string, ms int64) {
	fx := buffFx(t)
	fx.until[kind] = time.Now().Add(time.Duration(ms) * time.Millisecond)
	fx.charge = 0 // CC interrupts Tái tạo năng lượng (NPoint.update:1984)
	buffEff(z, t, kind, ms)
}

// buffLockMob: mobs only attack, so a lock postpones their next attack.
func buffLockMob(z *Zone, m *Mob, kind string, ms int64) {
	until := time.Now().Add(time.Duration(ms) * time.Millisecond)
	if until.After(m.attackAt) {
		m.attackAt = until
	}
	buffMobEff(z, m, kind, ms)
}

func buffLockBoss(z *Zone, b *Boss, kind string, ms int64) {
	until := time.Now().Add(time.Duration(ms) * time.Millisecond)
	if until.After(b.attackAt) {
		b.attackAt = until
	}
	buffBossEff(z, b, kind, ms)
}

func buffReach(lv *SkillLevel) int { return max(lv.Dx, meleeRange) + rangeSlack }

// buffHitPlayer is skillOnPlayer without the range check.
func (z *Zone) buffHitPlayer(p, t *Player, h hitResult) {
	if t == nil || t.Dead || t == p {
		return
	}
	dmg := t.takeDamageFrom(h.Dmg, p.Mods.Pierce, DamageSrc{Player: p})
	z.broadcast(Encode("player_hit", map[string]any{"id": t.ID(), "hp": t.Char.HP, "dmg": dmg, "crit": h.Crit, "by": p.ID()}), 0)
	if t.Char.HP <= 0 {
		z.playerDie(t, p)
	}
}

// buffHitMob hits a mob; full=true lets the hit kill a full-HP mob
// (Mob.injured dieWhenHpFull, used by QCKK/Makankosappo/Tự sát).
func (z *Zone) buffHitMob(p *Player, m *Mob, h hitResult, full bool) {
	if !m.Alive() {
		return
	}
	if full && m.HP == m.MaxHP && h.Dmg >= m.HP && m.T.ID != trainingDummy {
		m.HP-- // hitMob refuses to one-shot a full-HP mob
	}
	z.hitMob(p, m, h)
}

func buffSkillHook(z *Zone, p *Player, a *AttackCtx) bool {
	buffIntrinsicCooldown(p, a)
	buffIntrinsicAfterSkill(p, a.Tmpl.ID)
	lv := a.Level.Point
	now := time.Now()
	fx := buffFx(p)
	switch a.Tmpl.ID {
	case skTDHS: // useSkillAlone:603
		ms := buffTimeStun(lv)
		rng := buffRangeStun(lv)
		for _, t := range z.players {
			if t != p && !t.Dead && dist(p.Char.X, p.Char.Y, t.Char.X, t.Char.Y) <= rng && z.canPvP(p, t) {
				buffLock(z, t, "stun", ms)
			}
		}
		for _, m := range z.mobs {
			if m.Alive() && dist(p.Char.X, p.Char.Y, m.X, m.Y) <= rng {
				buffLockMob(z, m, "stun", ms)
			}
		}
		for _, b := range z.bosses {
			if dist(p.Char.X, p.Char.Y, b.X, b.Y) <= rng {
				buffLockBoss(z, b, "stun", ms)
			}
		}
		buffEff(z, p, "tdhs", ms)
	case skTriThuong: // useSkillBuffToPlayer:789
		buffTriThuong(z, p, a.Target, lv)
	case skTTNL: // EffectSkillService.startCharge + NPoint.update:1982
		fx.charge, fx.chargePct = 10, buffPctCharge(lv)
		buffEff(z, p, "charge", 10000)
	case skQCKK: // useSkillAttack:443, NPoint.getDameAttack:1446
		buffEff(z, p, "prepare", 4000)
		fx.schedule(z, now.Add(4*time.Second), func(z *Zone) { buffQCKK(z, p, a) })
	case skMakanko: // useSkillAttack:482, NPoint.getDameAttack:1439
		buffEff(z, p, "prepare", 3000)
		fx.schedule(z, now.Add(3*time.Second), func(z *Zone) {
			h := hitResult{Dmg: max(p.MaxMP*a.Level.Damage/100, 1)}
			buffHitTarget(z, p, a, h, true)
		})
	case skDeTrung: // useSkillAlone:643, MobMe.java:21-26
		fx.eggDmg = max(p.Dmg*buffEggPct(lv)/100, 1)
		fx.eggUntil = now.Add(time.Duration(buffTimeMonkey(lv)*2) * time.Millisecond)
		buffEff(z, p, "egg", buffTimeMonkey(lv)*2)
	case skBienKhi: // EffectSkillService.startUseSkillMonkey:202 -> setIsMonkey:224
		fx.until["transform"] = now.Add(1500 * time.Millisecond)
		fx.monkeyAt = now.Add(1500 * time.Millisecond)
		fx.monkeyLv = -lv // pending; applied in tick
		buffEff(z, p, "transform", 1500)
	case skTuSat: // useSkillAlone:723
		buffEff(z, p, "prepare", 2000)
		fx.schedule(z, now.Add(2*time.Second), func(z *Zone) { buffTuSat(z, p, lv) })
	case skSocola: // useSkillAttack:501
		ms := buffTimeSocola()
		if t := a.Target; t != nil && t != p && z.canPvP(p, t) {
			buffFx(t).until["socola"] = now.Add(time.Duration(ms) * time.Millisecond)
			buffEff(z, t, "socola", ms)
		}
		if a.Mob != nil && a.Mob.Alive() {
			buffLockMob(z, a.Mob, "socola", ms)
		}
	case skKhien: // useSkillAlone:657
		ms := buffTimeShield(lv)
		fx.until["shield"] = now.Add(time.Duration(ms) * time.Millisecond)
		buffEff(z, p, "shield", ms)
	case skDCTT: // useSkillAttack:514
		buffDCTT(z, p, a, lv)
	case skHuytSao: // useSkillAlone:663
		pct := buffPctHuytSao(lv)
		for _, t := range z.players {
			if t.Dead || t.Char.Gender == 1 { // Namek is not affected
				continue
			}
			tf := buffFx(t)
			tf.huytSao = pct
			tf.until["huyt_sao"] = now.Add(30 * time.Second)
			buffRefresh(z, t)
			t.Char.HP = min(t.MaxHP, t.Char.HP+t.Char.HP*pct/100)
			t.Send("hpmp", map[string]int64{"hp": t.Char.HP, "mp": t.Char.MP})
			buffEff(z, t, "huyt_sao", 30000)
		}
	case skThoiMien: // useSkillAttack:536
		ms := buffTimeThoiMien(lv)
		buffCC(z, p, a, "sleep", ms)
	case skTroi: // useSkillAttack:550
		ms := buffTimeTroi(lv)
		buffCC(z, p, a, "hold", ms)
	case skSuperKame, skLienHoanCh: // updateSkillSpecial:202-247
		dir := buffDir(p, a)
		buffEff(z, p, "gong", int64(buffTimeGong/time.Millisecond))
		for i := range 4 {
			fx.schedule(z, now.Add(buffTimeGong+time.Duration(i)*500*time.Millisecond), func(z *Zone) { buffBeam(z, p, a, dir) })
		}
	case skMaPhongBa: // updateSkillSpecial:157, finishUseMafuba
		buffEff(z, p, "gong", int64(buffTimeGong/time.Millisecond))
		fx.schedule(z, now.Add(buffTimeGong), func(z *Zone) { buffMafuba(z, p) })
	default:
		buffEggAttack(z, p, a)
		return false
	}
	return true
}

func (f *buffFxState) schedule(z *Zone, due time.Time, fn func(z *Zone)) {
	f.pending = append(f.pending, buffPending{due, z, fn})
}

// buffCC applies sleep/hold to the targeted player or mob in reach.
func buffCC(z *Zone, p *Player, a *AttackCtx, kind string, ms int64) {
	reach := buffReach(a.Level)
	if t := a.Target; t != nil && t != p && !t.Dead && z.canPvP(p, t) && dist(p.Char.X, p.Char.Y, t.Char.X, t.Char.Y) <= reach {
		buffLock(z, t, kind, ms)
	}
	if m := a.Mob; m != nil && m.Alive() && dist(p.Char.X, p.Char.Y, m.X, m.Y) <= reach {
		buffLockMob(z, m, kind, ms)
	}
	if b := a.Boss; b != nil && dist(p.Char.X, p.Char.Y, b.X, b.Y) <= reach+30 {
		buffLockBoss(z, b, kind, ms)
	}
}

// buffHitTarget hits whatever the skill targeted, ignoring range (charged skills).
func buffHitTarget(z *Zone, p *Player, a *AttackCtx, h hitResult, full bool) {
	if p.Dead {
		return
	}
	switch {
	case a.Mob != nil:
		z.buffHitMob(p, a.Mob, h, full)
	case a.Target != nil && z.players[a.Target.ID()] == a.Target && z.canPvP(p, a.Target):
		z.buffHitPlayer(p, a.Target, h)
	case a.Boss != nil && z.bossByKey(a.Boss.Key) == a.Boss:
		z.hitBoss(p, a.Boss, h)
	}
}

func buffTriThuong(z *Zone, p, target *Player, lv int) {
	pct := buffPctTriThuong(lv)
	targets := []*Player{}
	if target != nil && target != p && z.players[target.ID()] == target {
		targets = append(targets, target)
		if lv > 1 {
			for _, t := range z.players {
				if t != p && t != target && dist(p.Char.X, p.Char.Y, t.Char.X, t.Char.Y) <= 300 {
					targets = append(targets, t)
				}
			}
		}
	}
	p.Char.HP = min(p.MaxHP, p.Char.HP+p.MaxHP*pct/100)
	p.Send("hpmp", map[string]int64{"hp": p.Char.HP, "mp": p.Char.MP})
	for _, t := range targets {
		wasDead := t.Dead
		t.Char.HP = min(t.MaxHP, t.Char.HP+t.MaxHP*pct/100)
		t.Char.MP = min(t.MaxMP, t.Char.MP+t.MaxMP*pct/100)
		t.dirty = true
		if wasDead {
			t.Dead = false
			z.broadcast(Encode("revive", map[string]int64{"id": t.ID(), "hp": t.Char.HP}), 0)
			t.Send("self", t.Self())
		}
		t.Send("hpmp", map[string]int64{"hp": t.Char.HP, "mp": t.Char.MP})
		z.broadcast(Encode("chat", map[string]any{"id": t.ID(), "name": t.Char.Name, "text": "Cảm ơn " + p.Char.Name + " đã cứu mình"}), 0)
		buffEff(z, t, "heal", 500)
	}
}

func buffQCKK(z *Zone, p *Player, a *AttackCtx) {
	if p.Dead {
		return
	}
	rng := buffRangeQCKK(a.Level.Point)
	var hpSum int64
	for _, m := range z.mobs {
		if m.Alive() && dist(p.Char.X, p.Char.Y, m.X, m.Y) <= rng {
			hpSum += m.HP
		}
	}
	for _, t := range z.players {
		if t != p && !t.Dead && dist(p.Char.X, p.Char.Y, t.Char.X, t.Char.Y) <= rng {
			hpSum += t.Char.HP
		}
	}
	dmg := hpSum*10/100 + p.Dmg*10
	dmg += dmg * int64(rand.IntN(11)-5) / 100
	h := hitResult{Dmg: max(dmg, 1)}
	cx, cy := p.Char.X, p.Char.Y
	switch {
	case a.Mob != nil:
		cx, cy = a.Mob.X, a.Mob.Y
	case a.Target != nil:
		cx, cy = a.Target.Char.X, a.Target.Char.Y
	}
	buffHitTarget(z, p, a, h, true)
	for _, m := range z.mobs {
		if m != a.Mob && m.Alive() && dist(cx, cy, m.X, m.Y) <= rng {
			z.buffHitMob(p, m, h, true)
		}
	}
}

func buffTuSat(z *Zone, p *Player, lv int) {
	if p.Dead {
		return
	}
	rng := buffRangeBom(lv)
	dmg := p.Char.HP
	for _, m := range z.mobs {
		if m.Alive() && dist(p.Char.X, p.Char.Y, m.X, m.Y) <= rng {
			z.buffHitMob(p, m, hitResult{Dmg: dmg}, true)
		}
	}
	for _, t := range z.players {
		if t != p && !t.Dead && z.canPvP(p, t) && dist(p.Char.X, p.Char.Y, t.Char.X, t.Char.Y) <= rng {
			z.buffHitPlayer(p, t, hitResult{Dmg: dmg})
		}
	}
	for _, b := range append([]*Boss(nil), z.bosses...) {
		if dist(p.Char.X, p.Char.Y, b.X, b.Y) <= rng {
			z.hitBoss(p, b, hitResult{Dmg: dmg / 2}) // bosses take half (useSkillAlone:769)
		}
	}
	buffEff(z, p, "explode", 1000)
	z.playerDie(p, nil)
}

func buffDCTT(z *Zone, p *Player, a *AttackCtx, lv int) {
	ms := buffTimeDCTT(lv)
	h := p.attack(a)
	if !h.Crit { // DCTT always crits (NPoint.getDameAttack:1434)
		h.Dmg *= 2
		h.Crit = true
	}
	tx, ty := 0, 0
	switch {
	case a.Target != nil && a.Target != p && z.canPvP(p, a.Target):
		tx, ty = a.Target.Char.X, a.Target.Char.Y
		z.buffHitPlayer(p, a.Target, h)
		if !a.Target.Dead {
			buffLock(z, a.Target, "blind", ms)
		}
	case a.Mob != nil && a.Mob.Alive():
		tx, ty = a.Mob.X, a.Mob.Y
		z.hitMob(p, a.Mob, h)
		buffLockMob(z, a.Mob, "blind", ms)
	case a.Boss != nil:
		tx, ty = a.Boss.X, a.Boss.Y
		z.hitBoss(p, a.Boss, h)
		buffLockBoss(z, a.Boss, "blind", ms)
	default:
		return
	}
	p.Char.X, p.Char.Y = tx, ty
	p.dirty = true
	z.moved[p.ID()] = p
	p.Send("pos", map[string]int{"x": tx, "y": ty})
}

func buffDir(p *Player, a *AttackCtx) int {
	switch {
	case a.Mob != nil:
		return sign(a.Mob.X - p.Char.X)
	case a.Target != nil:
		return sign(a.Target.Char.X - p.Char.X)
	case a.Boss != nil:
		return sign(a.Boss.X - p.Char.X)
	}
	return p.Dir
}

// buffBeam is one damage step of Super Kame / Liên hoàn chưởng: everything in
// front of the caster within (point+400) x dy (NewSkill.setSkillSpecial).
// Legacy getDameAttack has no case for these skills, so the base damage is used.
func buffBeam(z *Zone, p *Player, a *AttackCtx, dir int) {
	if p.Dead || !canAct(p) {
		return
	}
	rx, ry := a.Level.Point+400, max(a.Level.Dy, 100)
	base := &AttackCtx{Tmpl: a.Tmpl, Level: &SkillLevel{Point: a.Level.Point, Damage: 100}}
	in := func(x, y int) bool {
		dx := (x - p.Char.X) * dir
		return dx > 0 && dx <= rx && buffAbs(y-p.Char.Y) <= ry
	}
	for _, m := range z.mobs {
		if m.Alive() && in(m.X, m.Y) {
			b := *base
			b.Mob = m
			z.buffHitMob(p, m, p.attack(&b), false)
		}
	}
	for _, t := range z.players {
		if t != p && !t.Dead && z.canPvP(p, t) && in(t.Char.X, t.Char.Y) {
			b := *base
			b.Target = t
			z.buffHitPlayer(p, t, p.attack(&b))
		}
	}
	for _, bs := range append([]*Boss(nil), z.bosses...) {
		if in(bs.X, bs.Y) {
			b := *base
			b.Boss = bs
			z.hitBoss(p, bs, p.attack(&b))
		}
	}
}

func buffMafuba(z *Zone, p *Player) {
	if p.Dead {
		return
	}
	for _, t := range z.players {
		if t != p && !t.Dead && z.canPvP(p, t) && dist(p.Char.X, p.Char.Y, t.Char.X, t.Char.Y) <= buffMafubaRng {
			buffLock(z, t, "binh", buffBinhTime)
		}
	}
	for _, m := range z.mobs {
		if m.Alive() && dist(p.Char.X, p.Char.Y, m.X, m.Y) <= buffMafubaRng {
			buffLockMob(z, m, "binh", buffBinhTime)
		}
	}
	for _, b := range z.bosses {
		if dist(p.Char.X, p.Char.Y, b.X, b.Y) <= buffMafubaRng {
			buffLockBoss(z, b, "binh", buffBinhTime)
		}
	}
}

// buffEggAttack: the egg (MobMe.attack) joins normal attacks; it never kills.
func buffEggAttack(z *Zone, p *Player, a *AttackCtx) {
	fx := buffFx(p)
	if fx.eggDmg == 0 || time.Now().After(fx.eggUntil) {
		return
	}
	dmg := fx.eggDmg
	if m := a.Mob; m != nil && m.Alive() && m.HP > dmg {
		m.HP -= dmg
		p.gainPower(potentialFor(m, dmg))
		z.broadcast(Encode("mob_hit", map[string]any{"i": m.Idx, "hp": m.HP, "dmg": dmg, "by": p.ID(), "egg": true}), 0)
	}
	if t := a.Target; t != nil && t != p && !t.Dead && z.canPvP(p, t) && t.Char.HP > dmg && t.Char.HP > t.MaxHP/20 {
		z.buffHitPlayer(p, t, hitResult{Dmg: dmg})
	}
}

func buffFxOutDamage(p *Player, dmg int64, a *AttackCtx) int64 {
	fx := buffFx(p)
	now := time.Now()
	if fx.active("socola", now) { // a chocolate deals 1 damage
		return 1
	}
	if fx.nextDmg > 0 { // NPoint.getDameAttack:1494 dameAfter
		dmg += dmg * fx.nextDmg / 100
		fx.nextDmg = 0
	}
	if t := a.Target; t != nil && buffFx(t).active("hold", now) { // playerAttackPlayer:904 crit on held target
		dmg *= 2
	}
	return dmg
}

func buffFxTick(z *Zone, now time.Time) {
	for _, p := range z.players {
		fx, ok := p.Tmp["buffs.fx"].(*buffFxState)
		if !ok {
			continue
		}
		if fx.monkeyLv < 0 && !now.Before(fx.monkeyAt) { // finishUseMonkey -> setIsMonkey
			lv := -fx.monkeyLv
			fx.monkeyLv = 0
			if !p.Dead {
				fx.monkeyLv = lv
				ms := buffTimeMonkey(lv)
				fx.until["monkey"] = now.Add(time.Duration(ms) * time.Millisecond)
				buffRefresh(z, p)
				p.Char.HP = min(p.MaxHP, p.Char.HP*2)
				p.Send("hpmp", map[string]int64{"hp": p.Char.HP, "mp": p.Char.MP})
				buffEff(z, p, "monkey", ms)
			}
		}
		if len(fx.pending) == 0 {
			continue
		}
		var keep []buffPending
		var due []buffPending
		for _, pd := range fx.pending {
			switch {
			case pd.zone != z:
				// left the zone: drop
			case now.Before(pd.due):
				keep = append(keep, pd)
			default:
				due = append(due, pd)
			}
		}
		fx.pending = keep
		for _, pd := range due {
			pd.fn(z)
		}
	}
}

func buffFxSecond(z *Zone, now time.Time) {
	for _, p := range z.players {
		fx, ok := p.Tmp["buffs.fx"].(*buffFxState)
		if !ok {
			continue
		}
		if p.Dead { // EffectSkill.removeSkillEffectWhenDie
			if fx.monkeyLv != 0 || fx.huytSao != 0 {
				fx.monkeyLv, fx.huytSao = 0, 0
				p.Recalc(z.World.Data)
			}
			clear(fx.until)
			fx.charge, fx.eggDmg, fx.pending = 0, 0, nil
			continue
		}
		changed := false
		if fx.monkeyLv > 0 && !fx.active("monkey", now) { // monkeyDown
			fx.monkeyLv = 0
			changed = true
			buffEff(z, p, "monkey", 0)
		}
		if fx.huytSao > 0 && !fx.active("huyt_sao", now) {
			fx.huytSao = 0
			changed = true
			buffEff(z, p, "huyt_sao", 0)
		}
		for k, u := range fx.until {
			if !now.Before(u) {
				delete(fx.until, k)
				if k != "monkey" && k != "huyt_sao" {
					buffEff(z, p, k, 0)
				}
			}
		}
		if fx.eggDmg > 0 && now.After(fx.eggUntil) {
			fx.eggDmg = 0
			buffEff(z, p, "egg", 0)
		}
		if changed {
			buffRefresh(z, p)
		}
		if fx.charge > 0 { // NPoint.update:1982
			c := p.Char
			if !canAct(p) || (c.HP >= p.MaxHP && c.MP >= p.MaxMP) {
				fx.charge = 0
				buffEff(z, p, "charge", 0)
				continue
			}
			c.HP = min(p.MaxHP, c.HP+p.MaxHP/100*fx.chargePct)
			c.MP = min(p.MaxMP, c.MP+p.MaxMP/100*fx.chargePct)
			p.Send("hpmp", map[string]int64{"hp": c.HP, "mp": c.MP})
			if fx.charge--; fx.charge == 0 {
				buffEff(z, p, "charge", 0)
			}
		}
	}
}

func buffAbs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
