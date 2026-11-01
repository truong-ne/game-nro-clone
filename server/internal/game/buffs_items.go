package game

import (
	"context"
	"math/rand/v2"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Timed item buffs (item/ItemTime.java, UseItem.useItemTime:1520) and charms
// (player/Charms.java, InventoryService.addItemSpecial:704).

const (
	buffItemCharm = 13 // item_template.type of bùa
	buffOptMinute = 1  // "Thời gian sử dụng # phút", added to charm shop items
)

func init() {
	for id := range buffCfg.byID {
		useByID[id] = buffUseItem
	}
	useByType[buffItemCharm] = func(z *Zone, p *Player, slot int, it *store.Item) bool {
		if buffActivateCharm(z, p, it) {
			p.takeItem(store.ContainerBag, slot, 1)
		}
		return true
	}
	// Tag charm shop items with their duration so the bag item knows it.
	dataHooks = append(dataHooks, func(ctx context.Context, pool *pgxpool.Pool, d *Data) error {
		buffTagCharmShops(d)
		return nil
	})
	recalcHooks = append(recalcHooks, func(p *Player, d *Data, m *Mods) {
		s := buffItems(p)
		now := time.Now()
		for k := range s.Until {
			if sp := buffCfg.byKey[k]; sp != nil && s.on(k, now) { // NPoint.setHpMax:869, setMpMax:1003, setDame:1096,1120
				m.HPPct += sp.HPPct
				m.MPPct += sp.MPPct
				m.DmgPct += sp.DmgPct
			}
		}
	})
	inDamageHooks = append(inDamageHooks, func(p *Player, dmg int64, src DamageSrc) int64 {
		s := buffItems(p)
		now := time.Now()
		for k := range s.Until {
			if sp := buffCfg.byKey[k]; sp != nil && sp.TakenPct > 0 && s.on(k, now) { // Player.injured:1163 giáp xên
				dmg = dmg * sp.TakenPct / 100
			}
		}
		if src.FromMonster() && buffCharm(p, "da_trau") { // Mob.java:401: mob hits only
			dmg /= 2
		}
		if src.FromMonster() && buffCharm(p, "bat_tu") && dmg >= p.Char.HP { // Player.injured:1185: mob hits only
			dmg = max(p.Char.HP-1, 0)
		}
		return dmg
	})
	outDamageHooks = append(outDamageHooks, func(p *Player, dmg int64, a *AttackCtx) int64 {
		if a.Mob != nil && buffCharm(p, "manh_me") { // SkillService.playerAttackMob:983
			dmg += dmg * 150 / 100
		}
		return dmg
	})
	powerHooks = append(powerHooks, func(p *Player, n int64) int64 {
		base := n
		if buffCharm(p, "tri_tue") { // NPoint.calSucManhTiemNang:1586 (x2)
			n += base
		}
		if buffCharm(p, "tri_tue3") { // x3 (the legacy code added 3x/8x; item text says x3/x4)
			n += base * 2
		}
		if buffCharm(p, "tri_tue4") {
			n += base * 3
		}
		s := buffItems(p)
		now := time.Now()
		for k := range s.Until {
			if sp := buffCfg.byKey[k]; sp != nil && sp.PowerExtra > 0 && s.on(k, now) { // :1608 đuôi khỉ
				n += base * sp.PowerExtra
			}
		}
		return n
	})
	secondHooks = append(secondHooks, buffItemsSecond)
	worldStartHooks = append(worldStartHooks, buffInstallEvents)
}

func buffTagCharmShops(d *Data) {
	for _, s := range d.Shops {
		mins, ok := buffCfg.CharmShopMin[s.Tag]
		if !ok {
			continue
		}
		for _, tab := range s.Tabs {
			for _, it := range tab.Items {
				if t := d.ItemsFull[it.TemplateID]; t != nil && t.Type == buffItemCharm {
					it.Options = append(it.Options, [2]int{buffOptMinute, mins})
				}
			}
		}
	}
}

func buffUseItem(z *Zone, p *Player, slot int, it *store.Item) bool {
	sp := buffCfg.byID[it.TemplateID]
	s := buffItems(p)
	now := time.Now()
	if sp.Group != "" {
		for k := range s.Until {
			if o := buffCfg.byKey[k]; o != nil && o.Group == sp.Group && o.Key != sp.Key && s.on(k, now) {
				p.Send("notice", map[string]string{"text": "Chỉ có thể sự dụng cùng lúc 1 vật phẩm bổ trợ cùng loại"})
				return true
			}
		}
	}
	add := int64(sp.Minutes) * 60_000
	end := now.UnixMilli() + add
	if sp.MaxMinutes > 0 && s.on(sp.Key, now) { // ẩn danh: +10 phút, tối đa 30
		end = min(s.Until[sp.Key]+add, now.UnixMilli()+int64(sp.MaxMinutes)*60_000)
	}
	s.Until[sp.Key] = end
	s.Seen = now.UnixMilli()
	p.takeItem(store.ContainerBag, slot, 1)
	p.Send("notice", map[string]string{"text": "Đã dùng " + sp.Name})
	buffRefresh(z, p)
	buffTimers(p)
	return true
}

func buffActivateCharm(z *Zone, p *Player, it *store.Item) bool {
	key := buffCfg.Charms[it.TemplateID]
	if key == "" {
		return false
	}
	mins := buffCfg.CharmDefaultMn
	for _, o := range it.Options {
		if o[0] == buffOptMinute && o[1] > 0 {
			mins = o[1]
		}
	}
	c := buffCharms(p)
	now := time.Now().UnixMilli()
	c.Until[key] = max(c.Until[key], now) + int64(mins)*60_000 // Charms.addTimeCharms
	p.dirty = true
	p.Send("notice", map[string]string{"text": "Kích hoạt " + buffCharmNames[key] + " " + itoa(mins) + " phút"})
	buffTimers(p)
	return true
}

func buffItemsSecond(z *Zone, now time.Time) {
	for _, p := range z.players {
		s := buffItems(p)
		s.Seen = now.UnixMilli()
		expired := false
		for k, u := range s.Until {
			if u <= now.UnixMilli() {
				delete(s.Until, k)
				expired = true
			}
		}
		if expired {
			buffRefresh(z, p)
			buffTimers(p)
		}
		// Bought charms go to the bag (shopBuy); activate them right away like
		// the legacy addItemSpecial did.
		for i := 0; i < len(p.Char.Items); i++ {
			it := p.Char.Items[i]
			t := z.World.Data.ItemsFull[it.TemplateID]
			if it.Container == store.ContainerBag && t != nil && t.Type == buffItemCharm && buffCfg.Charms[it.TemplateID] != "" {
				for range it.Quantity {
					buffActivateCharm(z, p, &it)
				}
				p.takeItem(store.ContainerBag, it.Slot, it.Quantity)
				p.sendInventory()
				i--
			}
		}
		// Bùa thu hút: own drops fly into the bag (Mob.hutItem:561).
		if buffCharm(p, "thu_hut") {
			for id, g := range z.ground {
				if g.Owner == p.ID() && p.AddItem(z.World.Data, g.TemplateID, g.Qty, g.Options) {
					delete(z.ground, id)
					z.broadcast(Encode("item_gone", map[string]any{"id": id, "by": p.ID()}), 0)
					z.World.Events.Emit(Event{Kind: EvPickup, Player: p, Zone: z, Item: g.TemplateID, Count: g.Qty})
					p.sendInventory()
				}
			}
		}
	}
}

// buffInstallEvents subscribes to kill/pickup events (máy dò, radar drops, gold intrinsic).
func buffInstallEvents(w *World) {
	w.Events.On(EvKillMob, func(e Event) {
		p, z := e.Player, e.Zone
		md := buffCfg.MayDo
		if buffItems(p).on("may_do", time.Now()) && e.Mob >= md.MobFrom && e.Mob <= md.MobTo &&
			rand.Float64()*100 < md.Chance { // Mob.mobReward:614
			z.dropItem(md.Item, 1, nil, p.Char.X, p.Char.Y, p.ID())
		}
		buffRadarDrop(z, p, e.Mob)
	})
	w.Events.On(EvPickup, func(e Event) {
		buffIntrinsicGold(e)
	})
}
