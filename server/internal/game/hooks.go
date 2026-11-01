package game

import (
	"context"
	"encoding/json"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Extension points. Feature files register into these from init(), so each
// system (buffs, combine, side tasks, tournaments...) lives in its own file
// without editing the core. All hooks run on the zone goroutine unless noted.

// AttackCtx describes one skill use for attack hooks.
type AttackCtx struct {
	Tmpl   *SkillTemplate
	Level  *SkillLevel
	Mob    *Mob    // target mob, if any
	Target *Player // target player, if any
	Boss   *Boss   // target boss, if any
}

var (
	// dataHooks load extra data at startup (after templates and content.json).
	dataHooks []func(ctx context.Context, pool *pgxpool.Pool, d *Data) error
	// worldStartHooks run once when the world starts (may spawn goroutines).
	worldStartHooks []func(w *World)
	// joinHooks run when a player logs in, before entering a zone (not on a zone goroutine).
	joinHooks []func(w *World, p *Player)
	// enterHooks run after a player entered a zone.
	enterHooks []func(z *Zone, p *Player)
	// recalcHooks add stats after worn items and before percentages apply.
	recalcHooks []func(p *Player, d *Data, m *Mods)
	// skillHooks may fully handle a skill (special effects); return true if handled.
	skillHooks []func(z *Zone, p *Player, a *AttackCtx) bool
	// outDamageHooks adjust damage dealt by p (buffs, charms, intrinsics).
	outDamageHooks []func(p *Player, dmg int64, a *AttackCtx) int64
	// inDamageHooks adjust damage taken by p (shields, charms); return the new value.
	inDamageHooks []func(p *Player, dmg int64, src DamageSrc) int64
	// deathHooks run when a player dies; killer is nil for mobs/bosses.
	deathHooks []func(z *Zone, victim, killer *Player)
	// canActHooks may forbid moving/attacking (stun, sleep, hold).
	canActHooks []func(p *Player) bool
	// tickHooks run every zone tick; secondHooks once per second per active zone.
	tickHooks   []func(z *Zone, now time.Time)
	secondHooks []func(z *Zone, now time.Time)
	// powerHooks scale power/potential gains (charms, events); return new value.
	powerHooks []func(p *Player, n int64) int64
	// useByID / useByType handle usable items; return true if handled.
	useByID   = map[int]func(z *Zone, p *Player, slot int, it *store.Item) bool{}
	useByType = map[int]func(z *Zone, p *Player, slot int, it *store.Item) bool{}
	// menuActions handle NPC menu actions declared in content files.
	menuActions = map[string]func(z *Zone, p *Player, arg string){}
	// npcOptions add menu entries for an NPC template id.
	npcOptions []func(z *Zone, p *Player, npc int) []MenuOption
)

// DamageSrc says who dealt damage; at most one field is set (none = environment).
type DamageSrc struct {
	Mob    *Mob
	Player *Player
	Boss   *Boss
}

// FromMonster reports a hit by a mob or boss (charms like da trâu only cover these).
func (s DamageSrc) FromMonster() bool { return s.Mob != nil || s.Boss != nil }

func canAct(p *Player) bool {
	for _, f := range canActHooks {
		if !f(p) {
			return false
		}
	}
	return true
}

// State returns the feature state stored under key for p, loading it from
// character.extra on first use. The returned pointer is saved automatically
// with the character. T must be a JSON-serializable struct.
func State[T any](p *Player, key string) *T {
	if v, ok := p.persist[key]; ok {
		return v.(*T)
	}
	v := new(T)
	if raw, ok := p.Char.Extra.Feat[key]; ok {
		json.Unmarshal(raw, v)
	}
	if p.persist == nil {
		p.persist = map[string]any{}
	}
	p.persist[key] = v
	return v
}

// flushState writes feature state back into Extra before a snapshot.
func (p *Player) flushState() {
	if len(p.persist) == 0 {
		return
	}
	if p.Char.Extra.Feat == nil {
		p.Char.Extra.Feat = map[string]json.RawMessage{}
	}
	for k, v := range p.persist {
		if b, err := json.Marshal(v); err == nil {
			p.Char.Extra.Feat[k] = b
		}
	}
}
