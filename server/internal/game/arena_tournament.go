package game

import (
	"encoding/json"
	"math/rand/v2"
	"strconv"
	"sync"
	"time"
)

// Đại hội võ thuật thế giới (matches/giai_dau/WorldMartialArtsTournamentManager.java,
// matches/dai_hoi_vo_thuat/WorldMartialArtsTournament*.java, consts/ConstTournament.java).
// Each hour from 8h has a cup with a power bracket; registration closes at
// :25, rounds start at :30 in the lobby (map 52). Pairs fight a 180 s duel in a
// private copy of the arena map (51); winners wait for the next round, the
// last one standing is champion.

type tourMatch struct {
	a, b    int64
	names   [2]string
	in      *instance
	created time.Time
	phase   int
	fightAt time.Time
	endAt   time.Time
}

type tourState struct {
	mu        sync.Mutex
	day       string
	hour      int
	reg       []int64
	wait      []int64
	champs    map[string]bool
	matches   map[*Zone]*tourMatch
	round     int
	nextRound time.Time
	finished  bool
	seq       int
}

var arenaTour = &tourState{champs: map[string]bool{}, matches: map[*Zone]*tourMatch{}, hour: -1}

func init() {
	npcOptions = append(npcOptions, func(z *Zone, p *Player, npc int) []MenuOption {
		if npc == arena.Tournament.Npc {
			return []MenuOption{{Label: "Đại hội võ thuật", Action: "tour_menu"}}
		}
		return nil
	})
	menuActions["tour_menu"] = func(z *Zone, p *Player, _ string) { arenaTourInfo(p) }
	register("tour_info", func(z *Zone, p *Player, _ json.RawMessage) { arenaTourInfo(p) })
	register("tour_reg", func(z *Zone, p *Player, _ json.RawMessage) { arenaTourRegister(z, p) })
	register("tour_unreg", func(z *Zone, p *Player, _ json.RawMessage) {
		t := arenaTour
		t.mu.Lock()
		t.reg = arenaRemoveID(t.reg, p.ID())
		t.mu.Unlock()
		notice(p, "Bạn đã hủy đăng ký thành công.") // ConstTournament.TEXT_HUY_DANG_KY
		arenaTourInfo(p)
	})
	secondHooks = append(secondHooks, arenaTourSecond)
	worldStartHooks = append(worldStartHooks, func(w *World) {
		go func() {
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for {
				select {
				case <-w.Done():
					return
				case <-tick.C:
					arenaTourTick(w, arenaNow())
				}
			}
		}()
	})
}

// arenaCupAt is WorldMartialArtsTournamentService.getTournament:17-39.
func arenaCupAt(hour int) int {
	for i, c := range arena.Tournament.Cups {
		if containsInt(c.Hours, hour) {
			return i
		}
	}
	return -1
}

// arenaCupForPower is the bracket check of dangky_huy:166-197; Ngoại hạng
// (max_power 0) accepts anyone.
func arenaCupForPower(power int64) int {
	for i, c := range arena.Tournament.Cups {
		if c.MaxPower > 0 && power >= c.MinPower && power < c.MaxPower {
			return i
		}
	}
	return len(arena.Tournament.Cups) - 1
}

func arenaRemoveID(s []int64, id int64) []int64 {
	out := s[:0]
	for _, x := range s {
		if x != id {
			out = append(out, x)
		}
	}
	return out
}

func arenaContainsID(s []int64, id int64) bool {
	for _, x := range s {
		if x == id {
			return true
		}
	}
	return false
}

func arenaNextCupHour(h int) int { // getNextTournamentTime
	h++
	if h > 23 || h < 8 {
		h = 8
	}
	return h
}

func arenaTourInfo(p *Player) {
	cfg := arena.Tournament
	now := arenaNow()
	cup := arenaCupAt(now.Hour())
	t := arenaTour
	t.mu.Lock()
	info := map[string]any{"cup": "", "can_reg": cup >= 0 && now.Minute() < cfg.RegUntil,
		"registered": arenaContainsID(t.reg, p.ID()), "waiting": arenaContainsID(t.wait, p.ID()), "count": len(t.reg),
		"round": t.round, "next_hour": arenaNextCupHour(now.Hour()), "start_minute": cfg.StartMinute}
	var champs []string
	for n := range t.champs {
		champs = append(champs, n)
	}
	t.mu.Unlock()
	info["champs"] = champs
	if cup >= 0 {
		c := cfg.Cups[cup]
		info["cup"], info["fee_gem"], info["fee_gold"] = c.Name, c.Gem, c.Gold
	}
	var sched []string
	for _, c := range cfg.Cups {
		s := c.Name + ":"
		for _, h := range c.Hours {
			s += " " + itoa(h) + "h"
		}
		sched = append(sched, s)
	}
	info["schedule"] = sched
	p.Send("tour_info", info)
}

func arenaTourRegister(z *Zone, p *Player) {
	cfg := arena.Tournament
	if !arenaNearNpc(z, p, cfg.Npc) {
		notice(p, "Hãy đến gặp Ghi danh tại Đại hội võ thuật")
		return
	}
	now := arenaNow()
	cup := arenaCupAt(now.Hour())
	if cup < 0 || now.Minute() >= cfg.RegUntil {
		notice(p, "Đã hết hạn đăng ký thi đấu, xin vui lòng chờ đến giải sau vào lúc "+itoa(arenaNextCupHour(now.Hour()))+"h")
		return
	}
	c := cfg.Cups[cup]
	t := arenaTour
	t.mu.Lock()
	defer t.mu.Unlock()
	switch {
	case t.champs[p.Char.Name]:
		notice(p, "Bạn đã vô địch giải trước, vui lòng đợi giải đấu tiếp theo.")
		return
	case arenaContainsID(t.reg, p.ID()):
		notice(p, "Bạn đã đăng ký rồi")
		return
	}
	if want := arenaCupForPower(p.Char.Power); c.MaxPower > 0 && want != cup {
		notice(p, "Bạn chỉ có thể tham gia giải Ngoại hạng hoặc giải "+cfg.Cups[want].Name+".")
		return
	}
	if p.Char.Gold < c.Gold {
		notice(p, "Bạn không đủ vàng, còn thiếu "+strconv.FormatInt(c.Gold-p.Char.Gold, 10)+" vàng nữa")
		return
	}
	if p.Char.Gem < c.Gem {
		notice(p, "Bạn không đủ ngọc, còn thiếu "+strconv.FormatInt(c.Gem-p.Char.Gem, 10)+" ngọc nữa")
		return
	}
	p.Char.Gold -= c.Gold
	p.Char.Gem -= c.Gem
	p.dirty = true
	p.Send("self", p.Self())
	t.reg = append(t.reg, p.ID())
	notice(p, "Bạn đã đăng ký thành công! Hãy có mặt trước "+itoa(now.Hour())+"h"+itoa(cfg.StartMinute)+", đến trễ sẽ bị loại.")
}

// arenaTourTick drives registration and rounds (WorldMartialArtsTournamentManager.update:110-188).
func arenaTourTick(w *World, now time.Time) {
	cfg := arena.Tournament
	t := arenaTour
	var after []func()
	t.mu.Lock()
	if d := arenaDay(now); d != t.day {
		t.day, t.champs = d, map[string]bool{} // listChamp cleared after midnight
	}
	if now.Hour() != t.hour {
		t.hour, t.reg, t.wait, t.round, t.finished = now.Hour(), nil, nil, 0, false
	}
	for z, m := range t.matches { // safety net: matches whose zone never reported back
		// (the instance itself expires through the core, which expels players first)
		if now.Sub(m.created) > time.Duration(cfg.MatchS+90)*time.Second {
			delete(t.matches, z)
		}
	}
	cup := arenaCupAt(now.Hour())
	run := cup >= 0 && !t.finished && now.Minute() >= cfg.StartMinute && now.Minute() <= cfg.EndMinute && len(t.matches) == 0
	if run && t.round > 0 && len(t.wait) == 1 && len(t.reg) == 0 {
		champ := t.wait[0]
		t.wait, t.finished = nil, true
		after = append(after, func() { arenaTourChampion(w, champ, cfg.Cups[cup].Name) })
		run = false
	}
	if run && t.round > 0 {
		if len(t.wait) > 1 && len(t.reg) == 0 && now.After(t.nextRound) {
			t.reg, t.wait = t.wait, nil
		}
	}
	if run && len(t.reg) > 0 {
		t.round++
		var valid []*Player
		for _, id := range t.reg {
			p := w.onlineChar(id)
			if p == nil {
				continue
			}
			if pz := p.Zone(); pz == nil || pz.Map.ID != cfg.LobbyMap || pz.instance.inst != nil {
				after = append(after, func() {
					w.postToPlayer(p, func(*Zone) {
						notice(p, "Bạn đã không đến đúng giờ, rất tiếc bạn bị truất quyền tham gia.")
					})
				})
				continue
			}
			valid = append(valid, p)
		}
		t.reg = nil
		round := t.round
		if len(valid)%2 == 1 { // odd one advances (TEXT_DOI_THU_BO_CUOC)
			lucky := valid[len(valid)-1]
			valid = valid[:len(valid)-1]
			t.wait = append(t.wait, lucky.ID())
			after = append(after, func() {
				w.postToPlayer(lucky, func(*Zone) {
					notice(lucky, "Bạn đã thắng vì đối thủ bỏ cuộc, hãy chờ tại đây để thi đấu vòng tiếp theo.")
				})
			})
		}
		for i := 0; i+1 < len(valid); i += 2 {
			a, b := valid[i], valid[i+1]
			z, m := arenaTourNewMatch(w, a, b, now)
			if z == nil {
				t.wait = append(t.wait, a.ID())
				continue
			}
			t.matches[z] = m
			after = append(after, func() {
				for i, p := range []*Player{a, b} {
					x := 328 + i*115 // WorldMartialArtsTournament.init:43-44
					w.postToPlayer(p, func(cz *Zone) {
						notice(p, "Vòng "+itoa(round)+": "+a.Char.Name+" đấu với "+b.Char.Name)
						arenaMove(cz, p, z, x, 262)
					})
				}
			})
		}
		t.nextRound = now.Add(time.Duration(cfg.RoundGapS) * time.Second)
	}
	t.mu.Unlock()
	for _, f := range after {
		f()
	}
}

// arenaTourNewMatch creates the private arena (called with arenaTour.mu held).
func arenaTourNewMatch(w *World, a, b *Player, now time.Time) (*Zone, *tourMatch) {
	cfg := arena.Tournament
	arenaTour.seq++
	key := "arena_tour/" + itoa(arenaTour.seq)
	spec := &DungeonSpec{Name: "Đại hội võ thuật", Maps: []int{cfg.ArenaMap}, Minutes: cfg.MatchS/60 + 3, MobHPMult: 1}
	w.instances.mu.Lock()
	in := w.newInstance(key, spec)
	z := in.zones[cfg.ArenaMap]
	if z == nil {
		w.instances.mu.Unlock()
		close(in.stop)
		return nil, nil
	}
	w.instances.byID[key] = in
	w.instances.mu.Unlock()
	return z, &tourMatch{a: a.ID(), b: b.ID(), names: [2]string{a.Char.Name, b.Char.Name}, in: in, created: now}
}

// arenaTourSecond is WorldMartialArtsTournament.update: wait for both, count
// down, fight 180 s, then decide by death, leaving, or remaining HP.
func arenaTourSecond(z *Zone, now time.Time) {
	t := arenaTour
	t.mu.Lock()
	m := t.matches[z]
	t.mu.Unlock()
	if m == nil {
		return
	}
	pa, pb := z.players[m.a], z.players[m.b]
	switch m.phase {
	case 0:
		if pa != nil && pb != nil {
			m.phase, m.fightAt = 1, now.Add(4*time.Second)
			for _, p := range []*Player{pa, pb} {
				p.Char.HP, p.Char.MP = p.MaxHP, p.MaxMP
				p.Send("self", p.Self())
			}
			z.broadcast(Encode("notice", map[string]string{"text": "Trận đấu giữa " + m.names[0] + " và " + m.names[1] + " sắp diễn ra"}), 0)
			return
		}
		if now.Sub(m.created) > 30*time.Second {
			arenaTourFinish(z, m, pa, pb, now)
		}
	case 1:
		if pa == nil || pb == nil {
			arenaTourFinish(z, m, pa, pb, now)
			return
		}
		if left := int(m.fightAt.Sub(now).Seconds()); left > 0 {
			z.broadcast(Encode("notice", map[string]string{"text": itoa(left)}), 0)
			return
		}
		m.phase, m.endAt = 2, now.Add(time.Duration(arena.Tournament.MatchS)*time.Second)
		pa.duel, pb.duel = pb.ID(), pa.ID()
		z.broadcast(Encode("duel_start", map[string]int64{"a": pa.ID(), "b": pb.ID()}), 0)
		z.broadcast(Encode("notice", map[string]string{"text": "Trận đấu bắt đầu"}), 0)
	case 2:
		aOut := pa == nil || pa.Dead
		bOut := pb == nil || pb.Dead
		if aOut || bOut || !now.Before(m.endAt) {
			arenaTourFinish(z, m, pa, pb, now)
		}
	}
}

func arenaTourFinish(z *Zone, m *tourMatch, pa, pb *Player, now time.Time) {
	cfg := arena.Tournament
	alive := func(p *Player) bool { return p != nil && !p.Dead }
	var win, lose *Player
	var winID int64
	switch {
	case alive(pa) && !alive(pb):
		win, lose, winID = pa, pb, m.a
	case alive(pb) && !alive(pa):
		win, lose, winID = pb, pa, m.b
	case alive(pa) && alive(pb): // time out: less damage taken wins (timeOut:117-135)
		if pa.Char.HP*100/max(pa.MaxHP, 1) >= pb.Char.HP*100/max(pb.MaxHP, 1) {
			win, lose, winID = pa, pb, m.a
		} else {
			win, lose, winID = pb, pa, m.b
		}
	}
	for _, p := range []*Player{pa, pb} {
		if p != nil {
			p.duel, p.duelInvite = 0, 0
			arenaRevive(z, p)
		}
	}
	if win != nil {
		z.broadcast(Encode("duel_end", map[string]int64{"winner": win.ID(), "loser": m.a + m.b - win.ID()}), 0)
		cup := arenaCupAt(arenaNow().Hour())
		if cup >= 0 { // finish:215-226 (the legacy code added 1 gem/gold by mistake)
			win.Char.Gem += cfg.Cups[cup].Gem
		}
		win.Char.Gold = min(win.Char.Gold+int64(cfg.WinGoldMin+rand.IntN(max(cfg.WinGoldMax-cfg.WinGoldMin, 1))), maxGold)
		win.dirty = true
		win.Send("self", win.Self())
		notice(win, "Bạn đã thắng vòng này, xin chờ tại đây ít phút để thi đấu vòng tiếp theo.")
		z.leaveTo(win, cfg.LobbyMap, -1, 200+rand.IntN(300), 336)
	}
	if lose != nil {
		notice(lose, "Xin lỗi vì điều không mong muốn đã xảy ra.")
		z.leaveTo(lose, HomeMap(lose.Char.Gender), -1, 300, 336)
	}
	t := arenaTour
	t.mu.Lock()
	delete(t.matches, z)
	if winID != 0 {
		t.wait = append(t.wait, winID)
	}
	t.nextRound = now.Add(time.Duration(cfg.RoundGapS) * time.Second)
	t.mu.Unlock()
	z.World.closeInstance(m.in)
}

// arenaTourChampion is WorldMartialArtsTournamentManager.reward:204-215.
func arenaTourChampion(w *World, id int64, cup string) {
	p := w.onlineChar(id)
	if p == nil {
		return
	}
	arenaTour.mu.Lock()
	arenaTour.champs[p.Char.Name] = true
	arenaTour.mu.Unlock()
	cfg := arena.Tournament
	w.postToPlayer(p, func(*Zone) {
		p.Char.Gem += cfg.ChampionGems
		for _, it := range cfg.ChampionItems {
			p.AddItem(w.Data, it, 1, nil)
		}
		p.dirty = true
		p.Send("self", p.Self())
		p.sendInventory()
		notice(p, "Chúc mừng bạn đã vô địch giải đấu!")
	})
	w.Broadcast(Encode("notice", map[string]string{"text": "Chúc mừng " + p.Char.Name + " đã vô địch giải " + cup + "!"}))
}
