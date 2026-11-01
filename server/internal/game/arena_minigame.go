package game

import (
	"encoding/json"
	"math/rand/v2"
	"sort"
	"strconv"
	"sync"
	"time"
)

// Minigames run on one world goroutine and talk to players only through
// postToPlayer/Broadcast; bets are paid on the player's zone goroutine.
//   Con số may mắn  minigame/ConSoMayManGold.java, ConSoMayManGem.java: 50 s of
//     betting (≤10 numbers 0-99 per player, 1 thỏi vàng or 5 ngọc each), 10 s
//     wait, then one number wins 90 thỏi vàng / 450 ngọc.
//   Chọn ai đây    minigame/ChonAiDay_Gold.java, ChonAiDay_Gem.java: 5 min rounds,
//     normal and VIP pools; a random one of the 5 biggest stakers takes 80% / 90%.

type luckyBet struct {
	pid int64
	n   int
}

type luckyGame struct {
	cfg         *arenaLucky
	betting     bool
	left        int
	bets        []luckyBet
	last        int
	lastWinners []string
}

type chooseGame struct {
	cfg        *arenaChoose
	stakes     [2]map[int64]int64 // 0 normal, 1 vip
	names      map[int64]string
	endAt      time.Time
	lastWinner [2]string
}

var arenaMini = struct {
	mu     sync.Mutex
	lucky  []*luckyGame
	choose []*chooseGame
}{}

// arenaRand is replaced in tests for deterministic draws.
var arenaRand = rand.IntN

func init() {
	for _, c := range arena.Minigames.Lucky {
		arenaMini.lucky = append(arenaMini.lucky, &luckyGame{cfg: c, betting: true, left: arena.Minigames.LuckyBetS, last: -1})
	}
	for _, c := range arena.Minigames.Choose {
		arenaMini.choose = append(arenaMini.choose, &chooseGame{cfg: c, stakes: [2]map[int64]int64{{}, {}}, names: map[int64]string{}})
	}
	npcOptions = append(npcOptions, func(z *Zone, p *Player, npc int) []MenuOption {
		if npc == arena.Minigames.Npc {
			return []MenuOption{{Label: "Trò chơi may mắn", Action: "mini_menu"}}
		}
		return nil
	})
	menuActions["mini_menu"] = func(z *Zone, p *Player, _ string) { arenaMiniState(p) }
	register("mini_info", func(z *Zone, p *Player, _ json.RawMessage) { arenaMiniState(p) })
	register("mini_lucky", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct {
			Game string
			N    int
		}
		if json.Unmarshal(d, &r) == nil {
			arenaLuckyBet(z, p, r.Game, r.N)
		}
	})
	register("mini_choose", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct {
			Game string
			Vip  bool
		}
		if json.Unmarshal(d, &r) == nil {
			arenaChooseBet(z, p, r.Game, r.Vip)
		}
	})
	worldStartHooks = append(worldStartHooks, func(w *World) {
		go func() {
			tick := time.NewTicker(time.Second)
			defer tick.Stop()
			for {
				select {
				case <-w.Done():
					return
				case now := <-tick.C:
					arenaMiniTick(w, now)
				}
			}
		}()
	})
}

func arenaFindLucky(id string) *luckyGame {
	for _, g := range arenaMini.lucky {
		if g.cfg.ID == id {
			return g
		}
	}
	return nil
}

func arenaFindChoose(id string) *chooseGame {
	for _, g := range arenaMini.choose {
		if g.cfg.ID == id {
			return g
		}
	}
	return nil
}

// arenaPay takes cost from p: an item (thỏi vàng), gems or gold.
func arenaPay(z *Zone, p *Player, item int, gem bool, cost int64) bool {
	switch {
	case item > 0:
		if !p.RemoveItems(item, int(cost)) {
			name := "vật phẩm"
			if t := z.World.Data.ItemsFull[item]; t != nil {
				name = t.Name
			}
			notice(p, "Bạn không đủ "+strconv.FormatInt(cost, 10)+" "+name+" để thực hiện")
			return false
		}
		p.sendInventory()
	case gem:
		if p.Char.Gem < cost {
			notice(p, "Bạn không đủ ngọc")
			return false
		}
		p.Char.Gem -= cost
	default:
		if p.Char.Gold < cost {
			notice(p, "Bạn không đủ vàng")
			return false
		}
		p.Char.Gold -= cost
	}
	p.dirty = true
	p.Send("self", p.Self())
	return true
}

// arenaLuckyBet is ConSoMayManGold.newData/ramdom1SoLe/ramdom1SoChan (n=-1 odd, -2 even).
func arenaLuckyBet(z *Zone, p *Player, game string, n int) {
	mg := arena.Minigames
	arenaMini.mu.Lock()
	defer arenaMini.mu.Unlock()
	g := arenaFindLucky(game)
	if g == nil {
		return
	}
	if !g.betting {
		notice(p, "Đã hết thời gian đặt cược cho vòng này. Vui lòng chờ vòng mới.")
		return
	}
	mine := map[int]bool{}
	for _, b := range g.bets {
		if b.pid == p.ID() {
			mine[b.n] = true
		}
	}
	if len(mine) >= mg.LuckyMaxN {
		notice(p, "Bạn đã chọn "+itoa(mg.LuckyMaxN)+" số rồi không thể chọn thêm")
		return
	}
	if n < 0 {
		var free []int
		for v := 0; v <= mg.LuckyMax; v++ {
			if v%2 == map[bool]int{true: 1, false: 0}[n == -1] && !mine[v] {
				free = append(free, v)
			}
		}
		if len(free) == 0 {
			return
		}
		n = free[arenaRand(len(free))]
	}
	if n > mg.LuckyMax {
		return
	}
	if mine[n] {
		notice(p, "Số này bạn đã chọn rồi vui lòng chọn số khác.")
		return
	}
	if !arenaPay(z, p, g.cfg.Item, true, int64(g.cfg.Cost)) {
		return
	}
	g.bets = append(g.bets, luckyBet{p.ID(), n})
	notice(p, "Bạn đã chọn số "+itoa(n))
	p.Send("mini_state", arenaMiniStateLocked(p))
}

func arenaChooseBet(z *Zone, p *Player, game string, vip bool) {
	arenaMini.mu.Lock()
	defer arenaMini.mu.Unlock()
	g := arenaFindChoose(game)
	if g == nil {
		return
	}
	amount, pool := g.cfg.Normal, 0
	if vip {
		amount, pool = g.cfg.Vip, 1
	}
	if !arenaPay(z, p, 0, g.cfg.Gem, amount) {
		return
	}
	g.stakes[pool][p.ID()] += amount
	g.names[p.ID()] = p.Char.Name
	p.Send("mini_state", arenaMiniStateLocked(p))
}

func arenaMiniState(p *Player) {
	arenaMini.mu.Lock()
	s := arenaMiniStateLocked(p)
	arenaMini.mu.Unlock()
	p.Send("mini_state", s)
}

func arenaSumStakes(m map[int64]int64) int64 {
	var s int64
	for _, v := range m {
		s += v
	}
	return s
}

func arenaMiniStateLocked(p *Player) map[string]any {
	var lucky, choose []map[string]any
	for _, g := range arenaMini.lucky {
		var mine []int
		for _, b := range g.bets {
			if b.pid == p.ID() {
				mine = append(mine, b.n)
			}
		}
		lucky = append(lucky, map[string]any{"id": g.cfg.ID, "name": g.cfg.Name, "betting": g.betting, "left": g.left,
			"cost": g.cfg.Cost, "reward": g.cfg.Reward, "mine": nonNil(mine), "last": g.last, "winners": nonNil(g.lastWinners)})
	}
	for _, g := range arenaMini.choose {
		left := 0
		if !g.endAt.IsZero() {
			left = max(int(time.Until(g.endAt).Seconds()), 0)
		}
		e := map[string]any{"id": g.cfg.ID, "name": g.cfg.Name, "left": left, "gem": g.cfg.Gem,
			"normal_cost": g.cfg.Normal, "vip_cost": g.cfg.Vip, "last": g.lastWinner}
		for i, k := range []string{"normal", "vip"} {
			total := arenaSumStakes(g.stakes[i])
			mine := g.stakes[i][p.ID()]
			e[k+"_pool"], e[k+"_mine"] = total, mine
			if total > 0 {
				e[k+"_chance"] = mine * 100 / total // Player.percentGold
			}
		}
		choose = append(choose, e)
	}
	return map[string]any{"lucky": nonNil(lucky), "choose": nonNil(choose)}
}

// arenaMiniTick advances every game by one second.
func arenaMiniTick(w *World, now time.Time) {
	mg := arena.Minigames
	var after []func()
	arenaMini.mu.Lock()
	for _, g := range arenaMini.lucky {
		g.left--
		if g.left > 0 {
			continue
		}
		if g.betting {
			g.betting, g.left = false, mg.LuckyWaitS
			continue
		}
		after = append(after, arenaLuckyDraw(w, g)...)
		g.betting, g.left = true, mg.LuckyBetS
	}
	for _, g := range arenaMini.choose {
		if g.endAt.IsZero() {
			g.endAt = now.Add(time.Duration(mg.ChooseS) * time.Second)
		}
		if now.Before(g.endAt) {
			continue
		}
		after = append(after, arenaChooseDraw(w, g)...)
		g.endAt = now.Add(time.Duration(mg.ChooseS) * time.Second)
	}
	arenaMini.mu.Unlock()
	for _, f := range after {
		f()
	}
}

// arenaLuckyDraw is ConSoMayManGold.ResetGame/strFinish (lock held).
func arenaLuckyDraw(w *World, g *luckyGame) []func() {
	result := arenaRand(arena.Minigames.LuckyMax + 1)
	g.last = result
	g.lastWinners = nil
	paid := map[int64]bool{}
	var after []func()
	players := map[int64]bool{}
	for _, b := range g.bets {
		players[b.pid] = true
		if b.n != result || paid[b.pid] {
			continue
		}
		paid[b.pid] = true
		p := w.onlineChar(b.pid)
		if p == nil {
			continue // the legacy server also paid online players only
		}
		g.lastWinners = append(g.lastWinners, p.Char.Name)
		cfg := g.cfg
		after = append(after, func() {
			w.postToPlayer(p, func(*Zone) {
				if cfg.Item > 0 {
					p.AddItem(w.Data, cfg.Item, cfg.Reward, nil)
					p.sendInventory()
				} else {
					p.Char.Gem += int64(cfg.Reward)
				}
				p.dirty = true
				p.Send("self", p.Self())
				notice(p, "Chúc mừng bạn đã thắng "+itoa(cfg.Reward)+" với con số may mắn "+itoa(result))
			})
		})
	}
	msg := g.cfg.Name + " vòng này là: " + itoa(result)
	if len(g.lastWinners) > 0 {
		msg += ". Người thắng cuộc: "
		for i, n := range g.lastWinners {
			if i > 0 {
				msg += ", "
			}
			msg += n
		}
	} else {
		msg += ". Không có người thắng cuộc."
	}
	for pid := range players {
		if p := w.onlineChar(pid); p != nil {
			after = append(after, func() { w.postToPlayer(p, func(*Zone) { notice(p, msg) }) })
		}
	}
	g.bets = nil
	return after
}

// arenaChooseDraw is ChonAiDay_Gold.run:56-97 (lock held).
func arenaChooseDraw(w *World, g *chooseGame) []func() {
	var after []func()
	for i := range 2 {
		stakes := g.stakes[i]
		total := arenaSumStakes(stakes)
		if total == 0 {
			continue
		}
		ids := make([]int64, 0, len(stakes))
		for id := range stakes {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(a, b int) bool { return stakes[ids[a]] > stakes[ids[b]] })
		top := min(len(ids), max(arena.Minigames.ChooseTop, 1))
		winner := ids[arenaRand(top)]
		pct := g.cfg.NormalPct
		label := "giải thường"
		if i == 1 {
			pct, label = g.cfg.VipPct, "giải VIP"
		}
		prize := total * pct / 100
		name := g.names[winner]
		g.lastWinner[i] = name
		cfg := g.cfg
		if p := w.onlineChar(winner); p != nil {
			after = append(after, func() {
				w.postToPlayer(p, func(*Zone) {
					if cfg.Gem {
						p.Char.Gem += prize
					} else {
						p.Char.Gold = min(p.Char.Gold+prize, maxGold)
					}
					p.dirty = true
					p.Send("self", p.Self())
					notice(p, "Chúc mừng bạn đã dành chiến thắng và nhận được "+strconv.FormatInt(prize, 10))
				})
			})
		}
		text := name + " đã chiến thắng " + cfg.Name + " " + label
		after = append(after, func() { w.Broadcast(Encode("notice", map[string]string{"text": text})) })
		g.stakes[i] = map[int64]int64{}
	}
	g.names = map[int64]string{}
	return after
}
