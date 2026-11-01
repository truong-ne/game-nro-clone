package game

import (
	"encoding/json"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Dragon balls and wishes: SummonDragon.java (Shenron from 1/2/3-star balls in
// the home villages, one dragon server-wide, 5 minutes to wish, 10 minutes
// before the next summon) and SummonDragonNamek.java (Porunga, clan-wide wish).

const (
	progBall1  = 14 // SummonDragon.NGOC_RONG_1_SAO
	progBall7  = 20
	progShen   = "shenron"
	progPorung = "namek"
)

type progWish struct{ Label, Code string }

// Wish menus (SummonDragon.java:60-73). "other" switches the 1-star page.
var progWishes = map[string][]progWish{
	"1_0": {{"Giàu có +2 Tỏi Vàng", "gold_max"}, {"Găng tay đang mang lên 1 cấp", "gloves"}, {"Chí mạng Gốc +2%", "crit"},
		{"Thay Chiêu 2-3 Đệ tử", "pet_skill"}, {"Điều ước khác", "other"}},
	"1_1": {{"Đẹp trai nhất Vũ trụ", "avatar"}, {"Giàu có +10K Ngọc", "gem_10k"}, {"+200 Tr Sức mạnh và tiềm năng", "power_200m"},
		{"Găng tay đệ đang mang lên 1 cấp", "pet_gloves"}, {"Điều ước khác", "other"}},
	"2_0":   {{"Giàu có +2K Ngọc", "gem_2k"}, {"+20 Tr Sức mạnh và tiềm năng", "power_20m"}, {"Giàu có +200 Tr Vàng", "gold_200m"}},
	"3_0":   {{"Giàu có +200 Ngọc", "gem_200"}, {"+2 Tr Sức mạnh và tiềm năng", "power_2m"}, {"Giàu có +20 Tr Vàng", "gold_20m"}},
	"namek": {{"1-20 viên ngọc rồng 3 sao", "n_balls"}, {"Hổ mập vàng", "n_tiger"}, {"x99 bột mỳ", "n_flour"}},
}

type progDragonSlot struct {
	Active bool
	Owner  int64
	Star   int
	Page   int
	Map    int
	Zone   int
	X, Y   int
	Until  time.Time
}

type progDragonWorld struct {
	mu        sync.Mutex
	shen      progDragonSlot
	namek     progDragonSlot
	shenLast  time.Time // lastTimeShenronAppeared
	namekNext time.Time // NgocRongNamecService.tOpenNrNamec
}

var progDragons sync.Map // *World -> *progDragonWorld

func (w *World) progDragon() *progDragonWorld {
	v, _ := progDragons.LoadOrStore(w, &progDragonWorld{})
	return v.(*progDragonWorld)
}

// progDragonCrit is the permanent crit from wishes (nPoint.critdragon).
type progDragonCrit struct {
	Crit int `json:"crit"`
}

const progSummonHelp = "Có 3 cách gọi rồng thần. Gọi từ ngọc 1 sao, gọi từ ngọc 2 sao, hoặc gọi từ ngọc 3 sao\n" +
	"Để gọi rồng N sao cần ngọc từ N sao đến 7 sao\nNgọc rồng sẽ mất ngay khi gọi rồng dù bạn có ước hay không\n" +
	"Quá 5 phút nếu không ước rồng thần sẽ bay mất"

func (s *progDragonSlot) menuKey(kind string) string {
	if kind == progPorung {
		return "namek"
	}
	return strconv.Itoa(s.Star) + "_" + strconv.Itoa(s.Page)
}

func (w *World) progSendWishes(p *Player, kind string, s progDragonSlot) {
	ws := progWishes[s.menuKey(kind)]
	labels := make([]string, len(ws))
	for i, x := range ws {
		labels[i] = x.Label
	}
	text := "Ta sẽ ban cho người 1 điều ước, ngươi có 5 phút, hãy suy nghĩ thật kỹ trước khi quyết định"
	if kind == progPorung {
		text = "Ta sẽ ban cho cả bang hội ngươi 1 điều ước, ngươi có 5 phút, hãy suy nghĩ thật kỹ trước khi quyết định"
	}
	p.Send("dragon", map[string]any{"kind": kind, "star": s.Star, "text": text, "wishes": labels, "until": s.Until.Unix()})
}

func (w *World) progAppear(p *Player, z *Zone, kind string, s progDragonSlot) {
	who := "rồng thần"
	if kind == progPorung {
		who = "rồng thần namek"
	}
	w.Broadcast(Encode("notice", map[string]string{"text": p.Char.Name + " vừa gọi " + who + " tại " + z.Map.Name + " khu vực " + itoa(z.ID)}))
	w.Broadcast(Encode("dragon_appear", map[string]any{"kind": kind, "map": s.Map, "zone": s.Zone, "x": s.X, "y": s.Y, "owner": s.Owner}))
}

// progSummon follows SummonDragon.summonShenron.
func (w *World) progSummon(z *Zone, p *Player, star int) {
	c := progOf(w.Data).C.Dragon
	if star < 1 || star > 3 {
		return
	}
	if !containsInt(c.Maps, z.Map.ID) {
		notice(p, "Chỉ được gọi rồng thần ở ngôi làng trước nhà")
		return
	}
	for id := progBall1 + star - 1; id <= progBall7; id++ { // checkShenronBall
		if p.CountItem(id) < 1 {
			notice(p, "Bạn còn thiếu 1 viên ngọc rồng "+itoa(id-progBall1+1)+" sao")
			return
		}
	}
	dw := w.progDragon()
	dw.mu.Lock()
	if dw.shen.Active {
		dw.mu.Unlock()
		notice(p, "Không thể thực hiện")
		return
	}
	if left := time.Until(dw.shenLast.Add(time.Duration(c.CooldownS) * time.Second)); left > 0 {
		dw.mu.Unlock()
		notice(p, "Vui lòng đợi "+itoa(int(left.Seconds())+1)+" giây nữa")
		return
	}
	dw.shen = progDragonSlot{Active: true, Owner: p.ID(), Star: star, Map: z.Map.ID, Zone: z.ID, X: p.Char.X, Y: p.Char.Y,
		Until: time.Now().Add(time.Duration(c.WaitS) * time.Second)}
	s := dw.shen
	dw.mu.Unlock()
	for id := progBall1 + star - 1; id <= progBall7; id++ {
		p.RemoveItems(id, 1)
	}
	p.sendInventory()
	w.progAppear(p, z, progShen, s)
	w.progBadgeAdd(p, "dragon", "", 1) // SummonDragon.java:220 TRUM_UOC_RONG
	w.progSendWishes(p, progShen, s)
}

// progSummonNamek follows Dende.java:80-98 + SummonDragonNamek.summonNamec. The
// legacy map-wide Namek ball hunt (NgocRongNamecService) is reduced to holding
// the 7 Namek balls.
func (w *World) progSummonNamek(z *Zone, p *Player) {
	c := progOf(w.Data).C.Dragon.Namek
	if z.Map.ID != c.Map {
		return
	}
	if h := time.Now().Hour(); h > c.HourTo || h < c.HourFrom {
		notice(p, "Xin lỗi mấy anh, em đang bận buôn bán nên chỉ rảnh gọi Rồng vào khoảng 8h đến 22h")
		return
	}
	for _, id := range c.Balls {
		if p.CountItem(id) < 1 {
			notice(p, "Hãy gom đủ 7 viên Ngọc Rồng Namek")
			return
		}
	}
	dw := w.progDragon()
	dw.mu.Lock()
	if dw.namek.Active || time.Now().Before(dw.namekNext) {
		dw.mu.Unlock()
		notice(p, "Không thể thực hiện")
		return
	}
	dw.namekNext = time.Now().Add(time.Duration(c.CooldownS) * time.Second)
	dw.namek = progDragonSlot{Active: true, Owner: p.ID(), Map: z.Map.ID, Zone: z.ID, X: p.Char.X, Y: p.Char.Y,
		Until: time.Now().Add(time.Duration(progOf(w.Data).C.Dragon.WaitS) * time.Second)}
	s := dw.namek
	dw.mu.Unlock()
	for _, id := range c.Balls {
		p.RemoveItems(id, 1)
	}
	p.sendInventory()
	w.progAppear(p, z, progPorung, s)
	w.progSendWishes(p, progPorung, s)
}

// progLeave ends a dragon (shenronLeave).
func (w *World) progLeave(kind string, owner *Player, wished bool) {
	dw := w.progDragon()
	dw.mu.Lock()
	if kind == progPorung {
		dw.namek = progDragonSlot{}
	} else {
		dw.shen = progDragonSlot{}
		dw.shenLast = time.Now()
	}
	dw.mu.Unlock()
	if owner != nil {
		if wished {
			notice(owner, "Điều ước của ngươi đã trở thành sự thật\nHẹn gặp ngươi lần sau, ta đi ngủ đây, bái bai")
		} else {
			notice(owner, "Ta buồn ngủ quá rồi\nHẹn gặp ngươi lần sau, ta đi đây, bái bai")
		}
	}
	w.Broadcast(Encode("dragon_leave", map[string]string{"kind": kind}))
}

// progDragonTick makes the dragon leave after 5 minutes without a wish.
func (w *World) progDragonTick(now time.Time) {
	dw := w.progDragon()
	var expired []progDragonSlot
	var kinds []string
	dw.mu.Lock()
	for _, k := range []string{progShen, progPorung} {
		s := &dw.shen
		if k == progPorung {
			s = &dw.namek
		}
		if s.Active && now.After(s.Until) {
			expired = append(expired, *s)
			kinds = append(kinds, k)
		}
	}
	dw.mu.Unlock()
	for i, s := range expired {
		w.progLeave(kinds[i], w.onlineChar(s.Owner), false)
	}
}

// progWish applies one wish; false re-opens the menu (reOpenShenronWishes).
func (w *World) progWish(z *Zone, p *Player, kind string, i int) {
	dw := w.progDragon()
	dw.mu.Lock()
	slot := &dw.shen
	if kind == progPorung {
		slot = &dw.namek
	}
	if !slot.Active || slot.Owner != p.ID() {
		dw.mu.Unlock()
		return
	}
	ws := progWishes[slot.menuKey(kind)]
	if i < 0 || i >= len(ws) {
		dw.mu.Unlock()
		return
	}
	if ws[i].Code == "other" { // NpcFactory.java:293-307
		slot.Page = 1 - slot.Page
		s := *slot
		dw.mu.Unlock()
		w.progSendWishes(p, kind, s)
		return
	}
	s := *slot
	dw.mu.Unlock()
	if !w.progApplyWish(z, p, ws[i].Code) {
		w.progSendWishes(p, kind, s)
		return
	}
	p.dirty = true
	p.Recalc(w.Data)
	p.Send("self", p.Self())
	p.sendInventory()
	w.progLeave(kind, p, true)
}

func progAddGold(p *Player, n int64) { p.Char.Gold = min(p.Char.Gold+n, maxGold) }

// progApplyWish is SummonDragon.confirmWish / SummonDragonNamek.confirmWish.
func (w *World) progApplyWish(z *Zone, p *Player, code string) bool {
	d := w.Data
	switch code {
	case "gold_max":
		p.Char.Gold = maxGold
	case "gloves": // option 72 = upgrade level (max 7), option 0 damage +10%
		it := p.item(store.ContainerBody, equipSlot[ItemGloves])
		if it == nil {
			notice(p, "Ngươi hiện tại có đeo găng đâu")
			return false
		}
		lvl, found := 0, false
		for k := range it.Options {
			if it.Options[k][0] == 72 {
				lvl, found = it.Options[k][1], true
				if lvl < 7 {
					it.Options[k][1]++
				}
			}
		}
		if lvl >= 7 {
			notice(p, "Găng tay của ngươi đã đạt cấp tối đa")
			return false
		}
		if !found {
			it.Options = append(it.Options, [2]int{72, 1})
		}
		for k := range it.Options {
			if it.Options[k][0] == 0 {
				it.Options[k][1] += it.Options[k][1] * 10 / 100
				break
			}
		}
	case "crit":
		c := State[progDragonCrit](p, "dragon")
		if c.Crit >= 9 {
			notice(p, "Điều ước này đã quá sức với ta, ta sẽ cho ngươi chọn lại")
			return false
		}
		c.Crit += 2
	case "pet_skill":
		notice(p, "Ít nhất đệ tử ngươi phải có chiêu 2 chứ!") // pets have no skill slots yet
		return false
	case "pet_gloves":
		notice(p, "Đệ ngươi hiện tại có đeo găng đâu") // pets have no gear yet
		return false
	case "avatar":
		if progFreeSlots(p) <= 0 {
			notice(p, "Hành trang đã đầy")
			return false
		}
		id := 227 + min(max(p.Char.Gender, 0), 2)
		p.AddItem(d, id, 1, [][2]int{{97, 5 + rand.IntN(6)}, {77, 10 + rand.IntN(11)}})
	case "gem_10k":
		p.Char.Gem += 10000
	case "gem_2k":
		p.Char.Gem += 2000
	case "gem_200":
		p.Char.Gem += 200
	case "power_200m", "power_20m", "power_2m":
		n := map[string]int64{"power_200m": 200_000_000, "power_20m": 20_000_000, "power_2m": 2_000_000}[code]
		p.Char.Power += n
		p.Char.Potential += n
	case "gold_200m":
		progAddGold(p, 200_000_000)
	case "gold_20m":
		progAddGold(p, 20_000_000)
	case "n_balls", "n_tiger", "n_flour":
		w.progNamekGift(p, code)
	default:
		return false
	}
	return true
}

// progNamekGift gives the Porunga wish to every online clan member (legacy
// also wrote to offline members' saves), or to the summoner without a clan.
func (w *World) progNamekGift(p *Player, code string) {
	give := func(t *Player) {
		switch code {
		case "n_balls":
			t.AddItem(w.Data, 16, 1+rand.IntN(20), nil)
		case "n_tiger":
			opts := []int{77, 80, 81, 103, 50, 94, 5}
			opts2 := []int{14, 16, 17, 19, 27, 28, 5, 47, 87}
			param := 5 + rand.IntN(6)
			o := [][2]int{{opts[rand.IntN(len(opts))], param}}
			if rand.IntN(100) < 20 {
				o = append(o, [2]int{opts2[rand.IntN(len(opts2))], param})
			}
			t.AddItem(w.Data, 942, 1, append(o, [2]int{30, 0}))
		case "n_flour":
			t.AddItem(w.Data, 2053, 99, nil)
		}
		t.dirty = true
		t.sendInventory()
	}
	give(p)
	c := w.clan(p.Char.ClanID)
	if c == nil {
		return
	}
	w.clans.mu.Lock()
	var ids []int64
	for _, m := range c.Members {
		if m.ID != p.ID() {
			ids = append(ids, m.ID)
		}
	}
	w.clans.mu.Unlock()
	for _, id := range ids {
		if t := w.onlineChar(id); t != nil {
			w.postToPlayer(t, func(*Zone) { give(t) })
		}
	}
}

func init() {
	recalcHooks = append(recalcHooks, func(p *Player, d *Data, m *Mods) {
		if c := progPeek[progDragonCrit](p, "dragon"); c != nil {
			p.Crit += c.Crit
		}
	})
	enterHooks = append(enterHooks, func(z *Zone, p *Player) { // reSummonShenron after reconnect
		if progOf(z.World.Data) == nil {
			return
		}
		dw := z.World.progDragon()
		dw.mu.Lock()
		shen, namek := dw.shen, dw.namek
		dw.mu.Unlock()
		if shen.Active && shen.Owner == p.ID() {
			z.World.progSendWishes(p, progShen, shen)
		}
		if namek.Active && namek.Owner == p.ID() {
			z.World.progSendWishes(p, progPorung, namek)
		}
	})
	// UseItem.controllerCallRongThan (UseItem.java:1705-1720).
	for id := progBall1; id <= progBall7; id++ {
		useByID[id] = func(z *Zone, p *Player, slot int, it *store.Item) bool {
			if progOf(z.World.Data) == nil {
				return false
			}
			if star := it.TemplateID - progBall1 + 1; star <= 3 {
				p.Send("dragon_summon", map[string]any{"star": star, "text": "Bạn muốn gọi rồng thần ?", "help": progSummonHelp})
			} else {
				notice(p, "Bạn chỉ có thể gọi rồng từ ngọc 3 sao, 2 sao, 1 sao")
			}
			return true
		}
	}
	npcOptions = append(npcOptions, func(z *Zone, p *Player, npc int) []MenuOption {
		pd := progOf(z.World.Data)
		if pd == nil || npc != pd.C.Dragon.Namek.Npc || z.Map.ID != pd.C.Dragon.Namek.Map {
			return nil
		}
		return []MenuOption{{Label: "Gọi Rồng Thần Namek", Action: "dragon_namek"}}
	})
	menuActions["dragon_namek"] = func(z *Zone, p *Player, _ string) { z.World.progSummonNamek(z, p) }
	register("dragon_summon", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ Star int }
		if progOf(z.World.Data) != nil && json.Unmarshal(d, &r) == nil {
			z.World.progSummon(z, p, r.Star)
		}
	})
	register("dragon_wish", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct {
			I    int
			Kind string
		}
		if progOf(z.World.Data) == nil || json.Unmarshal(d, &r) != nil {
			return
		}
		if r.Kind != progPorung {
			r.Kind = progShen
		}
		z.World.progWish(z, p, r.Kind, r.I)
	})
	register("dragon_info", func(z *Zone, p *Player, _ json.RawMessage) {
		if progOf(z.World.Data) == nil {
			return
		}
		dw := z.World.progDragon()
		dw.mu.Lock()
		shen, namek, last := dw.shen, dw.namek, dw.shenLast
		dw.mu.Unlock()
		if shen.Active && shen.Owner == p.ID() {
			z.World.progSendWishes(p, progShen, shen)
			return
		}
		if namek.Active && namek.Owner == p.ID() {
			z.World.progSendWishes(p, progPorung, namek)
			return
		}
		next := last.Add(time.Duration(progOf(z.World.Data).C.Dragon.CooldownS) * time.Second).Unix()
		p.Send("dragon_state", map[string]any{"active": shen.Active, "next_summon": next, "help": progSummonHelp})
	})
}
