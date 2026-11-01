package game

import (
	"context"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Item template types (item_template.type) used by game logic.
const (
	ItemShirt   = 0
	ItemPants   = 1
	ItemGloves  = 2
	ItemShoes   = 3
	ItemRadar   = 4
	ItemCostume = 5
	ItemBean    = 6  // đậu thần
	ItemFlag    = 11 // cờ/balo
	ItemGold    = 9  // vàng rơi trên đất
	ItemGem     = 10
	ItemMount   = 23
)

// equipSlot maps an equippable item type to its body slot.
var equipSlot = map[int]int{ItemShirt: 0, ItemPants: 1, ItemGloves: 2, ItemShoes: 3, ItemRadar: 4,
	ItemCostume: 5, ItemFlag: 8, ItemMount: 9}

type SkillLevel struct {
	Point        int
	PowerRequire int64
	Damage       int64
	Dx, Dy       int
	Price        int64
	MaxFight     int
	ManaUse      int64
	CoolDown     int // ms
}

type SkillTemplate struct {
	NClass, ID  int
	Name        string
	MaxPoint    int
	ManaUseType int // 0 flat, 1 % of max KI, 2 all KI
	Type        int // 1 attack, 2 buff other, 3 self, 4 special
	IconID      int
	Levels      []SkillLevel // index = point-1
}

func (s *SkillTemplate) Level(point int) *SkillLevel {
	if point < 1 || point > len(s.Levels) {
		return nil
	}
	return &s.Levels[point-1]
}

type MobTemplate struct {
	ID        int
	Type      int
	Name      string
	HP        int64
	RangeMove int
	Speed     int
	PctDmg    int64
	PctPotent int64
}

func loadMoreData(ctx context.Context, pool *pgxpool.Pool, d *Data) error {
	d.Skills = map[[2]int]*SkillTemplate{}
	d.Mobs = map[int]*MobTemplate{}
	d.ItemsFull = map[int]*ItemTemplateFull{}

	rows, _ := pool.Query(ctx, `SELECT nclass, id, name, max_point, mana_use_type, type, icon_id FROM skill_template`)
	for rows.Next() {
		s := &SkillTemplate{}
		if err := rows.Scan(&s.NClass, &s.ID, &s.Name, &s.MaxPoint, &s.ManaUseType, &s.Type, &s.IconID); err != nil {
			return err
		}
		d.Skills[[2]int{s.NClass, s.ID}] = s
	}
	rows, _ = pool.Query(ctx, `SELECT nclass, template_id, point, power_require, damage, dx, dy, price, max_fight, mana_use, cool_down
		FROM skill_level ORDER BY nclass, template_id, point`)
	for rows.Next() {
		var nc, id int
		var l SkillLevel
		if err := rows.Scan(&nc, &id, &l.Point, &l.PowerRequire, &l.Damage, &l.Dx, &l.Dy, &l.Price, &l.MaxFight, &l.ManaUse, &l.CoolDown); err != nil {
			return err
		}
		if s := d.Skills[[2]int{nc, id}]; s != nil {
			s.Levels = append(s.Levels, l)
		}
	}
	rows, _ = pool.Query(ctx, `SELECT id, type, name, hp, range_move, speed, percent_dame, percent_potential FROM mob_template`)
	for rows.Next() {
		m := &MobTemplate{}
		if err := rows.Scan(&m.ID, &m.Type, &m.Name, &m.HP, &m.RangeMove, &m.Speed, &m.PctDmg, &m.PctPotent); err != nil {
			return err
		}
		d.Mobs[m.ID] = m
	}
	rows, _ = pool.Query(ctx, `SELECT id, type, gender, name, description, level, icon_id, part, stackable, power_require, gold, gem FROM item_template`)
	for rows.Next() {
		t := &ItemTemplateFull{}
		if err := rows.Scan(&t.ID, &t.Type, &t.Gender, &t.Name, &t.Description, &t.Level, &t.IconID, &t.Part,
			&t.Stackable, &t.PowerRequire, &t.Gold, &t.Gem); err != nil {
			return err
		}
		d.ItemsFull[t.ID] = t
	}
	return rows.Err()
}

// ItemTemplateFull carries the gameplay fields of item_template.
type ItemTemplateFull struct {
	ID, Type, Gender  int
	Name, Description string
	Level, IconID     int
	Part              int
	Stackable         bool
	PowerRequire      int64
	Gold              int64
	Gem               int64
}
