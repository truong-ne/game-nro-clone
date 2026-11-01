package game

import (
	"context"
	"encoding/json"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

const (
	CurrencyGold   = 0
	CurrencyGem    = 1
	CurrencyRuby   = 3
	CurrencyCoupon = 4
	sellRatio      = 4 // NPCs buy back at 1/4 of the template gold price
)

type ShopItem struct {
	ID         int      `json:"id"`
	TemplateID int      `json:"tid"`
	IsNew      bool     `json:"new"`
	Currency   int      `json:"currency"`
	Cost       int64    `json:"cost"`
	Options    [][2]int `json:"options"`
}

type ShopTab struct {
	Name  string      `json:"name"`
	Items []*ShopItem `json:"items"`
}

type Shop struct {
	ID    int       `json:"id"`
	NpcID int       `json:"npc"`
	Tag   string    `json:"tag"`
	Tabs  []ShopTab `json:"tabs"`
	items map[int]*ShopItem
}

func loadShops(ctx context.Context, pool *pgxpool.Pool, d *Data) error {
	d.Shops, d.ShopByNpc = map[int]*Shop{}, map[int][]*Shop{}
	rows, _ := pool.Query(ctx, `SELECT id, npc_id, tag_name FROM shop ORDER BY id`)
	for rows.Next() {
		s := &Shop{items: map[int]*ShopItem{}}
		if err := rows.Scan(&s.ID, &s.NpcID, &s.Tag); err != nil {
			return err
		}
		d.Shops[s.ID] = s
		d.ShopByNpc[s.NpcID] = append(d.ShopByNpc[s.NpcID], s)
	}
	tabIdx := map[int][2]int{} // tab id -> (shop id, index)
	rows, _ = pool.Query(ctx, `SELECT id, shop_id, name FROM shop_tab ORDER BY shop_id, id`)
	for rows.Next() {
		var id, shop int
		var name string
		if err := rows.Scan(&id, &shop, &name); err != nil {
			return err
		}
		if s := d.Shops[shop]; s != nil {
			tabIdx[id] = [2]int{shop, len(s.Tabs)}
			s.Tabs = append(s.Tabs, ShopTab{Name: name, Items: []*ShopItem{}})
		}
	}
	all := map[int]*ShopItem{}
	rows, _ = pool.Query(ctx, `SELECT id, tab_id, template_id, is_new, currency, cost FROM shop_item ORDER BY sort_order`)
	for rows.Next() {
		it := &ShopItem{Options: [][2]int{}}
		var tab int
		if err := rows.Scan(&it.ID, &tab, &it.TemplateID, &it.IsNew, &it.Currency, &it.Cost); err != nil {
			return err
		}
		if ti, ok := tabIdx[tab]; ok {
			s := d.Shops[ti[0]]
			s.Tabs[ti[1]].Items = append(s.Tabs[ti[1]].Items, it)
			s.items[it.ID] = it
			all[it.ID] = it
		}
	}
	rows, _ = pool.Query(ctx, `SELECT shop_item_id, option_id, param FROM shop_item_option`)
	for rows.Next() {
		var id, opt, param int
		if err := rows.Scan(&id, &opt, &param); err != nil {
			return err
		}
		if it := all[id]; it != nil {
			it.Options = append(it.Options, [2]int{opt, param})
		}
	}
	return rows.Err()
}

func (p *Player) wallet(currency int) *int64 {
	switch currency {
	case CurrencyGold:
		return &p.Char.Gold
	case CurrencyGem:
		return &p.Char.Gem
	case CurrencyRuby:
		return &p.Char.Ruby
	}
	return nil
}

func (z *Zone) openShop(p *Player, s *Shop) {
	p.shop = s.ID
	p.Send("shop", s)
}

func init() {
	register("shop_buy", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ ID, Qty int }
		if json.Unmarshal(d, &r) == nil {
			z.shopBuy(p, r.ID, max(r.Qty, 1))
		}
	})
	register("shop_sell", func(z *Zone, p *Player, d json.RawMessage) {
		var r slotReq
		if json.Unmarshal(d, &r) == nil {
			z.shopSell(p, r.Slot)
		}
	})
	register("shop_close", func(z *Zone, p *Player, _ json.RawMessage) { p.shop = -1 })
}

// shopBuy only works for the shop the player opened through its NPC.
func (z *Zone) shopBuy(p *Player, itemID, qty int) {
	s := z.World.Data.Shops[p.shop]
	if s == nil {
		return
	}
	it := s.items[itemID]
	if it == nil || qty > 99 {
		return
	}
	t := z.World.Data.ItemsFull[it.TemplateID]
	if !t.Stackable {
		qty = 1
	}
	w := p.wallet(it.Currency)
	total := it.Cost * int64(qty)
	if w == nil || *w < total {
		p.Send("notice", map[string]string{"text": "Không đủ tiền"})
		return
	}
	if !p.AddItem(z.World.Data, it.TemplateID, qty, append([][2]int(nil), it.Options...)) {
		p.Send("notice", map[string]string{"text": "Hành trang đã đầy"})
		return
	}
	*w -= total
	p.dirty = true
	p.Send("notice", map[string]string{"text": "Mua thành công " + t.Name})
	p.sendInventory()
}

func (z *Zone) shopSell(p *Player, slot int) {
	if p.shop < 0 {
		return
	}
	it := p.item(store.ContainerBag, slot)
	if it == nil {
		return
	}
	t := z.World.Data.ItemsFull[it.TemplateID]
	price := t.Gold / sellRatio * int64(it.Quantity)
	p.takeItem(store.ContainerBag, slot, it.Quantity)
	p.Char.Gold = min(p.Char.Gold+max(price, 1), maxGold)
	p.Send("notice", map[string]string{"text": "Đã bán " + t.Name})
	p.sendInventory()
}
