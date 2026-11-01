package store

import (
	"context"
	"encoding/json"
	"errors"

	"github.com/jackc/pgx/v5"
)

// Extra is per-character state stored in character.extra.
type MagicTree struct {
	Level  int   `json:"level"`
	Peas   int   `json:"peas"`
	GrowAt int64 `json:"grow_at"` // unix seconds the next pea is ready
}

type Extra struct {
	MagicTree    MagicTree       `json:"magic_tree"`
	Pet          *PetData        `json:"pet,omitempty"`
	Achievements map[string]int  `json:"achievements,omitempty"` // progress
	AchDone      map[string]bool `json:"ach_done,omitempty"`
	TaskProgress int             `json:"task_progress,omitempty"`
	// BagExtra is how many bag slots were unlocked beyond the base size.
	BagExtra int `json:"bag_extra,omitempty"`
	// Feat holds per-feature state (buffs, side tasks, ranks...) keyed by feature.
	Feat map[string]json.RawMessage `json:"feat,omitempty"`
}

type PetData struct {
	Name    string `json:"name"`
	Gender  int    `json:"gender"`
	Power   int64  `json:"power"`
	HPBase  int64  `json:"hp_base"`
	MPBase  int64  `json:"mp_base"`
	DmgBase int64  `json:"dmg_base"`
	Status  int    `json:"status"` // 0 follow, 1 protect, 2 attack, 3 home
}

type Clan struct {
	ID       int64        `json:"id"`
	Name     string       `json:"name"`
	Slogan   string       `json:"slogan"`
	LeaderID int64        `json:"leader_id"`
	Level    int          `json:"level"`
	Points   int64        `json:"points"`
	Members  []ClanMember `json:"members"`
}

type ClanMember struct {
	ID      int64  `json:"id"`
	Name    string `json:"name"`
	Role    int    `json:"role"`
	Donated int64  `json:"donated"`
	Power   int64  `json:"power"`
}

func (s *Store) CreateClan(ctx context.Context, name string, leader int64) (int64, error) {
	var id int64
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := tx.QueryRow(ctx, `INSERT INTO clan(name, leader_id) VALUES ($1,$2) RETURNING id`, name, leader).Scan(&id); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO clan_member(clan_id, character_id, role) VALUES ($1,$2,0)`, id, leader)
		return err
	})
	if isUnique(err) {
		return 0, ErrTaken
	}
	return id, err
}

func (s *Store) LoadClans(ctx context.Context) (map[int64]*Clan, error) {
	out := map[int64]*Clan{}
	rows, _ := s.pool.Query(ctx, `SELECT id, name, slogan, leader_id, level, points FROM clan`)
	for rows.Next() {
		c := &Clan{}
		if err := rows.Scan(&c.ID, &c.Name, &c.Slogan, &c.LeaderID, &c.Level, &c.Points); err != nil {
			return nil, err
		}
		out[c.ID] = c
	}
	rows, _ = s.pool.Query(ctx, `SELECT m.clan_id, m.character_id, c.name, m.role, m.donated, c.power
		FROM clan_member m JOIN character c ON c.id = m.character_id ORDER BY m.role, m.joined_at`)
	for rows.Next() {
		var cid int64
		var m ClanMember
		if err := rows.Scan(&cid, &m.ID, &m.Name, &m.Role, &m.Donated, &m.Power); err != nil {
			return nil, err
		}
		if c := out[cid]; c != nil {
			c.Members = append(c.Members, m)
		}
	}
	return out, rows.Err()
}

func (s *Store) AddClanMember(ctx context.Context, clan, char int64) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO clan_member(clan_id, character_id) VALUES ($1,$2)`, clan, char)
	if isUnique(err) {
		return ErrTaken
	}
	return err
}

func (s *Store) RemoveClanMember(ctx context.Context, clan, char int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM clan_member WHERE clan_id=$1 AND character_id=$2`, clan, char)
	return err
}

func (s *Store) DeleteClan(ctx context.Context, clan int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM clan WHERE id=$1`, clan)
	return err
}

func (s *Store) AddFriend(ctx context.Context, a, b int64) error {
	_, err := s.pool.Exec(ctx, `INSERT INTO friend(character_id, friend_id) VALUES ($1,$2) ON CONFLICT DO NOTHING`, a, b)
	return err
}

func (s *Store) RemoveFriend(ctx context.Context, a, b int64) error {
	_, err := s.pool.Exec(ctx, `DELETE FROM friend WHERE character_id=$1 AND friend_id=$2`, a, b)
	return err
}

type Friend struct {
	ID    int64  `json:"id"`
	Name  string `json:"name"`
	Power int64  `json:"power"`
}

func (s *Store) Friends(ctx context.Context, id int64) ([]Friend, error) {
	rows, _ := s.pool.Query(ctx, `SELECT c.id, c.name, c.power FROM friend f JOIN character c ON c.id=f.friend_id WHERE f.character_id=$1`, id)
	return pgx.CollectRows(rows, func(r pgx.CollectableRow) (Friend, error) {
		var f Friend
		err := r.Scan(&f.ID, &f.Name, &f.Power)
		return f, err
	})
}

func (s *Store) CharacterIDByName(ctx context.Context, name string) (int64, error) {
	var id int64
	err := s.pool.QueryRow(ctx, `SELECT id FROM character WHERE lower(name)=lower($1)`, name).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNotFound
	}
	return id, err
}

type GiftReward struct {
	Item    int      `json:"item"`
	Qty     int      `json:"qty"`
	Options [][2]int `json:"options"`
}

var ErrUsed = errors.New("already used")

// RedeemGiftcode atomically records one use and returns the rewards.
func (s *Store) RedeemGiftcode(ctx context.Context, code string, char int64) ([]GiftReward, error) {
	var rewards []GiftReward
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		var raw []byte
		err := tx.QueryRow(ctx, `UPDATE giftcode SET used = used + 1
			WHERE code=$1 AND used < max_uses AND (expires_at IS NULL OR expires_at > now()) RETURNING rewards`, code).Scan(&raw)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `INSERT INTO giftcode_use(code, character_id) VALUES ($1,$2)`, code, char); err != nil {
			if isUnique(err) {
				return ErrUsed
			}
			return err
		}
		return json.Unmarshal(raw, &rewards)
	})
	return rewards, err
}

func (s *Store) CreateGiftcode(ctx context.Context, code string, rewards []GiftReward, maxUses int) error {
	b, _ := json.Marshal(rewards)
	_, err := s.pool.Exec(ctx, `INSERT INTO giftcode(code, rewards, max_uses) VALUES ($1,$2,$3)`, code, b, maxUses)
	return err
}

func (s *Store) Audit(ctx context.Context, actor, action string, detail any) {
	b, _ := json.Marshal(detail)
	s.pool.Exec(ctx, `INSERT INTO audit_log(actor, action, detail) VALUES ($1,$2,$3)`, actor, action, b)
}

func (s *Store) SetBanned(ctx context.Context, username string, banned bool) error {
	tag, err := s.pool.Exec(ctx, `UPDATE account SET banned=$2 WHERE username=$1`, username, banned)
	if err == nil && tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return err
}
