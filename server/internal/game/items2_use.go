package game

import (
	"math/rand/v2"
	"strconv"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Usable items (legacy services_func/UseItem.java): thỏi vàng, gift boxes,
// bags/flags and mounts, capsule kích hoạt; plus the kích hoạt drop on
// farming maps (mob/Mob.java:787-892).

func init() {
	u := &i2cfg.Use
	if u.GoldBar > 0 {
		useByID[u.GoldBar] = i2UseGoldBar
	}
	for k := range u.Boxes {
		if id, err := strconv.Atoi(k); err == nil {
			useByID[id] = i2UseBox
		}
	}
	for k, text := range u.Notice {
		if id, err := strconv.Atoi(k); err == nil {
			msg := text
			useByID[id] = func(z *Zone, p *Player, slot int, it *store.Item) bool { i2Notice(p, msg); return true }
		}
	}
	for k, bodySlot := range u.EquipTypes {
		if typ, err := strconv.Atoi(k); err == nil {
			bs := bodySlot
			useByType[typ] = func(z *Zone, p *Player, slot int, it *store.Item) bool { return z.i2EquipTo(p, slot, bs) }
		}
	}
	if i2cfg.Kit.Capsule > 0 {
		useByID[i2cfg.Kit.Capsule] = i2UseCapsule
	}
	menuActions["items2_skh"] = func(z *Zone, p *Player, arg string) { z.i2OpenCapsule(p, arg) }
	recalcHooks = append(recalcHooks, i2LookHook)
	enterHooks = append(enterHooks, i2EnterLook)
	worldStartHooks = append(worldStartHooks, func(w *World) { i2InstallKitDrop(w) })
}

// i2UseGoldBar sells one thỏi vàng for gold (Input.java:495-518, BANSLL).
func i2UseGoldBar(z *Zone, p *Player, slot int, it *store.Item) bool {
	v := i2cfg.Use.GoldBarValue
	if p.Char.Gold+v > maxGold {
		i2Notice(p, "Vàng sau khi bán vượt quá giới hạn")
		return true
	}
	p.takeItem(store.ContainerBag, slot, 1)
	p.Char.Gold += v
	i2Notice(p, "Đã bán 1 Thỏi vàng thu được "+i2Money(v)+" vàng")
	p.Send("self", p.Self())
	return true
}

// i2UseBox opens a gift box: one weighted roll from its pool
// (openCSKB UseItem.java:1491, C5/C7 :2576-2628, ChuLunBox :2630, OpenHopQuaThuong :2006).
func i2UseBox(z *Zone, p *Player, slot int, it *store.Item) bool {
	d := z.World.Data
	pool := i2cfg.Use.Boxes[strconv.Itoa(it.TemplateID)]
	r := i2PickPool(pool)
	if r == nil {
		return false
	}
	if it.Quantity > 1 && p.freeSlot(store.ContainerBag) < 0 {
		i2Notice(p, "Hành trang đã đầy")
		return true
	}
	s := r.roll(d)
	p.takeItem(store.ContainerBag, slot, 1)
	if !p.AddItem(d, s.TID, s.Qty, s.Opts) {
		i2Notice(p, "Hành trang đã đầy")
		return true
	}
	i2Notice(p, "Bạn nhận được "+i2Stackname(d, s))
	p.Send("self", p.Self())
	return true
}

func i2Stackname(d *Data, s i2Stack) string {
	name := "?"
	if t := d.ItemsFull[s.TID]; t != nil {
		name = t.Name
	}
	if s.Qty > 1 {
		return itoa(s.Qty) + " " + name
	}
	return name
}

// i2EquipTo moves a bag item to a body slot (InventoryService.itemBagToBody),
// swapping out whatever was worn there.
func (z *Zone) i2EquipTo(p *Player, bagSlot, bodySlot int) bool {
	it := p.item(store.ContainerBag, bagSlot)
	if it == nil {
		return true
	}
	t := z.World.Data.ItemsFull[it.TemplateID]
	if t.Gender != 3 && t.Gender != p.Char.Gender {
		i2Notice(p, "Vật phẩm không dành cho hành tinh của bạn")
		return true
	}
	if p.Char.Power < t.PowerRequire {
		i2Notice(p, "Chưa đủ sức mạnh để sử dụng")
		return true
	}
	worn := p.item(store.ContainerBody, bodySlot)
	it.Container, it.Slot = store.ContainerBody, bodySlot
	if worn != nil {
		worn.Container, worn.Slot = store.ContainerBag, bagSlot
	}
	z.afterGearChange(p)
	return true
}

// look2 carries the bag/flag (body slot 8, Player.getFlagBag) and mount (slot 9).
type i2Look struct {
	ID    int64 `json:"id"`
	Bag   int   `json:"bag"`
	Icons []int `json:"icons,omitempty"`
	Mount int   `json:"mount"`
}

func i2LookOf(d *Data, p *Player) i2Look {
	l := i2Look{ID: p.ID(), Bag: -1, Mount: -1}
	for _, it := range p.Char.Items {
		if it.Container != store.ContainerBody {
			continue
		}
		switch it.Slot {
		case 8:
			if t := d.ItemsFull[it.TemplateID]; t != nil {
				l.Bag = t.Part
				l.Icons = i2FlagBags[t.Part]
			}
		case 9:
			l.Mount = it.TemplateID
		}
	}
	return l
}

// i2LookHook runs inside Recalc; when the bag or mount changed and p is in
// a zone, it tells the zone.
func i2LookHook(p *Player, d *Data, _ *Mods) {
	if p.Tmp == nil || p.Char == nil {
		return
	}
	l := i2LookOf(d, p)
	key := [2]int{l.Bag, l.Mount}
	prev, seen := p.Tmp["i2_look"].([2]int)
	p.Tmp["i2_look"] = key
	if !seen || prev == key {
		return
	}
	if z := p.Zone(); z != nil && z.players[p.ID()] == p {
		z.broadcast(Encode("look2", l), 0)
	}
}

func i2EnterLook(z *Zone, p *Player) {
	d := z.World.Data
	mine := i2LookOf(d, p)
	for _, o := range z.players {
		if o == p {
			continue
		}
		if l := i2LookOf(d, o); l.Bag >= 0 || l.Mount >= 0 {
			p.Send("look2", l)
		}
	}
	if mine.Bag >= 0 || mine.Mount >= 0 {
		z.broadcast(Encode("look2", mine), 0)
	}
}

// --- set kích hoạt ---

func i2UseCapsule(z *Zone, p *Player, slot int, it *store.Item) bool {
	opts := make([]menuOption, 0, len(i2cfg.Kit.TypeNames))
	labels := make([]string, 0, len(i2cfg.Kit.TypeNames))
	for i, n := range i2cfg.Kit.TypeNames {
		opts = append(opts, menuOption{Label: n, Action: "items2_skh", Arg: itoa(i), Npc: -1})
		labels = append(labels, n)
	}
	p.npcMenu = opts
	p.Send("menu", map[string]any{"npc": -1, "name": i2Name(z, it.TemplateID), "text": "Hãy chọn một món quà", "options": labels})
	return true
}

// i2KitItem builds a kích hoạt item (ItemService.createItemSKH, ItemService.java:256-267).
func i2KitItem(d *Data, tid, set, param int) [][2]int {
	opts := i2ShopOpts(d, tid)
	opts = append(opts, [2]int{set, param})
	for _, sub := range i2cfg.Kit.Subs[itoa(set)] {
		opts = append(opts, [2]int{sub, param})
	}
	return append(opts, [2]int{i2cfg.Kit.NoTrade, param})
}

// i2OpenCapsule: ItemService.OpenSKH (ItemService.java:200-254).
func (z *Zone) i2OpenCapsule(p *Player, arg string) {
	k := &i2cfg.Kit
	d := z.World.Data
	typ, err := strconv.Atoi(arg)
	g := min(max(p.Char.Gender, 0), 2)
	if err != nil || g >= len(k.Items) || typ < 0 || typ >= len(k.Items[g]) || g >= len(k.Sets) {
		return
	}
	if p.CountItem(k.Capsule) < 1 {
		return
	}
	var ids []int
	for _, id := range k.Items[g][typ] {
		if d.ItemsFull[id] != nil {
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return
	}
	tid := ids[rand.IntN(len(ids))]
	set := k.Sets[g][rand.IntN(len(k.Sets[g]))]
	opts := i2KitItem(d, tid, set, 1)
	if !p.i2CanAdd(d, tid, opts) {
		i2Notice(p, "Hành trang đã đầy")
		return
	}
	p.RemoveItems(k.Capsule, 1)
	p.AddItem(d, tid, 1, opts)
	p.dirty = true
	i2Notice(p, "Bạn đã nhận được "+i2Name(z, tid))
	p.sendInventory()
}

func i2Weighted(ws []float64) int {
	total := 0.0
	for _, w := range ws {
		total += w
	}
	r := rand.Float64() * total
	for i, w := range ws {
		if r < w {
			return i
		}
		r -= w
	}
	return len(ws) - 1
}

// i2RollKitDrop returns a kích hoạt item for gender g (randTempItemKichHoat +
// randOptionItemKichHoat), or tid -1 if the data is incomplete.
func i2RollKitDrop(d *Data, g int) (int, [][2]int) {
	dr := &i2cfg.Kit.Drop
	g = min(max(g, 0), 2)
	typ := i2Weighted(dr.TypeWeights)
	if typ >= len(dr.Items) || g >= len(dr.Items[typ]) || g >= len(dr.SetWeights) {
		return -1, nil
	}
	cand := dr.Items[typ][g]
	tid := cand[rand.IntN(len(cand))]
	if d.ItemsFull[tid] == nil {
		return -1, nil
	}
	ws := make([]float64, len(dr.SetWeights[g]))
	for i, s := range dr.SetWeights[g] {
		ws[i] = s[1]
	}
	set := int(dr.SetWeights[g][i2Weighted(ws)][0])
	return tid, i2KitItem(d, tid, set, 0)
}

func i2InstallKitDrop(w *World) {
	w.Events.On(EvKillMob, func(e Event) {
		dr := &i2cfg.Kit.Drop
		if e.Zone == nil || !i2Has(dr.Maps, e.Zone.Map.ID) || !i2Roll(dr.Chance) {
			return
		}
		if tid, opts := i2RollKitDrop(w.Data, e.Player.Char.Gender); tid >= 0 {
			e.Zone.dropItem(tid, 1, opts, e.Player.Char.X, e.Player.Char.Y, e.Player.ID())
		}
	})
}
