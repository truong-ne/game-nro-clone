# Teamobi2026

Archive of the existing Java server, Unity client build, game assets, libraries and Docker setup. This is the original baseline for a possible Godot + Go rebuild.

## Godot + Go rewrite (in progress)

- `server/`: new Go game server (WebSocket + HTTP), PostgreSQL schema and migration tool that imports game templates from `database team2026.sql` and `SRC/data`.
- `client/`: new Godot 4 client.
- `docs/REWRITE_PLAN.md`: architecture, milestones, current progress, what is not ported yet, and how to run everything (Vietnamese).

Quick start: `cd server && docker compose up -d postgres && docker compose --profile tools run --rm --build migrate && docker compose up -d --build gameserver`, then open `client/` with Godot 4.4+ and press F5.

- `SRC/src`: Java server source.
- `SRC/20.jar`, `SRC/lib`, `SRC/dist`, `SRC/build`: existing binaries and build outputs.
- `SRC/data`: server game assets.
- `ModLocal`: compiled Unity Windows client. This is not a Unity Editor source project.
- `docker`: isolated local server/database environment.
- `database team2026.sql`: original database dump, including its original records.
- `SRC/sql/nro1.sql`: original older SQL dump.

## Clone

Install Git LFS before cloning, or run `git lfs install` and `git lfs pull` after cloning. Some large binaries use Git LFS.

## Docker setup on a new machine

This is a full migration snapshot requested by the owner. It includes the original SQL dumps, local Docker credentials and private NetBeans configuration. Keep the repository private if these files must not be public.

After cloning and downloading Git LFS assets, start Docker Desktop and open `docker/start.cmd`. The included `.env` and `Config.properties` match each other. `DB_INIT_SQL=./database-current.sql` in `.env` restores the exported local database snapshot on the first start. If that variable is absent, initialization uses the original `database team2026.sql`.

`docker/database-current.sql` is a logical database backup exported during this migration. The Docker volume itself is not a Git file; this SQL backup transfers its saved game data to the new machine. Automatic import runs only when the database volume is empty.

NetBeans private settings contain paths from the original machine; update them if using NetBeans on another machine. Docker does not need NetBeans.

See `docker/README.md` for starting, stopping and viewing logs. The local client connects to `127.0.0.1:14445`.
