package game

import (
	"encoding/json"
	"sync"
	"sync/atomic"
	"time"

	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

// Envelope is the wire format: {"t": type, "d": payload}.
type Envelope struct {
	T string          `json:"t"`
	D json.RawMessage `json:"d,omitempty"`
}

func Encode(t string, d any) []byte {
	var raw json.RawMessage
	if d != nil {
		raw, _ = json.Marshal(d)
	}
	b, _ := json.Marshal(Envelope{T: t, D: raw})
	return b
}

// Conn is the network side of a player, implemented by the server package.
type Conn interface {
	Send(msg []byte) bool // false if the client is too slow and was dropped
	Close(reason string)
}

// Player is owned by the goroutine of the zone it is in; only that zone may
// touch its mutable fields. Other goroutines go through Zone.Post.
type Player struct {
	Char    *store.Character
	IsAdmin bool
	conn    Conn

	Body, Leg    int
	MaxHP, MaxMP int64
	Dmg, Def     int64
	Crit         int
	Mods         Mods

	Dir  int // 1 right, -1 left
	Anim int // anim.State*

	Dead          bool
	skillUsed     map[int]time.Time // skill template id -> last use
	lastHit       time.Time         // last time damaged, pauses regen
	Pet           *Pet
	trade         *trade
	duel          int64 // opponent id while dueling
	duelInvite    int64 // player we challenged
	lastWorldChat time.Time
	shop          int // shop id currently open, -1 none
	npcMenu       []menuOption
	persist       map[string]any // feature state, see State[T]
	Tmp           map[string]any // runtime-only feature state (not saved)

	zone      atomic.Pointer[Zone]
	left      atomic.Bool
	pending   atomic.Int32 // async DB operations whose callback has not run yet
	lastMove  time.Time
	lastChat  time.Time
	lastZone  time.Time
	dirty     bool
	closeOnce sync.Once
}

func NewPlayer(c *store.Character, isAdmin bool, conn Conn, d *Data) *Player {
	p := &Player{Char: c, IsAdmin: isAdmin, conn: conn, Dir: 1, skillUsed: map[int]time.Time{}, shop: -1,
		Tmp: map[string]any{}}
	p.Recalc(d)
	return p
}

func (p *Player) ID() int64   { return p.Char.ID }
func (p *Player) Zone() *Zone { return p.zone.Load() }

func (p *Player) Send(t string, d any) { p.SendRaw(Encode(t, d)) }

func (p *Player) SendRaw(b []byte) {
	if !p.conn.Send(b) {
		p.Kick("send buffer full")
	}
}

func (p *Player) Kick(reason string) {
	p.closeOnce.Do(func() { p.conn.Close(reason) })
}

// View is what other players see.
type View struct {
	ID     int64  `json:"id"`
	Name   string `json:"name"`
	Gender int    `json:"gender"`
	Head   int    `json:"head"`
	Body   int    `json:"body"`
	Leg    int    `json:"leg"`
	X      int    `json:"x"`
	Y      int    `json:"y"`
	HP     int64  `json:"hp"`
	MaxHP  int64  `json:"max_hp"`
	Dir    int    `json:"dir"`
	Anim   int    `json:"anim"`
}

func (p *Player) View() View {
	c := p.Char
	return View{c.ID, c.Name, c.Gender, c.Head, p.Body, p.Leg, c.X, c.Y, c.HP, p.MaxHP, p.Dir, p.Anim}
}

// Self is the full state sent to the owning client.
type Self struct {
	View
	MP        int64         `json:"mp"`
	MaxMP     int64         `json:"max_mp"`
	Dmg       int64         `json:"dmg"`
	Def       int64         `json:"def"`
	Crit      int           `json:"crit"`
	Power     int64         `json:"power"`
	Potential int64         `json:"potential"`
	Stamina   int           `json:"stamina"`
	Gold      int64         `json:"gold"`
	Gem       int64         `json:"gem"`
	Ruby      int64         `json:"ruby"`
	Items     []store.Item  `json:"items"`
	Skills    []store.Skill `json:"skills"`
	IsAdmin   bool          `json:"is_admin"`
}

func (p *Player) Self() Self {
	c := p.Char
	return Self{p.View(), c.MP, p.MaxMP, p.Dmg, p.Def, p.Crit, c.Power, c.Potential, c.Stamina,
		c.Gold, c.Gem, c.Ruby, c.Items, c.Skills, p.IsAdmin}
}

// Snapshot returns a deep copy safe to save from another goroutine.
func (p *Player) Snapshot() *store.Character {
	p.flushState()
	c := *p.Char
	c.Items = append([]store.Item(nil), p.Char.Items...)
	for i := range c.Items {
		c.Items[i].Options = append([][2]int(nil), c.Items[i].Options...)
	}
	c.Skills = append([]store.Skill(nil), p.Char.Skills...)
	// Extra holds maps and pointers; round-trip it so the saver owns a copy.
	if b, err := json.Marshal(p.Char.Extra); err == nil {
		c.Extra = store.Extra{}
		json.Unmarshal(b, &c.Extra)
	}
	return &c
}
