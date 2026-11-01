package game

import (
	"encoding/json"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

const (
	BodySlots       = 11
	BagSlots        = 30
	BoxSlots        = 30
	maxGold         = 2_000_000_000
	groundOwnerTime = 15 * time.Second
	groundLifetime  = 60 * time.Second
	pickupRange     = 60
)

// MaxBagSlots caps bag expansion items.
const MaxBagSlots = 100

func containerSize(c int) int {
	switch c {
	case store.ContainerBody:
		return BodySlots
	case store.ContainerBag:
		return BagSlots
	}
	return BoxSlots
}

// size is the container capacity for this player (the bag can be expanded).
func (p *Player) size(c int) int {
	if c == store.ContainerBag {
		return min(BagSlots+p.Char.Extra.BagExtra, MaxBagSlots)
	}
	return containerSize(c)
}

// item returns a pointer to the item in (container, slot), or nil.
func (p *Player) item(container, slot int) *store.Item {
	for i := range p.Char.Items {
		it := &p.Char.Items[i]
		if it.Container == container && it.Slot == slot {
			return it
		}
	}
	return nil
}

func (p *Player) freeSlot(container int) int {
	used := make([]bool, p.size(container))
	for _, it := range p.Char.Items {
		if it.Container == container && it.Slot < len(used) {
			used[it.Slot] = true
		}
	}
	for i, u := range used {
		if !u {
			return i
		}
	}
	return -1
}

func sameOptions(a, b [][2]int) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// AddItem puts items in the bag, stacking when the template allows it.
// Gold/gem pickups go straight to the wallet. Returns false if the bag is full.
func (p *Player) AddItem(d *Data, tid, qty int, opts [][2]int) bool {
	t := d.ItemsFull[tid]
	if t == nil || qty <= 0 {
		return false
	}
	switch t.Type {
	case ItemGold:
		p.Char.Gold = min(p.Char.Gold+int64(qty), maxGold)
		return true
	case ItemGem:
		p.Char.Gem += int64(qty)
		return true
	}
	if t.Stackable {
		for i := range p.Char.Items {
			it := &p.Char.Items[i]
			if it.Container == store.ContainerBag && it.TemplateID == tid && sameOptions(it.Options, opts) {
				it.Quantity += qty
				p.dirty = true
				return true
			}
		}
	}
	slot := p.freeSlot(store.ContainerBag)
	if slot < 0 {
		return false
	}
	p.Char.Items = append(p.Char.Items, store.Item{Container: store.ContainerBag, Slot: slot, TemplateID: tid, Quantity: qty, Options: opts})
	p.dirty = true
	return true
}

// takeItem removes qty from (container, slot); the slot is freed at zero.
func (p *Player) takeItem(container, slot, qty int) bool {
	for i := range p.Char.Items {
		it := &p.Char.Items[i]
		if it.Container == container && it.Slot == slot {
			if it.Quantity < qty {
				return false
			}
			it.Quantity -= qty
			if it.Quantity == 0 {
				p.Char.Items = append(p.Char.Items[:i], p.Char.Items[i+1:]...)
			}
			p.dirty = true
			return true
		}
	}
	return false
}

// CountItem returns how many of template tid are in the bag.
func (p *Player) CountItem(tid int) int {
	n := 0
	for _, it := range p.Char.Items {
		if it.Container == store.ContainerBag && it.TemplateID == tid {
			n += it.Quantity
		}
	}
	return n
}

// RemoveItems takes qty of template tid from the bag (any slots).
func (p *Player) RemoveItems(tid, qty int) bool {
	if p.CountItem(tid) < qty {
		return false
	}
	for qty > 0 {
		for _, it := range p.Char.Items {
			if it.Container == store.ContainerBag && it.TemplateID == tid {
				n := min(qty, it.Quantity)
				p.takeItem(it.Container, it.Slot, n)
				qty -= n
				break
			}
		}
	}
	return true
}

func (p *Player) sendInventory() {
	p.Send("inventory", map[string]any{"items": nonNil(p.Char.Items), "gold": p.Char.Gold, "gem": p.Char.Gem, "ruby": p.Char.Ruby})
}

type slotReq struct {
	Slot int `json:"slot"`
	Qty  int `json:"qty"`
}

func init() {
	register("inventory", func(z *Zone, p *Player, _ json.RawMessage) { p.sendInventory() })
	register("equip", func(z *Zone, p *Player, d json.RawMessage) {
		var r slotReq
		if json.Unmarshal(d, &r) == nil {
			z.equip(p, r.Slot)
		}
	})
	register("unequip", func(z *Zone, p *Player, d json.RawMessage) {
		var r slotReq
		if json.Unmarshal(d, &r) == nil {
			z.unequip(p, r.Slot)
		}
	})
	register("use", func(z *Zone, p *Player, d json.RawMessage) {
		var r slotReq
		if json.Unmarshal(d, &r) == nil {
			z.useItem(p, r.Slot)
		}
	})
	register("discard", func(z *Zone, p *Player, d json.RawMessage) {
		var r slotReq
		if json.Unmarshal(d, &r) == nil && p.item(store.ContainerBag, r.Slot) != nil {
			it := *p.item(store.ContainerBag, r.Slot)
			p.takeItem(store.ContainerBag, r.Slot, it.Quantity)
			p.sendInventory()
		}
	})
	register("box", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ From, Slot int } // From: 1 bag->box, 2 box->bag
		if json.Unmarshal(d, &r) == nil {
			z.moveBox(p, r.From, r.Slot)
		}
	})
	register("pickup", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ ID int }
		if json.Unmarshal(d, &r) == nil {
			z.pickup(p, r.ID)
		}
	})
}

func (z *Zone) equip(p *Player, bagSlot int) {
	it := p.item(store.ContainerBag, bagSlot)
	if it == nil {
		return
	}
	t := z.World.Data.ItemsFull[it.TemplateID]
	slot, ok := equipSlot[t.Type]
	if !ok {
		p.Send("notice", map[string]string{"text": "Không thể mặc vật phẩm này"})
		return
	}
	if t.Gender != 3 && t.Gender != p.Char.Gender {
		p.Send("notice", map[string]string{"text": "Vật phẩm không dành cho hành tinh của bạn"})
		return
	}
	if p.Char.Power < t.PowerRequire {
		p.Send("notice", map[string]string{"text": "Chưa đủ sức mạnh để sử dụng"})
		return
	}
	// Swap: the worn item (if any) takes the bag slot. Both pointers stay
	// valid because the slice is not resized.
	worn := p.item(store.ContainerBody, slot)
	it.Container, it.Slot = store.ContainerBody, slot
	if worn != nil {
		worn.Container, worn.Slot = store.ContainerBag, bagSlot
	}
	z.afterGearChange(p)
}

func (z *Zone) unequip(p *Player, bodySlot int) {
	it := p.item(store.ContainerBody, bodySlot)
	free := p.freeSlot(store.ContainerBag)
	if it == nil || free < 0 {
		return
	}
	it.Container, it.Slot = store.ContainerBag, free
	z.afterGearChange(p)
}

func (z *Zone) afterGearChange(p *Player) {
	p.Recalc(z.World.Data)
	p.dirty = true
	p.Send("self", p.Self())
	p.sendInventory()
	z.broadcast(Encode("look", map[string]any{"id": p.ID(), "body": p.Body, "leg": p.Leg, "head": p.Char.Head}), p.ID())
}

func (z *Zone) moveBox(p *Player, from, slot int) {
	src, dst := store.ContainerBag, store.ContainerBox
	if from == 2 {
		src, dst = dst, src
	}
	it := p.item(src, slot)
	free := p.freeSlot(dst)
	if it == nil || free < 0 {
		return
	}
	it.Container, it.Slot = dst, free
	p.dirty = true
	p.sendInventory()
}

// useItem handles consumables: beans heal by option 2 (HP/KI +#000) or by
// level; skill books teach/upgrade a skill. Other types are not usable yet.
func (z *Zone) useItem(p *Player, slot int) {
	it := p.item(store.ContainerBag, slot)
	if it == nil {
		return
	}
	d := z.World.Data
	t := d.ItemsFull[it.TemplateID]
	if h := useByID[it.TemplateID]; h != nil && h(z, p, slot, it) {
		p.dirty = true
		p.sendInventory()
		return
	}
	if h := useByType[t.Type]; h != nil && h(z, p, slot, it) {
		p.dirty = true
		p.sendInventory()
		return
	}
	switch {
	case t.Type == ItemBean:
		heal := int64(t.Level+1) * 100
		for _, o := range it.Options {
			if o[0] == 2 {
				heal = int64(o[1]) * 1000
			}
		}
		p.Char.HP = min(p.MaxHP, p.Char.HP+heal)
		p.Char.MP = min(p.MaxMP, p.Char.MP+heal)
		p.takeItem(store.ContainerBag, slot, 1)
		p.Send("hpmp", map[string]int64{"hp": p.Char.HP, "mp": p.Char.MP})
	case d.Content.SkillBooks[it.TemplateID] != nil:
		b := d.Content.SkillBooks[it.TemplateID]
		if b.Gender != 3 && b.Gender != p.Char.Gender {
			p.Send("notice", map[string]string{"text": "Sách không dành cho hành tinh của bạn"})
			return
		}
		learned := false
		for i := range p.Char.Skills {
			sk := &p.Char.Skills[i]
			tmpl := d.Skills[[2]int{p.Char.Gender, sk.TemplateID}]
			if tmpl == nil || strings.ToLower(tmpl.Name) != b.Name {
				continue
			}
			if sk.Level != b.Level-1 {
				p.Send("notice", map[string]string{"text": "Cần học cấp trước đó"})
				return
			}
			if lv := tmpl.Level(b.Level); lv == nil || p.Char.Power < lv.PowerRequire {
				p.Send("notice", map[string]string{"text": "Chưa đủ sức mạnh"})
				return
			}
			sk.Level = b.Level
			learned = true
			p.takeItem(store.ContainerBag, slot, 1)
			p.Send("notice", map[string]string{"text": "Đã học " + tmpl.Name + " cấp " + itoa(b.Level)})
			p.Send("self", p.Self())
			break
		}
		if !learned {
			return
		}
	default:
		p.Send("notice", map[string]string{"text": "Chưa thể sử dụng vật phẩm này"})
		return
	}
	p.dirty = true
	p.sendInventory()
}

// GroundItem is an item lying in a zone (drops, death gold).
type GroundItem struct {
	ID         int
	TemplateID int
	Qty        int
	Options    [][2]int
	X, Y       int
	Owner      int64
	dropped    time.Time
}

type GroundView struct {
	ID    int   `json:"id"`
	TID   int   `json:"tid"`
	Qty   int   `json:"qty"`
	X     int   `json:"x"`
	Y     int   `json:"y"`
	Owner int64 `json:"owner"`
}

func (g *GroundItem) View() GroundView {
	return GroundView{g.ID, g.TemplateID, g.Qty, g.X, g.Y, g.Owner}
}

func (z *Zone) itemsView() []GroundView {
	out := make([]GroundView, 0, len(z.ground))
	for _, g := range z.ground {
		out = append(out, g.View())
	}
	return out
}

func (z *Zone) dropItem(tid, qty int, opts [][2]int, x, y int, owner int64) {
	z.nextItem++
	g := &GroundItem{ID: z.nextItem, TemplateID: tid, Qty: qty, Options: opts, X: x + rand.IntN(41) - 20,
		Y: z.World.Data.GroundY(z.Map, x, y-24), Owner: owner, dropped: time.Now()}
	z.ground[g.ID] = g
	z.broadcast(Encode("item_drop", g.View()), 0)
}

func (z *Zone) expireGround(now time.Time) {
	for id, g := range z.ground {
		if now.Sub(g.dropped) > groundLifetime {
			delete(z.ground, id)
			z.broadcast(Encode("item_gone", map[string]int{"id": id}), 0)
		}
	}
}

func (z *Zone) pickup(p *Player, id int) {
	g := z.ground[id]
	if g == nil || dist(p.Char.X, p.Char.Y, g.X, g.Y) > pickupRange {
		return
	}
	if g.Owner != 0 && g.Owner != p.ID() && time.Since(g.dropped) < groundOwnerTime {
		p.Send("notice", map[string]string{"text": "Vật phẩm của người khác"})
		return
	}
	if !p.AddItem(z.World.Data, g.TemplateID, g.Qty, g.Options) {
		p.Send("notice", map[string]string{"text": "Hành trang đã đầy"})
		return
	}
	delete(z.ground, id)
	z.broadcast(Encode("item_gone", map[string]any{"id": id, "by": p.ID()}), 0)
	z.World.Events.Emit(Event{Kind: EvPickup, Player: p, Zone: z, Item: g.TemplateID, Count: g.Qty})
	p.sendInventory()
}

// onMobKilled rolls the data-driven drop table and notifies tasks.
func (z *Zone) onMobKilled(p *Player, m *Mob) {
	for _, r := range z.World.Data.Content.dropsFor(m.T.ID, z.Map.ID) {
		if rand.Float64()*100 >= r.Chance {
			continue
		}
		qty := r.Min
		if r.Max > r.Min {
			qty += rand.IntN(r.Max - r.Min + 1)
		}
		if r.PerLevel > 0 {
			qty += r.PerLevel * max(m.Level, 1)
		}
		z.dropItem(r.Item, max(qty, 1), nil, m.X, m.Y, p.ID())
	}
	z.World.Events.Emit(Event{Kind: EvKillMob, Player: p, Zone: z, Mob: m.T.ID})
}
