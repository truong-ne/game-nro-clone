package game

import (
	"strings"
	"testing"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// arenaWorld is a small world with the maps the arena features use.
func arenaWorld(t *testing.T) *World {
	t.Helper()
	mk := func(id int, mobs ...MapMob) *MapTemplate {
		return &MapTemplate{ID: id, Name: "Map " + itoa(id), Width: 40, Height: 20, Tiles: make([]byte, 800),
			MaxPlayer: 10, Zones: 1, Mobs: mobs}
	}
	maps := map[int]*MapTemplate{}
	for _, id := range []int{0, 21, 22, 23, 24, 25, 26, 51, 52, 85, 86, 113, 136, 138} {
		maps[id] = mk(id)
	}
	maps[135] = mk(135, MapMob{TemplateID: 1, X: 200, Y: 100}, MapMob{TemplateID: 1, X: 300, Y: 100})
	maps[137] = mk(137, MapMob{TemplateID: 2, X: 200, Y: 100})
	d := &Data{
		Maps:    maps,
		Items:   map[int]*ItemTemplate{},
		TileTop: map[int]map[byte]bool{},
		Mobs: map[int]*MobTemplate{1: {ID: 1, Name: "Khủng long", HP: 1000, PctDmg: 5, PctPotent: 100},
			2: {ID: 2, Name: "Lợn lòi", HP: 1000, PctDmg: 5, PctPotent: 100}},
		Skills: map[[2]int]*SkillTemplate{},
		ItemsFull: map[int]*ItemTemplateFull{
			190: {ID: 190, Type: ItemGold, Name: "Vàng", Stackable: true},
			457: {ID: 457, Type: 27, Name: "Thỏi vàng", Stackable: true},
			611: {ID: 611, Type: 27, Name: "Bản đồ kho báu", Stackable: true},
			372: {ID: 372, Type: 11, Name: "Ngọc rồng 1 sao đen"},
			373: {ID: 373, Type: 11, Name: "Ngọc rồng 2 sao đen"},
		},
		npcNames: map[int]string{},
	}
	if err := loadContent(d); err != nil {
		t.Fatal(err)
	}
	arenaInstallData(d)
	w := NewWorld(d, nil)
	t.Cleanup(func() { close(w.stop) })
	return w
}

func arenaJoin(t *testing.T, w *World, mapID int, id int64) *Player {
	c, err := NewCharacter(id, "player"+itoa(int(id)), 0, 64)
	if err != nil {
		t.Fatal(err)
	}
	c.ID = id
	p := NewPlayer(c, false, &fakeConn{}, w.Data)
	w.mu.Lock()
	w.byChar[id] = p
	w.mu.Unlock()
	z := w.zones[mapID][0]
	arenaOn(t, z, func() { z.add(p, 200, 100) })
	return p
}

// arenaOn runs f on z's goroutine and waits for it.
func arenaOn(t *testing.T, z *Zone, f func()) {
	t.Helper()
	done := make(chan struct{})
	z.Post(func() { f(); close(done) })
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		t.Fatal("zone did not run the function")
	}
}

// arenaWait polls cond (evaluated on the player's zone goroutine).
func arenaWait(t *testing.T, w *World, p *Player, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		ok := make(chan bool, 1)
		w.postToPlayer(p, func(*Zone) { ok <- cond() })
		select {
		case v := <-ok:
			if v {
				return
			}
		case <-time.After(200 * time.Millisecond):
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not reached")
}

func arenaClock(t *testing.T, h, m int) {
	prev := arenaNow
	at := time.Date(2026, 11, 1, h, m, 0, 0, vnZone)
	arenaNow = func() time.Time { return at }
	t.Cleanup(func() { arenaNow = prev })
}

func TestArenaContent(t *testing.T) {
	if len(arena.WorldBosses) < 30 {
		t.Fatalf("roster too small: %d", len(arena.WorldBosses))
	}
	fide := arena.stageByID["arena_fide"]
	if fide == nil || len(fide.templates) != 3 || fide.templates[2].ID != "arena_fide_s2" || len(fide.templates[2].Rewards) == 0 ||
		len(fide.templates[0].Rewards) != 0 {
		t.Fatalf("fide chain wrong: %+v", fide)
	}
	if fide.templates[0].HP != 10_000_000 || fide.templates[0].Dmg != 22000 { // BossesData.FIDE_DAI_CA_1
		t.Fatalf("fide stats: %d/%d", fide.templates[0].HP, fide.templates[0].Dmg)
	}
	w := arenaWorld(t)
	if w.bossTemplate("arena_kuku") == nil || w.Data.Content.Dungeons["arena_treasure"] == nil {
		t.Fatal("data hook did not install bosses/dungeons")
	}
	n := len(w.Data.Content.Bosses)
	arenaInstallData(w.Data)
	if len(w.Data.Content.Bosses) != n {
		t.Fatal("install is not idempotent")
	}
}

func TestArenaBossChain(t *testing.T) {
	w := arenaWorld(t)
	w.Events.On(EvKillBoss, func(e Event) { arenaOnBossKilled(w, e) })
	z := w.zones[0][0]
	p := arenaJoin(t, w, 0, 1)
	fide := arena.stageByID["arena_fide"]
	arenaOn(t, z, func() {
		z.spawnBoss(&Boss{T: fide.templates[0], Key: "arena_fide", HP: fide.templates[0].HP})
		for i := 1; i <= 2; i++ {
			z.hitBoss(p, z.bosses[0], hitResult{Dmg: 1 << 40})
			if len(z.bosses) != 1 || z.bosses[0].T.ID != fide.templates[i].ID {
				t.Errorf("stage %d not spawned: %d bosses", i, len(z.bosses))
				return
			}
		}
		z.hitBoss(p, z.bosses[0], hitResult{Dmg: 1 << 40})
		if len(z.bosses) != 0 || len(z.ground) == 0 {
			t.Errorf("final form: bosses=%d drops=%d", len(z.bosses), len(z.ground))
		}
	})
}

func TestArenaTreasureDungeon(t *testing.T) {
	w := arenaWorld(t)
	z := w.zones[0][0]
	p := arenaJoin(t, w, 0, 1)
	p.IsAdmin = true // skips the NPC distance and power checks
	w.clans.clans[7] = &store.Clan{ID: 7, Name: "Test", LeaderID: 1, Members: []store.ClanMember{{ID: 1}}}
	var run *arenaRun
	arenaOn(t, z, func() {
		arenaDgOpen(z, p, "arena_treasure", 5)
		if !strings.Contains(p.conn.(*fakeConn).msgs[len(p.conn.(*fakeConn).msgs)-1], "bang") {
			t.Error("non-clan player was not refused")
		}
		p.Char.ClanID = 7
		arenaDgOpen(z, p, "arena_treasure", 5)
		if arenaRunByKey(w, "arena:arena_treasure/clan:7") != nil {
			t.Error("opened without the map item")
		}
		p.AddItem(w.Data, 611, 1, nil)
		arenaDgOpen(z, p, "arena_treasure", 5)
		run = arenaRunByKey(w, "arena:arena_treasure/clan:7")
		if run == nil || p.CountItem(611) != 0 || arenaState(p).Entries["arena_treasure"] != 1 {
			t.Errorf("open failed: run=%v item=%d", run != nil, p.CountItem(611))
		}
	})
	if run == nil {
		t.FailNow()
	}
	// Mobs scale as level × 469799 × template id (BanDoKhoBau.init).
	bz := run.in.zones[137]
	var boss *Boss
	arenaOn(t, bz, func() {
		if got := bz.mobs[0].MaxHP; got != 5*469799*2 {
			t.Errorf("mob hp = %d", got)
		}
		if len(bz.bosses) != 1 || bz.bosses[0].HP != 5*20_000_000 {
			t.Errorf("boss not placed: %d", len(bz.bosses))
			return
		}
		boss = bz.bosses[0]
	})
	arenaWait(t, w, p, func() bool { return p.Zone() == run.in.zones[135] })
	arenaOn(t, bz, func() {
		bz.removeBoss(boss)
		boss.onDeath(p)
		arenaDungeonSecond(bz, time.Now())
	})
	run.mu.Lock()
	won := run.won
	run.mu.Unlock()
	if !won {
		t.Fatal("killing Trung úy Xanh Lơ did not clear the dungeon")
	}
	// After the countdown everyone is sent out and the instance closes.
	arenaOn(t, bz, func() { arenaDungeonSecond(bz, time.Now().Add(2*time.Minute)) })
	arenaWait(t, w, p, func() bool { return p.Zone() != nil && p.Zone().instance.inst == nil })
	if arenaRunByKey(w, "arena:arena_treasure/clan:7") != nil {
		t.Fatal("run still registered after closing")
	}
	// Daily limit: 3 entries (ChangeMapService.goToDBKB).
	pz := p.Zone()
	arenaOn(t, pz, func() {
		arenaState(p).Entries["arena_treasure"] = 3
		p.AddItem(w.Data, 611, 1, nil)
		arenaDgOpen(pz, p, "arena_treasure", 5)
		if p.CountItem(611) != 1 {
			t.Error("item consumed although the daily limit was reached")
		}
	})
}

func TestArenaBlackBallWar(t *testing.T) {
	w := arenaWorld(t)
	w.Events.On(EvPickup, arenaBBWPickup)
	w.Events.On(EvLeaveMap, arenaBBWLeave)
	z := w.zones[85][0]
	p := arenaJoin(t, w, 85, 1)
	arenaClock(t, 20, 10)
	var ballID int
	arenaOn(t, z, func() {
		arenaBBWSecond(z, time.Now())
		g := z.arenaBallOnGround(372)
		if g == nil {
			t.Error("ball not placed")
			return
		}
		p.Char.X, p.Char.Y = g.X, g.Y
		z.pickup(p, g.ID)
		if p.CountItem(372) != 0 || z.arenaBallOnGround(372) == nil {
			t.Error("picked before 20:30")
		}
		ballID = z.arenaBallOnGround(372).ID
	})
	arenaClock(t, 20, 40)
	baseDmg := p.Dmg
	arenaOn(t, z, func() {
		arenaBBWState(z).droppedAt = time.Time{}
		z.pickup(p, ballID)
		st := arenaBBWState(z)
		if st.holder != p.ID() || p.CountItem(372) != 1 {
			t.Errorf("holder=%d count=%d", st.holder, p.CountItem(372))
			return
		}
		st.since = time.Now().Add(-301 * time.Second)
		arenaBBWSecond(z, time.Now())
	})
	arenaWait(t, w, p, func() bool { return p.Zone() != nil && p.Zone().Map.ID == 24 })
	arenaWait(t, w, p, func() bool {
		return arenaState(p).BlackBall[1] > time.Now().Unix() && p.CountItem(372) == 0 && p.Dmg == baseDmg*121/100
	})
}

func TestArenaTournament(t *testing.T) {
	arenaTour = &tourState{champs: map[string]bool{}, matches: map[*Zone]*tourMatch{}, hour: -1}
	w := arenaWorld(t)
	lobby := w.zones[52][0]
	a := arenaJoin(t, w, 52, 1)
	b := arenaJoin(t, w, 52, 2)
	a.IsAdmin, b.IsAdmin = true, true
	a.Char.Gem, b.Char.Gem = 10, 10
	arenaClock(t, 8, 10) // Nhi đồng: 2 gems
	arenaTourTick(w, arenaNow())
	arenaOn(t, lobby, func() {
		arenaTourRegister(lobby, a)
		arenaTourRegister(lobby, b)
	})
	if len(arenaTour.reg) != 2 || a.Char.Gem != 8 {
		t.Fatalf("reg=%d gem=%d", len(arenaTour.reg), a.Char.Gem)
	}
	arenaClock(t, 8, 30)
	arenaTourTick(w, arenaNow())
	if len(arenaTour.matches) != 1 {
		t.Fatalf("matches = %d", len(arenaTour.matches))
	}
	var az *Zone
	for z := range arenaTour.matches {
		az = z
	}
	arenaWait(t, w, a, func() bool { return a.Zone() == az })
	arenaWait(t, w, b, func() bool { return b.Zone() == az })
	now := time.Now()
	arenaOn(t, az, func() {
		arenaTourSecond(az, now)
		arenaTourSecond(az, now.Add(5*time.Second))
		if !az.canPvP(a, b) {
			t.Error("duel not started")
		}
		b.Char.HP = 0
		az.playerDie(b, a)
		arenaTourSecond(az, now.Add(6*time.Second))
	})
	arenaWait(t, w, b, func() bool { return !b.Dead && b.Zone() != nil && b.Zone().Map.ID == 21 })
	arenaWait(t, w, a, func() bool { return a.Zone() != nil && a.Zone().Map.ID == 52 })
	arenaClock(t, 8, 31)
	arenaTourTick(w, arenaNow())
	arenaWait(t, w, a, func() bool { return a.Char.Gem == 8+2+arena.Tournament.ChampionGems })
	if !arenaTour.champs[a.Char.Name] || !arenaTour.finished {
		t.Fatal("champion not recorded")
	}
}

func TestArenaMinigames(t *testing.T) {
	w := arenaWorld(t)
	z := w.zones[0][0]
	p := arenaJoin(t, w, 0, 1)
	q := arenaJoin(t, w, 0, 2)
	prev := arenaRand
	arenaRand = func(n int) int { return min(7, n-1) }
	t.Cleanup(func() { arenaRand = prev })
	p.Char.Gem = 100
	arenaOn(t, z, func() {
		arenaLuckyBet(z, p, "gem", 7)
		arenaLuckyBet(z, p, "gem", 7) // duplicate number refused
	})
	if p.Char.Gem != 95 {
		t.Fatalf("gem after bet = %d", p.Char.Gem)
	}
	g := arenaFindLucky("gem")
	arenaMini.mu.Lock()
	g.betting, g.left = true, 1
	arenaMini.mu.Unlock()
	arenaMiniTick(w, time.Now()) // betting closes
	arenaMiniTick(w, time.Now()) // wait 10 s ... still waiting
	arenaMini.mu.Lock()
	g.left = 1
	arenaMini.mu.Unlock()
	arenaMiniTick(w, time.Now()) // draw: 7
	arenaWait(t, w, p, func() bool { return p.Char.Gem == 95+450 })

	p.Char.Gold, q.Char.Gold = 50_000_000, 50_000_000
	arenaOn(t, z, func() {
		arenaChooseBet(z, p, "gold", true)
		arenaChooseBet(z, q, "gold", false)
		arenaChooseBet(z, q, "gold", false)
	})
	c := arenaFindChoose("gold")
	arenaMini.mu.Lock()
	c.endAt = time.Now().Add(-time.Second)
	arenaMini.mu.Unlock()
	arenaRand = func(int) int { return 0 }
	arenaMiniTick(w, time.Now())
	// VIP pool 10M → p gets 90%; normal pool 2M → q gets 80%.
	arenaWait(t, w, p, func() bool { return p.Char.Gold == 40_000_000+9_000_000 })
	arenaWait(t, w, q, func() bool { return q.Char.Gold == 48_000_000+1_600_000 })
}

func TestArenaRules(t *testing.T) {
	if arenaCupAt(14) != 0 || arenaCupAt(23) != 4 || arenaCupAt(3) != -1 {
		t.Fatal("cup schedule")
	}
	if arenaCupForPower(1_000_000) != 0 || arenaCupForPower(20_000_000) != 2 || arenaCupForPower(2_000_000_000) != 4 {
		t.Fatal("power brackets")
	}
	me := store.ArenaRankRow{ID: 1, Rank: 8}
	if arenaRankCheck(me, store.ArenaRankRow{ID: 2, Rank: 5}) == "" { // top 10: at most 2 ranks above
		t.Fatal("challenge 3 ranks above in top 10 allowed")
	}
	if arenaRankCheck(me, store.ArenaRankRow{ID: 2, Rank: 9}) == "" {
		t.Fatal("challenge below allowed")
	}
	if arenaRankCheck(me, store.ArenaRankRow{ID: 2, Rank: 6}) != "" {
		t.Fatal("valid challenge refused")
	}
	if arenaRankGems(1) != 100 || arenaRankGems(50) != 5 || arenaRankGems(5000) != 0 {
		t.Fatal("daily gems")
	}
	if !arenaMabuOpen(time.Date(2026, 1, 1, 14, 30, 0, 0, vnZone)) || arenaMabuOpen(time.Date(2026, 1, 1, 15, 0, 0, 0, vnZone)) {
		t.Fatal("mabu hours")
	}
}
