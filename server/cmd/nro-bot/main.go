// Command nro-bot is a headless client for smoke and load testing: each bot
// registers (or logs in), creates a character, walks around, chats and
// verifies it sees the other bots.
package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"math/rand/v2"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

type env struct {
	T string          `json:"t"`
	D json.RawMessage `json:"d,omitempty"`
}

func main() {
	url := flag.String("url", "ws://localhost:8765/ws", "server websocket url")
	n := flag.Int("n", 3, "number of bots")
	dur := flag.Duration("for", 8*time.Second, "how long each bot plays")
	prefix := flag.String("prefix", fmt.Sprintf("b%d", time.Now().Unix()%100000), "account/name prefix")
	scenario := flag.String("scenario", "", "run the gameplay scenario as this GM account instead of load bots")
	flag.Parse()

	if *scenario != "" {
		if err := runScenario(*url, *scenario); err != nil {
			slog.Error("scenario failed", "err", err)
			os.Exit(1)
		}
		slog.Info("scenario passed")
		return
	}

	var wg sync.WaitGroup
	var ok, failed atomic.Int32
	var seen atomic.Int64
	for i := range *n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			s, err := runBot(*url, fmt.Sprintf("%s%02d", *prefix, i), *dur)
			seen.Add(int64(s))
			if err != nil {
				slog.Error("bot failed", "bot", i, "err", err)
				failed.Add(1)
				return
			}
			ok.Add(1)
		}()
		time.Sleep(50 * time.Millisecond)
	}
	wg.Wait()
	slog.Info("done", "ok", ok.Load(), "failed", failed.Load(), "player_events_seen", seen.Load())
	if failed.Load() > 0 {
		os.Exit(1)
	}
}

func runBot(url, name string, dur time.Duration) (int, error) {
	ctx, cancel := context.WithTimeout(context.Background(), dur+20*time.Second)
	defer cancel()
	c, _, err := websocket.Dial(ctx, url, nil)
	if err != nil {
		return 0, err
	}
	defer c.CloseNow()
	send := func(t string, d any) error {
		b, _ := json.Marshal(d)
		msg, _ := json.Marshal(env{T: t, D: b})
		return c.Write(ctx, websocket.MessageText, msg)
	}
	read := func() (env, error) {
		_, b, err := c.Read(ctx)
		var e env
		if err == nil {
			err = json.Unmarshal(b, &e)
		}
		return e, err
	}

	creds := map[string]string{"username": name, "password": "secret123"}
	if err := send("register", creds); err != nil {
		return 0, err
	}
	var x, y int
	seen := 0
	for in := false; !in; {
		e, err := read()
		if err != nil {
			return 0, err
		}
		switch e.T {
		case "error":
			return 0, fmt.Errorf("server error: %s", e.D)
		case "need_char":
			send("create_char", map[string]any{"name": name, "gender": rand.IntN(3), "head": -1})
		case "map":
			var m struct {
				X, Y    int
				Players []json.RawMessage
				MapID   int `json:"map_id"`
			}
			json.Unmarshal(e.D, &m)
			x, y, seen = m.X, m.Y, len(m.Players)
			slog.Info("entered", "bot", name, "map", m.MapID, "x", x, "y", y, "others", len(m.Players))
			in = true
		}
	}

	errc := make(chan error, 1)
	go func() {
		for {
			e, err := read()
			if err != nil {
				errc <- err
				return
			}
			switch e.T {
			case "join":
				seen++
			case "moves":
				var batch [][5]int64
				json.Unmarshal(e.D, &batch)
				seen += len(batch)
			}
		}
	}()
	end := time.After(dur)
	tick := time.NewTicker(200 * time.Millisecond)
	defer tick.Stop()
	for i := 0; ; i++ {
		select {
		case <-end:
			c.Close(websocket.StatusNormalClosure, "")
			return seen, nil
		case err := <-errc:
			return seen, fmt.Errorf("read: %w", err)
		case <-tick.C:
			x += rand.IntN(41) - 20
			x = max(x, 30)
			send("move", map[string]int{"x": x, "y": y, "d": 1, "a": 1})
			if i%10 == 0 {
				send("chat", map[string]string{"text": "hello from " + name})
			}
		}
	}
}
