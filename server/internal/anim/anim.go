// Package anim holds the character animation table and the legacy mob
// animation file parser. The same data is sent to the Godot client.
package anim

// Entry picks a slot of a part and offsets it; dy grows upward from the feet.
type Entry struct {
	Slot int `json:"slot"`
	Dx   int `json:"dx"`
	Dy   int `json:"dy"`
}

type Frame struct {
	Name string `json:"name"`
	Head Entry  `json:"head"`
	Leg  Entry  `json:"leg"`
	Body Entry  `json:"body"`
}

// Frames is the character pose table. Head parts have 3 slots, bodies 17 and
// legs 14; each frame chooses one slot of each. Slot meanings were read off
// rendered part sheets (nro-render parts): legs 1 stand, 2-6 run, 7 jump,
// 8 fall, 10-12 fly; bodies 1 stand, 2-6 run, 7 jump, 8 fall, 10-15 attack.
var Frames = []Frame{
	0:  {"stand", Entry{0, -13, 34}, Entry{1, -8, 10}, Entry{1, -9, 16}},
	1:  {"stand2", Entry{0, -13, 35}, Entry{1, -8, 10}, Entry{1, -9, 17}},
	2:  {"run1", Entry{1, -10, 33}, Entry{2, -10, 11}, Entry{2, -8, 16}},
	3:  {"run2", Entry{1, -10, 32}, Entry{3, -11, 9}, Entry{3, -8, 15}},
	4:  {"run3", Entry{1, -10, 34}, Entry{4, -9, 11}, Entry{4, -8, 17}},
	5:  {"run4", Entry{1, -10, 34}, Entry{5, -11, 11}, Entry{5, -8, 17}},
	6:  {"run5", Entry{1, -10, 32}, Entry{6, -10, 9}, Entry{6, -8, 15}},
	7:  {"jump", Entry{0, -13, 35}, Entry{7, -8, 11}, Entry{7, -9, 17}},
	8:  {"fall", Entry{0, -13, 35}, Entry{8, -9, 12}, Entry{8, -9, 18}},
	9:  {"fly1", Entry{1, -10, 31}, Entry{10, -14, 14}, Entry{7, -7, 17}},
	10: {"fly2", Entry{1, -10, 31}, Entry{11, -14, 14}, Entry{7, -7, 17}},
	11: {"attack1", Entry{0, -13, 34}, Entry{1, -8, 10}, Entry{10, -9, 16}},
	12: {"attack2", Entry{0, -13, 34}, Entry{1, -8, 10}, Entry{11, -9, 16}},
	13: {"attack3", Entry{0, -13, 34}, Entry{1, -8, 10}, Entry{12, -9, 16}},
}

// Animation states, sent over the wire as small ints.
const (
	StateStand = iota
	StateRun
	StateJump
	StateFall
	StateFly
	StateAttack
	NumStates
)

// Animations lists each state's frame sequence (indexes into Frames), in state order.
var Animations = [NumStates][]int{
	StateStand:  {0, 1},
	StateRun:    {2, 3, 4, 5, 6},
	StateJump:   {7},
	StateFall:   {8},
	StateFly:    {9, 10},
	StateAttack: {11, 12, 13},
}
