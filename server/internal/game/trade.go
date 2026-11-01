package game

import (
	"encoding/json"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

const (
	tradeRange    = 200
	maxTradeItems = 12
)

// trade is a two-party exchange; both players are in the same zone, so the
// whole exchange runs on one goroutine and is atomic.
type trade struct {
	a, b    *Player
	offer   map[int64]*tradeOffer
	pending bool // b has not accepted yet
}

type tradeOffer struct {
	Slots  []int `json:"slots"`
	Gold   int64 `json:"gold"`
	Locked bool  `json:"locked"`
}

func (t *trade) other(p *Player) *Player {
	if t.a == p {
		return t.b
	}
	return t.a
}

func init() {
	register("trade_req", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ ID int64 }
		if json.Unmarshal(d, &r) != nil {
			return
		}
		o := z.players[r.ID]
		if o == nil || o == p || o.trade != nil || p.trade != nil || dist(p.Char.X, p.Char.Y, o.Char.X, o.Char.Y) > tradeRange {
			return
		}
		t := &trade{a: p, b: o, pending: true, offer: map[int64]*tradeOffer{p.ID(): {}, o.ID(): {}}}
		p.trade, o.trade = t, t
		o.Send("trade_invite", map[string]any{"id": p.ID(), "name": p.Char.Name})
	})
	register("trade_accept", func(z *Zone, p *Player, _ json.RawMessage) {
		t := p.trade
		if t == nil || t.b != p || !t.pending {
			return
		}
		t.pending = false
		t.a.Send("trade_open", map[string]any{"id": t.b.ID(), "name": t.b.Char.Name})
		t.b.Send("trade_open", map[string]any{"id": t.a.ID(), "name": t.a.Char.Name})
	})
	register("trade_offer", func(z *Zone, p *Player, d json.RawMessage) {
		t := p.trade
		var o tradeOffer
		if t == nil || t.pending || json.Unmarshal(d, &o) != nil || len(o.Slots) > maxTradeItems || o.Gold < 0 {
			return
		}
		if t.offer[p.ID()].Locked {
			return
		}
		o.Locked = false
		t.offer[p.ID()] = &o
		t.offer[t.other(p).ID()].Locked = false // any change unlocks the other side
		z.sendTrade(t)
	})
	register("trade_lock", func(z *Zone, p *Player, _ json.RawMessage) {
		t := p.trade
		if t == nil || t.pending {
			return
		}
		t.offer[p.ID()].Locked = true
		if t.offer[t.a.ID()].Locked && t.offer[t.b.ID()].Locked {
			z.completeTrade(t)
			return
		}
		z.sendTrade(t)
	})
	register("trade_cancel", func(z *Zone, p *Player, _ json.RawMessage) { z.cancelTrade(p) })
}

func (z *Zone) sendTrade(t *trade) {
	view := func(p *Player) map[string]any {
		o := t.offer[p.ID()]
		var items []store.Item
		for _, s := range o.Slots {
			if it := p.item(store.ContainerBag, s); it != nil {
				items = append(items, *it)
			}
		}
		return map[string]any{"id": p.ID(), "items": nonNil(items), "gold": o.Gold, "locked": o.Locked}
	}
	msg := Encode("trade", map[string]any{"a": view(t.a), "b": view(t.b)})
	t.a.SendRaw(msg)
	t.b.SendRaw(msg)
}

func (z *Zone) cancelTrade(p *Player) {
	t := p.trade
	if t == nil {
		return
	}
	t.a.trade, t.b.trade = nil, nil
	t.a.Send("trade_close", nil)
	t.b.Send("trade_close", nil)
}

// completeTrade validates both offers against current inventories and swaps.
func (z *Zone) completeTrade(t *trade) {
	defer z.cancelTrade(t.a)
	type moved struct{ items []store.Item }
	take := func(p *Player) (moved, bool) {
		o := t.offer[p.ID()]
		if o.Gold > p.Char.Gold {
			return moved{}, false
		}
		var m moved
		seen := map[int]bool{}
		for _, s := range o.Slots {
			it := p.item(store.ContainerBag, s)
			if it == nil || seen[s] {
				return moved{}, false
			}
			seen[s] = true
			m.items = append(m.items, *it)
		}
		return m, true
	}
	ma, okA := take(t.a)
	mb, okB := take(t.b)
	freeA, freeB := countFree(t.a), countFree(t.b)
	if !okA || !okB || freeA+len(ma.items) < len(mb.items) || freeB+len(mb.items) < len(ma.items) {
		t.a.Send("notice", map[string]string{"text": "Giao dịch thất bại"})
		t.b.Send("notice", map[string]string{"text": "Giao dịch thất bại"})
		return
	}
	give := func(from, to *Player, m moved) {
		for _, it := range m.items {
			from.takeItem(store.ContainerBag, it.Slot, it.Quantity)
		}
		g := t.offer[from.ID()].Gold
		from.Char.Gold -= g
		to.Char.Gold = min(to.Char.Gold+g, maxGold)
	}
	give(t.a, t.b, ma)
	give(t.b, t.a, mb)
	for _, it := range ma.items {
		t.b.addRaw(it)
	}
	for _, it := range mb.items {
		t.a.addRaw(it)
	}
	for _, p := range []*Player{t.a, t.b} {
		p.dirty = true
		p.Send("notice", map[string]string{"text": "Giao dịch thành công"})
		p.sendInventory()
	}
}

func countFree(p *Player) int {
	n := p.size(store.ContainerBag)
	for _, it := range p.Char.Items {
		if it.Container == store.ContainerBag {
			n--
		}
	}
	return n
}

// addRaw places a whole item stack (with its options) into a free bag slot.
func (p *Player) addRaw(it store.Item) {
	it.Container, it.Slot = store.ContainerBag, p.freeSlot(store.ContainerBag)
	if it.Slot >= 0 {
		p.Char.Items = append(p.Char.Items, it)
	}
}
