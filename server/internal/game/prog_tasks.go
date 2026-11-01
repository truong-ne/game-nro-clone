package game

import (
	"strconv"
	"time"
)

// Main tasks 7..29: custom triggers the core inference cannot express
// (power thresholds, per-race NPCs/mobs, bosses, clan membership, pickups)
// and the legacy per-subtask and per-task rewards (TaskService.java).

func init() {
	joinHooks = append(joinHooks, func(w *World, p *Player) {
		p.Tmp["prog_task"] = progMemo{p.Char.TaskID, p.Char.TaskIndex}
	})
	secondHooks = append(secondHooks, func(z *Zone, now time.Time) {
		w := z.World
		if progOf(w.Data) == nil {
			return
		}
		for _, p := range z.players {
			w.progCheckState(p, z)
			w.progWatch(p)
			w.progAchTick(p)
		}
		w.progDragonTick(now)
	})
}

// progInstall subscribes the progression systems to the event bus. It runs
// from worldStartHooks, after the core task listeners, so the watcher sees
// the subtask the core listeners just advanced.
func (w *World) progInstall() {
	if progOf(w.Data) == nil {
		return
	}
	for _, k := range []EventKind{EvKillMob, EvKillBoss, EvTalkNpc, EvEnterMap, EvPickup, EvAddPoint} {
		w.Events.On(k, w.progEvent)
	}
}

func (w *World) progEvent(e Event) {
	p := e.Player
	w.progMainEvent(e)
	w.progCheckState(p, e.Zone)
	w.progDailyEvent(e)
	w.progBadgeEvent(e)
	w.progAchEvent(e)
	w.progWatch(p)
}

func (w *World) progCur(p *Player) (*TaskSub, *progSub) {
	pd := progOf(w.Data)
	if pd == nil {
		return nil, nil
	}
	_, s := w.taskFor(p)
	if s == nil {
		return nil, nil
	}
	return s, pd.subs[s]
}

func progGenderList(list []int, gender int, perGender bool) []int {
	if perGender && len(list) == 3 && gender >= 0 && gender < 3 {
		return list[gender : gender+1]
	}
	return list
}

func (w *World) progMainEvent(e Event) {
	p := e.Player
	s, sp := w.progCur(p)
	if sp == nil {
		return
	}
	is := func(x *TaskSub) bool { return x == s }
	switch sp.Kind {
	case "p_kill":
		if e.Kind != EvKillMob {
			return
		}
		mobs := sp.Mobs
		if len(sp.GMobs) == 3 && p.Char.Gender < 3 {
			mobs = sp.GMobs[p.Char.Gender]
		}
		if !containsInt(mobs, e.Mob) {
			return
		}
		if sp.ClanOnly && p.Char.ClanID == 0 { // TaskService.java:514
			return
		}
		n := 1
		if sp.ClanDouble > 0 && e.Zone != nil { // x2 with clan mates in the zone (TaskService.java:522)
			mates := 0
			for _, o := range e.Zone.players {
				if o.Char.ClanID == p.Char.ClanID {
					mates++
				}
			}
			if mates >= sp.ClanDouble {
				n = 2
			}
		}
		if sp.Drop > 0 && e.Zone != nil { // Mob.java:621 drops the 7-star ball
			e.Zone.dropItem(sp.Drop, 1, nil, p.Char.X, p.Char.Y, p.ID())
		}
		w.progress(p, is, n)
	case "p_talk":
		if e.Kind != EvTalkNpc || !containsInt(progGenderList(sp.Npcs, p.Char.Gender, sp.PerGender), e.Npc) {
			return
		}
		if sp.OnMap != nil && (e.Zone == nil || e.Zone.Map.ID != *sp.OnMap) {
			return
		}
		w.progress(p, is, 1<<20)
	case "p_boss":
		if e.Kind == EvKillBoss && w.progBossMatch(e.Boss, sp.Bosses) {
			w.progress(p, is, 1)
		}
	case "p_pick":
		if e.Kind == EvPickup && e.Item == sp.Item {
			w.progress(p, is, 1) // one per pickup (TaskService.java:354)
		}
	}
}

// progCheckState completes state-based subtasks (power, damage, clan size);
// several may complete at once, as in checkDoneTaskPower.
func (w *World) progCheckState(p *Player, z *Zone) {
	for range 8 {
		s, sp := w.progCur(p)
		if sp == nil {
			return
		}
		ok := false
		switch sp.Kind {
		case "p_power":
			ok = p.Char.Power >= sp.Value
		case "p_dmg":
			ok = p.Char.DmgBase >= sp.Value
		case "p_clan":
			if c := w.clan(p.Char.ClanID); c != nil {
				w.clans.mu.Lock()
				ok = int64(len(c.Members)) >= sp.Value
				w.clans.mu.Unlock()
			}
		}
		if !ok {
			return
		}
		w.progress(p, func(x *TaskSub) bool { return x == s }, 1<<20)
	}
}

type progMemo struct{ ID, Idx int }

// progWatch detects advanced subtasks/tasks and pays the legacy rewards the
// core advanceTask does not give.
func (w *World) progWatch(p *Player) {
	cur := progMemo{p.Char.TaskID, p.Char.TaskIndex}
	old, ok := p.Tmp["prog_task"].(progMemo)
	p.Tmp["prog_task"] = cur
	if !ok || old == cur {
		return
	}
	switch {
	case cur.ID != old.ID:
		n := 0
		for _, t := range w.Data.Tasks {
			if t.ID == old.ID {
				n = len(t.Subs)
			}
		}
		for i := old.Idx; i < n; i++ {
			w.progSubReward(p, old.ID, i)
		}
		w.progTaskReward(p, old.ID)
	case cur.Idx > old.Idx:
		for i := old.Idx; i < cur.Idx; i++ {
			w.progSubReward(p, old.ID, i)
		}
	}
}

// progSubReward: items given when a subtask completes (TaskService.java:745 capsules, :783 skill book).
func (w *World) progSubReward(p *Player, task, sub int) {
	pd := progOf(w.Data)
	sent := false
	for _, r := range pd.C.Tasks.SubRewards[strconv.Itoa(task)+"."+strconv.Itoa(sub)] {
		id := r.Item
		if len(r.ByGender) == 3 && p.Char.Gender < 3 {
			id = r.ByGender[p.Char.Gender]
		}
		if p.AddItem(w.Data, id, max(r.Qty, 1), nil) {
			notice(p, "Bạn nhận được "+progItemName(w.Data, id))
			sent = true
		}
	}
	if sent {
		p.sendInventory()
	}
}

// progTaskReward tops the core reward up to the legacy gold (TaskService.java:1112-1115:
// tasks 5..24 give 500000 gold).
func (w *World) progTaskReward(p *Player, task int) {
	lg := progOf(w.Data).C.Tasks.LegacyGold
	if task < lg.From || task > lg.To {
		return
	}
	extra := lg.Gold - w.Data.Content.Tasks.RewardGold*int64(task+1)
	if extra <= 0 {
		return
	}
	p.Char.Gold = min(p.Char.Gold+extra, maxGold)
	p.dirty = true
	notice(p, "Thưởng nhiệm vụ: +"+strconv.FormatInt(extra, 10)+" vàng")
	p.Send("self", p.Self())
}
