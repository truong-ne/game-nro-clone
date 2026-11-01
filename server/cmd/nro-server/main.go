// Command nro-server runs the game server.
package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"syscall"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/db"
	"github.com/truong-ne/game-nro-clone/server/internal/game"
	"github.com/truong-ne/game-nro-clone/server/internal/server"
	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

func main() {
	ctx := context.Background()
	pool, err := db.Connect(ctx, envOr("DATABASE_URL", "postgres://nro:nro@localhost:55432/nro?sslmode=disable"))
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		fail(err)
	}
	start := time.Now()
	data, err := game.LoadData(ctx, pool)
	if err != nil {
		fail(err)
	}
	slog.Info("templates loaded", "maps", len(data.Maps), "items", len(data.Items), "took", time.Since(start))

	if v, err := strconv.ParseInt(os.Getenv("EXP_RATE"), 10, 64); err == nil && v > 0 {
		game.ExpRate = v
	}
	st := store.New(pool)
	world := game.NewWorld(data, st)
	if err := world.LoadClans(ctx); err != nil {
		fail(err)
	}
	if err := world.LoadServerState(ctx); err != nil {
		fail(err)
	}
	world.Start()
	srv := server.New(world, st, os.Getenv("ASSET_DIR"))
	srv.AdminToken = os.Getenv("ADMIN_TOKEN")
	httpSrv := &http.Server{Addr: envOr("LISTEN_ADDR", ":8765"), Handler: srv.Handler(), ReadHeaderTimeout: 10 * time.Second}

	go func() {
		slog.Info("listening", "addr", httpSrv.Addr)
		if err := httpSrv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			fail(err)
		}
	}()

	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
	<-sig
	slog.Info("shutting down, saving players")
	world.Shutdown()
	sctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	httpSrv.Shutdown(sctx)
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fail(err error) {
	slog.Error("nro-server failed", "err", err)
	os.Exit(1)
}
