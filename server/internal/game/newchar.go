package game

import (
	"errors"
	"slices"
	"strings"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Starter equipment per gender (0 Earth, 1 Namek, 2 Saiyan), from PlayerDAO.createNewPlayer.
var (
	StarterShirt = [3]int{0, 1, 2}
	StarterPants = [3]int{6, 7, 8}

	// Hair choices offered at character creation, per gender.
	StarterHeads = [3][]int{{64, 30, 31}, {9, 29, 32}, {6, 27, 28}}

	// Skill templates each race owns; the first one starts at level 1.
	StarterSkills = [3][]int{
		{0, 1, 6, 9, 10, 20, 22, 19},
		{2, 3, 7, 11, 12, 17, 18, 19},
		{4, 5, 8, 13, 14, 21, 23, 19},
	}
)

const (
	SpawnX = 100
	SpawnY = 384
)

func SpawnMap(gender int) int { return 39 + gender }
func HomeMap(gender int) int  { return 21 + gender }

var ErrBadName = errors.New("tên nhân vật phải dài 5-10 ký tự, chỉ gồm chữ thường và số")

func ValidName(name string) bool {
	if len(name) < 5 || len(name) > 10 {
		return false
	}
	for _, r := range name {
		if !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return !slices.Contains(reservedNames, name)
}

var reservedNames = []string{"admin", "server", "system", "gamemaster", "moderator"}

// NewCharacter builds a level-1 character with the legacy starting kit.
func NewCharacter(accountID int64, name string, gender, head int) (*store.Character, error) {
	name = strings.ToLower(strings.TrimSpace(name))
	if !ValidName(name) {
		return nil, ErrBadName
	}
	if gender < 0 || gender > 2 {
		return nil, errors.New("hành tinh không hợp lệ")
	}
	if !slices.Contains(StarterHeads[gender], head) {
		head = StarterHeads[gender][0]
	}
	hp, mp, dmg := int64(100), int64(100), int64(10)
	if gender == 0 {
		hp = 200
	}
	if gender == 1 {
		mp = 200
	}
	if gender == 2 {
		dmg = 15
	}
	def := 2
	if gender == 2 {
		def = 3
	}
	pantsHP := 20
	if gender == 0 {
		pantsHP = 30
	}
	c := &store.Character{
		AccountID: accountID, Name: name, Gender: gender, Head: head,
		MapID: SpawnMap(gender), X: SpawnX, Y: SpawnY,
		Power: 2000, Potential: 2000, Stamina: 1000,
		HPBase: hp, MPBase: mp, DmgBase: dmg, HP: hp, MP: mp,
		Gold: 2000, Gem: 10000,
		Items: []store.Item{
			{Container: store.ContainerBody, Slot: 0, TemplateID: StarterShirt[gender], Quantity: 1, Options: [][2]int{{47, def}}},
			{Container: store.ContainerBody, Slot: 1, TemplateID: StarterPants[gender], Quantity: 1, Options: [][2]int{{6, pantsHP}}},
			{Container: store.ContainerBag, Slot: 0, TemplateID: 63, Quantity: 10, Options: [][2]int{{2, 8}}},
			{Container: store.ContainerBox, Slot: 0, TemplateID: 12, Quantity: 1},
		},
	}
	for i, id := range StarterSkills[gender] {
		sk := store.Skill{TemplateID: id}
		if i == 0 {
			sk.Level = 1
			slot := 0
			sk.Shortcut = &slot
		}
		c.Skills = append(c.Skills, sk)
	}
	return c, nil
}
