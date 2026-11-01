package game

import (
	"testing"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// i2World is gameplayWorld plus the templates and NPCs the item economy uses.
func i2World(t *testing.T) (*World, *Zone, *Player, *fakeConn) {
	t.Helper()
	w, z := gameplayWorld(t)
	add := func(id, typ int, name string, stack bool) {
		w.Data.ItemsFull[id] = &ItemTemplateFull{ID: id, Type: typ, Gender: 3, Name: name, Stackable: stack}
	}
	add(189, ItemGold, "Vàng", true)
	add(223, 14, "Đá Titan", true)
	add(987, 27, "Đá bảo vệ", true)
	add(441, 30, "Sao pha lê đỏ", true)
	add(15, 12, "Ngọc Rồng 2 sao", true)
	add(14, 12, "Ngọc Rồng 1 sao", true)
	add(454, 27, "Bông tai Porata", true)
	add(921, 27, "Bông tai Porata", true)
	add(933, 27, "Mảnh vỡ bông tai", true)
	add(457, 27, "Thỏi vàng", true)
	add(380, 27, "Viên Capsule kì bí", true)
	add(381, 29, "Cuồng nộ", true)
	add(1655, 27, "Capsule kích hoạt", true)
	add(1798, 75, "Tayaki", true)
	add(821, 27, "Vé quay", true)
	add(11, ItemFlag, "Balo", false)
	add(33, ItemShirt, "Áo kích hoạt", false)
	for _, id := range []int{0, 3, 136, 137, 138, 139, 230, 231, 232, 233, 34} {
		if w.Data.ItemsFull[id] == nil {
			add(id, ItemShirt, "Áo", false)
		}
	}
	z.Map.Npcs = append(z.Map.Npcs, MapNpc{TemplateID: 21, X: 200, Y: 100}, MapNpc{TemplateID: 19, X: 200, Y: 100},
		MapNpc{TemplateID: 42, X: 200, Y: 100})
	p, fc := joinTest(t, w, z, 1)
	p.Char.Gold, p.Char.Gem = 1_000_000_000, 100_000
	return w, z, p, fc
}

func i2Bag(p *Player, tid int) *store.Item {
	for i := range p.Char.Items {
		if it := &p.Char.Items[i]; it.Container == store.ContainerBag && it.TemplateID == tid {
			return it
		}
	}
	return nil
}

func TestI2ConfigConsistent(t *testing.T) {
	u, c := i2cfg.Upgrade, i2cfg.Crystal
	if len(u.Stones) != u.MaxLevel || len(u.Gold) != u.MaxLevel || len(u.Rate) != u.MaxLevel {
		t.Fatalf("upgrade tables %d/%d/%d for max level %d", len(u.Stones), len(u.Gold), len(u.Rate), u.MaxLevel)
	}
	if len(c.Gold) != c.MaxStar || len(c.Gem) != c.MaxStar || len(c.Rate) != c.MaxStar {
		t.Fatal("crystallize tables do not match max_star")
	}
	if len(i2cfg.Kit.Items) != 3 || len(i2cfg.Kit.Sets) != 3 || len(i2cfg.Lucky.Pool) == 0 {
		t.Fatal("kit/lucky data incomplete")
	}
	for _, r := range i2cfg.Recipes {
		if len(r.Inputs) == 0 || len(r.Outputs) == 0 {
			t.Fatalf("recipe %s incomplete", r.ID)
		}
	}
}

func TestI2Upgrade(t *testing.T) {
	_, z, p, _ := i2World(t)
	saved := append([]float64(nil), i2cfg.Upgrade.Rate...)
	defer func() { i2cfg.Upgrade.Rate = saved }()
	p.AddItem(z.World.Data, 3, 1, [][2]int{{47, 100}})
	gear := i2Bag(p, 3).Slot
	p.AddItem(z.World.Data, 223, 100, nil)
	gold := p.Char.Gold

	i2cfg.Upgrade.Rate = []float64{100, 100, 0, 0, 0, 0, 0, 0}
	z.Handle(p, "craft_do", raw(map[string]any{"kind": "upgrade", "slot": gear}))
	it := p.item(store.ContainerBag, gear)
	if i2OptVal(it.Options, 47) != 110 || i2OptVal(it.Options, 72) != 1 || p.CountItem(223) != 97 || p.Char.Gold != gold-10000 {
		t.Fatalf("upgrade +1: opts=%v stones=%d gold=%d", it.Options, p.CountItem(223), gold-p.Char.Gold)
	}
	z.Handle(p, "craft_do", raw(map[string]any{"kind": "upgrade", "slot": gear}))
	// Level 2 fails and drops back to +1 (NangCapVatPham.java:220-237).
	z.Handle(p, "craft_do", raw(map[string]any{"kind": "upgrade", "slot": gear}))
	it = p.item(store.ContainerBag, gear)
	if i2OptVal(it.Options, 72) != 1 || i2OptVal(it.Options, 209) != 1 {
		t.Fatalf("downgrade: %v", it.Options)
	}
	// With a protection stone the level is kept.
	p.AddItem(z.World.Data, 987, 1, nil)
	z.Handle(p, "craft_do", raw(map[string]any{"kind": "upgrade", "slot": gear})) // +1 -> +2 (rate 100)
	z.Handle(p, "craft_do", raw(map[string]any{"kind": "upgrade", "slot": gear, "protect": true}))
	it = p.item(store.ContainerBag, gear)
	if i2OptVal(it.Options, 72) != 2 || p.CountItem(987) != 0 {
		t.Fatalf("protect: %v protect=%d", it.Options, p.CountItem(987))
	}
}

func TestI2CrystallizeAndSocket(t *testing.T) {
	_, z, p, fc := i2World(t)
	saved := i2cfg.Crystal.Rate[0]
	defer func() { i2cfg.Crystal.Rate[0] = saved }()
	i2cfg.Crystal.Rate[0] = 100
	p.AddItem(z.World.Data, 3, 1, nil)
	gear := i2Bag(p, 3).Slot
	z.Handle(p, "craft_info", raw(map[string]any{"kind": "crystallize", "slot": gear}))
	if !sent(fc, "craft_info") {
		t.Fatal("no preview")
	}
	gem := p.Char.Gem
	z.Handle(p, "craft_do", raw(map[string]any{"kind": "crystallize", "slot": gear, "n": 10}))
	if i2OptVal(p.item(store.ContainerBag, gear).Options, 107) != 1 || p.Char.Gem != gem-1 {
		t.Fatalf("crystallize: %v gem=%d", p.item(store.ContainerBag, gear).Options, gem-p.Char.Gem)
	}
	p.AddItem(z.World.Data, 441, 2, nil)
	stone := i2Bag(p, 441).Slot
	z.Handle(p, "craft_do", raw(map[string]any{"kind": "socket", "slot": gear, "slot2": stone}))
	opts := p.item(store.ContainerBag, gear).Options
	if i2OptVal(opts, 95) != 5 || i2OptVal(opts, 102) != 1 || p.CountItem(441) != 1 {
		t.Fatalf("socket: %v", opts)
	}
	// No free hole left.
	z.Handle(p, "craft_do", raw(map[string]any{"kind": "socket", "slot": gear, "slot2": i2Bag(p, 441).Slot}))
	if p.CountItem(441) != 1 {
		t.Fatal("socketed past the hole count")
	}
}

func TestI2RecipeAndEarring(t *testing.T) {
	_, z, p, _ := i2World(t)
	p.AddItem(z.World.Data, 15, 8, nil)
	z.Handle(p, "craft_do", raw(map[string]any{"kind": "recipe", "recipe": "nr2"}))
	if p.CountItem(15) != 1 || p.CountItem(14) != 1 {
		t.Fatalf("nhập ngọc rồng: 2 sao=%d 1 sao=%d", p.CountItem(15), p.CountItem(14))
	}
	saved := i2cfg.Earrings[0].Rate
	defer func() { i2cfg.Earrings[0].Rate = saved }()
	i2cfg.Earrings[0].Rate = 100
	p.AddItem(z.World.Data, 454, 1, nil)
	p.AddItem(z.World.Data, 933, 10000, nil)
	z.Handle(p, "craft_do", raw(map[string]any{"kind": "earring", "slot": i2Bag(p, 454).Slot}))
	if p.CountItem(454) != 0 || p.CountItem(921) != 1 || p.i2FragCount(933) != 1 {
		t.Fatalf("earring: 454=%d 921=%d frags=%d", p.CountItem(454), p.CountItem(921), p.i2FragCount(933))
	}
	if i2OptVal(i2Bag(p, 921).Options, 72) != 2 {
		t.Fatal("earring level option missing")
	}
	// Legacy fragments keep their count in option 31.
	p.AddItem(z.World.Data, 933, 1, [][2]int{{31, 50}})
	p.i2FragTake(933, 20)
	if p.i2FragCount(933) != 31 {
		t.Fatalf("fragments = %d", p.i2FragCount(933))
	}
}

func TestI2UseItems(t *testing.T) {
	w, z, p, fc := i2World(t)
	p.Char.Gold = 0
	p.AddItem(w.Data, 457, 2, nil)
	z.useItem(p, i2Bag(p, 457).Slot)
	if p.Char.Gold != 37_000_000 || p.CountItem(457) != 1 {
		t.Fatalf("gold bar: gold=%d bars=%d", p.Char.Gold, p.CountItem(457))
	}
	p.AddItem(w.Data, 380, 1, nil)
	z.useItem(p, i2Bag(p, 380).Slot)
	if p.CountItem(380) != 0 || (p.CountItem(381) == 0 && p.Char.Gold == 37_000_000) {
		t.Fatal("capsule kì bí gave nothing")
	}
	// Flag bag goes to body slot 8 and is announced with look2.
	w.Data.ItemsFull[11].Part = 3
	p.AddItem(w.Data, 11, 1, nil)
	z.useItem(p, i2Bag(p, 11).Slot)
	if worn := p.item(store.ContainerBody, 8); worn == nil || worn.TemplateID != 11 || !sent(fc, "look2") {
		t.Fatal("bag not equipped")
	}
	// Capsule kích hoạt: menu, then pick "Áo".
	p.AddItem(w.Data, 1655, 1, nil)
	z.useItem(p, i2Bag(p, 1655).Slot)
	z.Handle(p, "menu", raw(map[string]int{"i": 0}))
	if p.CountItem(1655) != 0 {
		t.Fatal("capsule not consumed")
	}
	found := false
	for _, it := range p.Char.Items {
		if it.Container == store.ContainerBag && i2OptVal(it.Options, 30) == 1 {
			for _, s := range i2cfg.Kit.Sets[p.Char.Gender] {
				if i2OptIdx(it.Options, s) >= 0 {
					found = true
				}
			}
		}
	}
	if !found {
		t.Fatal("no kích hoạt item")
	}
}

func TestI2DropsMerged(t *testing.T) {
	w, _, _, _ := i2World(t)
	i2MergeDrops(w.Data)
	rules := w.Data.Content.dropsFor(1, 0)
	has := func(item int) bool {
		for _, r := range rules {
			if r.Item == item {
				return true
			}
		}
		return false
	}
	if !has(76) || !has(441) || !has(1798) {
		t.Fatalf("map 0 rules missing default or legacy drops: %+v", rules)
	}
	if has(225) { // map 0 is excluded from the 3-planet group
		t.Fatal("3-planet rule applied to map 0")
	}
	tid, opts := i2RollKitDrop(w.Data, 0)
	if tid >= 0 && i2OptIdx(opts, i2cfg.Kit.NoTrade) < 0 {
		t.Fatal("kit drop without no-trade option")
	}
}

func TestI2Lucky(t *testing.T) {
	w, z, p, fc := i2World(t)
	gem := p.Char.Gem
	z.Handle(p, "lucky_spin", raw(map[string]any{"count": 3, "currency": "gem"}))
	st := State[i2LuckyState](p, i2LuckyKey)
	if len(st.Box) != 3 || p.Char.Gem != gem-3*i2cfg.Lucky.PriceGem || !sent(fc, "lucky_result") {
		t.Fatalf("spin: box=%d gem=%d", len(st.Box), gem-p.Char.Gem)
	}
	z.Handle(p, "lucky_spin", raw(map[string]any{"count": 1, "currency": "ticket"}))
	if len(st.Box) != 3 {
		t.Fatal("spun without a ticket")
	}
	st.Box = []i2Stack{{TID: 189, Qty: 1000}, {TID: 15, Qty: 2}}
	gold := p.Char.Gold
	z.Handle(p, "lucky_claim", raw(map[string]int{"i": -1}))
	if len(st.Box) != 0 || p.Char.Gold != gold+1000 || p.CountItem(15) != 2 {
		t.Fatalf("claim: box=%d gold=%d balls=%d", len(st.Box), p.Char.Gold-gold, p.CountItem(15))
	}
	_ = w
}

func TestI2Limit(t *testing.T) {
	_, z, p, _ := i2World(t)
	z.i2OpenLimit(p, 42, "basic")
	if State[i2LimitState](p, i2LimitKey).DoneAt != 0 {
		t.Fatal("opened without enough power")
	}
	p.Char.Power = powerLimits[0].Power
	z.i2OpenLimit(p, 42, "basic")
	st := State[i2LimitState](p, i2LimitKey)
	if st.DoneAt == 0 || p.Char.Limit != 0 {
		t.Fatal("basic open did not start")
	}
	i2LimitTick(p, time.Unix(st.DoneAt, 0))
	if p.Char.Limit != 1 || st.DoneAt != 0 {
		t.Fatalf("limit after timer = %d", p.Char.Limit)
	}
	gold := p.Char.Gold
	z.i2OpenLimit(p, 42, "speed")
	if p.Char.Limit != 2 || p.Char.Gold != gold-i2cfg.Limit.SpeedGold {
		t.Fatalf("speed: limit=%d", p.Char.Limit)
	}
	p.Char.Limit = 5
	z.i2OpenLimit(p, 42, "speed")
	if p.Char.Limit != 5 {
		t.Fatal("Quốc Vương opened past tier 5")
	}
}

func TestI2ConsignRules(t *testing.T) {
	ball := &ItemTemplateFull{ID: 15, Type: 12}
	shirt := &ItemTemplateFull{ID: 3, Type: ItemShirt}
	if !i2CanConsign(ball, &store.Item{TemplateID: 15}) || i2CanConsign(shirt, &store.Item{TemplateID: 3}) {
		t.Fatal("consign whitelist")
	}
	if !i2CanConsign(shirt, &store.Item{Options: [][2]int{{86, 0}}}) || i2CanConsign(ball, &store.Item{Options: [][2]int{{30, 0}}}) {
		t.Fatal("consign option rules")
	}
	if i2ConsignTab(shirt) != 0 || i2ConsignTab(&ItemTemplateFull{Type: 4}) != 1 || i2ConsignTab(ball) != 3 {
		t.Fatal("consign tabs")
	}
	if _, _, ok := i2ConsignPrice(0, "bar"); ok {
		t.Fatal("zero price accepted")
	}
	if b, g, ok := i2ConsignPrice(5, "gem"); !ok || b != 0 || g != 5 {
		t.Fatal("gem price")
	}
}
