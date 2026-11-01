package game

import (
	"context"
	"encoding/json"
	"math"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"
)

// Clan dungeons built on the core instance mechanism (dungeon.go):
//   Bản Đồ Kho Báu   map/phoban/BanDoKhoBau.java, services_dungeon/TreasureUnderSeaService.java
//   Con Đường Rắn Độc map/phoban/SnakeWay.java, services_dungeon/SnakeWayService.java
//   Khí Gas Hủy Diệt  map/phoban/DestronGas.java, services_dungeon/DestronGasService.java
//   Doanh Trại Độc Nhãn map/phoban/RedRibbonHQ.java, services_dungeon/RedRibbonHQService.java
// One instance per clan and dungeon; mobs and bosses scale with the chosen
// level (or the clan's strength for Doanh trại); clearing it starts a short
// kick-out countdown, as the legacy kickout* flags did.

const arenaMaxInt32 = math.MaxInt32

type arenaRun struct {
	mu         sync.Mutex
	dg         *arenaDungeon
	in         *instance
	key        string
	clanID     int64
	level      int64
	opened     time.Time
	scaleHP    int64 // Doanh trại: sum of members' max HP (RedRibbonHQ.init:122-128)
	scaleDmg   int64 // ...and damage
	mobsLeft   map[int]int
	bossesLeft int
	winLeft    int
	winTotal   int
	afterDone  bool
	won        bool
	kickAt     time.Time
	closing    bool
}

var arenaRuns = struct {
	mu     sync.Mutex
	byKey  map[string]*arenaRun
	byInst map[*instance]*arenaRun
	clan   map[string]int // day|dungeon|clan -> openings today
}{byKey: map[string]*arenaRun{}, byInst: map[*instance]*arenaRun{}, clan: map[string]int{}}

func init() {
	npcOptions = append(npcOptions, func(z *Zone, p *Player, npc int) []MenuOption {
		var out []MenuOption
		for id, dg := range arena.Dungeons {
			if dg.Npc == npc {
				out = append(out, MenuOption{Label: dg.Name, Action: "dg_menu", Arg: id})
			}
		}
		return out
	})
	menuActions["dg_menu"] = func(z *Zone, p *Player, id string) { arenaSendDgInfo(z, p, id) }
	register("dg_info", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ ID string }
		if json.Unmarshal(d, &r) == nil {
			arenaSendDgInfo(z, p, r.ID)
		}
	})
	register("dg_open", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct {
			ID    string
			Level int
		}
		if json.Unmarshal(d, &r) == nil {
			arenaDgOpen(z, p, r.ID, r.Level)
		}
	})
	secondHooks = append(secondHooks, arenaDungeonSecond)
}

func arenaRunKey(dg *arenaDungeon, p *Player) string {
	if p.Char.ClanID != 0 {
		return "arena:" + dg.ID + "/clan:" + strconv.FormatInt(p.Char.ClanID, 10)
	}
	return "arena:" + dg.ID + "/player:" + strconv.FormatInt(p.ID(), 10)
}

// arenaRunByKey returns the live run, dropping runs whose instance was closed
// (time out handled by the core's instanceTick/runSchedules).
func arenaRunByKey(w *World, key string) *arenaRun {
	arenaRuns.mu.Lock()
	run := arenaRuns.byKey[key]
	arenaRuns.mu.Unlock()
	if run == nil {
		return nil
	}
	w.instances.mu.Lock()
	live := w.instances.byID[key] == run.in
	w.instances.mu.Unlock()
	if !live {
		arenaDropRun(run)
		return nil
	}
	return run
}

func arenaDropRun(run *arenaRun) {
	arenaRuns.mu.Lock()
	if arenaRuns.byKey[run.key] == run {
		delete(arenaRuns.byKey, run.key)
	}
	delete(arenaRuns.byInst, run.in)
	arenaRuns.mu.Unlock()
}

func arenaRunOf(z *Zone) *arenaRun {
	if z.instance.inst == nil {
		return nil
	}
	arenaRuns.mu.Lock()
	defer arenaRuns.mu.Unlock()
	return arenaRuns.byInst[z.instance.inst]
}

func arenaSendDgInfo(z *Zone, p *Player, id string) {
	dg := arena.Dungeons[id]
	if dg == nil {
		return
	}
	info := map[string]any{"id": id, "name": dg.Name, "leveled": dg.leveled(), "min_level": dg.MinLevel,
		"max_level": dg.MaxLevel, "clan_only": dg.ClanOnly, "leader_only": dg.LeaderOnly, "min_power": dg.MinPower,
		"minutes": dg.Minutes, "running": false}
	if dg.Item > 0 {
		if t := z.World.Data.ItemsFull[dg.Item]; t != nil {
			info["item"] = t.Name
		}
	}
	if run := arenaRunByKey(z.World, arenaRunKey(dg, p)); run != nil {
		info["running"] = true
		info["level"] = run.level
		info["left_s"] = int(time.Until(run.in.expires).Seconds())
	}
	p.Send("dg_info", info)
}

// arenaEntryOK checks the per-player limits: BDKB 3 entries/day
// (ChangeMapService.goToDBKB:832-845), CDRD once per 7 days
// (SnakeWayService.openConDuongRanDoc:53-55). Re-entering the same run is free.
func arenaEntryOK(p *Player, dg *arenaDungeon, opened int64, commit bool) bool {
	st := arenaState(p)
	if opened != 0 && st.LastRun[dg.ID] == opened {
		return true
	}
	now := arenaNow()
	if dg.PlayerDaily > 0 && st.Entries[dg.ID] >= dg.PlayerDaily {
		notice(p, "Bạn đã vào "+dg.Name+" "+itoa(dg.PlayerDaily)+" lần trong hôm nay, hẹn gặp lại ngày mai")
		return false
	}
	if dg.PlayerWeekly > 0 {
		if last := st.WeekStart[dg.ID]; last != 0 && now.Unix()-last < 7*24*3600 {
			left := time.Duration(7*24*3600-(now.Unix()-last)) * time.Second
			notice(p, "Vui lòng đợi "+left.Round(time.Hour).String()+" nữa")
			return false
		}
	}
	if commit {
		st.Entries[dg.ID]++
		st.LastRun[dg.ID] = opened
		if dg.PlayerWeekly > 0 {
			st.WeekStart[dg.ID] = now.Unix()
		}
		p.dirty = true
	}
	return true
}

func arenaClanKey(dg *arenaDungeon, clanID int64) string {
	return arenaDay(arenaNow()) + "|" + dg.ID + "|" + strconv.FormatInt(clanID, 10)
}

func arenaDgOpen(z *Zone, p *Player, id string, level int) {
	dg := arena.Dungeons[id]
	w := z.World
	if dg == nil || p.Dead {
		return
	}
	if !arenaNearNpc(z, p, dg.Npc) {
		notice(p, "Hãy đến gặp "+npcName(w.Data, dg.Npc))
		return
	}
	if dg.ClanOnly && p.Char.ClanID == 0 {
		notice(p, "Chỉ dành cho thành viên bang hội")
		return
	}
	if dg.MinPower > 0 && p.Char.Power < dg.MinPower && !p.IsAdmin {
		notice(p, "Yêu cầu sức mạnh lớn hơn "+strconv.FormatInt(dg.MinPower, 10))
		return
	}
	key := arenaRunKey(dg, p)
	run := arenaRunByKey(w, key)
	opened := false
	if run == nil {
		if dg.LeaderOnly && !p.IsAdmin { // DestronGasService.openKhiGasHuyDiet:56
			if c := w.clan(p.Char.ClanID); c == nil || c.LeaderID != p.ID() {
				notice(p, "Chỉ bang chủ mới có thể mở "+dg.Name)
				return
			}
		}
		if dg.leveled() && (level < dg.MinLevel || level > dg.MaxLevel) {
			notice(p, "Cấp độ từ "+itoa(dg.MinLevel)+" đến "+itoa(dg.MaxLevel))
			return
		}
		if !dg.leveled() {
			level = 1
		}
		if dg.ClanDaily > 0 { // DestronGas 3/day, RedRibbonHQ once/day (RedRibbonHQService.joinDoanhTrai:47)
			arenaRuns.mu.Lock()
			n := arenaRuns.clan[arenaClanKey(dg, p.Char.ClanID)]
			arenaRuns.mu.Unlock()
			if n >= dg.ClanDaily {
				notice(p, "Hãy chờ đến ngày mai")
				return
			}
		}
		if !arenaEntryOK(p, dg, 0, false) {
			return
		}
		if dg.Item > 0 { // TreasureUnderSeaService.openBanDoKhoBau:46-65 consumes Bản đồ kho báu
			if !p.RemoveItems(dg.Item, 1) {
				name := "vật phẩm yêu cầu"
				if t := w.Data.ItemsFull[dg.Item]; t != nil {
					name = t.Name
				}
				notice(p, "Không tìm thấy "+name)
				return
			}
			p.sendInventory()
		}
		run = arenaOpenRun(z, p, dg, key, int64(level))
		if run == nil {
			notice(p, dg.Name+" đã đầy, hãy quay lại sau 30 phút")
			return
		}
		if dg.ClanDaily > 0 {
			arenaRuns.mu.Lock()
			arenaRuns.clan[arenaClanKey(dg, p.Char.ClanID)]++
			arenaRuns.mu.Unlock()
		}
		opened = true
	}
	if !arenaEntryOK(p, dg, run.opened.Unix(), true) {
		return
	}
	first := run.in.zones[dg.Maps[0]]
	if first == nil {
		return
	}
	var group []*Player
	if opened && dg.ScaleByClan { // RedRibbonHQ.openDoanhTrai:98-115 brings clanmates standing nearby
		for _, o := range z.players {
			if o != p && !o.Dead && o.Char.ClanID == p.Char.ClanID && dist(o.Char.X, o.Char.Y, p.Char.X, p.Char.Y) <= 400 &&
				arenaEntryOK(o, dg, run.opened.Unix(), true) {
				group = append(group, o)
			}
		}
	}
	for _, o := range append(group, p) {
		notice(o, "Vào "+dg.Name+", còn "+time.Until(run.in.expires).Round(time.Minute).String())
		arenaMove(z, o, first, SpawnX, 0)
	}
	if opened && p.Char.ClanID != 0 {
		if c := w.clan(p.Char.ClanID); c != nil {
			w.clanBroadcast(c, Encode("clan_chat", map[string]string{"name": "Hệ thống",
				"text": p.Char.Name + " đã mở " + dg.Name + " cấp " + itoa(int(run.level))}))
		}
	}
}

func arenaOpenRun(z *Zone, p *Player, dg *arenaDungeon, key string, level int64) *arenaRun {
	w := z.World
	spec := &DungeonSpec{Name: dg.Name, Maps: dg.Maps, Minutes: dg.Minutes, MobHPMult: 1}
	w.instances.mu.Lock()
	if old := w.instances.byID[key]; old != nil {
		w.instances.mu.Unlock()
		w.closeInstance(old)
		w.instances.mu.Lock()
	}
	in := w.newInstance(key, spec)
	if len(in.zones) == 0 || in.zones[dg.Maps[0]] == nil {
		w.instances.mu.Unlock()
		close(in.stop)
		return nil
	}
	w.instances.byID[key] = in
	w.instances.mu.Unlock()
	run := &arenaRun{dg: dg, in: in, key: key, clanID: p.Char.ClanID, level: level, opened: time.Now(), mobsLeft: map[int]int{}}
	if dg.ScaleByClan {
		for _, o := range z.players {
			if o == p || (o.Char.ClanID != 0 && o.Char.ClanID == p.Char.ClanID) {
				run.scaleHP += o.MaxHP
				run.scaleDmg += o.Dmg
			}
		}
	}
	var countWin func(fb *arenaFloorBoss, n int)
	countWin = func(fb *arenaFloorBoss, n int) {
		if fb.Win {
			run.winTotal += n
		}
		if fb.Then != nil {
			countWin(fb.Then, n)
		}
	}
	for i := range dg.Bosses {
		countWin(&dg.Bosses[i], max(dg.Bosses[i].Count, 1))
	}
	run.winLeft = run.winTotal
	for mid, iz := range in.zones {
		run.mobsLeft[mid] = len(iz.mobs)
	}
	arenaRuns.mu.Lock()
	arenaRuns.byKey[key] = run
	arenaRuns.byInst[in] = run
	arenaRuns.mu.Unlock()
	for _, iz := range in.zones {
		iz.Post(func() { arenaSetupZone(iz, run) })
	}
	return run
}

// arenaSetupZone scales mobs and places the floor bosses (zone goroutine).
// Mob HP follows each dungeon's init(): level × k × mob template id, capped to
// int32 (BanDoKhoBau.init:162-192, SnakeWay.init:145-162, DestronGas.init:166-191),
// or clan damage × template id for Doanh trại (RedRibbonHQ.init:130-138).
func arenaSetupZone(z *Zone, run *arenaRun) {
	dg := run.dg
	k := dg.MobHPK
	if v, ok := dg.MobHPKMaps[itoa(z.Map.ID)]; ok {
		k = v
	}
	for i, m := range z.mobs {
		tid := int64(max(m.T.ID, 1))
		switch {
		case dg.ScaleByClan:
			m.MaxHP = arenaCap(run.scaleDmg*tid, arenaMaxInt32)
		case dg.leveled() && k > 0:
			kk := k
			if dg.EliteIndex != nil && i == *dg.EliteIndex && dg.EliteK > 0 {
				kk = dg.EliteK
			}
			m.MaxHP = arenaCap(run.level*kk*tid, arenaMaxInt32)
		}
		m.HP = m.MaxHP
	}
	for i := range dg.Bosses {
		fb := &dg.Bosses[i]
		if fb.Map != z.Map.ID || fb.AfterClear {
			continue
		}
		for range max(fb.Count, 1) {
			arenaSpawnFloorBoss(z, run, fb, 0, 0)
		}
	}
}

func arenaFloorStats(run *arenaRun, fb *arenaFloorBoss, parentHP, parentDmg int64) (hp, dmg int64) {
	mult := max(fb.MultPct, 100)
	switch {
	case parentHP > 0 && fb.MultPct > 0: // Hatchiyack = 1.5 × Dr. Lychee (DrLychee.leaveMap:136-147)
		hp, dmg = parentHP*mult/100, parentDmg*mult/100
	case run.dg.ScaleByClan: // dame = totalHp/20, hp = totalDamage*50 (RedRibbonHQ.init:140-141)
		hp, dmg = run.scaleDmg*50*mult/100, run.scaleHP/20*mult/100
	default:
		hp, dmg = fb.HPLv*run.level, fb.DmgLv*run.level
	}
	return arenaCap(hp, 2_000_000_000), arenaCap(dmg, 200_000_000)
}

func arenaSpawnFloorBoss(z *Zone, run *arenaRun, fb *arenaFloorBoss, parentHP, parentDmg int64) {
	hp, dmg := arenaFloorStats(run, fb, parentHP, parentDmg)
	rw := arena.Rewards["gold"]
	if fb.Win {
		rw = arena.Rewards["rare"]
	}
	z.nextItem++
	t := &BossTemplate{ID: "dg_" + run.dg.ID, Name: fb.Name, Mob: fb.Mob, HP: hp, Dmg: dmg, Rewards: rw}
	t.normalize()
	b := &Boss{T: t, Key: "dg" + itoa(z.nextItem), HP: hp}
	run.mu.Lock()
	run.bossesLeft++
	run.mu.Unlock()
	b.onDeath = func(killer *Player) {
		run.mu.Lock()
		run.bossesLeft--
		if fb.Win {
			run.winLeft--
		}
		run.mu.Unlock()
		if killer != nil && fb.Then != nil {
			arenaSpawnFloorBoss(z, run, fb.Then, hp, dmg)
		}
	}
	z.spawnBoss(b)
}

// arenaDungeonSecond runs once a second in every active zone.
func arenaDungeonSecond(z *Zone, now time.Time) {
	run := arenaRunOf(z)
	if run == nil {
		return
	}
	alive := 0
	for _, m := range z.mobs {
		if m.Alive() {
			alive++
		}
	}
	dg := run.dg
	run.mu.Lock()
	run.mobsLeft[z.Map.ID] = alive
	cleared := true
	for _, n := range run.mobsLeft {
		if n > 0 {
			cleared = false
		}
	}
	spawnAfter := cleared && !run.afterDone
	if spawnAfter {
		run.afterDone = true
	}
	justWon := false
	if !run.won {
		switch dg.WinWhen {
		case "all":
			justWon = cleared && run.bossesLeft == 0 && run.afterDone
		default:
			justWon = run.winTotal > 0 && run.winLeft <= 0
		}
		if justWon {
			run.won = true
			wait := time.Duration(dg.KickAfterWinS) * time.Second
			if dg.PickMinutes > 0 {
				wait = time.Duration(dg.PickMinutes) * time.Minute
			}
			run.kickAt = now.Add(max(wait, 10*time.Second))
		}
	}
	won, kickAt := run.won, run.kickAt
	closing := won && !now.Before(kickAt) && !run.closing
	if closing {
		run.closing = true
	}
	run.mu.Unlock()

	if spawnAfter { // DestronGas.update:81-95: Dr. Lychee appears once every mob is dead
		for i := range dg.Bosses {
			fb := &dg.Bosses[i]
			if !fb.AfterClear {
				continue
			}
			if tz := run.in.zones[fb.Map]; tz != nil {
				tz.Post(func() {
					for range max(fb.Count, 1) {
						arenaSpawnFloorBoss(tz, run, fb, 0, 0)
					}
				})
			}
		}
	}
	if justWon {
		arenaDungeonWon(z.World, run)
	}
	if won && !closing && now.Second()%10 == 0 {
		left := int(time.Until(kickAt).Seconds())
		for _, p := range z.players {
			notice(p, dg.Name+" sẽ đóng sau "+itoa(max(left, 0))+" giây nữa")
		}
	}
	if closing {
		for _, iz := range run.in.zones {
			iz.Post(func() {
				for _, p := range iz.players {
					home := dg.HomeMap
					if home < 0 {
						home = HomeMap(p.Char.Gender)
					}
					notice(p, "Đã hết thời gian, bạn được đưa ra ngoài")
					iz.leaveTo(p, home, -1, 300, 0)
				}
			})
		}
		z.World.closeInstance(run.in)
		arenaDropRun(run)
	}
}

// arenaDungeonWon announces the clear, drops Doanh trại's dragon balls
// (RedRibbonHQ.randomNR:250-276) and records the clan achievement used by the
// dungeon leaderboards (BanDoKhoBau.sendThanhTichBanDoKhoBau:141-158).
func arenaDungeonWon(w *World, run *arenaRun) {
	dg := run.dg
	for _, iz := range run.in.zones {
		iz.Post(func() {
			for _, p := range iz.players {
				notice(p, "Chúc mừng! Bang đã chinh phục "+dg.Name)
			}
			if !dg.ScaleByClan {
				return
			}
			n := 3
			for _, den := range []int{2, 3, 4, 5} {
				if rand.IntN(den) == 0 {
					n++
				}
			}
			for range n {
				ball := 18 + rand.IntN(3) // 5-7 sao
				if rand.IntN(500) == 0 {
					ball = 14 + rand.IntN(4) // 1-4 sao, 1/500
				}
				x := rand.IntN(max(iz.Map.PixelWidth()-200, 1)) + 100
				iz.dropItem(ball, 1, nil, x, 100, 0)
			}
		})
	}
	if w.Store == nil || run.clanID == 0 {
		return
	}
	ms := time.Since(run.opened).Milliseconds()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		w.Store.ArenaRecordDungeon(ctx, run.clanID, dg.ID, int(run.level), ms)
	}()
}
