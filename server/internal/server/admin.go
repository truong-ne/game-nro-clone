package server

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"runtime"
	"strings"
	"sync/atomic"

	"github.com/truong-ne/game-nro-clone/server/internal/game"
	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Counters exported on /metrics.
var (
	metricConns     atomic.Int64
	metricMsgsIn    atomic.Int64
	metricMsgsOut   atomic.Int64
	metricDropped   atomic.Int64
	metricLoginFail atomic.Int64
)

func (s *Server) serveMetrics(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/plain; version=0.0.4")
	fmt.Fprintf(w, "nro_online_players %d\n", s.World.Online())
	fmt.Fprintf(w, "nro_active_zones %d\n", s.World.ActiveZones())
	fmt.Fprintf(w, "nro_ws_connections %d\n", metricConns.Load())
	fmt.Fprintf(w, "nro_messages_in_total %d\n", metricMsgsIn.Load())
	fmt.Fprintf(w, "nro_messages_out_total %d\n", metricMsgsOut.Load())
	fmt.Fprintf(w, "nro_slow_clients_dropped_total %d\n", metricDropped.Load())
	fmt.Fprintf(w, "nro_login_failures_total %d\n", metricLoginFail.Load())
	fmt.Fprintf(w, "go_goroutines %d\n", runtime.NumGoroutine())
	var ms runtime.MemStats
	runtime.ReadMemStats(&ms)
	fmt.Fprintf(w, "go_heap_alloc_bytes %d\n", ms.HeapAlloc)
}

// admin wraps handlers with bearer-token auth. With no ADMIN_TOKEN the admin
// API is disabled entirely.
func (s *Server) admin(h func(w http.ResponseWriter, r *http.Request) (any, error)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tok := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.AdminToken == "" || subtle.ConstantTimeCompare([]byte(tok), []byte(s.AdminToken)) != 1 {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		out, err := h(w, r)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		s.Store.Audit(r.Context(), "admin-api", r.Method+" "+r.URL.Path, out)
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(out)
	}
}

func decode(r *http.Request, v any) error {
	return json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<16)).Decode(v)
}

func (s *Server) adminRoutes(mux *http.ServeMux) {
	mux.HandleFunc("GET /admin/stats", s.admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		return map[string]any{"online": s.World.Online(), "active_zones": s.World.ActiveZones(),
			"goroutines": runtime.NumGoroutine()}, nil
	}))
	mux.HandleFunc("POST /admin/broadcast", s.admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct{ Text string }
		if err := decode(r, &req); err != nil || req.Text == "" {
			return nil, fmt.Errorf("text required")
		}
		s.World.Broadcast(game.Encode("notice", map[string]string{"text": req.Text}))
		return req, nil
	}))
	mux.HandleFunc("POST /admin/kick", s.admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct{ Name string }
		if err := decode(r, &req); err != nil {
			return nil, err
		}
		return map[string]any{"name": req.Name, "kicked": s.World.KickByName(req.Name)}, nil
	}))
	mux.HandleFunc("POST /admin/ban", s.admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			Username string
			Banned   bool
		}
		if err := decode(r, &req); err != nil {
			return nil, err
		}
		return req, s.Store.SetBanned(r.Context(), req.Username, req.Banned)
	}))
	mux.HandleFunc("POST /admin/giftcode", s.admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			Code    string
			Rewards []store.GiftReward
			MaxUses int `json:"max_uses"`
		}
		if err := decode(r, &req); err != nil || req.Code == "" || len(req.Rewards) == 0 {
			return nil, fmt.Errorf("code and rewards required")
		}
		return req, s.Store.CreateGiftcode(r.Context(), req.Code, req.Rewards, max(req.MaxUses, 1))
	}))
	mux.HandleFunc("POST /admin/give", s.admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct {
			Name string
			Item int
			Qty  int
		}
		if err := decode(r, &req); err != nil {
			return nil, err
		}
		if !s.World.GiveByName(req.Name, req.Item, max(req.Qty, 1)) {
			return nil, fmt.Errorf("player not online")
		}
		return req, nil
	}))
	mux.HandleFunc("POST /admin/boss", s.admin(func(w http.ResponseWriter, r *http.Request) (any, error) {
		var req struct{ ID string }
		if err := decode(r, &req); err != nil {
			return nil, err
		}
		return req, s.World.SpawnBossByID(req.ID)
	}))
}
