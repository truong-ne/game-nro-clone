package game

import (
	"math/rand/v2"
	"time"
)

// Multi-form bosses (Boss.java keeps a BossData[] and switches to the next
// form when one dies: Fide đại ca 1→2→3, Xên bọ hung 1→2→3, Cooler, Black Goku,
// Baby...). The core scheduler spawns the first form; when it is killed we
// spawn the next form in the same zone and keep the scheduler's "alive" flag
// set until the last form dies (or leaves), then start the respawn timer.

func init() {
	worldStartHooks = append(worldStartHooks, func(w *World) {
		w.Events.On(EvKillBoss, func(e Event) { arenaOnBossKilled(w, e) })
		go arenaMabuLoop(w)
	})
	npcOptions = append(npcOptions, func(z *Zone, p *Player, npc int) []MenuOption {
		if npc == arena.Mabu14H.Npc {
			return []MenuOption{{Label: "Đi Mabư 14h", Action: "dg_mabu"}}
		}
		return nil
	})
	menuActions["dg_mabu"] = arenaJoinMabu
}

func arenaOnBossKilled(w *World, e Event) {
	s := arena.stageByID[e.Boss]
	if s == nil || s == arena.Mabu14H.Boss || s.stageIndex(e.Boss) != 0 || len(s.templates) < 2 || e.Zone == nil {
		return
	}
	z := e.Zone
	scheduled := w.bossTemplate(s.ID) != nil
	release := func() {}
	if scheduled {
		release = func() { arenaReleaseScheduler(w, s) }
		// hitBoss calls the scheduler's onDeath right after this event; re-mark
		// the family alive once that has run so the first form does not respawn
		// while later forms are still fighting.
		z.Post(func() {
			if sc := w.bossSched; sc != nil {
				sc.mu.Lock()
				sc.alive[s.ID] = true
				sc.mu.Unlock()
			}
		})
	}
	arenaSpawnStage(z, s, 1, release)
}

func arenaReleaseScheduler(w *World, s *arenaBossSpec) {
	sc := w.bossSched
	if sc == nil {
		return
	}
	sc.mu.Lock()
	sc.alive[s.ID] = false
	sc.next[s.ID] = time.Now().Add(time.Duration(max(s.RespawnS, 30)) * time.Second)
	sc.mu.Unlock()
}

// arenaSpawnStage spawns form i of family s in z (zone goroutine). The next
// form follows only when a player killed this one; leaving ends the chain.
func arenaSpawnStage(z *Zone, s *arenaBossSpec, i int, done func()) *Boss {
	if i >= len(s.templates) {
		done()
		return nil
	}
	t := s.templates[i]
	b := &Boss{T: t, Key: t.ID, HP: t.HP}
	b.onDeath = func(killer *Player) {
		if killer != nil && i+1 < len(s.templates) {
			arenaSpawnStage(z, s, i+1, done)
			return
		}
		done()
	}
	z.spawnBoss(b)
	return b
}

// --- Mabư 14h (map/phoban/MajinBuu14H.java, services_dungeon/MajinBuu14HService.java) ---

func arenaMabuOpen(now time.Time) bool {
	h := now.Hour() // TimeUtil.isMabu14HOpen: 14 <= hour < 15 (utils/TimeUtil.java:205)
	return h >= arena.Mabu14H.HourOpen && h < arena.Mabu14H.HourClose
}

func arenaMabuLoop(w *World) {
	tick := time.NewTicker(5 * time.Second)
	defer tick.Stop()
	spawnedDay, wasOpen := "", false
	for {
		select {
		case <-w.Done():
			return
		case <-tick.C:
		}
		now := arenaNow()
		open := arenaMabuOpen(now)
		if open && spawnedDay != arenaDay(now) && arena.Mabu14H.Boss != nil {
			if zs := w.zones[arena.Mabu14H.BossMap]; len(zs) > 0 {
				spawnedDay = arenaDay(now)
				z := zs[rand.IntN(len(zs))]
				z.Post(func() { arenaSpawnStage(z, arena.Mabu14H.Boss, 0, func() {}) })
				w.Broadcast(Encode("notice", map[string]string{"text": "Mabư đã thoát khỏi vỏ bọc tại " + z.Map.Name + " khu " + itoa(z.ID)}))
			}
		}
		if wasOpen && !open {
			arenaMabuClose(w) // MajinBuu14H.finish/kickOut: everyone goes home
		}
		wasOpen = open
	}
}

func arenaMabuClose(w *World) {
	for _, mid := range []int{arena.Mabu14H.GateMap, arena.Mabu14H.BossMap} {
		for _, z := range w.zones[mid] {
			z.Post(func() {
				for _, b := range append([]*Boss(nil), z.bosses...) {
					if arena.stageByID[b.T.ID] == arena.Mabu14H.Boss {
						z.removeBoss(b)
						z.broadcast(Encode("boss_leave", map[string]string{"key": b.Key}), 0)
					}
				}
				for _, p := range z.players {
					if !p.IsAdmin {
						notice(p, "Mabư 14h đã kết thúc")
						z.leaveTo(p, HomeMap(p.Char.Gender), -1, 300, 336)
					}
				}
			})
		}
	}
}

// arenaJoinMabu is MajinBuu14HService.joinMaBu2H: a gate zone with fewer than
// 5 players, else any.
func arenaJoinMabu(z *Zone, p *Player, _ string) {
	cfg := arena.Mabu14H
	if !arenaMabuOpen(arenaNow()) {
		notice(p, "Vào lúc "+itoa(cfg.HourOpen)+"h Mabư sẽ thoát khỏi vỏ bọc, hãy quay lại sau")
		return
	}
	zoneID := -1
	for _, gz := range z.World.zones[cfg.GateMap] {
		if int(gz.count.Load()) < cfg.MaxPerZone {
			zoneID = gz.ID
			break
		}
	}
	z.leaveTo(p, cfg.GateMap, zoneID, 300, 312)
}
