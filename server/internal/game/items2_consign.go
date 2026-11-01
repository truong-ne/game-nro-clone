package game

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Cửa hàng ký gửi (shop_ky_gui/ConsignShopService.java). Listings live in
// table consign_item; every DB call runs through World.async and money or
// items taken before a call are refunded if it fails.
// Commands: consign_open, consign_list {tab,page}, consign_mine,
// consign_sell {slot,qty,price,currency}, consign_buy {id,price,currency},
// consign_cancel {id}, consign_claim {id}, consign_top {id}.
// currency: "bar" (thỏi vàng) or "gem".

func init() {
	npcOptions = append(npcOptions, func(z *Zone, p *Player, npc int) []MenuOption {
		if npc != i2cfg.Consign.Npc {
			return nil
		}
		return []MenuOption{{Label: "Cửa hàng ký gửi", Action: "items2_consign"}}
	})
	menuActions["items2_consign"] = func(z *Zone, p *Player, _ string) { i2SendConsignOpen(p) }
	type req struct {
		ID       int64  `json:"id"`
		Tab      int    `json:"tab"`
		Page     int    `json:"page"`
		Slot     int    `json:"slot"`
		Qty      int    `json:"qty"`
		Price    int    `json:"price"`
		Currency string `json:"currency"`
	}
	on := func(name string, f func(z *Zone, p *Player, r req)) {
		register(name, func(z *Zone, p *Player, d json.RawMessage) {
			var r req
			if json.Unmarshal(d, &r) != nil {
				return
			}
			if !i2NearNpc(z, p, i2cfg.Consign.Npc) {
				i2Notice(p, "Hãy đến Cửa hàng ký gửi")
				return
			}
			if z.World.Store == nil {
				i2Notice(p, "Cửa hàng ký gửi tạm đóng")
				return
			}
			f(z, p, r)
		})
	}
	register("consign_open", func(z *Zone, p *Player, _ json.RawMessage) {
		if i2NearNpc(z, p, i2cfg.Consign.Npc) {
			i2SendConsignOpen(p)
		}
	})
	on("consign_list", func(z *Zone, p *Player, r req) { z.i2ConsignList(p, r.Tab, r.Page) })
	on("consign_mine", func(z *Zone, p *Player, r req) { z.i2ConsignMine(p) })
	on("consign_sell", func(z *Zone, p *Player, r req) { z.i2ConsignSell(p, r.Slot, r.Qty, r.Price, r.Currency) })
	on("consign_buy", func(z *Zone, p *Player, r req) { z.i2ConsignBuy(p, r.ID, r.Price, r.Currency) })
	on("consign_cancel", func(z *Zone, p *Player, r req) { z.i2ConsignCancel(p, r.ID) })
	on("consign_claim", func(z *Zone, p *Player, r req) { z.i2ConsignClaim(p, r.ID) })
	on("consign_top", func(z *Zone, p *Player, r req) { z.i2ConsignTop(p, r.ID) })
}

func i2SendConsignOpen(p *Player) {
	c := &i2cfg.Consign
	p.Send("consign_open", map[string]any{"tabs": c.TabNames, "fee_bars": c.FeeBars, "uptop_bars": c.UpTopBars,
		"tax_pct": c.TaxPct, "max_qty": c.MaxQty, "gold_bar": c.GoldBar})
	p.sendInventory()
}

// i2ConsignTab: ConsignShopService.getTabKiGui (ConsignShopService.java:366-376).
func i2ConsignTab(t *ItemTemplateFull) int {
	switch {
	case t.Type >= 0 && t.Type <= 2:
		return 0
	case t.Type <= 4:
		return 1
	case t.Type == 29:
		return 2
	}
	return 3
}

// i2CanConsign: itemCanConsign (ConsignShopService.java:334-346) and the
// option-30 ban in KiGui (:385-392).
func i2CanConsign(t *ItemTemplateFull, it *store.Item) bool {
	c := &i2cfg.Consign
	for _, o := range it.Options {
		if i2Has(c.Forbidden, o[0]) {
			return false
		}
	}
	if i2Has(c.AllowedTypes, t.Type) || i2Has(c.AllowedIDs, t.ID) {
		return true
	}
	for _, o := range it.Options {
		if i2Has(c.AllowedOpts, o[0]) {
			return true
		}
	}
	return false
}

func i2ConsignErr(p *Player, err error, notFound string) bool {
	switch {
	case err == nil:
		return false
	case errors.Is(err, store.ErrNotFound):
		i2Notice(p, notFound)
	default:
		i2Notice(p, "Lỗi máy chủ")
	}
	return true
}

func (z *Zone) i2ConsignList(p *Player, tab, page int) {
	c := &i2cfg.Consign
	if tab < 0 || tab >= len(c.TabNames) || page < 0 || page > 1000 {
		return
	}
	var list []store.ConsignItem
	var total int
	w := z.World
	w.async(p, func(ctx context.Context) (err error) {
		list, total, err = w.Store.ConsignList(ctx, tab, p.ID(), page*c.PageSize, c.PageSize)
		return err
	}, func(z *Zone, err error) {
		if i2ConsignErr(p, err, "") {
			return
		}
		pages := max((total+c.PageSize-1)/c.PageSize, 1)
		p.Send("consign_list", map[string]any{"tab": tab, "page": page, "pages": pages, "items": nonNil(list)})
	})
}

func (z *Zone) i2ConsignMine(p *Player) {
	var list []store.ConsignItem
	w := z.World
	w.async(p, func(ctx context.Context) (err error) {
		list, err = w.Store.ConsignMine(ctx, p.ID())
		return err
	}, func(z *Zone, err error) {
		if !i2ConsignErr(p, err, "") {
			p.Send("consign_mine", map[string]any{"items": nonNil(list)})
		}
	})
}

// i2ConsignPrice validates a price and returns (bars, gem).
func i2ConsignPrice(price int, currency string) (int, int, bool) {
	c := &i2cfg.Consign
	switch {
	case price <= 0:
		return 0, 0, false
	case currency == "bar" && price <= c.MaxBars:
		return price, 0, true
	case currency == "gem" && price <= c.MaxGem:
		return 0, price, true
	}
	return 0, 0, false
}

// i2ConsignSell: KiGui (ConsignShopService.java:378-439). The fee and the
// item leave the bag first and come back if the insert fails.
func (z *Zone) i2ConsignSell(p *Player, slot, qty, price int, currency string) {
	c := &i2cfg.Consign
	d := z.World.Data
	it := p.item(store.ContainerBag, slot)
	if it == nil {
		return
	}
	t := d.ItemsFull[it.TemplateID]
	bars, gem, ok := i2ConsignPrice(price, currency)
	switch {
	case t == nil || !i2CanConsign(t, it):
		i2Notice(p, "Vật phẩm không thể kí gửi")
		return
	case !ok:
		i2Notice(p, "Giá không hợp lệ")
		return
	case qty < 1 || qty > it.Quantity || qty > c.MaxQty:
		i2Notice(p, "Ký gửi tối đa x"+itoa(c.MaxQty))
		return
	case it.TemplateID == c.GoldBar && p.CountItem(c.GoldBar) < qty+c.FeeBars,
		p.CountItem(c.GoldBar) < c.FeeBars:
		i2Notice(p, "Bạn cần có ít nhất "+itoa(c.FeeBars)+" thỏi vàng để làm phí đăng bán")
		return
	}
	row := &store.ConsignItem{SellerID: p.ID(), Tab: i2ConsignTab(t), TemplateID: it.TemplateID, Quantity: qty,
		Options: append([][2]int(nil), it.Options...), PriceBars: bars, PriceGem: gem}
	p.takeItem(store.ContainerBag, slot, qty) // it is invalid from here
	p.RemoveItems(c.GoldBar, c.FeeBars)
	p.dirty = true
	p.sendInventory()
	w := z.World
	w.async(p, func(ctx context.Context) error { return w.Store.ConsignInsert(ctx, row) }, func(z *Zone, err error) {
		if err != nil {
			p.AddItem(d, row.TemplateID, row.Quantity, row.Options)
			p.AddItem(d, c.GoldBar, c.FeeBars, nil)
			i2Notice(p, "Lỗi máy chủ, đã hoàn lại vật phẩm")
		} else {
			i2Notice(p, "Đăng bán thành công")
			// Save now so the item cannot exist both in the shop and in the saved bag.
			w.saveAsync(p.Snapshot())
		}
		p.sendInventory()
		z.i2ConsignMine(p)
	})
}

// i2ConsignBuy: buyItem (ConsignShopService.java:115-163). The legacy server
// charged gold equal to the bar count; here the buyer pays thỏi vàng items.
func (z *Zone) i2ConsignBuy(p *Player, id int64, price int, currency string) {
	c := &i2cfg.Consign
	d := z.World.Data
	bars, gem, ok := i2ConsignPrice(price, currency)
	switch {
	case !ok:
		return
	case p.Char.Power < c.BuyMinPower:
		i2Notice(p, "Yêu cầu sức mạnh lớn hơn "+i2Money(c.BuyMinPower))
		return
	case p.freeSlot(store.ContainerBag) < 0:
		i2Notice(p, "Hành trang đã đầy")
		return
	case int64(gem) > p.Char.Gem:
		i2Notice(p, "Bạn không đủ Ngọc Xanh để mua vật phẩm này!")
		return
	case p.CountItem(c.GoldBar) < bars:
		i2Notice(p, "Bạn không đủ thỏi vàng để mua vật phẩm")
		return
	}
	p.Char.Gem -= int64(gem)
	p.RemoveItems(c.GoldBar, bars)
	p.dirty = true
	var got *store.ConsignItem
	w := z.World
	w.async(p, func(ctx context.Context) (err error) {
		got, err = w.Store.ConsignBuy(ctx, id, p.ID(), bars, gem)
		return err
	}, func(z *Zone, err error) {
		if err != nil {
			p.Char.Gem += int64(gem)
			if bars > 0 {
				p.AddItem(d, c.GoldBar, bars, nil)
			}
			i2ConsignErr(p, err, "Vật phẩm không tồn tại hoặc đã được bán")
		} else {
			if !p.AddItem(d, got.TemplateID, got.Quantity, got.Options) {
				z.dropItem(got.TemplateID, got.Quantity, got.Options, p.Char.X, p.Char.Y, p.ID())
			}
			i2Notice(p, "Bạn đã nhận được "+i2Name(z, got.TemplateID))
			w.saveAsync(p.Snapshot())
		}
		p.Send("self", p.Self())
		p.sendInventory()
	})
}

// i2ConsignCancel: claimOrDel action 1 (ConsignShopService.java:273-294).
func (z *Zone) i2ConsignCancel(p *Player, id int64) {
	if p.freeSlot(store.ContainerBag) < 0 {
		i2Notice(p, "Hành trang đã đầy")
		return
	}
	var got *store.ConsignItem
	w := z.World
	w.async(p, func(ctx context.Context) (err error) {
		got, err = w.Store.ConsignCancel(ctx, id, p.ID())
		return err
	}, func(z *Zone, err error) {
		if i2ConsignErr(p, err, "Vật phẩm không tồn tại hoặc đã được bán") {
			return
		}
		if !p.AddItem(w.Data, got.TemplateID, got.Quantity, got.Options) {
			z.dropItem(got.TemplateID, got.Quantity, got.Options, p.Char.X, p.Char.Y, p.ID())
		}
		p.dirty = true
		i2Notice(p, "Hủy bán vật phẩm thành công")
		w.saveAsync(p.Snapshot())
		p.sendInventory()
		z.i2ConsignMine(p)
	})
}

// i2ConsignClaim: claimOrDel action 2, seller gets the price minus tax (:295-319).
func (z *Zone) i2ConsignClaim(p *Player, id int64) {
	c := &i2cfg.Consign
	var got *store.ConsignItem
	w := z.World
	w.async(p, func(ctx context.Context) (err error) {
		got, err = w.Store.ConsignClaim(ctx, id, p.ID())
		return err
	}, func(z *Zone, err error) {
		if i2ConsignErr(p, err, "Vật phẩm không tồn tại hoặc chưa được bán") {
			return
		}
		if got.PriceBars > 0 {
			n := got.PriceBars - got.PriceBars*c.TaxPct/100
			if n > 0 && !p.AddItem(w.Data, c.GoldBar, n, nil) {
				z.dropItem(c.GoldBar, n, nil, p.Char.X, p.Char.Y, p.ID())
			}
		} else {
			p.Char.Gem += int64(got.PriceGem - got.PriceGem*c.TaxPct/100)
		}
		p.dirty = true
		i2Notice(p, "Bạn đã bán vật phẩm thành công")
		w.saveAsync(p.Snapshot())
		p.Send("self", p.Self())
		p.sendInventory()
		z.i2ConsignMine(p)
	})
}

// i2ConsignTop: upItemToTop/StartupItemToTop, costs thỏi vàng (:235-268).
func (z *Zone) i2ConsignTop(p *Player, id int64) {
	c := &i2cfg.Consign
	if !p.RemoveItems(c.GoldBar, c.UpTopBars) {
		i2Notice(p, "Bạn cần có ít nhất "+itoa(c.UpTopBars)+" thỏi vàng đưa vật phẩm lên trang đầu")
		return
	}
	p.dirty = true
	w := z.World
	w.async(p, func(ctx context.Context) error { return w.Store.ConsignUpTop(ctx, id, p.ID()) }, func(z *Zone, err error) {
		if err != nil {
			p.AddItem(w.Data, c.GoldBar, c.UpTopBars, nil)
			i2ConsignErr(p, err, "Vật phẩm không tồn tại hoặc đã được bán")
		} else {
			i2Notice(p, "Đưa vật phẩm lên trang đầu thành công")
		}
		p.sendInventory()
		z.i2ConsignMine(p)
	})
}
