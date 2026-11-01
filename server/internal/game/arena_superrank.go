package game

import (
	"context"
	"encoding/json"
	"errors"
	"math/rand/v2"
	"sync"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Giải Siêu Hạng (matches/dai_hoi_vo_thuat/SuperRank*.java, managers/SuperRankManager.java,
// boss/sieu_hang/Rival.java). Every character keeps a stored copy (stats at
// their last visit); a challenger fights the copy of a better-ranked player in
// a private arena (map 113) for 180 s, and on a win the two swap ranks.

type rankFight struct {
	pid     int64
	rival   store.ArenaRankRow
	in      *instance
	created time.Time
	started bool
	endAt   time.Time
	won     bool
}

var (
	arenaRankFights arenaZoneRegistry[*rankFight]
	arenaRankBusy   = struct {
		mu sync.Mutex
		m  map[int64]bool
	}{m: map[int64]bool{}}
)

func init() {
	npcOptions = append(npcOptions, func(z *Zone, p *Player, npc int) []MenuOption {
		if npc == arena.SuperRank.Npc && z.Map.ID == arena.SuperRank.Map {
			return []MenuOption{{Label: "Giải siêu hạng", Action: "rank_menu"}}
		}
		return nil
	})
	menuActions["rank_menu"] = func(z *Zone, p *Player, _ string) { arenaRankInfo(z, p) }
	register("rank_info", func(z *Zone, p *Player, _ json.RawMessage) { arenaRankInfo(z, p) })
	register("rank_challenge", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ ID int64 }
		if json.Unmarshal(d, &r) == nil {
			arenaRankChallenge(z, p, r.ID)
		}
	})
	register("rank_claim", func(z *Zone, p *Player, _ json.RawMessage) { arenaRankClaim(z, p) })
	secondHooks = append(secondHooks, arenaRankSecond)
}

// arenaRankGems is SuperRankService.reward:203-215 (gems per day by rank).
func arenaRankGems(rank int) int64 {
	for _, g := range arena.SuperRank.DailyGems {
		if rank <= g.MaxRank {
			return g.Gems
		}
	}
	return 0
}

func arenaRankInfo(z *Zone, p *Player) {
	w := z.World
	if w.Store == nil {
		notice(p, "Giải siêu hạng chưa sẵn sàng")
		return
	}
	name, hp, dmg, def := p.Char.Name, p.MaxHP, p.Dmg, p.Def
	var me store.ArenaRankRow
	var top, near []store.ArenaRankRow
	w.async(p, func(ctx context.Context) (err error) {
		if me, err = w.Store.ArenaRankEnsure(ctx, p.ID(), name, hp, dmg, def); err != nil {
			return err
		}
		if top, err = w.Store.ArenaRankRange(ctx, 1, 10); err != nil {
			return err
		}
		near, err = w.Store.ArenaRankRange(ctx, max(me.Rank-10, 11), me.Rank)
		return err
	}, func(z *Zone, err error) {
		if err != nil {
			notice(p, "Lỗi máy chủ")
			return
		}
		st := arenaState(p)
		p.Send("rank_info", map[string]any{"me": me, "top": nonNil(top), "near": nonNil(near), "tickets": st.Tickets,
			"gem_cost": arena.SuperRank.GemCost, "daily_gems": arenaRankGems(me.Rank), "claimed": st.RankClaim == st.Day})
	})
}

func arenaRankChallenge(z *Zone, p *Player, rivalID int64) {
	cfg := arena.SuperRank
	w := z.World
	if w.Store == nil || z.Map.ID != cfg.Map || z.instance.inst != nil || p.Dead {
		return
	}
	arenaRankBusy.mu.Lock()
	busy := arenaRankBusy.m[p.ID()] || arenaRankBusy.m[rivalID]
	arenaRankBusy.mu.Unlock()
	if busy {
		notice(p, "Đối thủ đang thi đấu") // ConstSuperRank.TEXT_DOI_THU_DANG_THI_DAU / TEXT_DANG_THI_DAU
		return
	}
	st := arenaState(p)
	if st.Tickets <= 0 && p.Char.Gem < cfg.GemCost {
		notice(p, "Bạn không đủ ngọc, còn thiếu "+itoa(int(cfg.GemCost-p.Char.Gem))+" ngọc nữa")
		return
	}
	name, hp, dmg, def := p.Char.Name, p.MaxHP, p.Dmg, p.Def
	var me, rival store.ArenaRankRow
	w.async(p, func(ctx context.Context) (err error) {
		if me, err = w.Store.ArenaRankEnsure(ctx, p.ID(), name, hp, dmg, def); err != nil {
			return err
		}
		rival, err = w.Store.ArenaRankGet(ctx, rivalID)
		return err
	}, func(z *Zone, err error) {
		switch {
		case errors.Is(err, store.ErrNotFound):
			notice(p, "Không tìm thấy đối thủ")
			return
		case err != nil:
			notice(p, "Lỗi máy chủ")
			return
		}
		if msg := arenaRankCheck(me, rival); msg != "" {
			notice(p, msg)
			return
		}
		if z.Map.ID != cfg.Map || z.instance.inst != nil || p.Dead {
			return
		}
		arenaRankStart(z, p, rival)
	})
}

// arenaRankCheck is SuperRankService.competing:41-66.
func arenaRankCheck(me, rival store.ArenaRankRow) string {
	switch {
	case me.ID == rival.ID || me.Rank == rival.Rank:
		return "Không thể thi đấu với chính mình"
	case me.Rank < rival.Rank:
		return "Không thể thi đấu dưới hạng mình"
	case rival.Rank < 10 && me.Rank-rival.Rank > 2:
		return "Không thể thi đấu trên mình 2 hạng"
	}
	return ""
}

func arenaRankStart(z *Zone, p *Player, rival store.ArenaRankRow) {
	cfg := arena.SuperRank
	w := z.World
	st := arenaState(p)
	if st.Tickets > 0 {
		st.Tickets--
	} else if p.Char.Gem >= cfg.GemCost {
		p.Char.Gem -= cfg.GemCost
	} else {
		notice(p, "Bạn không đủ ngọc")
		return
	}
	p.dirty = true
	arenaRankBusy.mu.Lock()
	if arenaRankBusy.m[p.ID()] || arenaRankBusy.m[rival.ID] {
		arenaRankBusy.mu.Unlock()
		return
	}
	arenaRankBusy.m[p.ID()], arenaRankBusy.m[rival.ID] = true, true
	arenaRankBusy.mu.Unlock()

	key := "arena_rank/" + itoa(int(p.ID()))
	spec := &DungeonSpec{Name: "Siêu hạng", Maps: []int{cfg.Map}, Minutes: cfg.MatchS/60 + 2, MobHPMult: 1}
	w.instances.mu.Lock()
	in := w.newInstance(key, spec)
	az := in.zones[cfg.Map]
	if az == nil {
		w.instances.mu.Unlock()
		close(in.stop)
		arenaRankRelease(p.ID(), rival.ID)
		return
	}
	w.instances.byID[key] = in
	w.instances.mu.Unlock()
	f := &rankFight{pid: p.ID(), rival: rival, in: in, created: time.Now()}
	arenaRankFights.set(az, f)
	// Rival.java: a boss built from the defender's stored stats.
	t := &BossTemplate{ID: "rank_rival", Name: rival.Name + " [" + itoa(rival.Rank) + "]", Mob: cfg.RivalMob,
		HP: max(rival.HP, 1), Dmg: max(rival.Dmg, 1), Def: rival.Def}
	t.normalize()
	b := &Boss{T: t, Key: "rival" + itoa(int(rival.ID)), HP: t.HP}
	b.onDeath = func(killer *Player) {
		if killer != nil {
			f.won = true
		}
	}
	az.Post(func() {
		az.spawnBoss(b)
		b.X = 434 // SuperRank.handleMatchStart:116-117
	})
	notice(p, "Trận đấu bắt đầu")
	arenaMove(z, p, az, 334, 264)
}

func arenaRankRelease(ids ...int64) {
	arenaRankBusy.mu.Lock()
	for _, id := range ids {
		delete(arenaRankBusy.m, id)
	}
	arenaRankBusy.mu.Unlock()
}

// arenaRankSecond is SuperRank.update:89-110.
func arenaRankSecond(z *Zone, now time.Time) {
	f, ok := arenaRankFights.get(z)
	if !ok {
		return
	}
	p := z.players[f.pid]
	if !f.started {
		if p != nil {
			f.started, f.endAt = true, now.Add(time.Duration(arena.SuperRank.MatchS)*time.Second)
		} else if now.Sub(f.created) > 15*time.Second {
			arenaRankFinish(z, f, nil, false)
		}
		return
	}
	switch {
	case f.won:
		arenaRankFinish(z, f, p, true)
	case p == nil || p.Dead || !now.Before(f.endAt):
		arenaRankFinish(z, f, p, false)
	}
}

func arenaRankFinish(z *Zone, f *rankFight, p *Player, won bool) {
	cfg := arena.SuperRank
	w := z.World
	arenaRankFights.del(z)
	z.bosses = nil
	if p != nil {
		arenaRevive(z, p)
		z.leaveTo(p, cfg.Map, -1, 250+rand.IntN(200), 360)
	}
	w.closeInstance(f.in)
	pid, rival := f.pid, f.rival
	go func() {
		defer arenaRankRelease(pid, rival.ID)
		if w.Store == nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rank, err := w.Store.ArenaRankResult(ctx, pid, rival.ID, won)
		pl := w.onlineChar(pid)
		if err != nil || pl == nil {
			return
		}
		w.postToPlayer(pl, func(*Zone) {
			if won {
				notice(pl, "Thắng rồi haha, lên hạng "+itoa(rank))
			} else {
				notice(pl, "Thua rồi")
			}
		})
		if won && rank <= 10 { // ConstSuperRank.TEXT_TOP_10
			w.Broadcast(Encode("notice", map[string]string{"text": pl.Char.Name + " vừa lên Top " + itoa(rank) + " Giải Siêu Hạng"}))
		}
	}()
}

func arenaRankClaim(z *Zone, p *Player) {
	w := z.World
	if w.Store == nil {
		return
	}
	var me store.ArenaRankRow
	w.async(p, func(ctx context.Context) (err error) {
		me, err = w.Store.ArenaRankGet(ctx, p.ID())
		return err
	}, func(z *Zone, err error) {
		st := arenaState(p)
		gems := arenaRankGems(me.Rank)
		switch {
		case err != nil:
			notice(p, "Bạn chưa tham gia giải siêu hạng")
		case st.RankClaim == st.Day:
			notice(p, "Hôm nay bạn đã nhận thưởng rồi")
		case gems <= 0:
			notice(p, "Hạng của bạn chưa có thưởng")
		default:
			st.RankClaim = st.Day
			p.Char.Gem += gems
			p.dirty = true
			p.Send("self", p.Self())
			notice(p, "Nhận "+itoa(int(gems))+" ngọc thưởng hạng "+itoa(me.Rank))
		}
	})
}
