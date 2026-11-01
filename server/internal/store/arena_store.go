package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
)

// ArenaRankRow is one fighter of the Siêu hạng ladder (legacy SuperRankBuilder).
type ArenaRankRow struct {
	ID      int64    `json:"id"`
	Name    string   `json:"name"`
	Rank    int      `json:"rank"`
	HP      int64    `json:"hp"`
	Dmg     int64    `json:"dmg"`
	Def     int64    `json:"def"`
	Win     int      `json:"win"`
	Lose    int      `json:"lose"`
	History []string `json:"history"`
}

const arenaRankCols = `character_id, name, rank, hp, dmg, def, win, lose, history`

func scanArenaRank(r pgx.Row) (ArenaRankRow, error) {
	var a ArenaRankRow
	var hist []byte
	err := r.Scan(&a.ID, &a.Name, &a.Rank, &a.HP, &a.Dmg, &a.Def, &a.Win, &a.Lose, &hist)
	if errors.Is(err, pgx.ErrNoRows) {
		return a, ErrNotFound
	}
	json.Unmarshal(hist, &a.History)
	return a, err
}

// ArenaRankEnsure refreshes the stored copy of a character, joining the
// ladder at the bottom the first time (SuperRankDAO).
func (s *Store) ArenaRankEnsure(ctx context.Context, id int64, name string, hp, dmg, def int64) (ArenaRankRow, error) {
	var out ArenaRankRow
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, `LOCK TABLE arena_super_rank IN SHARE ROW EXCLUSIVE MODE`); err != nil {
			return err
		}
		var err error
		out, err = scanArenaRank(tx.QueryRow(ctx, `INSERT INTO arena_super_rank(character_id, name, rank, hp, dmg, def)
			VALUES ($1, $2, (SELECT COALESCE(MAX(rank), 0) + 1 FROM arena_super_rank), $3, $4, $5)
			ON CONFLICT (character_id) DO UPDATE SET name=$2, hp=$3, dmg=$4, def=$5, updated_at=now()
			RETURNING `+arenaRankCols, id, name, hp, dmg, def))
		return err
	})
	return out, err
}

func (s *Store) ArenaRankGet(ctx context.Context, id int64) (ArenaRankRow, error) {
	return scanArenaRank(s.pool.QueryRow(ctx, `SELECT `+arenaRankCols+` FROM arena_super_rank WHERE character_id=$1`, id))
}

// ArenaRankRange lists ladder rows with from <= rank <= to.
func (s *Store) ArenaRankRange(ctx context.Context, from, to int) ([]ArenaRankRow, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+arenaRankCols+` FROM arena_super_rank WHERE rank BETWEEN $1 AND $2 ORDER BY rank`, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArenaRankRow
	for rows.Next() {
		r, err := scanArenaRank(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

// ArenaRankResult records a challenge; when the challenger wins against a
// better-ranked fighter the two swap ranks (SuperRank.updateResults:174-207).
// Returns the challenger's rank after the match.
func (s *Store) ArenaRankResult(ctx context.Context, challenger, defender int64, won bool) (int, error) {
	var rank int
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		a, err := scanArenaRank(tx.QueryRow(ctx, `SELECT `+arenaRankCols+` FROM arena_super_rank WHERE character_id=$1 FOR UPDATE`, challenger))
		if err != nil {
			return err
		}
		b, err := scanArenaRank(tx.QueryRow(ctx, `SELECT `+arenaRankCols+` FROM arena_super_rank WHERE character_id=$1 FOR UPDATE`, defender))
		if err != nil {
			return err
		}
		rank = a.Rank
		winner, loser := b, a
		if won {
			winner, loser = a, b
			if a.Rank > b.Rank {
				rank = b.Rank
				if _, err := tx.Exec(ctx, `UPDATE arena_super_rank SET rank = CASE character_id WHEN $1 THEN $3 ELSE $4 END
					WHERE character_id IN ($1, $2)`, a.ID, b.ID, b.Rank, a.Rank); err != nil {
					return err
				}
				winner.Rank, loser.Rank = b.Rank, a.Rank
			}
		}
		hist := func(r ArenaRankRow, line string) string {
			h := append([]string{line}, r.History...)
			if len(h) > 10 {
				h = h[:10]
			}
			b, _ := json.Marshal(h)
			return string(b)
		}
		if _, err := tx.Exec(ctx, `UPDATE arena_super_rank SET win = win + 1, history = $2 WHERE character_id=$1`,
			winner.ID, hist(winner, fmt.Sprintf("Hạ %s[%d]", loser.Name, loser.Rank))); err != nil {
			return err
		}
		_, err = tx.Exec(ctx, `UPDATE arena_super_rank SET lose = lose + 1, history = $2 WHERE character_id=$1`,
			loser.ID, hist(loser, fmt.Sprintf("Thua %s[%d]", winner.Name, winner.Rank)))
		return err
	})
	return rank, err
}

// ArenaAddBlackBall stores a black-ball buff for an offline clan member.
func (s *Store) ArenaAddBlackBall(ctx context.Context, charID int64, star int, expires int64) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO arena_blackball_reward(character_id, star, expires) VALUES ($1,$2,$3)
		ON CONFLICT (character_id, star) DO UPDATE SET expires = GREATEST(arena_blackball_reward.expires, $3)`, charID, star, expires)
	return err
}

// ArenaTakeBlackBalls returns and deletes pending buffs as [star, expires].
func (s *Store) ArenaTakeBlackBalls(ctx context.Context, charID int64) ([][2]int64, error) {
	rows, err := s.pool.Query(ctx, `DELETE FROM arena_blackball_reward WHERE character_id=$1 RETURNING star, expires`, charID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out [][2]int64
	for rows.Next() {
		var star int16
		var exp int64
		if err := rows.Scan(&star, &exp); err != nil {
			return nil, err
		}
		out = append(out, [2]int64{int64(star), exp})
	}
	return out, rows.Err()
}

// ArenaRecordDungeon keeps the clan's best result: higher level, then faster.
func (s *Store) ArenaRecordDungeon(ctx context.Context, clanID int64, dungeon string, level int, ms int64) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO arena_dungeon_record(clan_id, dungeon, level, ms) VALUES ($1,$2,$3,$4)
		ON CONFLICT (clan_id, dungeon) DO UPDATE SET level=$3, ms=$4, at=now()
		WHERE $3 > arena_dungeon_record.level OR ($3 = arena_dungeon_record.level AND $4 < arena_dungeon_record.ms)`,
		clanID, dungeon, level, ms)
	return err
}

// ArenaTopRow is one leaderboard line.
type ArenaTopRow struct {
	Rank  int    `json:"rank"`
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Value int64  `json:"value"`
	Extra string `json:"extra,omitempty"`
}

// ArenaTop computes a leaderboard. Kinds: power, gold, clan, super_rank,
// dungeon:<id> (managers/TopBanDoKhoBau.java and friends).
func (s *Store) ArenaTop(ctx context.Context, kind string, limit int) ([]ArenaTopRow, error) {
	var q string
	args := []any{limit}
	switch kind {
	case "power":
		q = `SELECT id, name, power, '' FROM character ORDER BY power DESC LIMIT $1`
	case "gold":
		q = `SELECT id, name, gold, '' FROM character ORDER BY gold DESC LIMIT $1`
	case "clan":
		q = `SELECT c.id, c.name, COALESCE(SUM(ch.power), 0)::bigint, COUNT(ch.id)::text FROM clan c
			LEFT JOIN clan_member m ON m.clan_id = c.id LEFT JOIN character ch ON ch.id = m.character_id
			GROUP BY c.id, c.name ORDER BY 3 DESC LIMIT $1`
	case "super_rank":
		q = `SELECT character_id, name, rank, win || ':' || lose FROM arena_super_rank ORDER BY rank LIMIT $1`
	default:
		if len(kind) <= 8 || kind[:8] != "dungeon:" {
			return nil, fmt.Errorf("unknown top %q", kind)
		}
		q = `SELECT c.id, c.name, r.level, r.ms::text FROM arena_dungeon_record r JOIN clan c ON c.id = r.clan_id
			WHERE r.dungeon = $2 ORDER BY r.level DESC, r.ms LIMIT $1`
		args = append(args, kind[8:])
	}
	rows, err := s.pool.Query(ctx, q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ArenaTopRow
	for rows.Next() {
		var r ArenaTopRow
		if err := rows.Scan(&r.ID, &r.Name, &r.Value, &r.Extra); err != nil {
			return nil, err
		}
		r.Rank = len(out) + 1
		out = append(out, r)
	}
	return out, rows.Err()
}
