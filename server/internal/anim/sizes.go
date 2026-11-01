package anim

import (
	"bytes"
	"image"
	"image/png"
)

// FillSizes computes missing image sizes (text-format mobs) from the sprite
// sheet: each image is the bounding box of the opaque region connected to its
// top-left corner. zoom is the sheet's scale relative to rect units.
func (m *Mob) FillSizes(zoom int) {
	missing := false
	for _, r := range m.Images {
		if r.W == 0 || r.H == 0 {
			missing = true
			break
		}
	}
	if !missing {
		return
	}
	img, err := png.Decode(bytes.NewReader(m.PNG))
	if err != nil {
		return
	}
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	opaque := func(x, y int) bool {
		if x < 0 || y < 0 || x >= w || y >= h {
			return false
		}
		_, _, _, a := img.At(b.Min.X+x, b.Min.Y+y).RGBA()
		return a > 0x1000
	}
	for id, r := range m.Images {
		if r.W != 0 && r.H != 0 {
			continue
		}
		sx, sy := r.X*zoom, r.Y*zoom
		// Find the first opaque pixel near the corner.
		found := false
	search:
		for dy := 0; dy < 8*zoom; dy++ {
			for dx := 0; dx < 8*zoom; dx++ {
				if opaque(sx+dx, sy+dy) {
					sx, sy, found = sx+dx, sy+dy, true
					break search
				}
			}
		}
		if !found {
			continue
		}
		minX, minY, maxX, maxY := sx, sy, sx, sy
		seen := map[image.Point]bool{{sx, sy}: true}
		stack := []image.Point{{sx, sy}}
		for len(stack) > 0 && len(seen) < 200000 {
			p := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			minX, minY, maxX, maxY = min(minX, p.X), min(minY, p.Y), max(maxX, p.X), max(maxY, p.Y)
			for _, d := range [4]image.Point{{1, 0}, {-1, 0}, {0, 1}, {0, -1}} {
				q := p.Add(d)
				// Never grow above/left of the declared corner: that is another image.
				if q.X < r.X*zoom || q.Y < r.Y*zoom || seen[q] || !opaque(q.X, q.Y) {
					continue
				}
				seen[q] = true
				stack = append(stack, q)
			}
		}
		m.Images[id] = Rect{r.X, r.Y, (maxX-r.X*zoom)/zoom + 1, (maxY-r.Y*zoom)/zoom + 1}
	}
}
