package game

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"strconv"
	"strings"
	"sync/atomic"
)

// Daily side tasks (Bò Mộng, TaskService.java:1245-1469, SideTask.java,
// BoMong.java) and clan tasks (TaskService.java:1471-1649, ClanTask.java,
// DrDrief.java). Both pick a random template from a 59-row table whose index
// maps to one mob (or, for the last row, gold picked up).

type progDaily struct {
	Has      bool   `json:"has"`
	Tmpl     int    `json:"tmpl"`
	Count    int64  `json:"count"`
	Max      int64  `json:"max"`
	Level    int    `json:"level"`
	Left     int    `json:"left"`
	Day      string `json:"day"`
	Notified int    `json:"notified"` // last 10% step announced
}

// renew restores the daily quota after midnight (SideTask.renew).
func (s *progDaily) renew(maxPerDay int) {
	if t := progToday(); s.Day != t {
		s.Day, s.Left = t, maxPerDay
	}
}

func (s *progDaily) reset() {
	s.Has, s.Tmpl, s.Count, s.Max, s.Level, s.Notified = false, 0, 0, 0, 0, -1
}

func (s *progDaily) pct() int64 {
	if s.Max <= 0 || s.Count >= s.Max {
		return 100
	}
	return s.Count * 100 / s.Max
}

func (s *progDaily) name(tmpls []progTemplate) string {
	if !s.Has || s.Tmpl < 0 || s.Tmpl >= len(tmpls) {
		return "Hiện tại không có nhiệm vụ nào"
	}
	return strings.ReplaceAll(tmpls[s.Tmpl].Name, "%1", strconv.FormatInt(s.Max, 10))
}

// take assigns a random template at level (TaskService.changeSideTask).
func (s *progDaily) take(tmpls []progTemplate, level int) bool {
	if len(tmpls) == 0 {
		return false
	}
	s.reset()
	t := tmpls[rand.IntN(len(tmpls))]
	lo, hi := 1, 1
	if level < len(t.Min) && level < len(t.Max) {
		lo, hi = t.Min[level], max(t.Max[level], t.Min[level])
	}
	s.Has, s.Tmpl, s.Level = true, t.ID, level
	s.Max = int64(lo + rand.IntN(hi-lo+1))
	return true
}

// progress applies a kill/pickup event; returns true if it counted.
func (s *progDaily) progress(c *progContent, d *Data, e Event) bool {
	if !s.Has || s.Count >= s.Max {
		return false
	}
	switch e.Kind {
	case EvKillMob:
		if s.Tmpl >= 0 && s.Tmpl < len(c.Side.Mobs) && s.Tmpl != c.Side.GoldTemplate && c.Side.Mobs[s.Tmpl] == e.Mob {
			s.Count++
			return true
		}
	case EvPickup: // template 58: gold picked up (TaskService.java:1417)
		if t := d.ItemsFull[e.Item]; s.Tmpl == c.Side.GoldTemplate && t != nil && t.Type == ItemGold {
			s.Count += int64(max(e.Count, 1))
			return true
		}
	}
	return false
}

// announce mirrors notifyProcessSideTask: once per 10% step, then the done message.
func (s *progDaily) announce(p *Player, name, done string) {
	pct := s.pct()
	if pct >= 100 {
		if s.Notified < 10 {
			s.Notified = 10
			notice(p, done)
		}
		return
	}
	if step := int(pct / 10); step > s.Notified {
		s.Notified = step
		notice(p, "Nhiệm vụ: "+name+" đã hoàn thành: "+strconv.FormatInt(s.Count, 10)+"/"+strconv.FormatInt(s.Max, 10)+
			" ("+strconv.FormatInt(pct, 10)+"%)")
	}
}

func (w *World) progSide(p *Player) *progDaily {
	s := State[progDaily](p, "sidetask")
	s.renew(progOf(w.Data).C.Side.MaxPerDay)
	return s
}

func (w *World) progClanTask(p *Player) *progDaily {
	s := State[progDaily](p, "clantask")
	s.renew(progOf(w.Data).C.ClanTask.MaxPerDay)
	return s
}

func (w *World) progDailyEvent(e Event) {
	pd := progOf(w.Data)
	p := e.Player
	if e.Kind != EvKillMob && e.Kind != EvPickup {
		return
	}
	if s := progPeek[progDaily](p, "sidetask"); s != nil && s.progress(pd.C, w.Data, e) {
		p.dirty = true
		s.announce(p, s.name(pd.Side), "Chúc mừng bạn đã hoàn thành nhiệm vụ, bây giờ hãy quay về Bò Mộng trả nhiệm vụ.")
	}
	if s := progPeek[progDaily](p, "clantask"); s != nil && s.progress(pd.C, w.Data, e) {
		p.dirty = true
		s.announce(p, s.name(pd.Clan), "Tiếp theo hãy về Bang hội báo cáo.")
	}
}

type progDailyView struct {
	Active    bool   `json:"active"`
	Name      string `json:"name"`
	Level     int    `json:"level"`
	LevelName string `json:"level_name"`
	Count     int64  `json:"count"`
	Max       int64  `json:"max"`
	Pct       int64  `json:"pct"`
	Done      bool   `json:"done"`
	Left      int    `json:"left"`
	PerDay    int    `json:"per_day"`
	Levels    []int  `json:"levels,omitempty"`
	Points    int64  `json:"points,omitempty"`
}

func (w *World) progSendSide(p *Player) {
	pd := progOf(w.Data)
	s := w.progSide(p)
	v := progDailyView{Active: s.Has, Name: s.name(pd.Side), Level: s.Level, Count: s.Count, Max: s.Max, Pct: s.pct(),
		Done: s.Has && s.Count >= s.Max, Left: s.Left, PerDay: pd.C.Side.MaxPerDay, Levels: pd.C.Side.OfferLevels}
	if s.Level < len(pd.C.Side.LevelNames) {
		v.LevelName = pd.C.Side.LevelNames[s.Level]
	}
	p.Send("side_task", v)
}

func (w *World) progSendClanTask(p *Player) {
	pd := progOf(w.Data)
	s := w.progClanTask(p)
	v := progDailyView{Active: s.Has, Name: s.name(pd.Clan), Level: s.Level, Count: s.Count, Max: s.Max, Pct: s.pct(),
		Done: s.Has && s.Count >= s.Max, Left: s.Left, PerDay: pd.C.ClanTask.MaxPerDay}
	if c := w.clan(p.Char.ClanID); c != nil {
		w.clans.mu.Lock()
		v.Points = c.Points
		w.clans.mu.Unlock()
	}
	p.Send("ctask", v)
}

func (w *World) progNearSide(z *Zone, p *Player) bool {
	c := progOf(w.Data).C.Side
	if !containsInt(c.Maps, z.Map.ID) || !progNear(z, p, c.Npc) {
		notice(p, "Hãy đến gặp Bò Mộng")
		return false
	}
	return true
}

func (w *World) progNearClanNpc(z *Zone, p *Player) bool {
	for _, n := range progOf(w.Data).C.ClanTask.Npcs {
		if z.Map.ID == n.Map && progNear(z, p, n.Npc) {
			return true
		}
	}
	notice(p, "Hãy đến gặp người giao nhiệm vụ bang")
	return false
}

// progHellBonus: the legacy server gave the Christmas tree once per server run
// (TaskService.canNhanCayThong).
var progHellBonus atomic.Bool

func (w *World) progSidePay(p *Player) {
	pd := progOf(w.Data)
	c := pd.C.Side
	s := w.progSide(p)
	if !s.Has {
		return
	}
	if s.Count < s.Max {
		notice(p, "Bạn chưa hoàn thành nhiệm vụ")
		return
	}
	if progFreeSlots(p) <= 1 { // TaskService.java:1312
		notice(p, "Hành trang không đủ chỗ trống.")
		return
	}
	lv := min(max(s.Level, 0), len(c.Gold)-1)
	if lv == 4 && c.HellBonusItem > 0 && s.Left < 15 && progHellBonus.CompareAndSwap(false, true) {
		opts := [][2]int{}
		if rand.IntN(2) == 0 {
			opts = append(opts, [2]int{11 + rand.IntN(3), 100})
		}
		opts = append(opts, [2]int{24, 0}, [2]int{110, 118 + rand.IntN(9)}, [2]int{110, 110 + rand.IntN(4)}, [2]int{93, 30})
		p.AddItem(w.Data, c.HellBonusItem, 1, opts)
		notice(p, "Bạn nhận được "+progItemName(w.Data, c.HellBonusItem))
	} else if lv < len(c.Items) {
		p.AddItem(w.Data, c.Items[lv], 1, append([][2]int(nil), c.ItemOptions...))
		notice(p, "Bạn nhận được "+progItemName(w.Data, c.Items[lv]))
	}
	gold := c.Gold[lv]
	p.Char.Gold = min(p.Char.Gold+gold, maxGold)
	notice(p, "Bạn nhận được "+strconv.FormatInt(gold, 10)+" vàng")
	s.reset()
	p.dirty = true
	w.progBadgeAdd(p, "side_pay", "", 1) // NONG_DAN_CHAM_CHI (counted once, legacy counted 2-3 times)
	p.sendInventory()
	p.Send("self", p.Self())
	w.progSendSide(p)
}

// progClanPoints adds clan points in memory and persists them asynchronously.
func (w *World) progClanPoints(p *Player, clanID, n int64) {
	c := w.clan(clanID)
	if c == nil {
		return
	}
	w.clans.mu.Lock()
	c.Points += n
	w.clans.mu.Unlock()
	if w.Store != nil {
		w.async(p, func(ctx context.Context) error { return w.Store.ProgAddClanPoints(ctx, clanID, n) },
			func(*Zone, error) {})
	}
}

func init() {
	npcOptions = append(npcOptions, func(z *Zone, p *Player, npc int) []MenuOption {
		pd := progOf(z.World.Data)
		if pd == nil {
			return nil
		}
		var out []MenuOption
		if npc == pd.C.Side.Npc && containsInt(pd.C.Side.Maps, z.Map.ID) { // BoMong.java:33-35
			out = append(out, MenuOption{Label: "Nhiệm vụ hàng ngày", Action: "side_menu"},
				MenuOption{Label: "Nhiệm vụ thành tích", Action: "ach_menu"},
				MenuOption{Label: "Danh hiệu", Action: "badge_menu"})
		}
		if p.Char.ClanID != 0 {
			for _, n := range pd.C.ClanTask.Npcs {
				if n.Npc == npc && n.Map == z.Map.ID { // DrDrief.java:46
					s := z.World.progClanTask(p)
					out = append(out, MenuOption{Label: "Nhiệm vụ bang [" + itoa(s.Left) + "/" + itoa(pd.C.ClanTask.MaxPerDay) + "]", Action: "ctask_menu"})
				}
			}
		}
		return out
	})
	menuActions["side_menu"] = func(z *Zone, p *Player, _ string) { z.World.progSendSide(p) }
	menuActions["ctask_menu"] = func(z *Zone, p *Player, _ string) { z.World.progSendClanTask(p) }

	register("side_info", func(z *Zone, p *Player, _ json.RawMessage) {
		if progOf(z.World.Data) != nil {
			z.World.progSendSide(p)
		}
	})
	register("side_take", func(z *Zone, p *Player, d json.RawMessage) {
		w := z.World
		pd := progOf(w.Data)
		var r struct{ Level int }
		if pd == nil || json.Unmarshal(d, &r) != nil || !containsInt(pd.C.Side.OfferLevels, r.Level) || !w.progNearSide(z, p) {
			return
		}
		s := w.progSide(p)
		if s.Has {
			notice(p, "Bạn đang có nhiệm vụ: "+s.name(pd.Side))
			return
		}
		if s.Left <= 0 {
			notice(p, "Bạn đã nhận hết nhiệm vụ hôm nay. Hãy chờ tới ngày mai rồi nhận tiếp")
			return
		}
		if s.take(pd.Side, r.Level) {
			s.Left-- // TaskService.java:1260
			p.dirty = true
			notice(p, "Bạn nhận được nhiệm vụ: "+s.name(pd.Side))
		}
		w.progSendSide(p)
	})
	register("side_pay", func(z *Zone, p *Player, _ json.RawMessage) {
		if progOf(z.World.Data) != nil && z.World.progNearSide(z, p) {
			z.World.progSidePay(p)
		}
	})
	register("side_cancel", func(z *Zone, p *Player, _ json.RawMessage) {
		w := z.World
		if progOf(w.Data) == nil || !w.progNearSide(z, p) {
			return
		}
		s := w.progSide(p)
		if s.Has {
			notice(p, "Bạn vừa hủy bỏ nhiệm vụ "+s.name(progOf(w.Data).Side))
			s.reset() // TaskService.removeSideTask: the daily quota is not refunded
			p.dirty = true
		}
		w.progSendSide(p)
	})

	register("ctask_info", func(z *Zone, p *Player, _ json.RawMessage) {
		if progOf(z.World.Data) != nil {
			z.World.progSendClanTask(p)
		}
	})
	register("ctask_take", func(z *Zone, p *Player, _ json.RawMessage) {
		w := z.World
		pd := progOf(w.Data)
		if pd == nil || p.Char.ClanID == 0 || !w.progNearClanNpc(z, p) {
			return
		}
		s := w.progClanTask(p)
		switch {
		case s.Has:
		case s.Left <= 0:
			notice(p, "Đã hết nhiệm vụ cho hôm nay, hãy chờ đến ngày mai")
		default:
			s.take(pd.Clan, rand.IntN(max(pd.C.ClanTask.Levels, 1))) // DrDrief.java:90 random level 0..4
			p.dirty = true
			notice(p, "Nhiệm vụ hiện tại: "+s.name(pd.Clan))
		}
		w.progSendClanTask(p)
	})
	register("ctask_pay", func(z *Zone, p *Player, _ json.RawMessage) {
		w := z.World
		pd := progOf(w.Data)
		if pd == nil || !w.progNearClanNpc(z, p) {
			return
		}
		s := w.progClanTask(p)
		if !s.Has {
			return
		}
		if s.Count < s.Max {
			notice(p, "Bạn chưa hoàn thành nhiệm vụ")
			return
		}
		pts := int64(s.Level+1) * pd.C.ClanTask.PointsPerLevel // TaskService.java:1503
		s.Left--
		s.reset()
		p.dirty = true
		notice(p, "Bạn vừa nhận được "+strconv.FormatInt(pts, 10)+" capsule bang.")
		if p.Char.ClanID != 0 {
			w.progClanPoints(p, p.Char.ClanID, pts)
		}
		w.progSendClanTask(p)
	})
	register("ctask_cancel", func(z *Zone, p *Player, _ json.RawMessage) {
		w := z.World
		if progOf(w.Data) == nil || !w.progNearClanNpc(z, p) {
			return
		}
		s := w.progClanTask(p)
		if s.Has && s.Count < s.Max {
			notice(p, "Đã hủy nhiệm vụ bang.")
			s.Left-- // TaskService.java:1496
			s.reset()
			p.dirty = true
		}
		w.progSendClanTask(p)
	})
}
