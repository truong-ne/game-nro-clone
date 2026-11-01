// Package legacy converts the quirky column formats of the old Java/MariaDB
// database into plain Go values.
package legacy

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// FixJSON undoes the double encoding used by the old server, where arrays of
// arrays/objects were stored as arrays of strings: ["[1,2]","{...}"].
func FixJSON(s string) string {
	s = strings.TrimSpace(s)
	r := strings.NewReplacer(`"[`, `[`, `]"`, `]`, `"{`, `{`, `}"`, `}`)
	return r.Replace(s)
}

// Decode fixes and unmarshals a legacy JSON column.
func Decode(s string, out any) error {
	s = strings.TrimSpace(s)
	if s == "" {
		s = "[]"
	}
	if err := json.Unmarshal([]byte(FixJSON(s)), out); err != nil {
		return fmt.Errorf("decode %.80q: %w", s, err)
	}
	return nil
}

// Ints decodes a flat or nested array of numbers into [][]int64.
func IntRows(s string) ([][]int64, error) {
	var rows [][]int64
	err := Decode(s, &rows)
	return rows, err
}

var waypointRe = regexp.MustCompile(`\[\s*"?([^",\[\]]*)"?\s*((?:,\s*-?\d+\s*){9})\]`)

// Waypoint is [name, minX, minY, maxX, maxY, isEnter, isOffline, goMap, goX, goY].
type Waypoint struct {
	Name                   string
	MinX, MinY, MaxX, MaxY int
	IsEnter, IsOffline     bool
	GoMap, GoX, GoY        int
}

// Waypoints parses map_template.waypoints. Some rows are not valid JSON even
// after FixJSON, so this uses a regex over [name, n x 9] groups.
func Waypoints(s string) []Waypoint {
	var out []Waypoint
	for _, m := range waypointRe.FindAllStringSubmatch(s, -1) {
		parts := strings.Split(strings.TrimPrefix(strings.TrimSpace(m[2]), ","), ",")
		n := make([]int, len(parts))
		for i, p := range parts {
			n[i], _ = strconv.Atoi(strings.TrimSpace(p))
		}
		out = append(out, Waypoint{
			Name: m[1], MinX: n[0], MinY: n[1], MaxX: n[2], MaxY: n[3],
			IsEnter: n[4] == 1, IsOffline: n[5] == 1, GoMap: n[6], GoX: n[7], GoY: n[8],
		})
	}
	return out
}

// Range parses "min-max" strings like "1-20".
func Range(s string) (int, int) {
	a, b, ok := strings.Cut(strings.TrimSpace(s), "-")
	lo, _ := strconv.Atoi(strings.TrimSpace(a))
	if !ok {
		return lo, lo
	}
	hi, _ := strconv.Atoi(strings.TrimSpace(b))
	return lo, hi
}

// CSVInts parses "1017, 1018".
func CSVInts(s string) []int {
	var out []int
	for _, p := range strings.Split(s, ",") {
		if p = strings.TrimSpace(p); p != "" {
			if n, err := strconv.Atoi(p); err == nil {
				out = append(out, n)
			}
		}
	}
	return out
}
