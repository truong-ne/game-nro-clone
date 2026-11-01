package game

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"sort"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Radar cards (sưu tầm thẻ): radar/*.java, services/RadarService.java,
// UseItem.UseCard:2746, Controller.java:138 (activate), NPoint.java:252 (stats).
// Card items are item_template type 33 whose id equals the radar id.

const buffItemCard = 33

type buffRadarOpt struct {
	ID     int `json:"id"`
	Param  int `json:"param"`
	Active int `json:"activeCard"`
}

type buffRadarCard struct {
	ID, Icon, Rank, Max, Type, Mob int
	Name, Info                     string
	Options                        []buffRadarOpt
	Require, RequireLevel          int
}

// buffRadar is loaded once at startup (dataHooks) and read-only afterwards.
var buffRadar = map[int]*buffRadarCard{}

type buffCard struct {
	Amount int  `json:"amount"`
	Level  int  `json:"level"` // -1 first card, then 1, 2 (UseCard:2764)
	Used   bool `json:"used"`
}

type buffRadarState struct {
	Cards map[int]*buffCard `json:"cards"`
}

func buffCards(p *Player) *buffRadarState {
	s := State[buffRadarState](p, "radar")
	if s.Cards == nil {
		s.Cards = map[int]*buffCard{}
	}
	return s
}

func init() {
	dataHooks = append(dataHooks, func(ctx context.Context, pool *pgxpool.Pool, d *Data) error {
		rows, err := pool.Query(ctx, `SELECT id, icon_id, rank, max, type, mob_id, name, info, options, require, require_level FROM radar`)
		if err != nil {
			return err
		}
		defer rows.Close()
		m := map[int]*buffRadarCard{}
		for rows.Next() {
			c := &buffRadarCard{}
			var opts []byte
			if err := rows.Scan(&c.ID, &c.Icon, &c.Rank, &c.Max, &c.Type, &c.Mob, &c.Name, &c.Info, &opts, &c.Require, &c.RequireLevel); err != nil {
				return err
			}
			json.Unmarshal(opts, &c.Options)
			m[c.ID] = c
		}
		buffRadar = m
		return rows.Err()
	})
	useByType[buffItemCard] = func(z *Zone, p *Player, slot int, it *store.Item) bool {
		buffUseCard(z, p, slot, it.TemplateID)
		return true
	}
	recalcHooks = append(recalcHooks, func(p *Player, d *Data, m *Mods) {
		for id, c := range buffCards(p).Cards {
			t := buffRadar[id]
			if !c.Used || t == nil {
				continue
			}
			tier := max(c.Level, 0) // level -1 uses tier 0 (NPoint.java:255)
			for _, o := range t.Options {
				if o.Active == tier {
					if f := optionEffects[o.ID]; f != nil {
						f(p, m, int64(o.Param))
					}
				}
			}
		}
	})
	register("buff_radar", func(z *Zone, p *Player, _ json.RawMessage) { buffSendRadar(p) })
	register("buff_radar_use", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ ID int }
		if json.Unmarshal(d, &r) == nil {
			buffToggleCard(z, p, r.ID)
		}
	})
}

// buffUseCard is UseItem.UseCard: collect one card, levelling up at max.
func buffUseCard(z *Zone, p *Player, slot, id int) {
	t := buffRadar[id]
	if t == nil {
		p.Send("notice", map[string]string{"text": "Thẻ không hợp lệ"})
		return
	}
	cards := buffCards(p)
	if t.Require != -1 {
		req := buffRadar[t.Require]
		rc := cards.Cards[t.Require]
		if req == nil || rc == nil || rc.Level < t.RequireLevel {
			name := "thẻ yêu cầu"
			if req != nil {
				name = req.Name
			}
			p.Send("notice", map[string]string{"text": "Bạn cần sưu tầm " + name + " ở cấp độ " + itoa(t.RequireLevel) + " mới có thể sử dụng thẻ này"})
			return
		}
	}
	c := cards.Cards[id]
	if c == nil {
		cards.Cards[id] = &buffCard{Amount: 1, Level: -1}
	} else {
		if c.Level >= 2 {
			p.Send("notice", map[string]string{"text": "Thẻ này đã đạt cấp tối đa"})
			return
		}
		c.Amount++
		if c.Amount >= t.Max {
			c.Amount = 0
			if c.Level == -1 {
				c.Level = 1
			} else {
				c.Level++
			}
			buffRefresh(z, p)
		}
	}
	p.takeItem(store.ContainerBag, slot, 1)
	buffSendRadar(p)
}

// buffToggleCard: only one card may be active at a time (Controller.java:152).
func buffToggleCard(z *Zone, p *Player, id int) {
	cards := buffCards(p)
	c := cards.Cards[id]
	if c == nil || c.Level == 0 {
		return
	}
	if !c.Used {
		for _, o := range cards.Cards {
			if o.Used {
				p.Send("notice", map[string]string{"text": "Số thẻ sử dụng đã đạt tối đa"})
				return
			}
		}
	}
	c.Used = !c.Used
	buffRefresh(z, p)
	buffSendRadar(p)
}

func buffSendRadar(p *Player) {
	cards := buffCards(p)
	ids := make([]int, 0, len(buffRadar))
	for id := range buffRadar {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	out := make([]map[string]any, 0, len(ids))
	for _, id := range ids {
		t := buffRadar[id]
		row := map[string]any{"id": id, "name": t.Name, "info": t.Info, "icon": t.Icon, "rank": t.Rank, "max": t.Max,
			"mob": t.Mob, "options": t.Options, "amount": 0, "level": 0, "used": false}
		if c := cards.Cards[id]; c != nil {
			row["amount"], row["level"], row["used"] = c.Amount, c.Level, c.Used
		}
		out = append(out, row)
	}
	p.Send("buff_radar", map[string]any{"cards": out})
}

// buffRadarDrop: killing a card's mob may drop its card (monster cards, type 0).
func buffRadarDrop(z *Zone, p *Player, mob int) {
	for _, t := range buffRadar {
		if t.Type == 0 && t.Mob == mob && rand.Float64()*100 < buffCfg.Radar.DropChance &&
			z.World.Data.ItemsFull[t.ID] != nil {
			z.dropItem(t.ID, 1, nil, p.Char.X, p.Char.Y, p.ID())
		}
	}
}
