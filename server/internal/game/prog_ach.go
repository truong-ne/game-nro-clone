package game

import (
	"encoding/json"
	"strconv"
	"strings"
)

// Achievements from achievement_template (AchievementService.java,
// player/Achievement.java). Progress sources are declared per index in
// prog_content.json; claiming pays `money` gems.

type progAchState struct {
	Count   map[int]int64 `json:"count"` // idx -> progress (play time in seconds)
	Claimed map[int]bool  `json:"claimed"`
}

func progAchOf(p *Player) *progAchState {
	s := State[progAchState](p, "ach")
	if s.Count == nil {
		s.Count = map[int]int64{}
	}
	if s.Claimed == nil {
		s.Claimed = map[int]bool{}
	}
	return s
}

// progAchValue is Achievement.getCompleted: some entries are derived live.
func (w *World) progAchValue(p *Player, s *progAchState, idx int) int64 {
	switch progOf(w.Data).C.Achievements[strconv.Itoa(idx)].On {
	case "power":
		return p.Char.Power
	case "tree":
		return int64(p.Char.Extra.MagicTree.Level)
	case "play_hours":
		return s.Count[idx] / 3600
	}
	return s.Count[idx]
}

func (w *World) progAchAdd(p *Player, match func(a progAchSpec) bool, n int64) {
	pd := progOf(w.Data)
	var s *progAchState
	for k, a := range pd.C.Achievements {
		if !match(a) {
			continue
		}
		if s == nil {
			s = progAchOf(p)
		}
		idx, _ := strconv.Atoi(k)
		s.Count[idx] += n
		p.dirty = true
	}
}

func (w *World) progAchEvent(e Event) {
	switch e.Kind {
	case EvKillMob: // AchievementService.checkDoneTaskKillMob
		mt := -1
		if m := w.Data.Mobs[e.Mob]; m != nil {
			mt = m.Type
		}
		w.progAchAdd(e.Player, func(a progAchSpec) bool {
			return (a.On == "kill_type" && a.Value == mt) || (a.On == "kill_mob" && a.Value == e.Mob)
		}, 1)
	case EvKillBoss: // TaskService.java:396 TRUM_KET_LIEU_BOSS
		w.progAchAdd(e.Player, func(a progAchSpec) bool { return a.On == "kill_boss" }, 1)
	case EvPickup: // TaskService.java:358 TRUM_NHAT_NGOC, +1 per pickup
		w.progAchAdd(e.Player, func(a progAchSpec) bool { return a.On == "pickup_item" && a.Value == e.Item }, 1)
	}
}

// progAchTick counts play time (HOAT_DONG_CHAM_CHI).
func (w *World) progAchTick(p *Player) {
	w.progAchAdd(p, func(a progAchSpec) bool { return a.On == "play_hours" }, 1)
}

type progAchView struct {
	I       int    `json:"i"`
	Name    string `json:"name"`
	Info    string `json:"info"`
	Money   int64  `json:"money"`
	Count   int64  `json:"count"`
	Max     int64  `json:"max"`
	Done    bool   `json:"done"`
	Claimed bool   `json:"claimed"`
}

// progAchText is AchievementService.regex.
func progAchText(gender int, s string) string {
	g := min(max(gender, 0), 2)
	s = strings.ReplaceAll(s, "%1", []string{"Siêu nhân", "Siêu Namếc", "Siêu Xayda"}[g])
	return strings.ReplaceAll(s, "%2", []string{"Bunma", "Dende", "Appule"}[g])
}

func (w *World) progSendAch(p *Player) {
	pd := progOf(w.Data)
	s := progAchOf(p)
	out := make([]progAchView, 0, len(pd.Achs))
	for i, a := range pd.Achs {
		v := w.progAchValue(p, s, i)
		out = append(out, progAchView{I: i, Name: a.Info1, Info: progAchText(p.Char.Gender, a.Info2), Money: a.Money,
			Count: min(v, a.Max), Max: a.Max, Done: s.Claimed[i] || v >= a.Max, Claimed: s.Claimed[i]})
	}
	p.Send("achievements", map[string]any{"list": out})
}

func init() {
	menuActions["ach_menu"] = func(z *Zone, p *Player, _ string) { z.World.progSendAch(p) }
	register("ach_list", func(z *Zone, p *Player, _ json.RawMessage) {
		if progOf(z.World.Data) != nil {
			z.World.progSendAch(p)
		}
	})
	register("ach_claim", func(z *Zone, p *Player, d json.RawMessage) { // AchievementService.confirmAchievement
		w := z.World
		pd := progOf(w.Data)
		var r struct{ I int }
		if pd == nil || json.Unmarshal(d, &r) != nil || r.I < 0 || r.I >= len(pd.Achs) {
			return
		}
		s := progAchOf(p)
		a := pd.Achs[r.I]
		switch {
		case s.Claimed[r.I]:
			notice(p, "Bạn đã nhận thưởng thành tích này")
		case w.progAchValue(p, s, r.I) < a.Max:
			notice(p, "Chưa hoàn thành thành tích")
		case progFreeSlots(p) <= 0:
			notice(p, "Cần tối thiểu 1 ô trống hành trang để nhận thưởng")
		default:
			s.Claimed[r.I] = true
			p.Char.Gem += a.Money
			p.dirty = true
			notice(p, "Bạn vừa nhận được "+strconv.FormatInt(a.Money, 10)+" ngọc.")
			p.Send("self", p.Self())
		}
		w.progSendAch(p)
	})
}
