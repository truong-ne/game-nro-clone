package game

import (
	"encoding/json"
)

// Vòng quay may mắn at Thượng Đế (services_func/LuckyRound.java,
// npc_list/ThuongDe.java:98-149). Prizes go to a side box
// (items_box_lucky_round, max 100) and are claimed into the bag.
// Commands: lucky_open, lucky_spin {count, currency}, lucky_box,
// lucky_claim {i} (-1 = all), lucky_clear.

type i2LuckyState struct {
	Box []i2Stack `json:"box"`
}

const i2LuckyKey = "items2_lucky"

func init() {
	npcOptions = append(npcOptions, func(z *Zone, p *Player, npc int) []MenuOption {
		if npc != i2cfg.Lucky.Npc {
			return nil
		}
		return []MenuOption{{Label: "Vòng quay may mắn", Action: "items2_lucky"}}
	})
	menuActions["items2_lucky"] = func(z *Zone, p *Player, _ string) { i2SendLucky(z, p) }
	register("lucky_open", func(z *Zone, p *Player, _ json.RawMessage) {
		if i2NearNpc(z, p, i2cfg.Lucky.Npc) {
			i2SendLucky(z, p)
		}
	})
	register("lucky_spin", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct {
			Count    int    `json:"count"`
			Currency string `json:"currency"`
		}
		if json.Unmarshal(d, &r) == nil && i2NearNpc(z, p, i2cfg.Lucky.Npc) {
			z.i2LuckySpin(p, r.Count, r.Currency)
		}
	})
	register("lucky_box", func(z *Zone, p *Player, _ json.RawMessage) { i2SendLuckyBox(p) })
	register("lucky_claim", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct {
			I int `json:"i"`
		}
		if json.Unmarshal(d, &r) == nil {
			z.i2LuckyClaim(p, r.I)
		}
	})
	register("lucky_clear", func(z *Zone, p *Player, _ json.RawMessage) {
		State[i2LuckyState](p, i2LuckyKey).Box = nil
		p.dirty = true
		i2SendLuckyBox(p)
	})
}

func i2SendLucky(z *Zone, p *Player) {
	l := &i2cfg.Lucky
	p.Send("lucky_open", map[string]any{"price_gold": l.PriceGold, "price_gem": l.PriceGem, "ticket": l.Ticket,
		"tickets": p.CountItem(l.Ticket), "max_spin": l.MaxSpin, "box": len(State[i2LuckyState](p, i2LuckyKey).Box), "box_max": l.BoxMax})
}

func i2SendLuckyBox(p *Player) {
	p.Send("lucky_box", map[string]any{"items": nonNil(State[i2LuckyState](p, i2LuckyKey).Box)})
}

// i2LuckySpin: openBallByGold/Gem/Ticket (LuckyRound.java:113-165).
func (z *Zone) i2LuckySpin(p *Player, count int, currency string) {
	l := &i2cfg.Lucky
	if count < 1 || count > l.MaxSpin {
		return
	}
	st := State[i2LuckyState](p, i2LuckyKey)
	if len(st.Box)+count > l.BoxMax {
		i2Notice(p, "Rương phụ đã đầy")
		return
	}
	n := int64(count)
	switch currency {
	case "gold":
		if p.Char.Gold < n*l.PriceGold {
			i2Notice(p, "Bạn không đủ vàng để mở")
			return
		}
		p.Char.Gold -= n * l.PriceGold
	case "gem":
		if p.Char.Gem < n*l.PriceGem {
			i2Notice(p, "Bạn không đủ ngọc để mở")
			return
		}
		p.Char.Gem -= n * l.PriceGem
	case "ticket":
		if !p.RemoveItems(l.Ticket, count) {
			i2Notice(p, "Bạn không đủ "+i2Name(z, l.Ticket)+" để quay")
			return
		}
	default:
		return
	}
	won := make([]i2Stack, 0, count)
	for range count {
		if r := i2PickPool(l.Pool); r != nil {
			won = append(won, r.roll(z.World.Data))
		}
	}
	st.Box = append(st.Box, won...)
	p.dirty = true
	p.Send("lucky_result", map[string]any{"items": won, "box": len(st.Box)})
	p.Send("self", p.Self())
	p.sendInventory()
}

// i2LuckyClaim moves box items into the bag (gold goes to the wallet).
func (z *Zone) i2LuckyClaim(p *Player, i int) {
	st := State[i2LuckyState](p, i2LuckyKey)
	d := z.World.Data
	take := func(k int) bool {
		s := st.Box[k]
		if d.ItemsFull[s.TID] == nil {
			return true // unknown template: drop it from the box
		}
		return p.AddItem(d, s.TID, s.Qty, append([][2]int(nil), s.Opts...))
	}
	full := false
	if i >= 0 {
		if i >= len(st.Box) {
			return
		}
		if take(i) {
			st.Box = append(st.Box[:i], st.Box[i+1:]...)
		} else {
			full = true
		}
	} else {
		kept := st.Box[:0]
		for k := range st.Box {
			if full || !take(k) {
				full = true
				kept = append(kept, st.Box[k])
			}
		}
		st.Box = kept
	}
	if full {
		i2Notice(p, "Hành trang đã đầy")
	}
	p.dirty = true
	i2SendLuckyBox(p)
	p.Send("self", p.Self())
	p.sendInventory()
}
