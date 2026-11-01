package game

import (
	"encoding/json"
	"testing"

	"github.com/truong-ne/game-nro-clone/server/internal/anim"
	"github.com/truong-ne/game-nro-clone/server/internal/store"
)

func TestNewCharacter(t *testing.T) {
	c, err := NewCharacter(1, "  GokuSon ", 2, 999)
	if err != nil {
		t.Fatal(err)
	}
	if c.Name != "gokuson" || c.MapID != 41 || c.Head != 6 || c.DmgBase != 15 {
		t.Fatalf("%+v", c)
	}
	if len(c.Skills) != 8 || c.Skills[0].Level != 1 || c.Skills[1].Level != 0 {
		t.Fatalf("skills %+v", c.Skills)
	}
	for _, bad := range []string{"abc", "abcdefghijk", "ab cd", "admin", "tên123"} {
		if _, err := NewCharacter(1, bad, 0, 64); err == nil {
			t.Errorf("name %q accepted", bad)
		}
	}
}

func testData() (*Data, *MapTemplate) {
	// 3x3 map, ground (tile 1) on the bottom row.
	m := &MapTemplate{ID: 0, TileID: 1, Width: 3, Height: 3, Tiles: []byte{0, 0, 0, 0, 0, 0, 1, 1, 1}, MaxPlayer: 2, Zones: 2}
	d := &Data{
		Maps:    map[int]*MapTemplate{0: m},
		Items:   map[int]*ItemTemplate{0: {ID: 0, Type: 0, Part: 10}, 6: {ID: 6, Type: 1, Part: 11}},
		TileTop: map[int]map[byte]bool{1: {1: true}},
	}
	return d, m
}

func TestGroundY(t *testing.T) {
	d, m := testData()
	if y := d.GroundY(m, 30, 0); y != 48 {
		t.Fatalf("ground = %d", y)
	}
	if y := d.GroundY(m, 500, 7); y != 7 {
		t.Fatalf("out of map = %d", y)
	}
}

func TestRecalc(t *testing.T) {
	d, _ := testData()
	c, _ := NewCharacter(1, "player1", 0, 64)
	p := NewPlayer(c, false, nil, d)
	if p.Body != 10 || p.Leg != 11 {
		t.Fatalf("parts %d %d", p.Body, p.Leg)
	}
	if p.MaxHP != 230 || p.Def != 2 {
		t.Fatalf("hp %d def %d", p.MaxHP, p.Def)
	}
	c.Items = []store.Item{}
	p.Recalc(d)
	if p.MaxHP != 200 || p.Body != 10 {
		t.Fatalf("naked hp %d body %d", p.MaxHP, p.Body)
	}
}

type fakeConn struct{ msgs []string }

func (f *fakeConn) Send(b []byte) bool { f.msgs = append(f.msgs, string(b)); return true }
func (f *fakeConn) Close(string)       {}

func TestPickZoneNeverLoops(t *testing.T) {
	d, m := testData()
	w := &World{Data: d, zones: map[int][]*Zone{}}
	w.zones[0] = []*Zone{newZone(w, m, 0), newZone(w, m, 1)}
	w.zones[0][0].count.Store(2)
	w.zones[0][1].count.Store(2)
	if z := w.pickZone(0, -1); z == nil {
		t.Fatal("nil zone when all full")
	}
	w.zones[0][1].count.Store(1)
	if z := w.pickZone(0, -1); z.ID != 1 {
		t.Fatalf("picked %d", z.ID)
	}
}

func TestMoveSpeedCheck(t *testing.T) {
	d, m := testData()
	m.Width, m.Height = 100, 30
	m.Tiles = make([]byte, 3000)
	w := &World{Data: d, zones: map[int][]*Zone{}, mapJSON: map[int]json.RawMessage{}, mapVer: map[int]string{}}
	z := newZone(w, m, 0)
	w.zones[0] = []*Zone{z}
	c, _ := NewCharacter(1, "player1", 0, 64)
	c.ID = 7
	fc := &fakeConn{}
	p := NewPlayer(c, false, fc, d)
	z.add(p, 100, 100)
	z.move(p, 5000, 100, 1, anim.StateRun) // teleport attempt
	if c.X != 100 {
		t.Fatalf("teleport accepted: x=%d", c.X)
	}
	z.move(p, 150, 100, -1, anim.StateRun)
	if c.X != 150 || p.Dir != -1 || p.Anim != anim.StateRun {
		t.Fatalf("legit move rejected: x=%d dir=%d anim=%d", c.X, p.Dir, p.Anim)
	}
}

func TestMovesAreBatchedPerTick(t *testing.T) {
	d, m := testData()
	m.Width, m.Height = 100, 30
	m.Tiles = make([]byte, 3000)
	w := &World{Data: d, zones: map[int][]*Zone{}, mapJSON: map[int]json.RawMessage{}, mapVer: map[int]string{}}
	z := newZone(w, m, 0)
	w.zones[0] = []*Zone{z}
	var conns []*fakeConn
	var ps []*Player
	for i := range 3 {
		c, _ := NewCharacter(1, "player"+itoa(i), 0, 64)
		c.ID = int64(i + 1)
		fc := &fakeConn{}
		p := NewPlayer(c, false, fc, d)
		z.add(p, 100, 100)
		conns, ps = append(conns, fc), append(ps, p)
	}
	for _, fc := range conns {
		fc.msgs = nil
	}
	for _, p := range ps {
		z.move(p, 120, 100, 1, anim.StateRun)
		z.move(p, 130, 100, 1, anim.StateRun)
	}
	if len(conns[0].msgs) != 0 {
		t.Fatalf("moves sent before tick: %v", conns[0].msgs)
	}
	z.tick()
	if len(conns[0].msgs) != 1 {
		t.Fatalf("want 1 batched message, got %d", len(conns[0].msgs))
	}
	var env struct {
		T string
		D [][5]int64
	}
	json.Unmarshal([]byte(conns[0].msgs[0]), &env)
	if env.T != "moves" || len(env.D) != 3 || env.D[0][1] != 130 {
		t.Fatalf("batch = %s", conns[0].msgs[0])
	}
}

func TestFlyingDrainsKI(t *testing.T) {
	d, m := testData()
	w := &World{Data: d, zones: map[int][]*Zone{}, mapJSON: map[int]json.RawMessage{}, mapVer: map[int]string{}}
	z := newZone(w, m, 0)
	c, _ := NewCharacter(1, "player1", 1, 9) // Namek: 200 KI
	p := NewPlayer(c, false, &fakeConn{}, d)
	p.Anim = anim.StateFly
	z.drainFlight(p)
	if c.MP != 198 {
		t.Fatalf("mp = %d", c.MP)
	}
	c.MP = 0
	z.add(p, 10, 10)
	z.move(p, 12, 10, 1, anim.StateFly)
	if p.Anim != anim.StateFall {
		t.Fatalf("flying with no KI: anim=%d", p.Anim)
	}
}
