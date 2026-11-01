package game

import (
	"context"
	"encoding/json"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Intrinsics (nội tại): intrinsic/Intrinsic.java, services/IntrinsicService.java,
// effects in NPoint.getDameAttack:1350, SkillService.affterUseSkill:1077 and
// setLastTimeUseSkill:1126, NPoint.calSucManhTiemNang:1619.

type buffIntrinsic struct {
	ID, Gender   int
	Name         string
	P1Min, P1Max int
	P2Min, P2Max int
	Icon         int
}

// buffIntrinsics is loaded once at startup (dataHooks) and read-only afterwards.
var buffIntrinsics []*buffIntrinsic

type buffIntrinsicState struct {
	ID    int `json:"id"`
	P1    int `json:"p1"`
	P2    int `json:"p2"`
	Count int `json:"count"` // countOpen: index into the gold price table
}

func buffIntr(p *Player) *buffIntrinsicState { return State[buffIntrinsicState](p, "intrinsic") }

// Skill damage bonus by intrinsic id -> skill template id (getDameAttack:1362-1426).
var buffIntrDmgSkill = map[int]int{1: 0, 2: 1, 8: 2, 13: 17, 16: 4, 17: 5, 9: 3}

// Cooldown reduction by intrinsic id -> skill template id (setLastTimeUseSkill:1129-1171).
var buffIntrCDSkill = map[int]int{3: skTDHS, 4: skQCKK, 5: skKhien, 15: skKhien, 20: skKhien, 10: skTriThuong,
	11: skMakanko, 12: skDeTrung, 19: skTuSat, 21: skHuytSao, 9: 3}

// "Sát thương đòn kế" after a control skill (affterUseSkill:1080-1098).
var buffIntrNextSkill = map[int]int{6: skDCTT, 7: skThoiMien, 14: skSocola, 22: skTroi}

func init() {
	dataHooks = append(dataHooks, func(ctx context.Context, pool *pgxpool.Pool, d *Data) error {
		rows, err := pool.Query(ctx, `SELECT id, gender, name, p1_min, p1_max, p2_min, p2_max, icon FROM intrinsic ORDER BY id`)
		if err != nil {
			return err
		}
		defer rows.Close()
		var list []*buffIntrinsic
		for rows.Next() {
			it := &buffIntrinsic{}
			if err := rows.Scan(&it.ID, &it.Gender, &it.Name, &it.P1Min, &it.P1Max, &it.P2Min, &it.P2Max, &it.Icon); err != nil {
				return err
			}
			list = append(list, it)
		}
		buffIntrinsics = list
		return rows.Err()
	})
	outDamageHooks = append(outDamageHooks, func(p *Player, dmg int64, a *AttackCtx) int64 {
		st := buffIntr(p)
		if st.ID == 0 {
			return dmg
		}
		if sk, ok := buffIntrDmgSkill[st.ID]; ok && a.Tmpl != nil && a.Tmpl.ID == sk {
			dmg += dmg * int64(st.P1) / 100
		}
		if st.ID == 18 && buffFx(p).monkeyLv > 0 { // :1484
			dmg += dmg * int64(st.P1) / 100
		}
		return dmg
	})
	powerHooks = append(powerHooks, func(p *Player, n int64) int64 {
		if st := buffIntr(p); st.ID == 24 { // :1619
			n += n * int64(st.P1) / 100
		}
		return n
	})
	// Intrinsic 25: always crit while HP <= p1% (NPoint.setIsCrit:1339).
	recalcHooks = append(recalcHooks, func(p *Player, d *Data, m *Mods) {
		if buffFx(p).lowHPCrit {
			p.Crit = max(p.Crit, 100)
		}
	})
	secondHooks = append(secondHooks, func(z *Zone, now time.Time) {
		for _, p := range z.players {
			st := buffIntr(p)
			fx := buffFx(p)
			low := st.ID == 25 && !p.Dead && p.MaxHP > 0 && p.Char.HP*100/p.MaxHP <= int64(st.P1)
			if low != fx.lowHPCrit {
				fx.lowHPCrit = low
				buffRefresh(z, p)
			}
		}
	})
	npcOptions = append(npcOptions, func(z *Zone, p *Player, npc int) []MenuOption {
		if containsInt(buffCfg.Intrinsic.Npcs, npc) {
			return []MenuOption{{Label: "Nội tại", Action: "buff_intrinsic"}}
		}
		return nil
	})
	menuActions["buff_intrinsic"] = func(z *Zone, p *Player, _ string) { buffIntrinsicMenu(p) }
	menuActions["buff_intrinsic_all"] = func(z *Zone, p *Player, _ string) { buffSendIntrinsicAll(p) }
	menuActions["buff_intrinsic_open"] = func(z *Zone, p *Player, _ string) { buffIntrinsicOpen(p, false) }
	menuActions["buff_intrinsic_vip"] = func(z *Zone, p *Player, _ string) { buffIntrinsicOpen(p, true) }
	register("buff_intrinsic", func(z *Zone, p *Player, _ json.RawMessage) { buffSendIntrinsic(p) })
	register("buff_intrinsic_menu", func(z *Zone, p *Player, _ json.RawMessage) { buffIntrinsicMenu(p) })
	register("buff_intrinsic_open", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ Vip bool }
		json.Unmarshal(d, &r)
		buffIntrinsicOpen(p, r.Vip)
	})
}

func buffIntrinsicByID(id int) *buffIntrinsic {
	for _, it := range buffIntrinsics {
		if it.ID == id {
			return it
		}
	}
	return nil
}

// buffIntrinsicName is Intrinsic.getName: params filled in.
func buffIntrinsicName(st *buffIntrinsicState) string {
	it := buffIntrinsicByID(st.ID)
	if it == nil || st.ID == 0 {
		return "Chưa kích hoạt nội tại"
	}
	n := strings.ReplaceAll(it.Name, "p0% đến p1", "p0")
	n = strings.ReplaceAll(n, "p2% đến p3", "p1")
	n = strings.ReplaceAll(n, "p0", itoa(st.P1))
	return strings.ReplaceAll(n, "p1", itoa(st.P2))
}

// buffIntrinsicDesc is Intrinsic.getDescription: the ranges.
func buffIntrinsicDesc(it *buffIntrinsic) string {
	r := strings.NewReplacer("p0", itoa(it.P1Min), "p1", itoa(it.P1Max), "p2", itoa(it.P2Min), "p3", itoa(it.P2Max))
	return r.Replace(it.Name)
}

func buffIntrinsicPool(gender int) []*buffIntrinsic {
	var out []*buffIntrinsic
	for _, it := range buffIntrinsics {
		if it.ID != 0 && (it.Gender == gender || it.Gender == 3) {
			out = append(out, it)
		}
	}
	return out
}

func buffIntrinsicCost(st *buffIntrinsicState) int64 {
	c := buffCfg.Intrinsic.CostOpen
	return c[min(max(st.Count, 0), len(c)-1)] * 1_000_000
}

func buffSendIntrinsic(p *Player) {
	st := buffIntr(p)
	icon := 0
	if it := buffIntrinsicByID(st.ID); it != nil {
		icon = it.Icon
	}
	p.Send("buff_intrinsic", map[string]any{"id": st.ID, "name": buffIntrinsicName(st), "icon": icon,
		"cost": buffIntrinsicCost(st), "vip_gem": buffCfg.Intrinsic.VipGem, "min_power": buffCfg.Intrinsic.MinPower})
}

func buffSendIntrinsicAll(p *Player) {
	list := []string{}
	for _, it := range buffIntrinsicPool(p.Char.Gender) {
		list = append(list, buffIntrinsicDesc(it))
	}
	p.Send("buff_intrinsic_all", map[string]any{"list": list})
}

// buffIntrinsicMenu is IntrinsicService.showMenu (a "con mèo" menu, no NPC needed).
func buffIntrinsicMenu(p *Player) {
	st := buffIntr(p)
	p.npcMenu = []menuOption{
		{"Xem tất cả Nội Tại", "buff_intrinsic_all", "", -1},
		{"Mở Nội Tại (" + itoa(int(buffIntrinsicCost(st)/1_000_000)) + " Tr vàng)", "buff_intrinsic_open", "", -1},
		{"Mở VIP (" + itoa(int(buffCfg.Intrinsic.VipGem)) + " ngọc)", "buff_intrinsic_vip", "", -1},
	}
	labels := make([]string, len(p.npcMenu))
	for i, o := range p.npcMenu {
		labels[i] = o.Label
	}
	p.Send("menu", map[string]any{"npc": -1, "name": "Nội tại",
		"text": "Nội tại là một kỹ năng bị động hỗ trợ đặc biệt\nHiện tại: " + buffIntrinsicName(st) +
			"\nBạn có muốn mở hoặc thay đổi nội tại không?", "options": labels})
}

// buffIntrinsicOpen is IntrinsicService.open / openVip.
func buffIntrinsicOpen(p *Player, vip bool) {
	if p.Char.Power < buffCfg.Intrinsic.MinPower {
		p.Send("notice", map[string]string{"text": "Yêu cầu sức mạnh tối thiểu " + itoa(int(buffCfg.Intrinsic.MinPower/1_000_000_000)) + " tỷ"})
		return
	}
	st := buffIntr(p)
	pool := buffIntrinsicPool(p.Char.Gender)
	if len(pool) == 0 {
		return
	}
	if vip {
		if p.Char.Gem < buffCfg.Intrinsic.VipGem {
			p.Send("notice", map[string]string{"text": "Bạn không có đủ ngọc"})
			return
		}
		p.Char.Gem -= buffCfg.Intrinsic.VipGem
		st.Count = 0
	} else {
		cost := buffIntrinsicCost(st)
		if p.Char.Gold < cost {
			p.Send("notice", map[string]string{"text": "Bạn không đủ vàng, cần " + itoa(int(cost)) + " vàng"})
			return
		}
		p.Char.Gold -= cost
		if st.Count < len(buffCfg.Intrinsic.CostOpen)-1 {
			st.Count++
		}
	}
	it := pool[rand.IntN(len(pool))]
	st.ID = it.ID
	st.P1 = it.P1Min + rand.IntN(max(it.P1Max-it.P1Min, 0)+1)
	st.P2 = it.P2Min + rand.IntN(max(it.P2Max-it.P2Min, 0)+1)
	p.dirty = true
	p.Send("notice", map[string]string{"text": "Bạn nhận được Nội tại:\n" + buffIntrinsicName(st)})
	p.Send("self", p.Self())
	buffSendIntrinsic(p)
}

// buffIntrinsicCooldown shortens the cooldown just started by useSkill.
func buffIntrinsicCooldown(p *Player, a *AttackCtx) {
	st := buffIntr(p)
	if sk, ok := buffIntrCDSkill[st.ID]; ok && sk == a.Tmpl.ID {
		if t, ok := p.skillUsed[a.Tmpl.ID]; ok {
			p.skillUsed[a.Tmpl.ID] = t.Add(-time.Duration(int64(a.Level.CoolDown)*int64(st.P1)/100) * time.Millisecond)
		}
	}
}

// buffIntrinsicAfterSkill arms the next-hit bonus after a control skill.
func buffIntrinsicAfterSkill(p *Player, skill int) {
	st := buffIntr(p)
	if sk, ok := buffIntrNextSkill[st.ID]; ok && sk == skill {
		buffFx(p).nextDmg = int64(st.P1)
	}
}

// buffIntrinsicGold: intrinsic 23 "Vàng rơi từ quái +p0%" on gold pickups.
func buffIntrinsicGold(e Event) {
	p := e.Player
	st := buffIntr(p)
	t := e.Zone.World.Data.ItemsFull[e.Item]
	if st.ID != 23 || t == nil || t.Type != ItemGold {
		return
	}
	p.Char.Gold = min(p.Char.Gold+int64(e.Count)*int64(st.P1)/100, maxGold)
	p.dirty = true
}
