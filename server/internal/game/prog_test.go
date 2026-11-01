package game

import (
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

func progTestWorld(t *testing.T) *World {
	t.Helper()
	mk := func(id int, npcs ...MapNpc) *MapTemplate {
		return &MapTemplate{ID: id, Name: "M" + itoa(id), Width: 100, Height: 30, Tiles: make([]byte, 3000), MaxPlayer: 10, Zones: 1, Npcs: npcs}
	}
	d := &Data{
		Maps: map[int]*MapTemplate{0: mk(0), 5: mk(5, MapNpc{TemplateID: 13, X: 200, Y: 100}),
			47: mk(47, MapNpc{TemplateID: 17, X: 200, Y: 100}), 21: mk(21)},
		Items:     map[int]*ItemTemplate{0: {ID: 0, Part: 14}, 6: {ID: 6, Type: 1, Part: 15}},
		TileTop:   map[int]map[byte]bool{},
		Mobs:      map[int]*MobTemplate{},
		ItemsFull: map[int]*ItemTemplateFull{},
		npcNames:  map[int]string{0: "Ông Gôhan"},
	}
	for id := 0; id <= 69; id++ {
		typ := 1
		if id == 7 || id == 8 || id == 9 || id == 10 || id == 11 || id == 12 {
			typ = 4
		}
		d.Mobs[id] = &MobTemplate{ID: id, Type: typ, Name: "mob" + itoa(id), HP: 100}
	}
	add := func(typ int, stack bool, ids ...int) {
		for _, id := range ids {
			d.ItemsFull[id] = &ItemTemplateFull{ID: id, Type: typ, Name: "item" + itoa(id), Stackable: stack, Gender: 3}
		}
	}
	add(12, true, 14, 15, 16, 17, 18, 19, 20, 704, 705, 706, 707, 708)
	add(27, true, 193, 380)
	add(7, false, 94, 101, 108)
	add(5, false, 227, 228, 229)
	add(ItemGloves, false, 1)
	add(ItemGold, true, 76)
	add(ItemGem, true, 77)
	// Main tasks 0..29 with as many subtasks as the overrides mention.
	pd0 := progParse()
	subs := map[int]int{}
	for k := range pd0.Tasks.Overrides {
		ts, ss, _ := strings.Cut(k, ".")
		ti, _ := strconv.Atoi(ts)
		si, _ := strconv.Atoi(ss)
		subs[ti] = max(subs[ti], si+1)
	}
	for id := 0; id < 30; id++ {
		tm := &TaskMain{ID: id, Name: "T" + itoa(id)}
		for i := 0; i < max(subs[id], 1); i++ {
			tm.Subs = append(tm.Subs, &TaskSub{Name: "s", MaxCount: 1, Kind: TrigManual})
		}
		d.Tasks = append(d.Tasks, tm)
	}
	if err := loadContent(d); err != nil {
		t.Fatal(err)
	}
	err := progInit(d, func(pd *progData) error {
		for i := 0; i < 59; i++ {
			tp := progTemplate{ID: i, Name: "Tiêu diệt %1 x", Min: []int{2, 2, 2, 2, 2}, Max: []int{2, 2, 2, 2, 2}}
			pd.Side = append(pd.Side, tp)
			pd.Clan = append(pd.Clan, tp)
		}
		pd.Badges[223] = &progBadge{ID: 6, Effect: 223, Item: 1294, Name: "Nông dân chăm chỉ", Options: [][2]int{{77, 5}, {93, 30}}}
		pd.BadgeTasks = []progBadgeTask{{ID: 6, Max: 2, Reward: 223, Name: "side"}, {ID: 2, Max: 100, Reward: 219, Name: "dragon"}}
		for i := 0; i < 20; i++ {
			pd.Achs = append(pd.Achs, progAch{Idx: i, ID: i + 1, Info1: "A", Info2: "%1", Money: 10, Max: 2})
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	w := NewWorld(d, nil)
	t.Cleanup(func() { close(w.stop) })
	w.progInstall()
	return w
}

func progJoin(t *testing.T, w *World, mapID int, id int64) *Player {
	p, _ := joinTest(t, w, w.zones[mapID][0], id)
	p.Tmp["prog_task"] = progMemo{p.Char.TaskID, p.Char.TaskIndex}
	return p
}

func progSetTask(p *Player, id, idx int) {
	p.Char.TaskID, p.Char.TaskIndex, p.Char.TaskCount = id, idx, 0
	p.Tmp["prog_task"] = progMemo{id, idx}
}

func TestProgOverridesApplied(t *testing.T) {
	w := progTestWorld(t)
	d := w.Data
	if s := d.Tasks[22].Subs[1]; s.Kind != TrigTalk || s.Npc != 38 {
		t.Fatalf("22.1 = %+v", s)
	}
	if s := d.Tasks[9].Subs[0]; s.Kind != TrigMap || len(s.Maps) != 3 {
		t.Fatalf("9.0 = %+v", s)
	}
	pd := progOf(d)
	if sp := pd.subs[d.Tasks[19].Subs[0]]; sp == nil || sp.Kind != "p_boss" {
		t.Fatal("19.0 not a boss subtask")
	}
	if len(d.Content.Achievements) != 0 {
		t.Fatal("sample achievements not replaced")
	}
	found := false
	for _, r := range d.Content.Drops.TaskItems {
		found = found || (r.Task == 27 && r.Item == 380)
	}
	if !found {
		t.Fatal("capsule kì bí drop rule missing")
	}
}

func TestProgMainTaskChain(t *testing.T) {
	w := progTestWorld(t)
	z := w.zones[0][0]
	p := progJoin(t, w, 0, 1) // Earth
	progSetTask(p, 7, 0)
	p.Char.Power = 16000
	w.progCheckState(p, z)
	if p.Char.TaskIndex != 1 {
		t.Fatalf("7.0 power: idx=%d", p.Char.TaskIndex)
	}
	w.Data.Tasks[7].Subs[1].MaxCount = 2
	w.Events.Emit(Event{Kind: EvKillMob, Player: p, Zone: z, Mob: 8}) // Namek mob: no
	if p.Char.TaskCount != 0 {
		t.Fatal("wrong-race mob counted")
	}
	for range 2 {
		w.Events.Emit(Event{Kind: EvKillMob, Player: p, Zone: z, Mob: 7})
	}
	if p.Char.TaskIndex != 2 {
		t.Fatalf("7.1 kills: idx=%d", p.Char.TaskIndex)
	}
	w.Events.Emit(Event{Kind: EvTalkNpc, Player: p, Zone: z, Npc: 8})
	w.Events.Emit(Event{Kind: EvTalkNpc, Player: p, Zone: z, Npc: 7})
	if p.Char.TaskIndex != 3 || p.CountItem(193) != 75 {
		t.Fatalf("7.2 Bunma: idx=%d capsules=%d", p.Char.TaskIndex, p.CountItem(193))
	}
	gold := p.Char.Gold
	w.Events.Emit(Event{Kind: EvTalkNpc, Player: p, Zone: z, Npc: 0})
	if p.Char.TaskID != 8 || p.Char.Gold-gold != 500000 {
		t.Fatalf("task 7 done: task=%d gold+%d", p.Char.TaskID, p.Char.Gold-gold)
	}
	p.Char.Power = 40000
	w.progCheckState(p, z)
	w.Events.Emit(Event{Kind: EvKillMob, Player: p, Zone: z, Mob: 11})
	if p.Char.TaskIndex != 2 || len(z.ground) == 0 {
		t.Fatalf("8.1 ball: idx=%d ground=%d", p.Char.TaskIndex, len(z.ground))
	}

	// 10.2 gives the race's skill book.
	progSetTask(p, 10, 2)
	w.Events.Emit(Event{Kind: EvTalkNpc, Player: p, Zone: z, Npc: 14})
	w.Events.Emit(Event{Kind: EvTalkNpc, Player: p, Zone: z, Npc: 13})
	if p.Char.TaskID != 11 || p.CountItem(94) != 1 {
		t.Fatalf("10.2: task=%d book=%d", p.Char.TaskID, p.CountItem(94))
	}
	// 11.0..11.2 complete together.
	p.Char.Power = 700000
	w.progCheckState(p, z)
	if p.Char.TaskIndex != 3 {
		t.Fatalf("11.x power chain: idx=%d", p.Char.TaskIndex)
	}
}

func TestProgClanSubtasks(t *testing.T) {
	w := progTestWorld(t)
	z := w.zones[0][0]
	a := progJoin(t, w, 0, 1)
	b := progJoin(t, w, 0, 2)
	w.clans.clans[9] = &store.Clan{ID: 9, Name: "c", LeaderID: 1, Members: []store.ClanMember{{ID: 1}, {ID: 2}}}
	progSetTask(a, 13, 0)
	w.progCheckState(a, z)
	if a.Char.TaskIndex != 0 {
		t.Fatal("13.0 done without a clan")
	}
	a.Char.ClanID, b.Char.ClanID = 9, 9
	w.progCheckState(a, z)
	if a.Char.TaskIndex != 1 {
		t.Fatalf("13.0 idx=%d", a.Char.TaskIndex)
	}
	progSetTask(a, 14, 0)
	w.Data.Tasks[14].Subs[0].MaxCount = 10
	w.Events.Emit(Event{Kind: EvKillMob, Player: a, Zone: z, Mob: 16})
	if a.Char.TaskCount != 2 {
		t.Fatalf("clan x2 kill count=%d", a.Char.TaskCount)
	}
}

func TestProgBossMatch(t *testing.T) {
	w := progTestWorld(t)
	cases := []struct {
		boss    string
		aliases []string
		want    bool
	}{
		{"fide_2", []string{"fide"}, true},
		{"Fide cấp 3", []string{"fide"}, true},
		{"piccolo", []string{"pic"}, false},
		{"Mập đầu đinh", []string{"map dau dinh"}, true},
		{"xencon7", []string{"xen con"}, true},
		{"sieu_bo_hung", []string{"xen bo hung"}, false},
	}
	for _, c := range cases {
		if got := w.progBossMatch(c.boss, c.aliases); got != c.want {
			t.Errorf("%q %v = %v", c.boss, c.aliases, got)
		}
	}
	z := w.zones[0][0]
	p := progJoin(t, w, 0, 1)
	progSetTask(p, 19, 0)
	w.Events.Emit(Event{Kind: EvKillBoss, Player: p, Zone: z, Boss: "rambo"})
	w.Events.Emit(Event{Kind: EvKillBoss, Player: p, Zone: z, Boss: "kuku"})
	if p.Char.TaskIndex != 1 {
		t.Fatalf("19.0 idx=%d", p.Char.TaskIndex)
	}
}

func progSideCycle(t *testing.T, w *World, z *Zone, p *Player) {
	t.Helper()
	z.Handle(p, "side_take", raw(map[string]int{"level": 0}))
	s := State[progDaily](p, "sidetask")
	if !s.Has || s.Max != 2 {
		t.Fatalf("take: %+v", s)
	}
	s.Tmpl = 0 // khủng long (mob 1)
	for range 2 {
		w.Events.Emit(Event{Kind: EvKillMob, Player: p, Zone: z, Mob: 1})
	}
	if s.Count != 2 {
		t.Fatalf("count=%d", s.Count)
	}
	z.Handle(p, "side_pay", nil)
	if s.Has {
		t.Fatal("not paid")
	}
}

func TestProgSideTaskAndBadge(t *testing.T) {
	w := progTestWorld(t)
	far := progJoin(t, w, 0, 2)
	w.zones[0][0].Handle(far, "side_take", raw(map[string]int{"level": 0}))
	if State[progDaily](far, "sidetask").Has {
		t.Fatal("took a side task away from Bò Mộng")
	}
	z := w.zones[47][0]
	p := progJoin(t, w, 47, 1)
	z.Handle(p, "side_take", raw(map[string]int{"level": 4}))
	if State[progDaily](p, "sidetask").Has {
		t.Fatal("level 4 is not offered by Bò Mộng")
	}
	hp := p.MaxHP
	gold := p.Char.Gold
	progSideCycle(t, w, z, p)
	if p.Char.Gold-gold != 5000 || p.CountItem(708) != 1 {
		t.Fatalf("reward: gold+%d ball=%d", p.Char.Gold-gold, p.CountItem(708))
	}
	if left := State[progDaily](p, "sidetask").Left; left != 9 {
		t.Fatalf("left=%d", left)
	}
	progSideCycle(t, w, z, p) // second payment completes badge task 6
	bs := State[progBadgeState](p, "badge")
	if bs.Active != 223 || p.MaxHP != hp*105/100 {
		t.Fatalf("badge active=%d hp %d -> %d", bs.Active, hp, p.MaxHP)
	}
	z.Handle(p, "badge_off", nil)
	if p.MaxHP != hp {
		t.Fatalf("badge off hp=%d", p.MaxHP)
	}
}

func TestProgClanTask(t *testing.T) {
	w := progTestWorld(t)
	z := w.zones[5][0]
	p := progJoin(t, w, 5, 1)
	w.clans.clans[9] = &store.Clan{ID: 9, Name: "c", LeaderID: 1, Members: []store.ClanMember{{ID: 1}}}
	p.Char.ClanID = 9
	z.Handle(p, "ctask_take", nil)
	s := State[progDaily](p, "clantask")
	if !s.Has {
		t.Fatal("no clan task")
	}
	s.Tmpl, s.Level = 3, 2 // khủng long mẹ (mob 4)
	for range 2 {
		w.Events.Emit(Event{Kind: EvKillMob, Player: p, Zone: z, Mob: 4})
	}
	z.Handle(p, "ctask_pay", nil)
	if w.clan(9).Points != 30 || s.Left != 4 || s.Has {
		t.Fatalf("points=%d left=%d has=%v", w.clan(9).Points, s.Left, s.Has)
	}
}

func TestProgAchievements(t *testing.T) {
	w := progTestWorld(t)
	z := w.zones[0][0]
	p := progJoin(t, w, 0, 1)
	gem := p.Char.Gem
	z.Handle(p, "ach_claim", raw(map[string]int{"i": 7}))
	if p.Char.Gem != gem {
		t.Fatal("claimed unfinished achievement")
	}
	for range 2 {
		w.Events.Emit(Event{Kind: EvKillMob, Player: p, Zone: z, Mob: 0}) // mộc nhân
	}
	z.Handle(p, "ach_claim", raw(map[string]int{"i": 7}))
	z.Handle(p, "ach_claim", raw(map[string]int{"i": 7}))
	if p.Char.Gem != gem+10 {
		t.Fatalf("gem %d -> %d", gem, p.Char.Gem)
	}
	w.Events.Emit(Event{Kind: EvKillMob, Player: p, Zone: z, Mob: 7}) // flying mob
	if progAchOf(p).Count[6] != 1 {
		t.Fatal("flying mob not counted")
	}
}

func TestProgDragon(t *testing.T) {
	w := progTestWorld(t)
	z := w.zones[0][0]
	p := progJoin(t, w, 0, 1)
	give := func() {
		for id := 14; id <= 20; id++ {
			p.AddItem(w.Data, id, 1, nil)
		}
	}
	give()
	gem := p.Char.Gem
	z.Handle(p, "dragon_summon", raw(map[string]int{"star": 1}))
	if p.CountItem(14) != 0 || p.CountItem(20) != 0 || !w.progDragon().shen.Active {
		t.Fatal("summon failed")
	}
	z.Handle(p, "dragon_wish", raw(map[string]int{"i": 4})) // other wishes page
	z.Handle(p, "dragon_wish", raw(map[string]int{"i": 1})) // +10K gems
	if p.Char.Gem != gem+10000 || w.progDragon().shen.Active {
		t.Fatalf("wish: gem+%d active=%v", p.Char.Gem-gem, w.progDragon().shen.Active)
	}
	give()
	z.Handle(p, "dragon_summon", raw(map[string]int{"star": 1}))
	if p.CountItem(14) != 1 {
		t.Fatal("summoned during the 10 minute cooldown")
	}
	w.progDragon().shenLast = time.Time{}
	crit := p.Crit
	z.Handle(p, "dragon_summon", raw(map[string]int{"star": 1}))
	z.Handle(p, "dragon_wish", raw(map[string]int{"i": 2})) // crit +2%
	if p.Crit != crit+2 {
		t.Fatalf("crit %d -> %d", crit, p.Crit)
	}
	if State[progBadgeState](p, "badge").Count[2] != 2 {
		t.Fatal("dragon badge task not counted")
	}
	// Timeout after 5 minutes.
	w.progDragon().shenLast = time.Time{}
	give()
	z.Handle(p, "dragon_summon", raw(map[string]int{"star": 3}))
	w.progDragonTick(time.Now().Add(6 * time.Minute))
	if w.progDragon().shen.Active {
		t.Fatal("dragon did not leave")
	}
}
