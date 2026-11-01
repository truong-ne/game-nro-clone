package game

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"time"
)

// persistent is server-wide state saved to the server_state table. save and
// load must do their own locking; they run on the saver goroutine.
type persistent struct {
	key  string
	save func() any
	load func(json.RawMessage) error
}

var persistents []persistent

// registerPersistent is called from feature init() functions.
func registerPersistent(key string, save func() any, load func(json.RawMessage) error) {
	persistents = append(persistents, persistent{key, save, load})
}

const persistEvery = 30 * time.Second

// LoadServerState restores registered state; call before Start.
func (w *World) LoadServerState(ctx context.Context) error {
	if w.Store == nil {
		return nil
	}
	saved, err := w.Store.LoadServerState(ctx)
	if err != nil {
		return err
	}
	for _, p := range persistents {
		if raw, ok := saved[p.key]; ok {
			if err := p.load(raw); err != nil {
				slog.Warn("server state not restored", "key", p.key, "err", err)
			}
		}
	}
	return nil
}

// saveServerState writes every registered state whose JSON changed.
func (w *World) saveServerState(last map[string][]byte) {
	if w.Store == nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	for _, p := range persistents {
		b, err := json.Marshal(p.save())
		if err != nil || bytes.Equal(b, last[p.key]) {
			continue
		}
		if err := w.Store.SaveServerState(ctx, p.key, b); err != nil {
			slog.Error("save server state", "key", p.key, "err", err)
			continue
		}
		last[p.key] = b
	}
}

func (w *World) runPersist(stop <-chan struct{}) {
	last := map[string][]byte{}
	tick := time.NewTicker(persistEvery)
	defer tick.Stop()
	for {
		select {
		case <-tick.C:
			w.saveServerState(last)
		case <-stop:
			return
		}
	}
}
