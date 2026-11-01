package game

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Clans are shared across zones, so they live in the world behind a mutex.
// Player fields (ClanID) are still only changed on the player's zone goroutine.
type clanRegistry struct {
	mu    sync.Mutex
	clans map[int64]*store.Clan
	// pending invites: invitee character id -> clan id
	invites map[int64]int64
}

func (w *World) LoadClans(ctx context.Context) error {
	cs, err := w.Store.LoadClans(ctx)
	if err != nil {
		return err
	}
	w.clans = clanRegistry{clans: cs, invites: map[int64]int64{}}
	return nil
}

func (w *World) clan(id int64) *store.Clan {
	w.clans.mu.Lock()
	defer w.clans.mu.Unlock()
	return w.clans.clans[id]
}

// online returns the player with this character id, if online.
func (w *World) onlineChar(id int64) *Player {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.byChar[id]
}

func (w *World) sendClan(p *Player) {
	c := w.clan(p.Char.ClanID)
	if c == nil {
		p.Send("clan", nil)
		return
	}
	w.clans.mu.Lock()
	b := Encode("clan", c)
	w.clans.mu.Unlock()
	p.SendRaw(b)
}

func (w *World) clanBroadcast(c *store.Clan, msg []byte) {
	w.clans.mu.Lock()
	ids := make([]int64, len(c.Members))
	for i, m := range c.Members {
		ids[i] = m.ID
	}
	w.clans.mu.Unlock()
	for _, id := range ids {
		if p := w.onlineChar(id); p != nil {
			p.SendRaw(msg)
		}
	}
}

// async runs a DB operation off the zone goroutine and then f on the player's zone.
// The callback always runs, even if the player logged out meanwhile: the final
// save waits for it (Player.pending), so refunds and deliveries are never lost.
func (w *World) async(p *Player, op func(ctx context.Context) error, f func(z *Zone, err error)) {
	p.pending.Add(1)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := op(ctx)
		if err != nil && !errors.Is(err, store.ErrTaken) && !errors.Is(err, store.ErrNotFound) {
			slog.Error("db op", "player", p.ID(), "err", err)
		}
		done := func(z *Zone) {
			if z != nil {
				f(z, err)
			}
			if p.pending.Add(-1) == 0 && p.left.Load() {
				w.finalSave(p)
			}
		}
		if p.Zone() == nil {
			done(nil)
			return
		}
		w.postToPlayer(p, done)
	}()
}

func notice(p *Player, s string) { p.Send("notice", map[string]string{"text": s}) }

func init() {
	register("clan_create", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ Name string }
		if json.Unmarshal(d, &r) != nil {
			return
		}
		w := z.World
		name := strings.TrimSpace(r.Name)
		cost := w.Data.Content.Clan.CreateGold
		switch {
		case p.Char.ClanID != 0:
			notice(p, "Bạn đã có bang hội")
			return
		case utf8.RuneCountInString(name) < 3 || utf8.RuneCountInString(name) > 20:
			notice(p, "Tên bang 3-20 ký tự")
			return
		case p.Char.Gold < cost:
			notice(p, "Cần "+itoa(int(cost))+" vàng để lập bang")
			return
		}
		p.Char.Gold -= cost // refunded on failure
		var id int64
		w.async(p, func(ctx context.Context) (err error) {
			id, err = w.Store.CreateClan(ctx, name, p.ID())
			return err
		}, func(z *Zone, err error) {
			if err != nil {
				p.Char.Gold += cost
				notice(p, map[bool]string{true: "Tên bang đã tồn tại", false: "Lỗi máy chủ"}[errors.Is(err, store.ErrTaken)])
				return
			}
			w.clans.mu.Lock()
			w.clans.clans[id] = &store.Clan{ID: id, Name: name, LeaderID: p.ID(), Level: 1,
				Members: []store.ClanMember{{ID: p.ID(), Name: p.Char.Name, Role: 0, Power: p.Char.Power}}}
			w.clans.mu.Unlock()
			p.Char.ClanID = id
			p.dirty = true
			notice(p, "Lập bang "+name+" thành công")
			w.sendClan(p)
		})
	})
	register("clan_invite", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ ID int64 }
		w := z.World
		c := w.clan(p.Char.ClanID)
		if json.Unmarshal(d, &r) != nil || c == nil {
			return
		}
		t := w.onlineChar(r.ID)
		if t == nil {
			return
		}
		w.clans.mu.Lock()
		full := len(c.Members) >= w.Data.Content.Clan.MaxMembers
		if !full {
			w.clans.invites[r.ID] = c.ID
		}
		w.clans.mu.Unlock()
		if full {
			notice(p, "Bang đã đủ thành viên")
			return
		}
		t.Send("clan_invite", map[string]any{"clan": c.ID, "name": c.Name, "from": p.Char.Name})
	})
	register("clan_join", func(z *Zone, p *Player, _ json.RawMessage) {
		w := z.World
		w.clans.mu.Lock()
		cid := w.clans.invites[p.ID()]
		delete(w.clans.invites, p.ID())
		w.clans.mu.Unlock()
		c := w.clan(cid)
		if c == nil || p.Char.ClanID != 0 {
			return
		}
		w.async(p, func(ctx context.Context) error { return w.Store.AddClanMember(ctx, cid, p.ID()) },
			func(z *Zone, err error) {
				if err != nil {
					notice(p, "Không thể vào bang")
					return
				}
				w.clans.mu.Lock()
				c.Members = append(c.Members, store.ClanMember{ID: p.ID(), Name: p.Char.Name, Role: 2, Power: p.Char.Power})
				w.clans.mu.Unlock()
				p.Char.ClanID = cid
				w.clanBroadcast(c, Encode("clan_chat", map[string]string{"name": "Hệ thống", "text": p.Char.Name + " đã vào bang"}))
				w.sendClan(p)
			})
	})
	register("clan_leave", func(z *Zone, p *Player, _ json.RawMessage) { z.World.clanRemove(p, p.ID()) })
	register("clan_kick", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ ID int64 }
		c := z.World.clan(p.Char.ClanID)
		if json.Unmarshal(d, &r) == nil && c != nil && c.LeaderID == p.ID() && r.ID != p.ID() {
			z.World.clanRemove(p, r.ID)
		}
	})
	register("clan_chat", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ Text string }
		c := z.World.clan(p.Char.ClanID)
		if json.Unmarshal(d, &r) != nil || c == nil || !chatOK(p, &r.Text) {
			return
		}
		z.World.clanBroadcast(c, Encode("clan_chat", map[string]string{"name": p.Char.Name, "text": r.Text}))
	})
	register("clan_info", func(z *Zone, p *Player, _ json.RawMessage) { z.World.sendClan(p) })

	register("friend_add", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ Name string }
		w := z.World
		if json.Unmarshal(d, &r) != nil {
			return
		}
		w.async(p, func(ctx context.Context) error {
			id, err := w.Store.CharacterIDByName(ctx, r.Name)
			if err != nil {
				return err
			}
			if id == p.ID() {
				return store.ErrTaken
			}
			return w.Store.AddFriend(ctx, p.ID(), id)
		}, func(z *Zone, err error) {
			if err != nil {
				notice(p, "Không tìm thấy người chơi")
				return
			}
			notice(p, "Đã thêm bạn")
			w.sendFriends(p)
		})
	})
	register("friend_remove", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ ID int64 }
		w := z.World
		if json.Unmarshal(d, &r) == nil {
			w.async(p, func(ctx context.Context) error { return w.Store.RemoveFriend(ctx, p.ID(), r.ID) },
				func(z *Zone, err error) { w.sendFriends(p) })
		}
	})
	register("friends", func(z *Zone, p *Player, _ json.RawMessage) { z.World.sendFriends(p) })
	register("world_chat", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct{ Text string }
		if json.Unmarshal(d, &r) == nil {
			z.worldChat(p, r.Text)
		}
	})
	register("pm", func(z *Zone, p *Player, d json.RawMessage) {
		var r struct {
			ID   int64
			Text string
		}
		if json.Unmarshal(d, &r) != nil || !chatOK(p, &r.Text) {
			return
		}
		if t := z.World.onlineChar(r.ID); t != nil {
			msg := Encode("pm", map[string]any{"from": p.ID(), "name": p.Char.Name, "text": r.Text})
			t.SendRaw(msg)
			p.SendRaw(msg)
		} else {
			notice(p, "Người chơi không trực tuyến")
		}
	})
}

// clanRemove removes member id from p's clan (leave or kick). A leader
// leaving disbands the clan.
func (w *World) clanRemove(p *Player, id int64) {
	c := w.clan(p.Char.ClanID)
	if c == nil {
		return
	}
	disband := id == c.LeaderID
	w.async(p, func(ctx context.Context) error {
		if disband {
			return w.Store.DeleteClan(ctx, c.ID)
		}
		return w.Store.RemoveClanMember(ctx, c.ID, id)
	}, func(z *Zone, err error) {
		if err != nil {
			return
		}
		w.clans.mu.Lock()
		var gone []int64
		for i, m := range c.Members {
			if disband || m.ID == id {
				gone = append(gone, m.ID)
			}
			if m.ID == id && !disband {
				c.Members = append(c.Members[:i], c.Members[i+1:]...)
				break
			}
		}
		if disband {
			delete(w.clans.clans, c.ID)
		}
		w.clans.mu.Unlock()
		for _, mid := range gone {
			if mp := w.onlineChar(mid); mp != nil {
				w.postToPlayer(mp, func(*Zone) {
					mp.Char.ClanID = 0
					notice(mp, "Bạn đã rời bang "+c.Name)
					mp.Send("clan", nil)
				})
			}
		}
	})
}

func (w *World) sendFriends(p *Player) {
	var list []store.Friend
	w.async(p, func(ctx context.Context) (err error) {
		list, err = w.Store.Friends(ctx, p.ID())
		return err
	}, func(z *Zone, err error) {
		out := make([]map[string]any, 0, len(list))
		for _, f := range list {
			out = append(out, map[string]any{"id": f.ID, "name": f.Name, "power": f.Power, "online": w.onlineChar(f.ID) != nil})
		}
		p.Send("friends", out)
	})
}

// chatOK trims and rate-limits a chat line.
func chatOK(p *Player, text *string) bool {
	t := strings.TrimSpace(*text)
	if t == "" || time.Since(p.lastChat) < chatCooldown {
		return false
	}
	if utf8.RuneCountInString(t) > maxChatRunes {
		t = string([]rune(t)[:maxChatRunes])
	}
	p.lastChat = time.Now()
	*text = t
	return true
}

// worldChat follows ChatGlobalService: power, gem and cooldown requirements.
func (z *Zone) worldChat(p *Player, text string) {
	cfg := z.World.Data.Content.WorldChat
	if !p.IsAdmin {
		switch {
		case p.Char.Power < cfg.MinPower:
			notice(p, "Cần sức mạnh "+itoa(int(cfg.MinPower))+" để chat thế giới")
			return
		case p.Char.Gem < cfg.MinGems:
			notice(p, "Cần ít nhất "+itoa(int(cfg.MinGems))+" ngọc")
			return
		case time.Since(p.lastWorldChat) < time.Duration(cfg.CooldownS)*time.Second:
			notice(p, "Vui lòng chờ để chat thế giới")
			return
		}
	}
	if !chatOK(p, &text) {
		return
	}
	if !p.IsAdmin {
		p.Char.Gem -= cfg.GemCost
		p.dirty = true
	}
	p.lastWorldChat = time.Now()
	z.World.Broadcast(Encode("world_chat", map[string]any{"id": p.ID(), "name": p.Char.Name, "text": text}))
}
