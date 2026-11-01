package game

import "encoding/json"

// Daily dungeon openings and open minigame bets survive restarts.

type miniSnapshot struct {
	Lucky  [][][2]int64         `json:"lucky"`  // per game: [player id, number]
	Choose [][2]map[int64]int64 `json:"choose"` // per game: normal/vip stakes
	Names  []map[int64]string   `json:"names"`
}

func init() {
	registerPersistent("arena.dungeon_daily", func() any {
		arenaRuns.mu.Lock()
		defer arenaRuns.mu.Unlock()
		out := make(map[string]int, len(arenaRuns.clan))
		for k, v := range arenaRuns.clan {
			out[k] = v
		}
		return out
	}, func(raw json.RawMessage) error {
		var m map[string]int
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		arenaRuns.mu.Lock()
		defer arenaRuns.mu.Unlock()
		if arenaRuns.clan == nil {
			arenaRuns.clan = map[string]int{}
		}
		for k, v := range m { // keys embed the day, so stale days are harmless
			arenaRuns.clan[k] = v
		}
		return nil
	})

	registerPersistent("arena.minigame_bets", func() any {
		arenaMini.mu.Lock()
		defer arenaMini.mu.Unlock()
		var s miniSnapshot
		for _, g := range arenaMini.lucky {
			var bets [][2]int64
			for _, b := range g.bets {
				bets = append(bets, [2]int64{b.pid, int64(b.n)})
			}
			s.Lucky = append(s.Lucky, bets)
		}
		for _, g := range arenaMini.choose {
			s.Choose = append(s.Choose, g.stakes)
			s.Names = append(s.Names, g.names)
		}
		return s
	}, func(raw json.RawMessage) error {
		var s miniSnapshot
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		arenaMini.mu.Lock()
		defer arenaMini.mu.Unlock()
		for i, bets := range s.Lucky {
			if i < len(arenaMini.lucky) {
				for _, b := range bets {
					arenaMini.lucky[i].bets = append(arenaMini.lucky[i].bets, luckyBet{b[0], int(b[1])})
				}
			}
		}
		for i, st := range s.Choose {
			if i >= len(arenaMini.choose) {
				continue
			}
			g := arenaMini.choose[i]
			for k := range 2 {
				for pid, amt := range st[k] {
					g.stakes[k][pid] += amt
				}
			}
			if i < len(s.Names) {
				for pid, n := range s.Names[i] {
					g.names[pid] = n
				}
			}
		}
		return nil
	})
}
