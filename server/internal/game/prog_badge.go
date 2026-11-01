package game

import (
	"encoding/json"
	"sort"
	"strconv"
	"time"
)

// Badges/titles (player_badges/*, task/BadgesTask*.java): daily badge tasks
// (task_badge_template) unlock a badge (table badge) for 30 days; one badge is
// active and its options apply like worn items.

type progBadgeState struct {
	Day    string        `json:"day"`
	Count  map[int]int   `json:"count"`  // badge task id -> progress
	Owned  map[int]int64 `json:"owned"`  // badge effect id -> expires (unix)
	Active int           `json:"active"` // effect id, 0 none
}

func (w *World) progBadges(p *Player) *progBadgeState {
	s := State[progBadgeState](p, "badge")
	if s.Count == nil {
		s.Count = map[int]int{}
	}
	if s.Owned == nil {
		s.Owned = map[int]int64{}
	}
	if t := progToday(); s.Day != t { // PlayerService.dailyLogin -> createAndResetTask
		s.Day = t
		s.Count = map[int]int{}
	}
	return s
}

// progBadgeAdd advances every badge task hooked on event `on`.
func (w *World) progBadgeAdd(p *Player, on, boss string, n int) {
	pd := progOf(w.Data)
	if pd == nil {
		return
	}
	var s *progBadgeState
	for _, bt := range pd.BadgeTasks {
		h, ok := pd.C.Badges.Tasks[strconv.Itoa(bt.ID)]
		if !ok || h.On != on || (on == "boss" && !w.progBossMatch(boss, h.Bosses)) {
			continue
		}
		if s == nil {
			s = w.progBadges(p)
		}
		s.Count[bt.ID] = min(s.Count[bt.ID]+n, bt.Max) // updateCountBagesTask clamps
		p.dirty = true
		if s.Count[bt.ID] < bt.Max {
			continue
		}
		// BadgesTaskService.updateDoneTask: unlock for 30 days and wear it.
		b := pd.badgeForReward(bt.Reward)
		if b == nil {
			continue
		}
		if exp, owned := s.Owned[b.Effect]; owned && exp > time.Now().Unix() {
			continue
		}
		s.Owned[b.Effect] = time.Now().Add(time.Duration(max(pd.C.Badges.Days, 1)) * 24 * time.Hour).Unix()
		s.Active = b.Effect
		s.Count[bt.ID] = 0
		notice(p, "Bạn nhận được danh hiệu "+b.Name)
		p.Recalc(w.Data)
		p.Send("self", p.Self())
	}
}

func (w *World) progBadgeEvent(e Event) {
	switch e.Kind {
	case EvKillBoss:
		w.progBadgeAdd(e.Player, "boss", e.Boss, 1)
	case EvPickup:
		w.progBadgeAdd(e.Player, "pickup", "", 1)
	}
}

type progBadgeView struct {
	Effect   int    `json:"id"`
	Item     int    `json:"item"`
	Name     string `json:"name"`
	Owned    bool   `json:"owned"`
	Days     int    `json:"days"`
	Active   bool   `json:"active"`
	Task     string `json:"task"`
	Progress int    `json:"progress"`
	Max      int    `json:"max"`
}

func (w *World) progSendBadges(p *Player) {
	pd := progOf(w.Data)
	s := w.progBadges(p)
	now := time.Now().Unix()
	byEffect := map[int]*progBadgeView{}
	var out []*progBadgeView
	for _, b := range pd.Badges {
		v := &progBadgeView{Effect: b.Effect, Item: b.Item, Name: b.Name}
		if exp := s.Owned[b.Effect]; exp > now {
			v.Owned, v.Days, v.Active = true, int((exp-now)/86400), s.Active == b.Effect
		}
		byEffect[b.Effect] = v
		out = append(out, v)
	}
	for _, bt := range pd.BadgeTasks {
		if b := pd.badgeForReward(bt.Reward); b != nil && byEffect[b.Effect] != nil {
			v := byEffect[b.Effect]
			v.Task, v.Progress, v.Max = bt.Name, s.Count[bt.ID], bt.Max
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Effect < out[j].Effect })
	p.Send("badges", map[string]any{"list": nonNil(out)})
}

func init() {
	// Active badge options apply like item options (NPoint.java:244 sendListItemOption).
	recalcHooks = append(recalcHooks, func(p *Player, d *Data, m *Mods) {
		pd := progOf(d)
		if pd == nil {
			return
		}
		s := progPeek[progBadgeState](p, "badge")
		if s == nil || s.Active == 0 || s.Owned[s.Active] <= time.Now().Unix() {
			return
		}
		if b := pd.Badges[s.Active]; b != nil {
			for _, o := range b.Options {
				if f := optionEffects[o[0]]; f != nil {
					f(p, m, int64(o[1]))
				}
			}
		}
	})
	menuActions["badge_menu"] = func(z *Zone, p *Player, _ string) { z.World.progSendBadges(p) }
	register("badge_list", func(z *Zone, p *Player, _ json.RawMessage) {
		if progOf(z.World.Data) != nil {
			z.World.progSendBadges(p)
		}
	})
	register("badge_use", func(z *Zone, p *Player, d json.RawMessage) { // BadgesService.turnOnBadges
		w := z.World
		var r struct{ ID int }
		if progOf(w.Data) == nil || json.Unmarshal(d, &r) != nil {
			return
		}
		s := w.progBadges(p)
		if s.Owned[r.ID] <= time.Now().Unix() {
			notice(p, "Bạn chưa sở hữu danh hiệu này")
			return
		}
		s.Active = r.ID
		p.dirty = true
		z.afterGearChange(p)
		w.progSendBadges(p)
	})
	register("badge_off", func(z *Zone, p *Player, _ json.RawMessage) {
		w := z.World
		if progOf(w.Data) == nil {
			return
		}
		w.progBadges(p).Active = 0
		p.dirty = true
		z.afterGearChange(p)
		w.progSendBadges(p)
	})
}
