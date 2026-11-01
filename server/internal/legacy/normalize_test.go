package legacy

import "testing"

func TestWaypoints(t *testing.T) {
	// Normal row and the malformed map-47 shape.
	for _, s := range []string{
		`["["Làng Aru",0,360,24,384,0,0,0,1188,432]","["Thung lũng tre",1512,336,1536,360,0,0,2,60,360]"]`,
		`["["Làng Aru",0,360,24,384,0,0,0,1188,432],["Thung lũng tre",1512,336,1536,360,0,0,2,60,360]"]`,
	} {
		w := Waypoints(s)
		if len(w) != 2 || w[0].Name != "Làng Aru" || w[0].GoX != 1188 || w[1].GoMap != 2 || w[1].MinX != 1512 {
			t.Fatalf("%s => %+v", s, w)
		}
	}
}

func TestDecodeDoubleEncoded(t *testing.T) {
	var v []struct {
		Damage int `json:"damage"`
	}
	if err := Decode(`["{"damage":100}","{"damage":110}"]`, &v); err != nil || len(v) != 2 || v[1].Damage != 110 {
		t.Fatalf("%v %+v", err, v)
	}
	rows, err := IntRows(`["[0,1,100,780,432]","[0,1,100,900,432]"]`)
	if err != nil || len(rows) != 2 || rows[1][3] != 900 {
		t.Fatalf("%v %v", err, rows)
	}
}

func TestRange(t *testing.T) {
	if a, b := Range(" 1-20"); a != 1 || b != 20 {
		t.Fatal(a, b)
	}
	if a, b := Range("5"); a != 5 || b != 5 {
		t.Fatal(a, b)
	}
}
