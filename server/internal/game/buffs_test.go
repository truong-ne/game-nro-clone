package game

import (
	"testing"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// buffWorld adds the special skills (all races share global template ids).
func buffWorld(t *testing.T) (*World, *Zone) {
	w, z := gameplayWorld(t)
	lv := []SkillLevel{{Point: 1, Damage: 200, Dx: 100, Dy: 100, ManaUse: 1, CoolDown: 10000, MaxFight: 1}}
	for _, id := range []int{skTDHS, skTriThuong, skTTNL, skQCKK, skMakanko, skDeTrung, skBienKhi, skTuSat,
		skSocola, skKhien, skDCTT, skHuytSao, skThoiMien, skTroi, skSuperKame, skLienHoanCh, skMaPhongBa, 3} {
		for g := range 3 {
			w.Data.Skills[[2]int{g, id}] = &SkillTemplate{NClass: g, ID: id, Name: "s" + itoa(id), Type: 1, Levels: lv}
		}
	}
	for _, it := range buffCfg.Items {
		w.Data.ItemsFull[it.ID] = &ItemTemplateFull{ID: it.ID, Type: 29, Name: it.Name}
	}
	for id := range buffCfg.Charms {
		w.Data.ItemsFull[id] = &ItemTemplateFull{ID: id, Type: buffItemCharm, Name: "Bùa"}
	}
	return w, z
}

func buffJoin(t *testing.T, w *World, z *Zone, id int64) (*Player, *fakeConn) {
	p, fc := joinTest(t, w, z, id)
	p.Char.DefBase = 0
	p.Recalc(w.Data)
	return p, fc
}

func buffLearn(p *Player, ids ...int) {
	for _, id := range ids {
		p.Char.Skills = append(p.Char.Skills, store.Skill{TemplateID: id, Level: 1})
	}
	p.Char.MP, p.Char.MPBase = 100000, 100000
	p.MaxMP = 100000
}

func buffUse(z *Zone, p *Player, req map[string]any) {
	p.skillUsed = map[int]time.Time{}
	p.Char.MP = p.MaxMP
	z.useSkill(p, raw(req))
}

// fire runs every pending charged/delayed action now.
func buffFire(z *Zone, p *Player) {
	fx := buffFx(p)
	for i := range fx.pending {
		fx.pending[i].due = time.Now().Add(-time.Millisecond)
	}
	fx.monkeyAt = time.Now().Add(-time.Millisecond)
	buffFxTick(z, time.Now())
}

func duel(a, b *Player) { a.duel, b.duel = b.ID(), a.ID() }

func TestBuffStunExpires(t *testing.T) {
	w, z := buffWorld(t)
	a, _ := buffJoin(t, w, z, 1)
	b, fc := buffJoin(t, w, z, 2)
	duel(a, b)
	buffLearn(a, skTDHS)
	m := z.mobs[0]
	buffUse(z, a, map[string]any{"id": skTDHS})
	if canAct(b) || canAct(a) == false {
		t.Fatal("stun did not lock the target only")
	}
	if !m.attackAt.After(time.Now().Add(2 * time.Second)) {
		t.Fatal("mob not stunned")
	}
	if !sent(fc, "eff") {
		t.Fatal("no eff broadcast")
	}
	z.Handle(b, "move", raw(map[string]int{"x": 230, "y": 100}))
	if b.Char.X != 200 {
		t.Fatal("stunned player moved")
	}
	buffFx(b).until["stun"] = time.Now().Add(-time.Second)
	buffFxSecond(z, time.Now())
	if !canAct(b) || len(buffFx(b).until) != 0 {
		t.Fatal("stun did not expire")
	}
}

func TestBuffShieldAndBreak(t *testing.T) {
	w, z := buffWorld(t)
	p, _ := buffJoin(t, w, z, 1)
	buffLearn(p, skKhien)
	buffUse(z, p, map[string]any{"id": skKhien})
	p.lastHit = time.Time{}
	hp := p.Char.HP
	if got := p.takeDamage(100, 0); got != 1 || p.Char.HP != hp-1 {
		t.Fatalf("shield let %d through", got)
	}
	p.lastHit = time.Time{}
	p.takeDamage(p.MaxHP*10, 0) // breaks the shield, still only 1
	if buffFx(p).active("shield", time.Now()) {
		t.Fatal("shield not broken")
	}
}

func TestBuffMonkeyTransform(t *testing.T) {
	w, z := buffWorld(t)
	p, _ := buffJoin(t, w, z, 1)
	buffLearn(p, skBienKhi)
	hp0, dmg0 := p.MaxHP, p.Dmg
	buffUse(z, p, map[string]any{"id": skBienKhi})
	if canAct(p) {
		t.Fatal("transforming player can act")
	}
	buffFire(z, p)
	if p.MaxHP != hp0*(100+buffPctHPMonkey(1))/100 || p.Dmg < dmg0 || p.Crit < 100 {
		t.Fatalf("monkey stats hp=%d/%d dmg=%d crit=%d", p.MaxHP, hp0, p.Dmg, p.Crit)
	}
	buffFx(p).until["monkey"] = time.Now().Add(-time.Second)
	buffFxSecond(z, time.Now())
	if p.MaxHP != hp0 || buffFx(p).monkeyLv != 0 {
		t.Fatal("monkey did not end")
	}
}

func TestBuffHealRevives(t *testing.T) {
	w, z := buffWorld(t)
	a, _ := buffJoin(t, w, z, 1)
	b, _ := buffJoin(t, w, z, 2)
	buffLearn(a, skTriThuong)
	z.playerDie(b, nil)
	buffUse(z, a, map[string]any{"id": skTriThuong, "player": 2})
	if b.Dead || b.Char.HP != b.MaxHP*buffPctTriThuong(1)/100 {
		t.Fatalf("heal: dead=%v hp=%d", b.Dead, b.Char.HP)
	}
}

func TestBuffMakankosappoFromKI(t *testing.T) {
	w, z := buffWorld(t)
	p, fc := buffJoin(t, w, z, 1)
	buffLearn(p, skMakanko)
	m := z.mobs[0]
	m.MaxHP, m.HP = 1_000_000, 1_000_000
	buffUse(z, p, map[string]any{"id": skMakanko, "mob": 0})
	if m.HP != m.MaxHP {
		t.Fatal("hit before charging")
	}
	buffFire(z, p)
	want := p.MaxMP * 200 / 100
	if lost := m.MaxHP - m.HP; lost < want || lost > want+1 || !sent(fc, "mob_hit") {
		t.Fatalf("makanko dmg=%d want %d", lost, want)
	}
}

func TestBuffQCKKAreaKills(t *testing.T) {
	w, z := buffWorld(t)
	p, _ := buffJoin(t, w, z, 1)
	buffLearn(p, skQCKK)
	p.Char.DmgBase = 1000
	p.Recalc(w.Data)
	buffUse(z, p, map[string]any{"id": skQCKK, "mob": 0})
	buffFire(z, p)
	if z.mobs[0].Alive() {
		t.Fatal("QCKK should one-shot a full-HP mob (dieWhenHpFull)")
	}
}

func TestBuffSleepHoldSocola(t *testing.T) {
	w, z := buffWorld(t)
	a, _ := buffJoin(t, w, z, 1)
	b, _ := buffJoin(t, w, z, 2)
	duel(a, b)
	buffLearn(a, skThoiMien, skTroi, skSocola)
	buffUse(z, a, map[string]any{"id": skThoiMien, "player": 2})
	if canAct(b) {
		t.Fatal("not asleep")
	}
	buffFx(b).until = map[string]time.Time{}
	buffUse(z, a, map[string]any{"id": skTroi, "mob": 0})
	if !z.mobs[0].attackAt.After(time.Now()) {
		t.Fatal("mob not held")
	}
	buffUse(z, a, map[string]any{"id": skSocola, "player": 2})
	if got := buffFxOutDamage(b, 500, &AttackCtx{}); got != 1 {
		t.Fatalf("chocolate deals %d", got)
	}
}

func TestBuffSuperKameDirection(t *testing.T) {
	w, z := buffWorld(t)
	p, _ := buffJoin(t, w, z, 1)
	buffLearn(p, skSuperKame)
	p.Char.X = 100 // mob at x=200 is in front (right)
	buffUse(z, p, map[string]any{"id": skSuperKame, "mob": 0})
	buffFire(z, p)
	if z.mobs[0].HP == z.mobs[0].MaxHP {
		t.Fatal("beam missed the mob in front")
	}
	m := z.mobs[0]
	m.HP = m.MaxHP
	p.Char.X = 300
	p.Dir = 1
	buffUse(z, p, map[string]any{"id": skLienHoanCh})
	buffFire(z, p)
	if m.HP != m.MaxHP {
		t.Fatal("beam hit a mob behind")
	}
}

func TestBuffItemTimers(t *testing.T) {
	w, z := buffWorld(t)
	p, _ := buffJoin(t, w, z, 1)
	hp0 := p.MaxHP
	p.AddItem(w.Data, 382, 1, nil)
	p.AddItem(w.Data, 1152, 1, nil)
	z.useItem(p, buffSlot(p, 382))
	if p.MaxHP != hp0*2 || p.CountItem(382) != 0 {
		t.Fatalf("bổ huyết hp=%d", p.MaxHP)
	}
	z.useItem(p, buffSlot(p, 1152))
	if p.CountItem(1152) != 1 {
		t.Fatal("same-group item used together")
	}
	// Offline time does not count: 5 minutes left stays 5 minutes.
	s := buffItems(p)
	s.Until["bo_huyet"] = time.Now().UnixMilli() + 5*60_000
	s.Seen = time.Now().UnixMilli() - 3600_000
	for _, h := range joinHooks {
		h(w, p)
	}
	if left := s.Until["bo_huyet"] - time.Now().UnixMilli(); left < 4*60_000 {
		t.Fatalf("offline time counted, left=%d", left)
	}
	s.Until["bo_huyet"] = time.Now().UnixMilli() - 1
	buffItemsSecond(z, time.Now())
	if p.MaxHP != hp0 {
		t.Fatal("buff did not expire")
	}
	// Giáp xên halves damage.
	s.Until["giap_xen"] = time.Now().UnixMilli() + 60_000
	p.lastHit = time.Time{}
	p.Char.HP = p.MaxHP
	p.Def = 0
	if got := p.takeDamage(100, 0); got != 50 {
		t.Fatalf("giáp xên took %d", got)
	}
}

func buffSlot(p *Player, tid int) int {
	for _, it := range p.Char.Items {
		if it.Container == store.ContainerBag && it.TemplateID == tid {
			return it.Slot
		}
	}
	return -1
}

func TestBuffCharms(t *testing.T) {
	w, z := buffWorld(t)
	p, _ := buffJoin(t, w, z, 1)
	// A bought charm (tagged with minutes by the shop hook) activates by itself.
	p.AddItem(w.Data, 213, 1, [][2]int{{buffOptMinute, 480}})
	p.AddItem(w.Data, 215, 1, nil)
	p.AddItem(w.Data, 214, 1, nil)
	buffItemsSecond(z, time.Now())
	if p.CountItem(213) != 0 || !buffCharm(p, "tri_tue") || !buffCharm(p, "da_trau") {
		t.Fatal("charms not activated")
	}
	if left := buffCharms(p).Until["tri_tue"] - time.Now().UnixMilli(); left < 479*60_000 {
		t.Fatalf("tri tue minutes=%d", left/60_000)
	}
	before := p.Char.Power
	p.gainPower(100)
	if p.Char.Power-before != 200 {
		t.Fatalf("trí tuệ gain=%d", p.Char.Power-before)
	}
	p.lastHit = time.Time{}
	p.Def = 0
	mobHit := DamageSrc{Mob: z.mobs[0]}
	if got := p.takeDamageFrom(100, 0, mobHit); got != 50 {
		t.Fatalf("da trâu took %d", got)
	}
	p.lastHit = time.Time{}
	if got := p.takeDamageFrom(100, 0, DamageSrc{Player: p}); got != 100 {
		t.Fatalf("da trâu must not cover player hits, took %d", got)
	}
	if got := p.attack(&AttackCtx{Level: &SkillLevel{Damage: 100}, Mob: z.mobs[0]}); got.Dmg < p.Dmg*2 {
		t.Fatalf("mạnh mẽ dmg=%d base=%d", got.Dmg, p.Dmg)
	}
	buffCharms(p).Until["bat_tu"] = time.Now().UnixMilli() + 60_000
	p.lastHit = time.Time{}
	p.takeDamageFrom(1<<40, 0, mobHit)
	if p.Char.HP != 1 {
		t.Fatalf("bất tử hp=%d", p.Char.HP)
	}
}

func TestBuffCharmShopTag(t *testing.T) {
	w, _ := buffWorld(t)
	s := &Shop{ID: 7, NpcID: 21, Tag: "BUA_8H", Tabs: []ShopTab{{Items: []*ShopItem{{ID: 1, TemplateID: 213}}}}}
	w.Data.Shops[7] = s
	buffTagCharmShops(w.Data)
	if o := s.Tabs[0].Items[0].Options; len(o) != 1 || o[0] != [2]int{buffOptMinute, 480} {
		t.Fatalf("options %v", o)
	}
}

func TestBuffIntrinsic(t *testing.T) {
	w, z := buffWorld(t)
	old := buffIntrinsics
	t.Cleanup(func() { buffIntrinsics = old })
	buffIntrinsics = []*buffIntrinsic{
		{ID: 0, Gender: 3, Name: "Chưa kích hoạt"},
		{ID: 1, Gender: 0, Name: "Chiêu đấm Dragon +p0% đến p1% sát thương", P1Min: 20, P1Max: 20},
		{ID: 8, Gender: 1, Name: "Demon", P1Min: 5, P1Max: 25},
	}
	p, fc := buffJoin(t, w, z, 1)
	p.Char.Power = buffCfg.Intrinsic.MinPower
	p.Char.Gold = 100_000_000
	z.menuAction(p, menuOption{Action: "buff_intrinsic_open"})
	st := buffIntr(p)
	if st.ID != 1 || st.P1 != 20 || st.Count != 1 || p.Char.Gold != 90_000_000 {
		t.Fatalf("open: %+v gold=%d", st, p.Char.Gold)
	}
	if buffIntrinsicName(st) != "Chiêu đấm Dragon +20% sát thương" {
		t.Fatalf("name %q", buffIntrinsicName(st))
	}
	tmpl := w.Data.Skills[[2]int{0, 0}]
	h := p.attack(&AttackCtx{Tmpl: tmpl, Level: &SkillLevel{Damage: 100}})
	if h.Dmg < p.Dmg*120/100*90/100 {
		t.Fatalf("intrinsic dmg=%d", h.Dmg)
	}
	z.Handle(p, "buff_intrinsic_menu", nil)
	if !sent(fc, "menu") || len(p.npcMenu) != 3 {
		t.Fatal("no intrinsic menu")
	}
	// Cooldown reduction (intrinsic 3: Thái Dương Hạ San).
	st.ID, st.P1 = 3, 50
	buffLearn(p, skTDHS)
	buffUse(z, p, map[string]any{"id": skTDHS})
	if since := time.Since(p.skillUsed[skTDHS]); since < 4900*time.Millisecond {
		t.Fatalf("cooldown not reduced: %v", since)
	}
}

func TestBuffRadarCards(t *testing.T) {
	w, z := buffWorld(t)
	old := buffRadar
	t.Cleanup(func() { buffRadar = old })
	buffRadar = map[int]*buffRadarCard{828: {ID: 828, Max: 2, Mob: 1, Require: -1,
		Options: []buffRadarOpt{{6, 1000, 0}, {6, 2000, 1}, {6, 3000, 2}}}}
	w.Data.ItemsFull[828] = &ItemTemplateFull{ID: 828, Type: buffItemCard, Name: "Mảnh Khủng long", Stackable: true}
	p, fc := buffJoin(t, w, z, 1)
	hp0 := p.MaxHP
	p.AddItem(w.Data, 828, 3, nil)
	for range 3 {
		z.useItem(p, buffSlot(p, 828))
	}
	c := buffCards(p).Cards[828]
	if c == nil || c.Level != 1 || c.Amount != 1 || p.CountItem(828) != 0 || !sent(fc, "buff_radar") {
		t.Fatalf("card %+v left=%d", c, p.CountItem(828))
	}
	z.Handle(p, "buff_radar_use", raw(map[string]int{"id": 828}))
	if p.MaxHP != hp0+2000 {
		t.Fatalf("card hp=%d want %d", p.MaxHP, hp0+2000)
	}
	// Drops from the card's mob.
	cfg := buffCfg.Radar.DropChance
	buffCfg.Radar.DropChance = 100
	t.Cleanup(func() { buffCfg.Radar.DropChance = cfg })
	n := len(z.ground)
	buffRadarDrop(z, p, 1)
	if len(z.ground) != n+1 {
		t.Fatal("card did not drop")
	}
}

func TestBuffChargeAndHuytSao(t *testing.T) {
	w, z := buffWorld(t)
	p, _ := buffJoin(t, w, z, 1)
	buffLearn(p, skTTNL, skHuytSao)
	p.Char.HP = 1
	buffUse(z, p, map[string]any{"id": skTTNL})
	buffFxSecond(z, time.Now())
	if p.Char.HP <= 1 {
		t.Fatal("no charge regen")
	}
	hp0 := p.MaxHP
	buffUse(z, p, map[string]any{"id": skHuytSao})
	if p.MaxHP != hp0*(100+buffPctHuytSao(1))/100 {
		t.Fatalf("huýt sáo hp=%d", p.MaxHP)
	}
}

func TestBuffMafubaAndTuSat(t *testing.T) {
	w, z := buffWorld(t)
	a, _ := buffJoin(t, w, z, 1)
	b, _ := buffJoin(t, w, z, 2)
	duel(a, b)
	buffLearn(a, skMaPhongBa, skTuSat)
	buffUse(z, a, map[string]any{"id": skMaPhongBa})
	buffFire(z, a)
	if canAct(b) || !buffFx(b).active("binh", time.Now()) {
		t.Fatal("not trapped")
	}
	buffUse(z, a, map[string]any{"id": skTuSat})
	buffFire(z, a)
	if !a.Dead || z.mobs[0].HP == z.mobs[0].MaxHP {
		t.Fatalf("explosion: dead=%v mob hp=%d", a.Dead, z.mobs[0].HP)
	}
}
