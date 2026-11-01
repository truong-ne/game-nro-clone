package game

import (
	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Mods are the percentage/derived modifiers collected from item options.
type Mods struct {
	HPPct, MPPct, DmgPct   int64
	Armor, Dodge, Pierce   int // percent
	Reflect, LeechHP       int
	LeechMP, XPPct         int
	FlatHP, FlatMP, FlatDm int64
}

// optionEffects maps item option ids to their stat effect. It replaces the
// legacy NPoint.addOption switch (copied three times for items, radar cards
// and titles). Unlisted options are cosmetic or handled by their own system.
var optionEffects = map[int]func(p *Player, m *Mods, v int64){
	0:   func(p *Player, m *Mods, v int64) { m.FlatDm += v },                              // Tấn công +#
	2:   func(p *Player, m *Mods, v int64) { m.FlatHP += v * 1000; m.FlatMP += v * 1000 }, // HP, KI +#000
	6:   func(p *Player, m *Mods, v int64) { m.FlatHP += v },                              // HP +#
	7:   func(p *Player, m *Mods, v int64) { m.FlatMP += v },                              // KI +#
	14:  func(p *Player, m *Mods, v int64) { p.Crit += int(v) },                           // Chí mạng +#%
	22:  func(p *Player, m *Mods, v int64) { m.FlatHP += v * 1000 },                       // HP +#000
	23:  func(p *Player, m *Mods, v int64) { m.FlatMP += v * 1000 },                       // KI +#000
	47:  func(p *Player, m *Mods, v int64) { p.Def += v },                                 // Giáp +#
	50:  func(p *Player, m *Mods, v int64) { m.DmgPct += v },                              // Sức đánh +#%
	147: func(p *Player, m *Mods, v int64) { m.DmgPct += v },
	77:  func(p *Player, m *Mods, v int64) { m.HPPct += v }, // HP +#%
	103: func(p *Player, m *Mods, v int64) { m.MPPct += v }, // KI +#%
	94:  func(p *Player, m *Mods, v int64) { m.Armor += int(v) },
	95:  func(p *Player, m *Mods, v int64) { m.LeechHP += int(v) },
	96:  func(p *Player, m *Mods, v int64) { m.LeechMP += int(v) },
	97:  func(p *Player, m *Mods, v int64) { m.Reflect += int(v) },
	98:  func(p *Player, m *Mods, v int64) { m.Pierce += int(v) },
	99:  func(p *Player, m *Mods, v int64) { m.Pierce += int(v) },
	101: func(p *Player, m *Mods, v int64) { m.XPPct += int(v) },
	108: func(p *Player, m *Mods, v int64) { m.Dodge += int(v) },
}

const (
	maxArmor = 86
	maxDodge = 90
)

// Recalc derives display parts and combat stats from base stats and worn items.
func (p *Player) Recalc(d *Data) {
	c := p.Char
	p.Crit, p.Def = c.CritBase, c.DefBase*4
	var m Mods
	p.Body, p.Leg = 0, 0
	if t := d.Items[StarterShirt[c.Gender]]; t != nil {
		p.Body = t.Part
	}
	if t := d.Items[StarterPants[c.Gender]]; t != nil {
		p.Leg = t.Part
	}
	for _, it := range c.Items {
		if it.Container != store.ContainerBody {
			continue
		}
		if t := d.Items[it.TemplateID]; t != nil {
			switch t.Type {
			case ItemShirt:
				p.Body = t.Part
			case ItemPants:
				p.Leg = t.Part
			}
		}
		for _, o := range it.Options {
			if f := optionEffects[o[0]]; f != nil {
				f(p, &m, int64(o[1]))
			}
		}
	}
	for _, h := range recalcHooks {
		h(p, d, &m)
	}
	// Percentages apply on top of base + flat bonuses.
	p.MaxHP = (c.HPBase + m.FlatHP) * (100 + m.HPPct) / 100
	p.MaxMP = (c.MPBase + m.FlatMP) * (100 + m.MPPct) / 100
	p.Dmg = (c.DmgBase + m.FlatDm) * (100 + m.DmgPct) / 100
	m.Armor, m.Dodge = min(m.Armor, maxArmor), min(m.Dodge, maxDodge)
	p.Mods = m
	c.HP = min(c.HP, p.MaxHP)
	c.MP = min(c.MP, p.MaxMP)
}

// Power limit tiers (NPoint.getPowerLimit and friends), indexed by limit level.
var powerLimits = []struct {
	Power     int64
	HPMP, Dmg int64
	Def, Crit int64
}{
	{17999999999, 220000, 11000, 550, 1},
	{19999999999, 240000, 12000, 600, 2},
	{24999999999, 300000, 15000, 700, 3},
	{29999999999, 350000, 18000, 800, 4},
	{39999999999, 400000, 20000, 1000, 5},
	{50010000000, 450000, 22000, 1200, 6},
	{60010000000, 500000, 24000, 1400, 7},
	{70010000000, 525000, 24500, 1500, 8},
	{80010000000, 550000, 25000, 1600, 9},
	{90010000000, 575000, 26000, 1800, 10},
}

// Stat kinds for add_point.
const (
	StatHP = iota
	StatMP
	StatDmg
	StatDef
	StatCrit
)

// PointCost returns the potential needed to add n points to a stat, and the
// resulting base value. ok is false when the power limit would be exceeded.
// Formulas follow NPoint.increasePoint; the legacy def cost ignored n, which
// is fixed here by charging per point.
func PointCost(c *store.Character, stat int, n int64, limitLevel int) (cost, newValue int64, ok bool) {
	lim := powerLimits[min(max(limitLevel, 0), len(powerLimits)-1)]
	switch stat {
	case StatHP, StatMP:
		cur := c.HPBase
		if stat == StatMP {
			cur = c.MPBase
		}
		add := n * 20
		return n * (2*(cur+1000) + add - 20) / 2, cur + add, cur+add <= lim.HPMP
	case StatDmg:
		return n * (2*c.DmgBase + n - 1) / 2 * 100, c.DmgBase + n, c.DmgBase+n <= lim.Dmg
	case StatDef:
		for i := range n {
			cost += (c.DefBase + i + 5) * 100000
		}
		return cost, c.DefBase + n, c.DefBase+n <= lim.Def
	case StatCrit:
		cost = 50_000_000
		for range c.CritBase {
			cost *= 5
		}
		return cost, int64(c.CritBase) + 1, int64(c.CritBase)+1 <= lim.Crit && n == 1
	}
	return 0, 0, false
}
