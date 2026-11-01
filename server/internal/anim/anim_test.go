package anim

import (
	"bytes"
	"encoding/binary"
	"testing"
)

func TestAnimationsReferenceFrames(t *testing.T) {
	for s, seq := range Animations {
		if len(seq) == 0 {
			t.Errorf("state %d has no frames", s)
		}
		for _, f := range seq {
			if f < 0 || f >= len(Frames) {
				t.Errorf("state %d frame %d out of range", s, f)
			}
		}
	}
	for i, f := range Frames {
		if f.Head.Slot > 2 || f.Body.Slot > 16 || f.Leg.Slot > 13 {
			t.Errorf("frame %d uses a slot parts do not have: %+v", i, f)
		}
	}
}

func TestBgLayerBottomsStackUpward(t *testing.T) {
	b := BgLayerBottoms(400, [4]int{80, 70, 77, 60})
	if b[0] <= 400 || !(b[0] > b[1] && b[1] > b[2] && b[2] > b[3]) {
		t.Fatalf("bottoms = %v", b)
	}
}

func TestGroundLine(t *testing.T) {
	// 3 columns: two have ground on row 2, one on row 1.
	tiles := []byte{0, 0, 0, 0, 0, 1, 1, 1, 0}
	if y := GroundLine(3, 3, tiles, func(v byte) bool { return v == 1 }); y != 48 {
		t.Fatalf("ground = %d", y)
	}
}

func be16(v int) []byte { return binary.BigEndian.AppendUint16(nil, uint16(int16(v))) }
func be32(v int) []byte { return binary.BigEndian.AppendUint32(nil, uint32(v)) }

func TestParseMobBinary(t *testing.T) {
	var data bytes.Buffer
	data.Write([]byte{1, 0, 2, 3, 10, 12}) // 1 image: id 0 at (2,3) 10x12
	data.Write(be16(1))                    // 1 frame
	data.WriteByte(1)
	data.Write(be16(-5))
	data.Write(be16(-12))
	data.WriteByte(0)
	data.Write(be16(2)) // sequence 0,0
	data.Write(be16(0))
	data.Write(be16(0))
	var file bytes.Buffer
	file.WriteByte(0)
	file.Write(be32(data.Len()))
	file.Write(data.Bytes())
	file.Write(be32(3))
	file.Write([]byte("png"))
	file.WriteByte(0) // empty trailer
	m, err := ParseMob(file.Bytes(), 1)
	if err != nil {
		t.Fatal(err)
	}
	if m.Images[0] != (Rect{2, 3, 10, 12}) || len(m.Frames) != 1 || m.Frames[0][0].Dy != -12 || len(m.Sequence) != 2 || string(m.PNG) != "png" {
		t.Fatalf("%+v", m)
	}
	// Same file with the extra leading id byte (mobs 92/93).
	m2, err := ParseMob(append([]byte{92}, append([]byte{1}, file.Bytes()[1:]...)...), 92)
	if err != nil || len(m2.Frames) != 1 {
		t.Fatalf("prefixed: %v %+v", err, m2)
	}
}

func TestParseMobText(t *testing.T) {
	txt := "==== SMALLIMAGES ====\r\n2\r\n0\r\n0\r\n5\r\n1\r\n7\r\n9\r\n==== FRAMES ===\r\n1\r\n2\r\n-3\r\n-4\r\n0\r\n1\r\n2\r\n1\r\n==== SEQUENCE ====\r\n0,0,0\r\n50\r\n50\r\n0,0,1\r\n"
	var file bytes.Buffer
	file.WriteByte(0)
	file.Write(be32(len(txt)))
	file.WriteString(txt)
	file.Write(be32(0))
	m, err := ParseMob(file.Bytes(), 70)
	if err != nil {
		t.Fatal(err)
	}
	if m.SheetScale != 1 || m.Images[1].X != 7 || m.Images[1].Y != 9 || len(m.Frames) != 1 || len(m.Frames[0]) != 2 ||
		m.Frames[0][1] != (FramePart{1, 2, 1}) || len(m.Sequence) != 3 || len(m.Sequences) != 1 {
		t.Fatalf("%+v", m)
	}
}
