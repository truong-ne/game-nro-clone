package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/coder/websocket"
)

func sign(v int) int {
	if v < 0 {
		return -1
	}
	return 1
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}

// runScenario plays through the gameplay loop with a GM account (is_admin):
// teleport, fight a mob to death, pick up drops, open a shop through an NPC,
// buy and equip an item, add a stat point, spawn and hit a boss.
func runScenario(url, user string) error {
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return err
	}
	defer c.CloseNow()
	c.SetReadLimit(1 << 22)
	send := func(t string, d any) {
		b, _ := json.Marshal(d)
		msg, _ := json.Marshal(env{T: t, D: b})
		c.Write(ctx, websocket.MessageText, msg)
	}
	inbox := make(chan env, 1024)
	go func() {
		for {
			_, b, err := c.Read(ctx)
			if err != nil {
				close(inbox)
				return
			}
			var e env
			if json.Unmarshal(b, &e) == nil {
				inbox <- e
			}
		}
	}()
	// wait returns the first message of type t (others go to onOther).
	var self struct {
		ID     int64 `json:"id"`
		X, Y   int
		Power  int64
		Gold   int64
		Skills []struct {
			TemplateID int `json:"template_id"`
			Level      int
		}
	}
	wait := func(t string, d time.Duration) (env, error) {
		deadline := time.After(d)
		for {
			select {
			case e, ok := <-inbox:
				if !ok {
					return env{}, fmt.Errorf("connection closed waiting for %s", t)
				}
				if e.T == "self" {
					json.Unmarshal(e.D, &self)
				}
				if e.T == "error" {
					slog.Warn("server error", "d", string(e.D))
				}
				if e.T == t {
					return e, nil
				}
			case <-deadline:
				return env{}, fmt.Errorf("timeout waiting for %s", t)
			}
		}
	}
	step := func(name string, err error) error {
		if err != nil {
			slog.Error("FAIL", "step", name, "err", err)
			return fmt.Errorf("%s: %w", name, err)
		}
		slog.Info("PASS", "step", name)
		return nil
	}

	send("login", map[string]string{"username": user, "password": "secret123"})
	if _, err := wait("map", 10*time.Second); step("login", err) != nil {
		return err
	}
	send("chat", map[string]string{"text": "/learn"})
	send("chat", map[string]string{"text": "/map 0"})
	e, err := wait("map", 10*time.Second)
	if step("teleport to map 0", err) != nil {
		return err
	}
	var m struct {
		X, Y int
		Mobs []struct {
			I, X, Y int
			HP      int64 `json:"hp"`
		}
	}
	json.Unmarshal(e.D, &m)
	if len(m.Mobs) == 0 {
		return step("mobs present", fmt.Errorf("no mobs on map 0"))
	}
	target := m.Mobs[0]
	x, y := m.X, m.Y
	walk := func(tx, ty int) {
		for x != tx || y != ty {
			x += max(min(tx-x, 50), -50)
			y += max(min(ty-y, 50), -50)
			send("move", map[string]int{"x": x, "y": y, "d": 1, "a": 1})
			time.Sleep(110 * time.Millisecond)
		}
	}
	walk(target.X-30, target.Y)
	skill := -1
	for _, s := range self.Skills {
		if s.Level > 0 {
			skill = s.TemplateID
			break
		}
	}
	power := self.Power
	killed := false
	for i := 0; i < 60 && !killed; i++ {
		send("skill", map[string]any{"id": skill, "mob": target.I})
		deadline := time.After(700 * time.Millisecond)
	drain:
		for {
			select {
			case ev := <-inbox:
				if ev.T == "self" {
					json.Unmarshal(ev.D, &self)
				}
				if ev.T == "exp" {
					var x struct{ Power int64 }
					json.Unmarshal(ev.D, &x)
					self.Power = x.Power
				}
				if ev.T == "mob_die" {
					var d struct{ I int }
					json.Unmarshal(ev.D, &d)
					killed = killed || d.I == target.I
				}
				if ev.T == "item_drop" {
					var g struct{ ID, X, Y int }
					json.Unmarshal(ev.D, &g)
					defer func(id, gx, gy int) { walk(gx, gy); send("pickup", map[string]int{"id": id}) }(g.ID, g.X, g.Y)
				}
			case <-deadline:
				break drain
			}
		}
	}
	if step("kill mob", map[bool]error{true: nil, false: fmt.Errorf("mob %d not killed", target.I)}[killed]) != nil {
		return fmt.Errorf("combat failed")
	}
	if step("gain power", map[bool]error{true: nil, false: fmt.Errorf("power %d -> %d", power, self.Power)}[self.Power > power]) != nil {
		return fmt.Errorf("no power")
	}
	send("chat", map[string]string{"text": "/gold 100000"})
	// Bunma (npc 7) sells clothes on Làng Aru; walk to it and open its shop.
	walk(228, 432) // npc index 0 on Làng Aru is Bunma
	send("npc", map[string]int{"i": 0})
	e, err = wait("menu", 5*time.Second)
	if step("npc menu", err) != nil {
		return err
	}
	var menu struct{ Options []string }
	json.Unmarshal(e.D, &menu)
	shopIdx := -1
	for i, o := range menu.Options {
		if strings.HasPrefix(o, "Cửa hàng") {
			shopIdx = i
			break
		}
	}
	if shopIdx < 0 {
		return step("shop option", fmt.Errorf("menu has no shop: %v", menu.Options))
	}
	send("menu", map[string]int{"i": shopIdx})
	e, err = wait("shop", 5*time.Second)
	if step("open shop", err) != nil {
		return err
	}
	var shop struct {
		Tabs []struct {
			Items []struct {
				ID, TID  int
				Currency int
				Cost     int64
			}
		}
	}
	json.Unmarshal(e.D, &shop)
	buy := -1
	for _, t := range shop.Tabs {
		for _, it := range t.Items {
			if it.Currency == 0 && it.Cost <= 50000 && buy < 0 {
				buy = it.ID
			}
		}
	}
	send("shop_buy", map[string]int{"id": buy, "qty": 1})
	if _, err = wait("inventory", 5*time.Second); step("buy item", err) != nil {
		return err
	}
	send("chat", map[string]string{"text": "/power 2000000000"})
	wait("self", 5*time.Second)
	send("add_point", map[string]int{"stat": 0, "n": 1000}) // +20000 HP to survive the boss
	if _, err := wait("self", 5*time.Second); step("add stat point", err) != nil {
		return err
	}
	send("chat", map[string]string{"text": "/boss nappa"})
	e, err = wait("boss_spawn", 5*time.Second)
	if step("spawn boss", err) != nil {
		return err
	}
	var b struct {
		Key  string
		X, Y int
	}
	json.Unmarshal(e.D, &b)
	// Starter beans sit in bag slot 0: eat one to refill KI spent on the mob.
	send("use", map[string]int{"slot": 0})
	if _, err := wait("hpmp", 5*time.Second); step("eat bean", err) != nil {
		return err
	}
	// The boss chases us; hit it as soon as it is in skill range (it must get
	// within 70px to hit back, our skill reaches ~140px).
	walk(b.X-sign(b.X-x)*120, b.Y)
	send("skill", map[string]any{"id": skill, "boss": b.Key})
	hit := false
	deadline := time.After(20 * time.Second)
	for !hit {
		select {
		case ev, ok := <-inbox:
			if !ok {
				return step("hit boss", fmt.Errorf("connection closed"))
			}
			switch ev.T {
			case "boss_move":
				json.Unmarshal(ev.D, &b)
				if abs(b.X-x) <= 130 && abs(b.Y-y) <= 130 {
					send("skill", map[string]any{"id": skill, "boss": b.Key})
				}
			case "boss_hit":
				hit = true
			case "die":
				var dd struct{ ID int64 }
				json.Unmarshal(ev.D, &dd)
				if dd.ID == self.ID {
					return step("hit boss", fmt.Errorf("bot died before hitting"))
				}
			}
		case <-deadline:
			return step("hit boss", fmt.Errorf("boss never came in range (boss %d,%d bot %d,%d)", b.X, b.Y, x, y))
		}
	}
	step("hit boss", nil)
	send("task", nil)
	if _, err := wait("task", 5*time.Second); step("task info", err) != nil {
		return err
	}
	// Each feature module answers its info request. (Crafting, lucky wheel and
	// consignment require standing next to their NPC, so they are not listed.)
	for _, c := range []struct {
		cmd  string
		d    any
		want string
	}{
		{"side_info", nil, "side_task"},
		{"buff_timers", nil, "buff_timers"},
		{"buff_intrinsic", nil, "buff_intrinsic"},
		{"top_get", map[string]string{"kind": "power"}, "top_list"},
		{"mini_info", nil, "mini_state"},
		{"rank_info", nil, "rank_info"},
		{"ach_list", nil, "achievements"},
		{"tour_info", nil, "tour_info"},
	} {
		send(c.cmd, c.d)
		if _, err := wait(c.want, 5*time.Second); step(c.cmd, err) != nil {
			return err
		}
	}
	c.Close(websocket.StatusNormalClosure, "")
	return nil
}
