package game

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"os"
	"regexp"
	"strconv"
	"strings"
)

//go:embed content.json
var embeddedContent []byte

type DropRule struct {
	Item     int     `json:"item"`
	Chance   float64 `json:"chance"` // percent
	Min      int     `json:"min"`
	Max      int     `json:"max"`
	PerLevel int     `json:"per_level"`
}

type TaskItemDrop struct {
	Task, Sub int
	Item      int
	Chance    float64
	Mobs      []int
}

type MenuOption struct {
	Label  string `json:"label"`
	Action string `json:"action"`
	Arg    string `json:"arg,omitempty"`
}

type NpcContent struct {
	Greeting string       `json:"greeting"`
	Task     bool         `json:"task"`
	Options  []MenuOption `json:"options"`
}

// SkillBook teaches the skill named Name at Level; Gender 3 fits any race.
type SkillBook struct {
	Name   string
	Gender int
	Level  int
}

type Reward struct {
	Item   int     `json:"item"`
	Min    int     `json:"min"`
	Max    int     `json:"max"`
	Chance float64 `json:"chance"`
}

type Content struct {
	Drops struct {
		Default   []DropRule            `json:"default"`
		Mobs      map[string][]DropRule `json:"mobs"`
		Maps      map[string][]DropRule `json:"maps"`
		TaskItems []struct {
			Task   int     `json:"task"`
			Sub    int     `json:"sub"`
			Item   int     `json:"item"`
			Chance float64 `json:"chance"`
			Mobs   []int   `json:"mobs"`
		} `json:"task_items"`
	} `json:"drops"`
	GrandpaNpc []int `json:"grandpa_npc"`
	GrandpaMap []int `json:"grandpa_map"`
	Tasks      struct {
		GenderVariants map[string][]int        `json:"gender_variants"`
		Overrides      map[string]TaskOverride `json:"overrides"`
		RewardGold     int64                   `json:"reward_gold_per_task"`
		RewardPower    int64                   `json:"reward_power_per_task"`
	} `json:"tasks"`
	Npcs      map[string]NpcContent `json:"npcs"`
	MagicTree struct {
		BeanByLevel []int   `json:"bean_by_level"`
		MaxPeas     []int   `json:"max_peas_by_level"`
		GrowSeconds []int   `json:"grow_seconds_by_level"`
		UpgradeGold []int64 `json:"upgrade_gold_by_level"`
	} `json:"magic_tree"`
	Pets struct {
		Name  string   `json:"name"`
		Parts [][3]int `json:"parts"`
		Base  struct {
			HP, MP, Dmg int64
		} `json:"base"`
		FollowDistance int `json:"follow_distance"`
		AttackRange    int `json:"attack_range"`
		AttackEveryMs  int `json:"attack_every_ms"`
	} `json:"pets"`
	Clan struct {
		CreateGold int64 `json:"create_gold"`
		MaxMembers int   `json:"max_members"`
	} `json:"clan"`
	WorldChat struct {
		MinPower  int64 `json:"min_power"`
		GemCost   int64 `json:"gem_cost"`
		MinGems   int64 `json:"min_gems"`
		CooldownS int   `json:"cooldown_s"`
	} `json:"world_chat"`
	Bosses       []*BossTemplate         `json:"bosses"`
	Dungeons     map[string]*DungeonSpec `json:"dungeons"`
	Events       []*EventSpec            `json:"events"`
	PvpMaps      []int                   `json:"pvp_maps"`
	Achievements []*AchievementSpec      `json:"achievements"`

	SkillBooks map[int]*SkillBook `json:"-"` // derived from item names
}

type TaskOverride struct {
	Kind string `json:"kind"` // move, map, talk, kill, collect, add_point
	Maps []int  `json:"maps"`
	Npc  *int   `json:"npc"`
	Mobs []int  `json:"mobs"`
	Item int    `json:"item"`
}

func (c *Content) dropsFor(mobTID, mapID int) []DropRule {
	if r, ok := c.Drops.Mobs[strconv.Itoa(mobTID)]; ok {
		return r
	}
	if r, ok := c.Drops.Maps[strconv.Itoa(mapID)]; ok {
		return r
	}
	return c.Drops.Default
}

func (c *Content) Npc(id int) NpcContent { return c.Npcs[strconv.Itoa(id)] }

// loadContent reads CONTENT_FILE if set, else the embedded content.json.
func loadContent(d *Data) error {
	b := embeddedContent
	if path := os.Getenv("CONTENT_FILE"); path != "" {
		var err error
		if b, err = os.ReadFile(path); err != nil {
			return err
		}
	}
	b = bytes.TrimPrefix(b, []byte("\xef\xbb\xbf"))
	c := &Content{}
	if err := json.Unmarshal(b, c); err != nil {
		return err
	}
	c.SkillBooks = deriveSkillBooks(d)
	for _, bt := range c.Bosses {
		bt.normalize()
	}
	for _, dg := range c.Dungeons {
		if dg.Boss != nil {
			dg.Boss.normalize()
		}
	}
	d.Content = c
	return nil
}

var bookRe = regexp.MustCompile(`(?i)^(.+?)\s+(?:lv|cấp)\s*(\d+)$`)

// deriveSkillBooks maps type-7 items named "<skill name> lv<n>" to skills,
// replacing the legacy hardcoded item-id ranges.
func deriveSkillBooks(d *Data) map[int]*SkillBook {
	names := map[string]bool{}
	for _, s := range d.Skills {
		names[strings.ToLower(s.Name)] = true
	}
	out := map[int]*SkillBook{}
	for _, it := range d.ItemsFull {
		if it.Type != 7 {
			continue
		}
		m := bookRe.FindStringSubmatch(strings.TrimSpace(it.Name))
		if m == nil {
			continue
		}
		name := strings.ToLower(strings.TrimSpace(m[1]))
		lv, _ := strconv.Atoi(m[2])
		if names[name] && lv >= 1 {
			out[it.ID] = &SkillBook{name, it.Gender, lv}
		}
	}
	return out
}
