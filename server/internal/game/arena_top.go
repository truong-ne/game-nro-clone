package game

import (
	"context"
	"encoding/json"
	"log/slog"
	"sync/atomic"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Leaderboards (managers/TopBanDoKhoBau.java, TopConDuongRanDoc.java,
// TopKhiGasHuyDiet.java, matches/TOP.java): computed from Postgres off the
// zone goroutines every top_refresh_s and served from memory with "top_get".

var arenaTopKinds = []struct{ ID, Name string }{
	{"power", "Top sức mạnh"},
	{"gold", "Top đại gia"},
	{"clan", "Top bang hội"},
	{"super_rank", "Top siêu hạng"},
	{"dungeon:arena_treasure", "Top Bản Đồ Kho Báu"},
	{"dungeon:arena_snake_way", "Top Con Đường Rắn Độc"},
	{"dungeon:arena_destron_gas", "Top Khí Gas Hủy Diệt"},
}

type arenaTopCache struct {
	at   time.Time
	rows map[string][]store.ArenaTopRow
}

var arenaTops atomic.Pointer[arenaTopCache]

func init() {
	register("top_get", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ Kind string }
		json.Unmarshal(d, &r)
		arenaSendTop(p, r.Kind)
	})
	worldStartHooks = append(worldStartHooks, func(w *World) {
		if w.Store == nil {
			return
		}
		go func() {
			every := time.Duration(max(arena.TopRefreshS, 30)) * time.Second
			tick := time.NewTicker(every)
			defer tick.Stop()
			for {
				arenaRefreshTops(w.Store)
				select {
				case <-w.Done():
					return
				case <-tick.C:
				}
			}
		}()
	})
}

func arenaRefreshTops(st *store.Store) {
	c := &arenaTopCache{at: time.Now(), rows: map[string][]store.ArenaTopRow{}}
	for _, k := range arenaTopKinds {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		rows, err := st.ArenaTop(ctx, k.ID, 100)
		cancel()
		if err != nil {
			slog.Warn("arena top", "kind", k.ID, "err", err)
			continue
		}
		c.rows[k.ID] = rows
	}
	arenaTops.Store(c)
}

func arenaSendTop(p *Player, kind string) {
	kinds := make([]map[string]string, 0, len(arenaTopKinds))
	name := ""
	for _, k := range arenaTopKinds {
		kinds = append(kinds, map[string]string{"id": k.ID, "name": k.Name})
		if k.ID == kind {
			name = k.Name
		}
	}
	if name == "" {
		kind, name = arenaTopKinds[0].ID, arenaTopKinds[0].Name
	}
	out := map[string]any{"kind": kind, "name": name, "kinds": kinds, "rows": []store.ArenaTopRow{}}
	if c := arenaTops.Load(); c != nil {
		out["rows"] = nonNil(c.rows[kind])
		out["updated"] = c.at.Unix()
	}
	p.Send("top_list", out)
}
