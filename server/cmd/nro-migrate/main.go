// Command nro-migrate applies the Postgres schema and imports game templates
// from the legacy MariaDB dump and legacy data files.
package main

import (
	"context"
	"flag"
	"log/slog"
	"os"

	"github.com/truong-ne/game-nro-clone/server/internal/db"
	"github.com/truong-ne/game-nro-clone/server/internal/importer"
)

func main() {
	dsn := flag.String("db", envOr("DATABASE_URL", "postgres://nro:nro@localhost:5432/nro?sslmode=disable"), "postgres url")
	dump := flag.String("dump", "../database team2026.sql", "legacy MariaDB dump")
	data := flag.String("data", "../SRC/data", "legacy data directory")
	schemaOnly := flag.Bool("schema-only", false, "only apply migrations")
	flag.Parse()

	ctx := context.Background()
	pool, err := db.Connect(ctx, *dsn)
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	if err := db.Migrate(ctx, pool); err != nil {
		fail(err)
	}
	slog.Info("schema up to date")
	if *schemaOnly {
		return
	}
	if err := importer.Run(ctx, pool, *dump, *data); err != nil {
		fail(err)
	}
	slog.Info("template import done")
}

func envOr(k, def string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return def
}

func fail(err error) {
	slog.Error("nro-migrate failed", "err", err)
	os.Exit(1)
}
