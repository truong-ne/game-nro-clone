package game

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"time"
)

// Ngọc Rồng Sao Đen (map/phoban/BlackBallWar.java, services_dungeon/
// BlackBallWarService.java). Daily 20:00-21:00 on the 7 planets (maps 85-91),
// the k-star black ball lies on planet k. Balls can be picked from 20:30; the
// holder must keep it for 5 minutes to win a 22 h buff for the whole clan
// (player/RewardBlackBall.java). Dying or leaving drops the ball.

type bbwZone struct {
	holder    int64
	since     time.Time
	finished  bool // BlackBallWar.win: zone.finishBlackBallWar
	droppedAt time.Time
	lastNote  time.Time
	day       string
}

var arenaBBW arenaZoneRegistry[*bbwZone]

func init() {
	npcOptions = append(npcOptions, func(z *Zone, p *Player, npc int) []MenuOption {
		if npc == arena.BlackBall.Npc {
			return []MenuOption{{Label: "Ngọc rồng sao đen", Action: "dg_bbw"}}
		}
		return nil
	})
	menuActions["dg_bbw"] = func(z *Zone, p *Player, _ string) { arenaSendBBW(z, p) }
	register("dg_bbw_info", func(z *Zone, p *Player, _ json.RawMessage) { arenaSendBBW(z, p) })
	register("dg_bbw_go", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ I int }
		if json.Unmarshal(d, &r) != nil || r.I < 0 || r.I >= len(arena.BlackBall.Maps) {
			return
		}
		// BlackBallWarService.changeMap:186-193 (only while the war is open).
		if !arenaBBWOpen(arenaNow()) {
			notice(p, "Trò chơi tìm ngọc hôm nay đã kết thúc, hẹn gặp lại vào "+itoa(arena.BlackBall.HourOpen)+"h ngày mai")
			return
		}
		if !arenaNearNpc(z, p, arena.BlackBall.Npc) && !arenaIsBBWMap(z.Map.ID) {
			return
		}
		z.leaveTo(p, arena.BlackBall.Maps[r.I], -1, 50, 50)
	})
	secondHooks = append(secondHooks, arenaBBWSecond)
	recalcHooks = append(recalcHooks, arenaBBWRecalc)
	joinHooks = append(joinHooks, arenaBBWJoin)
	worldStartHooks = append(worldStartHooks, func(w *World) {
		w.Events.On(EvPickup, arenaBBWPickup)
		w.Events.On(EvLeaveMap, arenaBBWLeave)
	})
}

func arenaBBWOpen(now time.Time) bool {
	h := now.Hour()
	return h >= arena.BlackBall.HourOpen && h < arena.BlackBall.HourClose
}

func arenaBBWCanPick(now time.Time) bool {
	return arenaBBWOpen(now) && (now.Hour() > arena.BlackBall.HourOpen || now.Minute() >= arena.BlackBall.PickMinute)
}

func arenaIsBBWMap(id int) bool { return containsInt(arena.BlackBall.Maps, id) }

// arenaBallOf returns the black ball template that lives on map id (0 if none).
func arenaBallOf(mapID int) int {
	for i, m := range arena.BlackBall.Maps {
		if m == mapID {
			return arena.BlackBall.FirstBall + i
		}
	}
	return 0
}

func arenaIsBall(tid int) bool {
	f := arena.BlackBall.FirstBall
	return tid >= f && tid < f+len(arena.BlackBall.Maps)
}

func arenaSendBBW(z *Zone, p *Player) {
	st := arenaState(p)
	now := time.Now().Unix()
	var buffs []map[string]any
	for star, exp := range st.BlackBall {
		if exp > now {
			buffs = append(buffs, map[string]any{"star": star, "left_s": exp - now})
		}
	}
	var planets []string
	for _, m := range arena.BlackBall.Maps {
		name := itoa(m)
		if t := z.World.Data.Map(m); t != nil {
			name = t.Name
		}
		planets = append(planets, name)
	}
	t := arenaNow()
	p.Send("dg_bbw", map[string]any{"open": arenaBBWOpen(t), "can_pick": arenaBBWCanPick(t), "planets": planets,
		"buffs": buffs, "hour_open": arena.BlackBall.HourOpen, "hour_close": arena.BlackBall.HourClose})
}

func arenaBBWState(z *Zone) *bbwZone {
	st, ok := arenaBBW.get(z)
	day := arenaDay(arenaNow())
	if !ok || st.day != day {
		st = &bbwZone{day: day}
		arenaBBW.set(z, st)
	}
	return st
}

func (z *Zone) arenaBallOnGround(tid int) *GroundItem {
	for _, g := range z.ground {
		if g.TemplateID == tid {
			return g
		}
	}
	return nil
}

// arenaBBWSecond keeps the ball in play and checks the holder (BlackBallWar.update).
func arenaBBWSecond(z *Zone, now time.Time) {
	ball := arenaBallOf(z.Map.ID)
	if ball == 0 || z.instance.inst != nil {
		return
	}
	t := arenaNow()
	st := arenaBBWState(z)
	if !arenaBBWOpen(t) { // kickOutOfMap: back to the space station
		for _, p := range z.players {
			if p.IsAdmin {
				continue
			}
			arenaTakeBalls(p)
			notice(p, "Trò chơi tìm ngọc hôm nay đã kết thúc, hẹn gặp lại vào "+itoa(arena.BlackBall.HourOpen)+"h ngày mai")
			z.leaveTo(p, p.Char.Gender+24, -1, 250, 0)
		}
		if g := z.arenaBallOnGround(ball); g != nil {
			delete(z.ground, g.ID)
			z.broadcast(Encode("item_gone", map[string]int{"id": g.ID}), 0)
		}
		st.holder = 0
		return
	}
	if st.finished {
		return
	}
	if st.holder != 0 {
		h := z.players[st.holder]
		switch {
		case h == nil || h.CountItem(ball) == 0:
			st.holder = 0
		case h.Dead:
			arenaDropBall(z, h, st, ball)
		case now.Sub(st.since) >= time.Duration(arena.BlackBall.HoldS)*time.Second:
			arenaBBWWin(z, h, st, ball)
			return
		case now.Sub(st.lastNote) >= 30*time.Second:
			st.lastNote = now
			left := time.Duration(arena.BlackBall.HoldS)*time.Second - now.Sub(st.since)
			notice(h, "Cố giữ ngọc thêm "+itoa(int(left.Seconds()))+" giây nữa sẽ thắng")
		}
	}
	if st.holder == 0 {
		g := z.arenaBallOnGround(ball)
		if g == nil {
			x := rand.IntN(max(z.Map.PixelWidth()-200, 1)) + 100
			z.dropItem(ball, 1, nil, x, 100, 0)
			g = z.arenaBallOnGround(ball)
		}
		if g != nil {
			g.dropped = time.Now() // never expires while the war is open
		}
	}
}

func arenaDropBall(z *Zone, p *Player, st *bbwZone, ball int) {
	if p.RemoveItems(ball, 1) {
		p.sendInventory()
		z.dropItem(ball, 1, nil, p.Char.X, p.Char.Y, 0)
		st.droppedAt = time.Now() // BlackBallWarService.dropBlackBall: lastTimeDropBlackBall
	}
	st.holder = 0
}

// arenaTakeBalls removes every black ball from the bag (end of event).
func arenaTakeBalls(p *Player) {
	for i := range arena.BlackBall.Maps {
		tid := arena.BlackBall.FirstBall + i
		if n := p.CountItem(tid); n > 0 {
			p.RemoveItems(tid, n)
		}
	}
}

func arenaBBWPickup(e Event) {
	z, p := e.Zone, e.Player
	if z == nil || !arenaIsBall(e.Item) {
		return
	}
	ball := arenaBallOf(z.Map.ID)
	if ball == 0 || z.instance.inst != nil {
		return
	}
	st := arenaBBWState(z)
	t := arenaNow()
	reject := ""
	switch {
	case !arenaBBWCanPick(t):
		reject = "Chưa thể nhặt lúc này, hãy đợi đến " + itoa(arena.BlackBall.HourOpen) + "h" + itoa(arena.BlackBall.PickMinute)
	case st.finished:
		reject = "Trò chơi tìm ngọc hôm nay đã kết thúc, hẹn gặp lại vào " + itoa(arena.BlackBall.HourOpen) + "h ngày mai"
	case time.Since(st.droppedAt) < time.Duration(arena.BlackBall.RepickS)*time.Second:
		reject = "Chưa thể nhặt lúc này, hãy đợi vài giây nữa"
	case st.holder != 0 && st.holder != p.ID():
		reject = "Ngọc đang có người giữ"
	}
	if reject != "" { // BlackBallWarService.pickBlackBall:91-124 refuses; put it back
		p.RemoveItems(e.Item, max(e.Count, 1))
		p.sendInventory()
		z.dropItem(e.Item, 1, nil, p.Char.X, p.Char.Y, 0)
		notice(p, reject)
		return
	}
	st.holder, st.since, st.lastNote = p.ID(), time.Now(), time.Now()
	z.broadcast(Encode("notice", map[string]string{"text": p.Char.Name + " đã nhặt ngọc rồng " + itoa(e.Item-arena.BlackBall.FirstBall+1) + " sao đen"}), 0)
}

func arenaBBWLeave(e Event) {
	z, p := e.Zone, e.Player
	if z == nil {
		return
	}
	ball := arenaBallOf(z.Map.ID)
	if ball == 0 || z.instance.inst != nil {
		return
	}
	if st, ok := arenaBBW.get(z); ok && st.holder == p.ID() {
		arenaDropBall(z, p, st, ball)
	} else if p.CountItem(ball) > 0 {
		p.RemoveItems(ball, p.CountItem(ball))
	}
}

// arenaBBWWin is BlackBallWar.win:106-129: buff the holder and their clan,
// end this planet for the day and send everyone back.
func arenaBBWWin(z *Zone, p *Player, st *bbwZone, ball int) {
	star := ball - arena.BlackBall.FirstBall + 1
	st.finished, st.holder = true, 0
	p.RemoveItems(ball, 1)
	p.sendInventory()
	w := z.World
	exp := time.Now().Add(time.Duration(arena.BlackBall.RewardHours) * time.Hour).Unix()
	arenaGiveBlackBall(z, p, star, exp)
	notice(p, "Chúc mừng bạn đã dành được Ngọc rồng "+itoa(star)+" sao đen cho bang")
	w.Broadcast(Encode("notice", map[string]string{"text": p.Char.Name + " đã giành được Ngọc rồng " + itoa(star) + " sao đen"}))
	if c := w.clan(p.Char.ClanID); c != nil {
		w.clans.mu.Lock()
		ids := make([]int64, 0, len(c.Members))
		for _, m := range c.Members {
			ids = append(ids, m.ID)
		}
		w.clans.mu.Unlock()
		for _, id := range ids {
			if id == p.ID() {
				continue
			}
			if mp := w.onlineChar(id); mp != nil {
				w.postToPlayer(mp, func(mz *Zone) { arenaGiveBlackBall(mz, mp, star, exp) })
			} else if w.Store != nil {
				go func() {
					ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					defer cancel()
					w.Store.ArenaAddBlackBall(ctx, id, star, exp)
				}()
			}
		}
	}
	for _, o := range z.players {
		if !o.IsAdmin {
			z.leaveTo(o, o.Char.Gender+24, -1, 250, 0)
		}
	}
}

func arenaGiveBlackBall(z *Zone, p *Player, star int, exp int64) {
	st := arenaState(p)
	st.BlackBall[star] = exp
	p.dirty = true
	p.Recalc(z.World.Data)
	p.Send("self", p.Self())
}

// arenaBBWRecalc applies active black-ball buffs (NPoint.java:230-241, 815, 944, 1132).
func arenaBBWRecalc(p *Player, _ *Data, m *Mods) {
	if _, ok := p.persist["arena"]; !ok {
		if _, saved := p.Char.Extra.Feat["arena"]; !saved {
			return // never touched arena features: no state to create
		}
	}
	st := arenaState(p)
	now := time.Now().Unix()
	for _, b := range arena.BlackBall.Buffs {
		if st.BlackBall[b.Star] <= now {
			continue
		}
		m.DmgPct += b.DmgPct
		m.HPPct += b.HPPct
		m.MPPct += b.MPPct
		m.LeechHP += b.LeechHP
		m.Reflect += b.Reflect
		m.Dodge += b.Dodge
	}
}

// arenaBBWJoin delivers buffs won by the clan while this member was offline.
func arenaBBWJoin(w *World, p *Player) {
	if w.Store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	rows, err := w.Store.ArenaTakeBlackBalls(ctx, p.ID())
	if err != nil || len(rows) == 0 {
		return
	}
	st := arenaState(p)
	for _, r := range rows {
		st.BlackBall[int(r[0])] = max(st.BlackBall[int(r[0])], r[1])
	}
	p.dirty = true
	p.Recalc(w.Data)
}
