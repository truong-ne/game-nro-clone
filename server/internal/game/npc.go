package game

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

const npcRange = 120

// menuOption is one entry of the menu currently shown to a player.
type menuOption struct {
	Label  string
	Action string
	Arg    string
	Npc    int
}

func init() {
	register("npc", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ I int } // index into the map's npc list
		if json.Unmarshal(d, &r) == nil {
			z.talkNpc(p, r.I)
		}
	})
	register("menu", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ I int }
		if json.Unmarshal(d, &r) == nil && r.I >= 0 && r.I < len(p.npcMenu) {
			opt := p.npcMenu[r.I]
			p.npcMenu = nil
			z.menuAction(p, opt)
		}
	})
	register("giftcode", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ Code string }
		if json.Unmarshal(d, &r) == nil {
			z.redeem(p, strings.TrimSpace(r.Code))
		}
	})
	register("task", func(z *Zone, p *Player, _ json.RawMessage) { z.World.sendTask(p) })
}

func (z *Zone) talkNpc(p *Player, idx int) {
	if idx < 0 || idx >= len(z.Map.Npcs) {
		return
	}
	n := z.Map.Npcs[idx]
	if dist(p.Char.X, p.Char.Y, n.X, n.Y) > npcRange {
		return
	}
	d := z.World.Data
	c := d.Content.Npc(n.TemplateID)
	var opts []menuOption
	if c.Task || n.TemplateID == z.World.grandpa(p) {
		opts = append(opts, menuOption{"Nhiệm vụ", "task", "", n.TemplateID})
	}
	for _, s := range d.ShopByNpc[n.TemplateID] {
		label := "Cửa hàng"
		if len(d.ShopByNpc[n.TemplateID]) > 1 {
			label += " " + strings.ToLower(s.Tag)
		}
		opts = append(opts, menuOption{label, "shop", itoa(s.ID), n.TemplateID})
	}
	for _, o := range c.Options {
		opts = append(opts, menuOption{o.Label, o.Action, o.Arg, n.TemplateID})
	}
	for _, h := range npcOptions {
		for _, o := range h(z, p, n.TemplateID) {
			opts = append(opts, menuOption{o.Label, o.Action, o.Arg, n.TemplateID})
		}
	}
	greeting := c.Greeting
	if greeting == "" {
		greeting = npcName(d, n.TemplateID) + " xin chào!"
	}
	p.npcMenu = opts
	labels := make([]string, len(opts))
	for i, o := range opts {
		labels[i] = o.Label
	}
	p.Send("menu", map[string]any{"npc": n.TemplateID, "name": npcName(d, n.TemplateID), "text": greeting, "options": labels})
	z.World.Events.Emit(Event{Kind: EvTalkNpc, Player: p, Zone: z, Npc: n.TemplateID})
}

func (z *Zone) menuAction(p *Player, o menuOption) {
	d := z.World.Data
	switch o.Action {
	case "task":
		z.World.sendTask(p)
	case "shop":
		var id int
		if parseInt(o.Arg, &id) && d.Shops[id] != nil {
			z.openShop(p, d.Shops[id])
		}
	case "box":
		p.Send("box", nil)
		p.sendInventory()
	case "harvest":
		z.harvestTree(p)
	case "upgrade_tree":
		z.upgradeTree(p)
	case "clan":
		z.World.sendClan(p)
	case "giftcode":
		p.Send("prompt", map[string]string{"cmd": "giftcode", "text": "Nhập giftcode"})
	case "dungeon":
		z.openDungeon(p, o.Arg)
	case "duel_info":
		p.Send("notice", map[string]string{"text": "Chọn người chơi rồi bấm Thách đấu để so tài"})
	default:
		if h := menuActions[o.Action]; h != nil {
			h(z, p, o.Arg)
		}
	}
}

// magic tree: peas regrow over time up to a per-level cap.
func (z *Zone) treeState(p *Player) *store.MagicTree {
	t := &p.Char.Extra.MagicTree
	mt := z.World.Data.Content.MagicTree
	if t.Level == 0 {
		t.Level, t.Peas, t.GrowAt = 1, 5, time.Now().Unix()
	}
	lvl := min(t.Level, len(mt.MaxPeas)) - 1
	now := time.Now().Unix()
	for t.Peas < mt.MaxPeas[lvl] && now >= t.GrowAt {
		t.Peas++
		t.GrowAt += int64(mt.GrowSeconds[lvl])
	}
	if t.Peas >= mt.MaxPeas[lvl] && t.GrowAt < now {
		t.GrowAt = now
	}
	return t
}

func (z *Zone) harvestTree(p *Player) {
	t := z.treeState(p)
	mt := z.World.Data.Content.MagicTree
	if t.Peas == 0 {
		p.Send("notice", map[string]string{"text": "Đậu chưa chín"})
		return
	}
	bean := mt.BeanByLevel[min(t.Level, len(mt.BeanByLevel))-1]
	if !p.AddItem(z.World.Data, bean, t.Peas, nil) {
		p.Send("notice", map[string]string{"text": "Hành trang đã đầy"})
		return
	}
	p.Send("notice", map[string]string{"text": "Thu hoạch " + itoa(t.Peas) + " hạt đậu"})
	t.Peas = 0
	t.GrowAt = time.Now().Unix() + int64(mt.GrowSeconds[min(t.Level, len(mt.GrowSeconds))-1])
	p.dirty = true
	p.sendInventory()
}

func (z *Zone) upgradeTree(p *Player) {
	t := z.treeState(p)
	mt := z.World.Data.Content.MagicTree
	if t.Level > len(mt.UpgradeGold) {
		p.Send("notice", map[string]string{"text": "Cây đậu đã đạt cấp tối đa"})
		return
	}
	cost := mt.UpgradeGold[t.Level-1]
	if p.Char.Gold < cost {
		p.Send("notice", map[string]string{"text": "Cần " + itoa(int(cost)) + " vàng"})
		return
	}
	p.Char.Gold -= cost
	t.Level++
	p.dirty = true
	p.Send("notice", map[string]string{"text": "Cây đậu lên cấp " + itoa(t.Level)})
	p.Send("self", p.Self())
}

// redeem runs the DB transaction off the zone goroutine, then posts the
// reward back to the player's current zone.
func (z *Zone) redeem(p *Player, code string) {
	if code == "" || len(code) > 32 {
		return
	}
	w := z.World
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		rewards, err := w.Store.RedeemGiftcode(ctx, code, p.ID())
		w.postToPlayer(p, func(*Zone) {
			switch {
			case errors.Is(err, store.ErrNotFound):
				p.Send("notice", map[string]string{"text": "Giftcode không hợp lệ hoặc đã hết hạn"})
			case errors.Is(err, store.ErrUsed):
				p.Send("notice", map[string]string{"text": "Bạn đã dùng giftcode này"})
			case err != nil:
				p.Send("notice", map[string]string{"text": "Lỗi máy chủ"})
			default:
				for _, r := range rewards {
					p.AddItem(w.Data, r.Item, max(r.Qty, 1), r.Options)
				}
				p.dirty = true
				p.Send("notice", map[string]string{"text": "Nhận quà giftcode thành công"})
				p.sendInventory()
			}
		})
	}()
}
