package anim

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
)

type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"w"`
	H int `json:"h"`
}

type FramePart struct {
	Dx  int `json:"dx"`
	Dy  int `json:"dy"`
	Img int `json:"img"`
}

// Mob is a decoded legacy mob animation file.
type Mob struct {
	Images    map[int]Rect  `json:"images"`
	Frames    [][]FramePart `json:"frames"`
	Sequence  []int         `json:"sequence"`
	Sequences [][]int       `json:"sequences,omitempty"` // boss-style named sequences from the trailer
	// SheetScale is the PNG's scale relative to rect units: 0 means "same as
	// the asset zoom folder"; text-format mobs ship one x1 sheet for every zoom.
	SheetScale int    `json:"sheet_scale"`
	PNG        []byte `json:"-"`
}

type reader struct {
	b   []byte
	p   int
	err error
}

func (r *reader) need(n int) bool {
	if r.err == nil && r.p+n > len(r.b) {
		r.err = errors.New("truncated")
	}
	return r.err == nil
}

func (r *reader) u8() int {
	if !r.need(1) {
		return 0
	}
	r.p++
	return int(r.b[r.p-1])
}

func (r *reader) i16() int {
	if !r.need(2) {
		return 0
	}
	r.p += 2
	return int(int16(binary.BigEndian.Uint16(r.b[r.p-2:])))
}

func (r *reader) i32() int {
	if !r.need(4) {
		return 0
	}
	r.p += 4
	return int(int32(binary.BigEndian.Uint32(r.b[r.p-4:])))
}

func (r *reader) bytes(n int) []byte {
	if n < 0 || !r.need(n) {
		r.err = errors.New("bad length")
		return nil
	}
	r.p += n
	return r.b[r.p-n : r.p]
}

// ParseMob decodes mob/x*/<id>: u8 type, i32 len, data, i32 len, png, [trailer].
// Some files (mobs 92, 93) start with an extra byte equal to the mob id.
func ParseMob(b []byte, id int) (*Mob, error) {
	r := &reader{b: b}
	typ := r.u8()
	if typ == id && len(b) > 1 && b[1] <= 2 && id > 2 {
		typ = r.u8()
	}
	data := r.bytes(r.i32())
	img := r.bytes(r.i32())
	if r.err != nil {
		return nil, fmt.Errorf("mob %d: %w", id, r.err)
	}
	var m *Mob
	var err error
	if bytes.HasPrefix(bytes.TrimSpace(data), []byte("=")) {
		m, err = parseText(data)
		if m != nil {
			m.SheetScale = 1
		}
	} else {
		m, err = parseBinary(data, typ)
	}
	if err != nil {
		return nil, fmt.Errorf("mob %d: %w", id, err)
	}
	m.PNG = img
	// Optional trailer: u8 nSeq, then per sequence u8 n + n frame indexes.
	if r.p < len(b) {
		if n := r.u8(); n > 0 {
			for range n {
				k := r.u8()
				seq := make([]int, 0, k)
				for range k {
					seq = append(seq, r.u8())
				}
				m.Sequences = append(m.Sequences, seq)
			}
			if r.err != nil {
				m.Sequences = nil
			}
		}
	}
	return m, nil
}

// parseText reads the text variant (used by mob 70):
//
//	==== SMALLIMAGES ====  n, then n × (id, x, y)   — no sizes, see FillSizes
//	==== FRAMES ===        n, then per frame: k, k × (dx, dy, img)
//	==== SEQUENCE ====     "a,b,c" line, w, h, then extra "a,b,c" sequences
func parseText(data []byte) (*Mob, error) {
	m := &Mob{Images: map[int]Rect{}}
	var section string
	var nums []int
	var seqLines []string
	flush := func() error {
		switch section {
		case "SMALLIMAGES":
			if len(nums) == 0 {
				return errors.New("no images")
			}
			for i := 1; i+2 < len(nums) && (i-1)/3 < nums[0]; i += 3 {
				m.Images[nums[i]] = Rect{X: nums[i+1], Y: nums[i+2]}
			}
		case "FRAMES":
			p := 1
			for f := 0; len(nums) > 0 && f < nums[0] && p < len(nums); f++ {
				k := nums[p]
				p++
				fr := make([]FramePart, 0, k)
				for j := 0; j < k && p+2 < len(nums)+0; j++ {
					fr = append(fr, FramePart{nums[p], nums[p+1], nums[p+2]})
					p += 3
				}
				m.Frames = append(m.Frames, fr)
			}
		}
		nums = nums[:0]
		return nil
	}
	for _, line := range bytes.Split(data, []byte("\n")) {
		s := string(bytes.TrimSpace(line))
		if s == "" {
			continue
		}
		if bytes.HasPrefix([]byte(s), []byte("====")) {
			if err := flush(); err != nil {
				return nil, err
			}
			section = string(bytes.Trim([]byte(s), "= "))
			continue
		}
		if section == "SEQUENCE" {
			seqLines = append(seqLines, s)
			continue
		}
		var n int
		if _, err := fmt.Sscan(s, &n); err == nil {
			nums = append(nums, n)
		}
	}
	if err := flush(); err != nil {
		return nil, err
	}
	for i, l := range seqLines {
		var seq []int
		for _, f := range bytes.Split([]byte(l), []byte(",")) {
			var n int
			if _, err := fmt.Sscan(string(f), &n); err == nil {
				seq = append(seq, n)
			}
		}
		switch {
		case i == 0:
			m.Sequence = seq
		case len(seq) > 1:
			m.Sequences = append(m.Sequences, seq)
		}
	}
	if len(m.Frames) == 0 {
		return nil, errors.New("no frames")
	}
	return m, nil
}

func parseBinary(data []byte, typ int) (*Mob, error) {
	r := &reader{b: data}
	m := &Mob{Images: map[int]Rect{}}
	n := r.u8()
	for range n {
		id := r.u8()
		var x, y int
		if typ == 2 {
			x, y = r.i16(), r.i16()
		} else {
			x, y = r.u8(), r.u8()
		}
		m.Images[id] = Rect{x, y, r.u8(), r.u8()}
	}
	nf := r.i16()
	for range nf {
		np := r.u8()
		f := make([]FramePart, 0, np)
		for range np {
			f = append(f, FramePart{r.i16(), r.i16(), r.u8()})
		}
		if r.err != nil {
			break
		}
		m.Frames = append(m.Frames, f)
	}
	if r.err != nil {
		return nil, fmt.Errorf("%w at byte %d/%d: images=%d frames=%d/%d", r.err, r.p, len(data), n, len(m.Frames), nf)
	}
	ns := r.i16()
	for range ns {
		m.Sequence = append(m.Sequence, r.i16())
	}
	if r.err != nil {
		m.Sequence = nil // sequence is optional for rendering
	}
	return m, nil
}
