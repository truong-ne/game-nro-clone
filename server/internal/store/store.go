// Package store persists accounts and characters in Postgres.
package store

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNotFound = errors.New("not found")
	ErrTaken    = errors.New("already taken")
	ErrBadLogin = errors.New("wrong username or password")
	ErrBanned   = errors.New("account banned")
)

// Item containers.
const (
	ContainerBody = 0
	ContainerBag  = 1
	ContainerBox  = 2
)

type Account struct {
	ID       int64
	Username string
	IsAdmin  bool
}

type Item struct {
	Container  int      `json:"container"`
	Slot       int      `json:"slot"`
	TemplateID int      `json:"template_id"`
	Quantity   int      `json:"quantity"`
	Options    [][2]int `json:"options"` // [optionId, param]
}

type Skill struct {
	TemplateID int  `json:"template_id"`
	Level      int  `json:"level"`
	Shortcut   *int `json:"shortcut,omitempty"`
}

type Character struct {
	ID        int64
	AccountID int64
	Name      string
	Gender    int
	Head      int
	MapID     int
	X, Y      int
	Power     int64
	Potential int64
	HPBase    int64
	MPBase    int64
	DmgBase   int64
	DefBase   int64
	CritBase  int
	Stamina   int
	HP, MP    int64
	Gold      int64
	Gem       int64
	Ruby      int64
	TaskID    int
	TaskIndex int
	TaskCount int
	Limit     int // opened power-limit tier
	Extra     Extra
	ClanID    int64 // derived from clan_member, not saved here
	Items     []Item
	Skills    []Skill
}

var dummyHash, _ = bcrypt.GenerateFromPassword([]byte("timing-equalizer"), bcrypt.DefaultCost)

type Store struct{ pool *pgxpool.Pool }

func New(pool *pgxpool.Pool) *Store { return &Store{pool} }

func isUnique(err error) bool {
	var pe *pgconn.PgError
	return errors.As(err, &pe) && pe.Code == "23505"
}

func (s *Store) Register(ctx context.Context, username, password string) (*Account, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return nil, err
	}
	a := &Account{Username: username}
	err = s.pool.QueryRow(ctx, `INSERT INTO account(username, password_hash) VALUES ($1, $2) RETURNING id`,
		username, string(hash)).Scan(&a.ID)
	if isUnique(err) {
		return nil, ErrTaken
	}
	return a, err
}

func (s *Store) Login(ctx context.Context, username, password, ip string) (*Account, error) {
	a := &Account{Username: username}
	var hash string
	var banned bool
	err := s.pool.QueryRow(ctx, `SELECT id, password_hash, is_admin, banned FROM account WHERE username=$1`, username).
		Scan(&a.ID, &hash, &a.IsAdmin, &banned)
	if errors.Is(err, pgx.ErrNoRows) {
		// Burn similar time as a real check to avoid username probing by timing.
		bcrypt.CompareHashAndPassword(dummyHash, []byte(password))
		return nil, ErrBadLogin
	}
	if err != nil {
		return nil, err
	}
	if bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) != nil {
		return nil, ErrBadLogin
	}
	if banned {
		return nil, ErrBanned
	}
	_, err = s.pool.Exec(ctx, `UPDATE account SET last_login_at=now(), last_ip=$2 WHERE id=$1`, a.ID, ip)
	return a, err
}

const charCols = `id, account_id, name, gender, head, map_id, x, y, power, potential, hp_base, mp_base,
	dmg_base, def_base, crit_base, stamina, hp, mp, gold, gem, ruby, task_id, task_index, task_count,
	limit_level, extra, COALESCE((SELECT clan_id FROM clan_member WHERE character_id = character.id), 0)`

func (s *Store) LoadCharacter(ctx context.Context, accountID int64) (*Character, error) {
	c := &Character{}
	err := s.pool.QueryRow(ctx, `SELECT `+charCols+` FROM character WHERE account_id=$1`, accountID).Scan(
		&c.ID, &c.AccountID, &c.Name, &c.Gender, &c.Head, &c.MapID, &c.X, &c.Y, &c.Power, &c.Potential,
		&c.HPBase, &c.MPBase, &c.DmgBase, &c.DefBase, &c.CritBase, &c.Stamina, &c.HP, &c.MP,
		&c.Gold, &c.Gem, &c.Ruby, &c.TaskID, &c.TaskIndex, &c.TaskCount, &c.Limit, &c.Extra, &c.ClanID)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNotFound
	}
	if err != nil {
		return nil, err
	}
	rows, _ := s.pool.Query(ctx, `SELECT container, slot, template_id, quantity, options FROM character_item WHERE character_id=$1 ORDER BY container, slot`, c.ID)
	for rows.Next() {
		var it Item
		var opts []byte
		if err := rows.Scan(&it.Container, &it.Slot, &it.TemplateID, &it.Quantity, &opts); err != nil {
			return nil, err
		}
		json.Unmarshal(opts, &it.Options)
		c.Items = append(c.Items, it)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	rows, _ = s.pool.Query(ctx, `SELECT template_id, level, shortcut FROM character_skill WHERE character_id=$1 ORDER BY template_id`, c.ID)
	for rows.Next() {
		var sk Skill
		if err := rows.Scan(&sk.TemplateID, &sk.Level, &sk.Shortcut); err != nil {
			return nil, err
		}
		c.Skills = append(c.Skills, sk)
	}
	return c, rows.Err()
}

// CreateCharacter inserts c (ID is filled in) with its items and skills.
func (s *Store) CreateCharacter(ctx context.Context, c *Character) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		err := tx.QueryRow(ctx, `INSERT INTO character(account_id, name, gender, head, map_id, x, y, power, potential,
			hp_base, mp_base, dmg_base, def_base, crit_base, stamina, hp, mp, gold, gem, ruby)
			VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17,$18,$19,$20) RETURNING id`,
			c.AccountID, c.Name, c.Gender, c.Head, c.MapID, c.X, c.Y, c.Power, c.Potential, c.HPBase, c.MPBase,
			c.DmgBase, c.DefBase, c.CritBase, c.Stamina, c.HP, c.MP, c.Gold, c.Gem, c.Ruby).Scan(&c.ID)
		if isUnique(err) {
			return ErrTaken
		}
		if err != nil {
			return err
		}
		return writeChildren(ctx, tx, c)
	})
}

// SaveCharacter writes the full character state in one transaction.
func (s *Store) SaveCharacter(ctx context.Context, c *Character) error {
	return pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		_, err := tx.Exec(ctx, `UPDATE character SET head=$2, map_id=$3, x=$4, y=$5, power=$6, potential=$7,
			hp_base=$8, mp_base=$9, dmg_base=$10, def_base=$11, crit_base=$12, stamina=$13, hp=$14, mp=$15,
			gold=$16, gem=$17, ruby=$18, task_id=$19, task_index=$20, task_count=$21, updated_at=$22,
			limit_level=$23, extra=$24 WHERE id=$1`,
			c.ID, c.Head, c.MapID, c.X, c.Y, c.Power, c.Potential, c.HPBase, c.MPBase, c.DmgBase, c.DefBase,
			c.CritBase, c.Stamina, c.HP, c.MP, c.Gold, c.Gem, c.Ruby, c.TaskID, c.TaskIndex, c.TaskCount, time.Now(),
			c.Limit, c.Extra)
		if err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM character_item WHERE character_id=$1`, c.ID); err != nil {
			return err
		}
		if _, err := tx.Exec(ctx, `DELETE FROM character_skill WHERE character_id=$1`, c.ID); err != nil {
			return err
		}
		return writeChildren(ctx, tx, c)
	})
}

func writeChildren(ctx context.Context, tx pgx.Tx, c *Character) error {
	items := make([][]any, 0, len(c.Items))
	for _, it := range c.Items {
		opts, _ := json.Marshal(it.Options)
		if it.Options == nil {
			opts = []byte("[]")
		}
		items = append(items, []any{c.ID, it.Container, it.Slot, it.TemplateID, it.Quantity, string(opts)})
	}
	if _, err := tx.CopyFrom(ctx, pgx.Identifier{"character_item"},
		[]string{"character_id", "container", "slot", "template_id", "quantity", "options"}, pgx.CopyFromRows(items)); err != nil {
		return err
	}
	skills := make([][]any, 0, len(c.Skills))
	for _, sk := range c.Skills {
		skills = append(skills, []any{c.ID, sk.TemplateID, sk.Level, sk.Shortcut})
	}
	_, err := tx.CopyFrom(ctx, pgx.Identifier{"character_skill"},
		[]string{"character_id", "template_id", "level", "shortcut"}, pgx.CopyFromRows(skills))
	return err
}
