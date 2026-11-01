package game

import (
	"encoding/json"
	"strconv"
	"time"
)

// Mở giới hạn sức mạnh (services/OpenPowerService.java). Quốc Vương handles
// tiers 0-5 and Tổ Sư Kaio 5-9 (QuocVuong.java:14-105, ToSuKaio.java:74-115).
// "basic" needs power >= current tier cap and finishes after a timer
// (ItemTime TIME_OPEN_POWER, ItemTime.java:207-216); "speed" costs gold.
// Commands: limit_info, limit_open {npc, mode}.

type i2LimitState struct {
	DoneAt int64 `json:"done_at"` // unix seconds; 0 = none pending
}

const i2LimitKey = "items2_limit"

func init() {
	npcOptions = append(npcOptions, func(z *Zone, p *Player, npc int) []MenuOption {
		if _, ok := i2cfg.Limit.Npcs[strconv.Itoa(npc)]; !ok {
			return nil
		}
		return []MenuOption{
			{Label: "Nâng giới hạn sức mạnh", Action: "items2_limit", Arg: itoa(npc) + ":basic"},
			{Label: "Nâng ngay " + i2Money(i2cfg.Limit.SpeedGold) + " vàng", Action: "items2_limit", Arg: itoa(npc) + ":speed"},
		}
	})
	menuActions["items2_limit"] = func(z *Zone, p *Player, arg string) {
		for i := range arg {
			if arg[i] == ':' {
				npc, _ := strconv.Atoi(arg[:i])
				z.i2OpenLimit(p, npc, arg[i+1:])
				return
			}
		}
	}
	register("limit_info", func(z *Zone, p *Player, _ json.RawMessage) { i2SendLimit(p) })
	register("limit_open", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct {
			Npc  int    `json:"npc"`
			Mode string `json:"mode"`
		}
		if json.Unmarshal(d, &r) == nil && i2NearNpc(z, p, r.Npc) {
			z.i2OpenLimit(p, r.Npc, r.Mode)
		}
	})
	secondHooks = append(secondHooks, func(z *Zone, now time.Time) {
		for _, p := range z.players {
			i2LimitTick(p, now)
		}
	})
}

func i2SendLimit(p *Player) {
	st := State[i2LimitState](p, i2LimitKey)
	next := int64(0)
	if lv := p.LimitLevel(); lv < len(powerLimits) {
		next = powerLimits[lv].Power
	}
	p.Send("limit_info", map[string]any{"limit": p.Char.Limit, "max": i2cfg.Limit.Max, "power_needed": next,
		"done_at": st.DoneAt, "speed_gold": i2cfg.Limit.SpeedGold})
}

func (z *Zone) i2OpenLimit(p *Player, npc int, mode string) {
	rng, ok := i2cfg.Limit.Npcs[strconv.Itoa(npc)]
	if !ok {
		return
	}
	cur := p.Char.Limit
	if cur >= i2cfg.Limit.Max {
		i2Notice(p, "Sức mạnh của con đã đạt tới giới hạn")
		return
	}
	if cur < rng[0] || cur >= rng[1] {
		if cur < rng[0] {
			i2Notice(p, "Sức mạnh của ngươi chưa đủ để ta giúp")
		} else {
			i2Notice(p, "Giới hạn sức mạnh của bạn đã đạt tới mức tối đa hiện tại")
		}
		return
	}
	st := State[i2LimitState](p, i2LimitKey)
	switch mode {
	case "basic": // OpenPowerService.openPowerBasic
		if st.DoneAt != 0 {
			i2Notice(p, "Đang mở giới hạn sức mạnh, hãy chờ")
			return
		}
		if p.Char.Power < powerLimits[p.LimitLevel()].Power { // NPoint.canOpenPower
			i2Notice(p, "Sức mạnh của bạn không đủ để thực hiện")
			return
		}
		st.DoneAt = time.Now().Unix() + i2cfg.Limit.BasicSeconds
		p.dirty = true
		i2Notice(p, "Ta đang truyền năng lượng, giới hạn sức mạnh sẽ mở sau "+itoa(int(i2cfg.Limit.BasicSeconds/60))+" phút")
	case "speed": // OpenPowerService.openPowerSpeed (legacy checks only the gold)
		if p.Char.Gold < i2cfg.Limit.SpeedGold {
			i2Notice(p, "Bạn không đủ vàng để mở, còn thiếu "+i2Money(i2cfg.Limit.SpeedGold-p.Char.Gold)+" vàng")
			return
		}
		p.Char.Gold -= i2cfg.Limit.SpeedGold
		p.Char.Limit++
		st.DoneAt = 0
		p.dirty = true
		i2Notice(p, "Giới hạn sức mạnh của bạn đã được tăng lên 1 bậc")
		p.Send("self", p.Self())
	default:
		return
	}
	i2SendLimit(p)
}

// i2LimitTick finishes a pending basic opening.
func i2LimitTick(p *Player, now time.Time) {
	if _, loaded := p.persist[i2LimitKey]; !loaded {
		if _, saved := p.Char.Extra.Feat[i2LimitKey]; !saved {
			return
		}
	}
	st := State[i2LimitState](p, i2LimitKey)
	if st.DoneAt == 0 || now.Unix() < st.DoneAt {
		return
	}
	st.DoneAt = 0
	p.Char.Limit = min(p.Char.Limit+1, i2cfg.Limit.Max)
	p.dirty = true
	i2Notice(p, "Giới hạn sức mạnh của bạn đã được tăng lên 1 bậc")
	i2SendLimit(p)
}
