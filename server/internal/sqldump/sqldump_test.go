package sqldump

import "testing"

func TestParse(t *testing.T) {
	src := "-- c\nINSERT INTO `map_template` (`id`, `NAME`, `waypoints`, `x`) VALUES\n" +
		"(0, 'Làng Aru', '[\\\"[\\\"Đồi\\\",1,2]\\\"]', NULL),\n" +
		"(1, 'It''s', 'a\\nb', 3);\n" +
		"INSERT INTO `map_template` VALUES (2, 'x', '', 4);"
	tables, err := Parse(src)
	if err != nil {
		t.Fatal(err)
	}
	m := tables["map_template"]
	if len(m.Rows) != 3 {
		t.Fatalf("rows = %d", len(m.Rows))
	}
	r := Row{m, m.Rows[0]}
	if r.Str("NAME") != "Làng Aru" || r.Str("waypoints") != `["["Đồi",1,2]"]` || !r.Get("x").Null {
		t.Fatalf("row0 = %+v", m.Rows[0])
	}
	r = Row{m, m.Rows[1]}
	if r.Str("name") != "It's" || r.Str("waypoints") != "a\nb" || r.Int("x") != 3 {
		t.Fatalf("row1 = %+v", m.Rows[1])
	}
}
