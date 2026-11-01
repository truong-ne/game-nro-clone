package game

import (
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Crafting at Bà Hạt Mít (legacy combine/ package). Each kind is one function
// that validates and describes the operation (preview) and, when do is set,
// performs it. Client commands: craft_open, craft_info, craft_do.

type craftReq struct {
	Kind    string `json:"kind"`
	Slot    int    `json:"slot"`
	Slot2   int    `json:"slot2"`
	N       int    `json:"n"`
	Protect bool   `json:"protect"`
	Recipe  string `json:"recipe"`
}

type craftFn func(z *Zone, p *Player, r craftReq, do bool) (info, errText string)

var i2CraftKinds = map[string]craftFn{
	"upgrade":     i2Upgrade,
	"crystallize": i2Crystallize,
	"socket":      i2Socket,
	"earring":     i2EarringUp,
	"earring_opt": i2EarringStat,
	"recipe":      i2RecipeDo,
	"disassemble": i2Disassemble,
}

func init() {
	npcOptions = append(npcOptions, func(z *Zone, p *Player, npc int) []MenuOption {
		if npc != i2cfg.CraftNpc {
			return nil
		}
		return []MenuOption{{Label: "Nâng cấp / pha lê / chế tạo", Action: "items2_craft"}}
	})
	menuActions["items2_craft"] = func(z *Zone, p *Player, _ string) { i2SendCraftOpen(p) }
	register("craft_open", func(z *Zone, p *Player, _ json.RawMessage) {
		if i2NearNpc(z, p, i2cfg.CraftNpc) {
			i2SendCraftOpen(p)
		}
	})
	register("craft_info", func(z *Zone, p *Player, d json.RawMessage) { z.i2Craft(p, d, false) })
	register("craft_do", func(z *Zone, p *Player, d json.RawMessage) { z.i2Craft(p, d, true) })
}

func i2SendCraftOpen(p *Player) {
	recipes := make([]map[string]any, 0, len(i2cfg.Recipes))
	for _, r := range i2cfg.Recipes {
		recipes = append(recipes, map[string]any{"id": r.ID, "name": r.Name, "gold": r.Gold, "rate": r.Rate})
	}
	p.Send("craft_open", map[string]any{"recipes": recipes})
	p.sendInventory()
}

func (z *Zone) i2Craft(p *Player, raw json.RawMessage, do bool) {
	var r craftReq
	if json.Unmarshal(raw, &r) != nil {
		return
	}
	f := i2CraftKinds[r.Kind]
	if f == nil {
		return
	}
	if do && !i2NearNpc(z, p, i2cfg.CraftNpc) {
		i2Notice(p, "Hãy đến gặp Bà Hạt Mít")
		return
	}
	info, errText := f(z, p, r, do)
	if !do {
		p.Send("craft_info", map[string]any{"kind": r.Kind, "text": info, "ok": errText == "", "error": errText})
		return
	}
	if errText != "" {
		p.Send("craft_result", map[string]any{"kind": r.Kind, "ok": false, "text": errText})
		return
	}
	p.dirty = true
	p.Send("craft_result", map[string]any{"kind": r.Kind, "ok": true, "text": info})
	p.Send("self", p.Self())
	p.sendInventory()
}

func i2Money(n int64) string {
	s := strconv.FormatInt(n, 10)
	out := []byte{}
	for i := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			out = append(out, '.')
		}
		out = append(out, s[i])
	}
	return string(out)
}

func i2Name(z *Zone, tid int) string {
	if t := z.World.Data.ItemsFull[tid]; t != nil {
		return t.Name
	}
	return "?"
}

// i2Upgrade: nâng cấp trang bị with đá nâng cấp (NangCapVatPham.java:126-262).
func i2Upgrade(z *Zone, p *Player, r craftReq, do bool) (string, string) {
	u := &i2cfg.Upgrade
	it := p.item(store.ContainerBag, r.Slot)
	if it == nil {
		return "", "Hãy chọn 1 trang bị và 1 loại đá nâng cấp"
	}
	t := z.World.Data.ItemsFull[it.TemplateID]
	if t == nil || t.Type >= 5 {
		return "", "Hãy chọn 1 trang bị và 1 loại đá nâng cấp"
	}
	stone := u.StoneByType[strconv.Itoa(t.Type)] // CombineSystem.isCoupleItemNangCapCheck
	level := i2OptVal(it.Options, u.LevelOpt)
	if level >= u.MaxLevel || level >= len(u.Stones) {
		return "", "Trang bị của ngươi đã đạt cấp tối đa"
	}
	need, gold, rate := u.Stones[level], u.Gold[level], u.Rate[level]
	info := fmt.Sprintf("%s (+%d) → (+%d)\nTỉ lệ thành công: %g%%\nCần %d %s\nCần %s vàng",
		t.Name, level, level+1, rate, need, i2Name(z, stone), i2Money(gold))
	if r.Protect {
		info += "\nCần 1 " + i2Name(z, u.Protect)
	} else if i2Has(u.Downgrade, level) {
		info += fmt.Sprintf("\nNếu thất bại sẽ rớt xuống (+%d)", level-1)
	}
	switch {
	case p.CountItem(stone) < need:
		return info, fmt.Sprintf("Còn thiếu %d %s", need-p.CountItem(stone), i2Name(z, stone))
	case p.Char.Gold < gold:
		return info, "Còn thiếu " + i2Money(gold-p.Char.Gold) + " vàng"
	case r.Protect && p.CountItem(u.Protect) < 1:
		return info, "Còn thiếu 1 đá bảo vệ"
	}
	if !do {
		return info, ""
	}
	p.Char.Gold -= gold
	it.Options = append([][2]int(nil), it.Options...)
	main, side := -1, -1
	for i, o := range it.Options { // the last matching option wins, as in the legacy loop
		if i2Has(u.MainOpts, o[0]) {
			main = i
		} else if i2Has(u.SideOpts, o[0]) {
			side = i
		}
	}
	var result string
	if i2Roll(rate) {
		for _, i := range []int{main, side} {
			if i >= 0 {
				it.Options[i][1] += max(it.Options[i][1]*10/100, 1)
			}
		}
		i2SetOpt(it, u.LevelOpt, level+1)
		result = fmt.Sprintf("Nâng cấp thành công %s lên +%d", t.Name, level+1)
	} else {
		if i2Has(u.Downgrade, level) && !r.Protect {
			for _, i := range []int{main, side} {
				if i >= 0 {
					it.Options[i][1] -= max(it.Options[i][1]*11/100, 1)
				}
			}
			i2SetOpt(it, u.LevelOpt, level-1)
			i2SetOpt(it, u.CounterOpt, i2OptVal(it.Options, u.CounterOpt)+1)
			result = fmt.Sprintf("Thất bại, %s rớt xuống +%d", t.Name, level-1)
		} else {
			result = "Lần này xui thôi, đập tiếp đi!"
		}
	}
	// it may move after removals below; it is not used past this point.
	if r.Protect {
		p.RemoveItems(u.Protect, 1)
	}
	p.RemoveItems(stone, need)
	return result, ""
}

// i2Crystallize: pha lê hoá, up to N tries until one succeeds (PhaLeHoaTrangBi.java:115-214).
func i2Crystallize(z *Zone, p *Player, r craftReq, do bool) (string, string) {
	c := &i2cfg.Crystal
	it := p.item(store.ContainerBag, r.Slot)
	if it == nil {
		return "", "Hãy chọn 1 vật phẩm để pha lê hóa"
	}
	t := z.World.Data.ItemsFull[it.TemplateID]
	if t == nil || !i2Has(c.Types, t.Type) {
		return "", "Vật phẩm này không thể đục lỗ"
	}
	star := i2OptVal(it.Options, c.SlotOpt)
	if star >= c.MaxStar || star >= len(c.Rate) {
		return "", "Vật phẩm đã đạt tối đa sao pha lê"
	}
	gold, gem, rate := c.Gold[star], c.Gem[star], c.Rate[star]
	info := fmt.Sprintf("%s (%d lỗ)\nTỉ lệ thành công: %g%%\nCần %s vàng, %d ngọc mỗi lần", t.Name, star, rate, i2Money(gold), gem)
	if p.Char.Gold < gold {
		return info, "Còn thiếu " + i2Money(gold-p.Char.Gold) + " vàng"
	}
	if p.Char.Gem < gem {
		return info, "Không đủ ngọc để thực hiện"
	}
	if !do {
		return info, ""
	}
	if last, ok := p.Tmp["i2_crystal"].(time.Time); ok && time.Since(last) < time.Duration(c.CooldownMs)*time.Millisecond {
		return "", "Thao tác quá nhanh"
	}
	p.Tmp["i2_crystal"] = time.Now()
	n := min(max(r.N, 1), c.MaxBatch)
	tries := 0
	for range n {
		if p.Char.Gold < gold || p.Char.Gem < gem {
			break
		}
		p.Char.Gold -= gold
		p.Char.Gem -= gem
		tries++
		if i2Roll(rate) {
			i2SetOpt(it, c.SlotOpt, star+1)
			return fmt.Sprintf("Pha lê hóa thành công %s lên %d lỗ sau %d lần", t.Name, star+1, tries), ""
		}
	}
	return fmt.Sprintf("Thất bại %d lần, còn vàng còn ngọc thì đập tiếp!", tries), ""
}

// i2Socket: ép sao pha lê vào lỗ (EpSaoTrangBi.java:80-167).
func i2Socket(z *Zone, p *Player, r craftReq, do bool) (string, string) {
	s := &i2cfg.Socket
	d := z.World.Data
	gear, stone := p.item(store.ContainerBag, r.Slot), p.item(store.ContainerBag, r.Slot2)
	if gear == nil || stone == nil || r.Slot == r.Slot2 {
		return "", "Cần 1 trang bị có lỗ sao pha lê và 1 loại đá pha lê để ép vào"
	}
	gt, st := d.ItemsFull[gear.TemplateID], d.ItemsFull[stone.TemplateID]
	if gt == nil || st == nil || !i2Has(i2cfg.Crystal.Types, gt.Type) {
		return "", "Cần 1 trang bị có lỗ sao pha lê và 1 loại đá pha lê để ép vào"
	}
	opt, ok := s.Stones[strconv.Itoa(stone.TemplateID)]
	if st.Type == s.StoneType && len(stone.Options) > 0 { // CombineSystem.getOptionDaPhaLe: type 30 carries its option
		opt, ok = [2]int{stone.Options[0][0], stone.Options[0][1]}, true
	}
	if !ok {
		return "", "Cần 1 trang bị có lỗ sao pha lê và 1 loại đá pha lê để ép vào"
	}
	star, slots := i2OptVal(gear.Options, s.StarOpt), i2OptVal(gear.Options, s.SlotOpt)
	info := fmt.Sprintf("%s: %d/%d sao\nThêm option %d +%d\nCần %d ngọc", gt.Name, star, slots, opt[0], opt[1], s.Gem)
	switch {
	case slots > s.MaxSlots:
		return info, "Lỗ sao tối đa là " + itoa(s.MaxSlots)
	case star >= slots:
		return info, "Không thể ép sao cao hơn lỗ"
	case star >= 7 && slots >= s.UnlockAt && i2OptVal(gear.Options, s.UnlockOpt) < slots: // CombineService.CheckSlot
		return info, "Cần cường hóa lỗ sao pha lê này trước"
	case p.Char.Gem < s.Gem:
		return info, "Không đủ ngọc để thực hiện"
	}
	if !do {
		return info, ""
	}
	p.Char.Gem -= s.Gem
	gear.Options = append([][2]int(nil), gear.Options...)
	if star+1 >= s.SepFrom { // stars 8 and 9 add a separate option
		gear.Options = append(gear.Options, opt)
	} else if i := i2OptIdx(gear.Options, opt[0]); i >= 0 {
		gear.Options[i][1] += opt[1]
	} else {
		gear.Options = append(gear.Options, opt)
	}
	i2SetOpt(gear, s.StarOpt, star+1)
	p.takeItem(store.ContainerBag, r.Slot2, 1)
	return fmt.Sprintf("Ép sao thành công, %s có %d sao", gt.Name, star+1), ""
}

// Earring fragments count their amount in option 31 (legacy) or in quantity.
func (p *Player) i2FragCount(tid int) int {
	n := 0
	for _, it := range p.Char.Items {
		if it.Container == store.ContainerBag && it.TemplateID == tid {
			if v := i2OptVal(it.Options, i2cfg.FragOpt); v > 0 {
				n += v * max(it.Quantity, 1)
			} else {
				n += it.Quantity
			}
		}
	}
	return n
}

func (p *Player) i2FragTake(tid, n int) {
	for n > 0 {
		found := false
		for i := range p.Char.Items {
			it := &p.Char.Items[i]
			if it.Container != store.ContainerBag || it.TemplateID != tid {
				continue
			}
			found = true
			if v := i2OptVal(it.Options, i2cfg.FragOpt); v > 0 && it.Quantity == 1 {
				take := min(v, n)
				n -= take
				if take == v {
					p.takeItem(it.Container, it.Slot, 1)
				} else {
					i2SetOpt(it, i2cfg.FragOpt, v-take)
				}
			} else {
				take := min(it.Quantity, n)
				n -= take
				p.takeItem(it.Container, it.Slot, take)
			}
			break
		}
		if !found {
			return
		}
	}
	p.dirty = true
}

// i2EarringUp: bông tai Porata cấp 2/3 (NangCapBongTai.java:97-147, NangCapBongTai3.java).
func i2EarringUp(z *Zone, p *Player, r craftReq, do bool) (string, string) {
	it := p.item(store.ContainerBag, r.Slot)
	if it == nil {
		return "", "Cần 1 Bông tai Porata và Mảnh vỡ bông tai"
	}
	var e *i2Earring
	for i := range i2cfg.Earrings {
		if i2cfg.Earrings[i].From == it.TemplateID {
			e = &i2cfg.Earrings[i]
		}
	}
	if e == nil {
		return "", "Cần 1 Bông tai Porata và Mảnh vỡ bông tai"
	}
	have := p.i2FragCount(e.Fragment)
	info := fmt.Sprintf("Bông tai Porata [+%d]\nTỉ lệ thành công: %g%%\nCần %d %s (có %d)\nCần %s vàng, %d ngọc\nThất bại -%d %s",
		e.Level, e.Rate, e.Need, i2Name(z, e.Fragment), have, i2Money(e.Gold), e.Gem, e.FailCost, i2Name(z, e.Fragment))
	switch {
	case have < e.Need:
		return info, fmt.Sprintf("Còn thiếu %d %s", e.Need-have, i2Name(z, e.Fragment))
	case p.Char.Gold < e.Gold:
		return info, "Không đủ vàng để thực hiện"
	case p.Char.Gem < e.Gem:
		return info, "Không đủ ngọc để thực hiện"
	case p.CountItem(e.To) > 0:
		return info, "Ngươi đã có bông tai cấp này trong hành trang rồi"
	case it.Quantity > 1 && p.freeSlot(store.ContainerBag) < 0:
		return info, "Hành trang đã đầy"
	}
	if !do {
		return info, ""
	}
	p.Char.Gold -= e.Gold
	p.Char.Gem -= e.Gem
	if !i2Roll(e.Rate) {
		p.i2FragTake(e.Fragment, e.FailCost)
		return "Nâng cấp thất bại", ""
	}
	p.takeItem(store.ContainerBag, r.Slot, 1)
	p.AddItem(z.World.Data, e.To, 1, [][2]int{{i2cfg.Upgrade.LevelOpt, e.Level}})
	p.i2FragTake(e.Fragment, e.Need)
	return fmt.Sprintf("Nâng cấp thành công Bông tai Porata cấp %d", e.Level), ""
}

// i2EarringStat: mở chỉ số bông tai (NangChiSoBongTai.java:91-135, NangChiSoBongTai3.java).
func i2EarringStat(z *Zone, p *Player, r craftReq, do bool) (string, string) {
	it := p.item(store.ContainerBag, r.Slot)
	if it == nil {
		return "", "Cần 1 Bông tai Porata, Mảnh hồn bông tai và 1 Đá xanh lam"
	}
	var e *i2EarStat
	for i := range i2cfg.EarringStats {
		if i2cfg.EarringStats[i].Item == it.TemplateID {
			e = &i2cfg.EarringStats[i]
		}
	}
	if e == nil {
		return "", "Cần 1 Bông tai Porata, Mảnh hồn bông tai và 1 Đá xanh lam"
	}
	have := p.i2FragCount(e.Soul)
	info := fmt.Sprintf("Mở chỉ số Bông tai Porata [+%d]\nTỉ lệ thành công: %g%%\nCần %d %s (có %d)\nCần 1 %s, %d ngọc\n+%d chỉ số ngẫu nhiên",
		e.Level, e.Rate, e.Need, i2Name(z, e.Soul), have, i2Name(z, e.Stone), e.Gem, e.Count)
	switch {
	case have < e.Need:
		return info, fmt.Sprintf("Còn thiếu %d %s", e.Need-have, i2Name(z, e.Soul))
	case p.CountItem(e.Stone) < 1:
		return info, "Còn thiếu 1 " + i2Name(z, e.Stone)
	case p.Char.Gem < e.Gem:
		return info, "Không đủ ngọc để thực hiện"
	}
	if !do {
		return info, ""
	}
	p.Char.Gem -= e.Gem
	result := "Mở chỉ số thất bại"
	if i2Roll(e.Rate) && len(i2cfg.EarringOpts) > 0 {
		opts := make([][2]int, 0, e.Count+1)
		for range e.Count {
			opts = append(opts, [2]int{i2cfg.EarringOpts[i2Rand(0, len(i2cfg.EarringOpts)-1)], i2Rand(i2cfg.EarringParam[0], i2cfg.EarringParam[1])})
		}
		it.Options = append(opts, [2]int{i2cfg.Upgrade.LevelOpt, e.Level})
		result = "Mở chỉ số thành công"
	}
	p.i2FragTake(e.Soul, e.Need)
	p.RemoveItems(e.Stone, 1)
	return result, ""
}

func i2FindRecipe(id string) *i2Recipe {
	for i := range i2cfg.Recipes {
		if i2cfg.Recipes[i].ID == id {
			return &i2cfg.Recipes[i]
		}
	}
	return nil
}

// i2RecipeDo runs a data-driven recipe (nhập ngọc rồng, nhập đá, hematite,
// dùi đục, đá mài, tái tạo capsule kích hoạt).
func i2RecipeDo(z *Zone, p *Player, r craftReq, do bool) (string, string) {
	rc := i2FindRecipe(r.Recipe)
	if rc == nil || len(rc.Outputs) == 0 {
		return "", "Công thức không tồn tại"
	}
	info := rc.Name + fmt.Sprintf("\nTỉ lệ thành công: %g%%", rc.Rate)
	chosen := make([]int, len(rc.Inputs))
	missing := ""
	for i, in := range rc.Inputs {
		chosen[i] = -1
		for _, id := range in.IDs {
			if p.CountItem(id) >= in.Qty {
				chosen[i] = id
				break
			}
		}
		info += fmt.Sprintf("\nCần %d %s", in.Qty, i2Name(z, in.IDs[0]))
		if chosen[i] < 0 && missing == "" {
			missing = fmt.Sprintf("Còn thiếu %d %s", in.Qty, i2Name(z, in.IDs[0]))
		}
	}
	if rc.Gold > 0 {
		info += "\nCần " + i2Money(rc.Gold) + " vàng"
	}
	if rc.Gem > 0 {
		info += fmt.Sprintf("\nCần %d ngọc", rc.Gem)
	}
	switch {
	case missing != "":
		return info, missing
	case p.Char.Gold < rc.Gold:
		return info, "Không đủ vàng để thực hiện"
	case p.Char.Gem < rc.Gem:
		return info, "Không đủ ngọc để thực hiện"
	case p.freeSlot(store.ContainerBag) < 0:
		return info, "Hành trang cần ít nhất 1 chỗ trống"
	}
	if !do {
		return info, ""
	}
	p.Char.Gold -= rc.Gold
	p.Char.Gem -= rc.Gem
	for i, in := range rc.Inputs {
		p.RemoveItems(chosen[i], in.Qty)
	}
	if !i2Roll(rc.Rate) {
		return "Làm phép thất bại", ""
	}
	out := rc.Outputs[i2Rand(0, len(rc.Outputs)-1)]
	p.AddItem(z.World.Data, out, 1, nil)
	return "Nhận được " + i2Name(z, out), ""
}

// i2Disassemble: phân rã trang bị kích hoạt (PhanRaTrangBiKichHoat.java:54-99).
func i2Disassemble(z *Zone, p *Player, r craftReq, do bool) (string, string) {
	c := &i2cfg.Disassemble
	it := p.item(store.ContainerBag, r.Slot)
	if it == nil {
		return "", "Cần đặt đúng vật phẩm!"
	}
	ok := false
	for _, o := range it.Options {
		if i2Has(c.Options, o[0]) {
			ok = true
		}
	}
	if !ok {
		return "", "Vật phẩm không đủ điều kiện để phân rã!"
	}
	info := fmt.Sprintf("Phân rã %s thành 1 %s\nCần %s vàng", i2Name(z, it.TemplateID), i2Name(z, c.Output), i2Money(c.Gold))
	if p.Char.Gold < c.Gold {
		return info, "Không đủ vàng để thực hiện!"
	}
	if !do {
		return info, ""
	}
	if it.Quantity == 1 || p.i2CanAdd(z.World.Data, c.Output, nil) {
		p.Char.Gold -= c.Gold
		p.takeItem(store.ContainerBag, r.Slot, 1)
		p.AddItem(z.World.Data, c.Output, 1, nil)
		return "Phân rã thành công!", ""
	}
	return "", "Hành trang đã đầy"
}
