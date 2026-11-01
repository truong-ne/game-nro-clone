package anim

// Background layout rules shared by nro-render and the Godot client
// (client/scripts/background.gd mirrors these).

// BgParallax is how fast each layer scrolls relative to the camera (0 nearest).
var BgParallax = [4]float64{0.7, 0.5, 0.3, 0.1}

// BgLayerBottoms returns the bottom y of each layer (0 nearest .. 3 farthest).
// The nearest layer sits slightly below the ground line (hidden by tiles);
// each farther layer peeks out above the previous one.
func BgLayerBottoms(ground int, heights [4]int) [4]int {
	var out [4]int
	y := ground + heights[0]/4
	for i := range 4 {
		out[i] = y
		if heights[i] > 0 {
			y -= heights[i] * 11 / 20
		}
	}
	return out
}

// GroundLine returns the y (x1 px) of the most common topmost ground tile
// across columns, or the map bottom if there is none.
func GroundLine(w, h int, tiles []byte, isTop func(byte) bool) int {
	count := map[int]int{}
	for col := range w {
		for row := range h {
			if isTop(tiles[row*w+col]) {
				count[row]++
				break
			}
		}
	}
	best, bestN := h, 0
	for row, n := range count {
		if n > bestN || n == bestN && row > best {
			best, bestN = row, n
		}
	}
	return best * 24
}
