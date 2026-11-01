package game

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// gameplayWorld builds a small world with one map, one mob, items, a skill,
// a shop and the embedded content.json.
func gameplayWorld(t *testing.T) (*World, *Zone) {
	t.Helper()
	m := &MapTemplate{ID: 0, Name: "Test", TileID: 1, Width: 100, Height: 30, Tiles: make([]byte, 3000), MaxPlayer: 10, Zones: 1,
		Mobs: []MapMob{{TemplateID: 1, Level: 1, HP: 0, X: 200, Y: 100}}}
	d := &Data{
		Maps:    map[int]*MapTemplate{0: m, 21: {ID: 21, Width: 10, Height: 10, Tiles: make([]byte, 100), MaxPlayer: 10, Zones: 1}},
		Items:   map[int]*ItemTemplate{0: {ID: 0, Type: 0, Part: 14}, 3: {ID: 3, Type: 0, Part: 20}, 6: {ID: 6, Type: 1, Part: 15}},
		TileTop: map[int]map[byte]bool{},
		Mobs:    map[int]*MobTemplate{1: {ID: 1, Name: "Khủng long", HP: 1000, PctDmg: 5, PctPotent: 100}},
		Skills: map[[2]int]*SkillTemplate{{0, 0}: {NClass: 0, ID: 0, Name: "Chiêu đấm Dragon", Type: 1,
			Levels: []SkillLevel{{Point: 1, Damage: 100, Dx: 40, ManaUse: 1, CoolDown: 0, MaxFight: 1}}}},
		ItemsFull: map[int]*ItemTemplateFull{
			0:  {ID: 0, Type: ItemShirt, Gender: 3, Name: "Áo", Part: 14},
			3:  {ID: 3, Type: ItemShirt, Gender: 3, Name: "Áo xịn", Part: 20},
			6:  {ID: 6, Type: ItemPants, Gender: 3, Name: "Quần", Part: 15},
			13: {ID: 13, Type: ItemBean, Name: "Đậu thần cấp 1", Stackable: true},
			63: {ID: 63, Type: ItemBean, Name: "Đậu thần cấp 5", Stackable: true},
			12: {ID: 12, Type: ItemRadar, Gender: 3, Name: "Rada"},
			76: {ID: 76, Type: ItemGold, Name: "Vàng", Stackable: true},
		},
		npcNames: map[int]string{0: "Ông Gôhan"},
	}
	if err := loadContent(d); err != nil {
		t.Fatal(err)
	}
	shop := &Shop{ID: 1, NpcID: 7, items: map[int]*ShopItem{}}
	shop.items[10] = &ShopItem{ID: 10, TemplateID: 3, Currency: CurrencyGold, Cost: 500, Options: [][2]int{{47, 5}}}
	d.Shops = map[int]*Shop{1: shop}
	d.Tasks = []*TaskMain{{ID: 0, Name: "T0", Subs: []*TaskSub{
		{Name: "Đánh 2 con Khủng long", MaxCount: 2, Kind: TrigKill, Mobs: []int{1}},
		{Name: "Báo cáo với %2", MaxCount: 1, Kind: TrigTalk, Npc: -2},
	}}, {ID: 1, Name: "T1", Subs: []*TaskSub{{Name: "x", MaxCount: 1, Kind: TrigManual}}}}
	w := NewWorld(d, nil)
	t.Cleanup(func() { close(w.stop) })
	return w, w.zones[0][0]
}

func joinTest(t *testing.T, w *World, z *Zone, id int64) (*Player, *fakeConn) {
	c, err := NewCharacter(id, "player"+itoa(int(id)), 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = id
	fc := &fakeConn{}
	p := NewPlayer(c, false, fc, w.Data)
	w.byChar[id] = p
	z.add(p, 200, 100)
	return p, fc
}

func sent(fc *fakeConn, typ string) bool {
	for _, m := range fc.msgs {
		if strings.HasPrefix(m, `{"t":"`+typ+`"`) {
			return true
		}
	}
	return false
}

func raw(v any) json.RawMessage { b, _ := json.Marshal(v); return b }

func TestPointCost(t *testing.T) {
	c := &store.Character{HPBase: 200, DmgBase: 10, DefBase: 0}
	if cost, val, ok := PointCost(c, StatHP, 1, 0); !ok || val != 220 || cost != 1200 {
		t.Fatalf("hp cost=%d val=%d ok=%v", cost, val, ok)
	}
	if cost, _, _ := PointCost(c, StatDmg, 1, 0); cost != 1000 {
		t.Fatalf("dmg cost=%d", cost)
	}
	// The legacy def cost ignored n; now each point is charged.
	one, _, _ := PointCost(c, StatDef, 1, 0)
	three, _, _ := PointCost(c, StatDef, 3, 0)
	if three <= one*2 {
		t.Fatalf("def cost not per point: 1=%d 3=%d", one, three)
	}
	c.HPBase = 219990
	if _, _, ok := PointCost(c, StatHP, 1, 0); ok {
		t.Fatal("hp over the power limit accepted")
	}
}

func TestOptionsRecalc(t *testing.T) {
	w, _ := gameplayWorld(t)
	c, _ := NewCharacter(1, "player1", 0, 64)
	c.Items = append(c.Items, store.Item{Container: store.ContainerBody, Slot: 2, TemplateID: 0,
		Options: [][2]int{{77, 50}, {50, 100}, {94, 200}}})
	p := NewPlayer(c, false, &fakeConn{}, w.Data)
	// HP: (200 base + 30 pants) * 150%; dmg: 10 * 200%; armor capped.
	if p.MaxHP != 345 || p.Dmg != 20 || p.Mods.Armor != maxArmor {
		t.Fatalf("hp=%d dmg=%d armor=%d", p.MaxHP, p.Dmg, p.Mods.Armor)
	}
}

func TestKillMobDropsAndTask(t *testing.T) {
	w, z := gameplayWorld(t)
	p, fc := joinTest(t, w, z, 1)
	p.Char.DmgBase = 2000
	p.Recalc(w.Data)
	m := z.mobs[0]
	before := p.Char.Power
	z.useSkill(p, raw(map[string]any{"id": 0, "mob": 0}))
	if m.HP != 1 {
		t.Fatalf("full-HP mob should survive at 1 HP, hp=%d", m.HP)
	}
	if p.Char.Power <= before {
		t.Fatal("no power gained")
	}
	p.skillUsed = map[int]time.Time{}
	z.useSkill(p, raw(map[string]any{"id": 0, "mob": 0}))
	if m.Alive() || !sent(fc, "mob_die") {
		t.Fatal("mob did not die")
	}
	if p.Char.TaskCount != 1 {
		t.Fatalf("task count = %d", p.Char.TaskCount)
	}
	// Respawn after 3s.
	m.diedAt = time.Now().Add(-4 * time.Second)
	z.updateMobs(time.Now())
	if !m.Alive() || m.HP != m.MaxHP {
		t.Fatal("mob did not respawn")
	}
}

func TestTaskChain(t *testing.T) {
	w, z := gameplayWorld(t)
	p, _ := joinTest(t, w, z, 1)
	for range 2 {
		w.Events.Emit(Event{Kind: EvKillMob, Player: p, Zone: z, Mob: 1})
	}
	if p.Char.TaskIndex != 1 {
		t.Fatalf("index = %d", p.Char.TaskIndex)
	}
	gold := p.Char.Gold
	w.Events.Emit(Event{Kind: EvTalkNpc, Player: p, Zone: z, Npc: 5}) // wrong npc
	w.Events.Emit(Event{Kind: EvTalkNpc, Player: p, Zone: z, Npc: 0}) // grandpa (Earth)
	if p.Char.TaskID != 1 || p.Char.TaskIndex != 0 || p.Char.Gold <= gold {
		t.Fatalf("task=%d idx=%d gold=%d", p.Char.TaskID, p.Char.TaskIndex, p.Char.Gold)
	}
}

func TestDeathAndRevive(t *testing.T) {
	w, z := gameplayWorld(t)
	p, fc := joinTest(t, w, z, 1)
	p.takeDamage(1_000_000, 0)
	if p.Char.HP != 0 {
		t.Fatalf("hp = %d", p.Char.HP)
	}
	z.playerDie(p, nil)
	z.Handle(p, "move", raw(map[string]int{"x": 210, "y": 100}))
	if p.Char.X != 200 {
		t.Fatal("dead player moved")
	}
	gems := p.Char.Gem
	z.Handle(p, "revive", raw(map[string]bool{"home": false}))
	if p.Dead || p.Char.HP != p.MaxHP || p.Char.Gem != gems-reviveGemCost || !sent(fc, "revive") {
		t.Fatalf("revive failed: dead=%v hp=%d gem=%d", p.Dead, p.Char.HP, p.Char.Gem)
	}
	if got := p.takeDamage(50, 0); got != 0 {
		t.Fatal("no invulnerability after revive")
	}
}

func TestInventoryEquipShopTrade(t *testing.T) {
	w, z := gameplayWorld(t)
	a, _ := joinTest(t, w, z, 1)
	b, _ := joinTest(t, w, z, 2)
	// Stacking beans.
	a.AddItem(w.Data, 13, 2, nil)
	a.AddItem(w.Data, 13, 3, nil)
	if a.CountItem(13) != 5 {
		t.Fatalf("beans = %d", a.CountItem(13))
	}
	// Buy from the opened shop, then equip: old shirt goes to the bag slot.
	a.shop = 1
	gold := a.Char.Gold
	z.shopBuy(a, 10, 1)
	if a.Char.Gold != gold-500 || a.CountItem(3) != 1 {
		t.Fatalf("buy: gold=%d count=%d", a.Char.Gold, a.CountItem(3))
	}
	var slot int
	for _, it := range a.Char.Items {
		if it.TemplateID == 3 {
			slot = it.Slot
		}
	}
	z.equip(a, slot)
	if worn := a.item(store.ContainerBody, 0); worn == nil || worn.TemplateID != 3 || a.Body != 20 {
		t.Fatalf("equip failed: %+v body=%d", worn, a.Body)
	}
	if old := a.item(store.ContainerBag, slot); old == nil || old.TemplateID != 0 {
		t.Fatalf("old shirt not swapped into bag: %+v", old)
	}
	// Trade: a gives 5 beans + 100 gold, b gives the radar from its box→bag.
	b.Char.Items[3].Container, b.Char.Items[3].Slot = store.ContainerBag, 5
	var beanSlot int
	for _, it := range a.Char.Items {
		if it.TemplateID == 13 {
			beanSlot = it.Slot
		}
	}
	z.Handle(a, "trade_req", raw(map[string]int64{"id": 2}))
	z.Handle(b, "trade_accept", nil)
	z.Handle(a, "trade_offer", raw(map[string]any{"slots": []int{beanSlot}, "gold": 100}))
	z.Handle(b, "trade_offer", raw(map[string]any{"slots": []int{5}}))
	ag, bg := a.Char.Gold, b.Char.Gold
	z.Handle(a, "trade_lock", nil)
	z.Handle(b, "trade_lock", nil)
	if b.CountItem(13) != 5 || a.CountItem(12) != 1 || a.Char.Gold != ag-100 || b.Char.Gold != bg+100 || a.trade != nil {
		t.Fatalf("trade: b beans=%d a radar=%d gold a=%d b=%d", b.CountItem(13), a.CountItem(12), a.Char.Gold, b.Char.Gold)
	}
}

func TestMagicTree(t *testing.T) {
	w, z := gameplayWorld(t)
	p, _ := joinTest(t, w, z, 1)
	z.harvestTree(p) // new tree starts with 5 peas
	if p.CountItem(13) != 5 {
		t.Fatalf("harvest = %d", p.CountItem(13))
	}
	// Peas ripen at grow_at, +60s, +120s...: 3.5 minutes ago means 4 are ready.
	p.Char.Extra.MagicTree.GrowAt = time.Now().Unix() - 3*60 - 30
	if tr := z.treeState(p); tr.Peas != 4 {
		t.Fatalf("regrown peas = %d", tr.Peas)
	}
}

func TestDuelAndBoss(t *testing.T) {
	w, z := gameplayWorld(t)
	a, _ := joinTest(t, w, z, 1)
	b, _ := joinTest(t, w, z, 2)
	if z.canPvP(a, b) {
		t.Fatal("pvp allowed without duel")
	}
	z.Handle(a, "duel_req", raw(map[string]int64{"id": 2}))
	z.Handle(b, "duel_accept", raw(map[string]int64{"id": 1}))
	if !z.canPvP(a, b) {
		t.Fatal("duel did not start")
	}
	z.playerDie(b, a)
	if a.duel != 0 || b.duel != 0 {
		t.Fatal("duel not ended on death")
	}

	died := false
	bt := &BossTemplate{ID: "x", Name: "X", HP: 100, Def: 10, Rewards: []Reward{{Item: 76, Min: 5, Max: 5, Chance: 100}}}
	boss := &Boss{T: bt, Key: "x", HP: 100, onDeath: func(*Player) { died = true }}
	z.spawnBoss(boss)
	z.hitBoss(a, boss, hitResult{Dmg: 200})
	if !died || len(z.bosses) != 0 || len(z.ground) == 0 {
		t.Fatalf("boss: died=%v bosses=%d ground=%d", died, len(z.bosses), len(z.ground))
	}
}
