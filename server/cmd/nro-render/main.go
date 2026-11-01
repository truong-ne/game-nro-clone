// Command nro-render draws maps and character parts from the legacy assets to
// PNG files. It is a debugging aid used to verify rendering rules (coordinate
// units, layer order, part slots) independently of the Godot client.
//
//	nro-render -out /out map 0 1 21
//	nro-render -out /out parts 64 57 58      # contact sheet of every slot
//	nro-render -out /out char 64 57 58       # all animation frames
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"

	"github.com/jackc/pgx/v5/pgxpool"
	xdraw "golang.org/x/image/draw"
	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"

	"github.com/truong-ne/game-nro-clone/server/internal/anim"
	"github.com/truong-ne/game-nro-clone/server/internal/db"
	"github.com/truong-ne/game-nro-clone/server/internal/game"
)

const Z = 2 // asset zoom used

var (
	dataDir = flag.String("data", "/data", "legacy SRC/data")
	outDir  = flag.String("out", "/out", "output directory")
	imgs    = map[string]image.Image{}
	parts   = map[int][][3]int{}
	tileTop map[int]map[byte]bool
)

func main() {
	flag.Parse()
	ctx := context.Background()
	pool, err := db.Connect(ctx, os.Getenv("DATABASE_URL"))
	if err != nil {
		fail(err)
	}
	rows, _ := pool.Query(ctx, `SELECT id, frames FROM part`)
	for rows.Next() {
		var id int
		var f []byte
		rows.Scan(&id, &f)
		var v [][3]int
		json.Unmarshal(f, &v)
		parts[id] = v
	}
	args := flag.Args()
	if len(args) < 2 {
		fail(fmt.Errorf("usage: map <ids...> | parts <head> <body> <leg> | char <head> <body> <leg>"))
	}
	var ids []int
	for _, a := range args[1:] {
		n, _ := strconv.Atoi(a)
		ids = append(ids, n)
	}
	switch args[0] {
	case "map":
		d, err := game.LoadData(ctx, pool)
		if err != nil {
			fail(err)
		}
		tileTop = d.TileTop
		bg := loadBgItems(ctx, pool)
		npcs := loadNpcs(ctx, pool)
		for _, id := range ids {
			if m := d.Map(id); m != nil {
				save(fmt.Sprintf("map_%d.png", id), renderMap(m, bg, npcs))
			}
		}
	case "parts":
		save(fmt.Sprintf("parts_%d_%d_%d.png", ids[0], ids[1], ids[2]), partSheet(ids))
	case "char":
		save(fmt.Sprintf("char_%d_%d_%d.png", ids[0], ids[1], ids[2]), charSheet(ids[0], ids[1], ids[2]))
	case "mobs":
		save("mobs.png", mobSheet(ids[0], ids[len(ids)-1]))
	}
}

func fail(err error) {
	slog.Error("nro-render", "err", err)
	os.Exit(1)
}

func save(name string, img image.Image) {
	f, err := os.Create(filepath.Join(*outDir, name))
	if err != nil {
		fail(err)
	}
	defer f.Close()
	png.Encode(f, img)
	slog.Info("wrote", "file", name, "size", img.Bounds().Size())
}

func load(rel string) image.Image {
	if im, ok := imgs[rel]; ok {
		return im
	}
	var im image.Image
	if b, err := os.ReadFile(filepath.Join(*dataDir, rel)); err == nil {
		im, _ = png.Decode(bytes.NewReader(b))
	}
	imgs[rel] = im
	return im
}

func icon(id int) image.Image { return load(fmt.Sprintf("icon/x%d/%d.png", Z, id)) }

func blit(dst draw.Image, im image.Image, x, y int, flip bool) {
	if im == nil {
		return
	}
	b := im.Bounds()
	if !flip {
		draw.Draw(dst, image.Rect(x, y, x+b.Dx(), y+b.Dy()), im, b.Min, draw.Over)
		return
	}
	for yy := 0; yy < b.Dy(); yy++ {
		for xx := 0; xx < b.Dx(); xx++ {
			c := im.At(b.Min.X+b.Dx()-1-xx, b.Min.Y+yy)
			if _, _, _, a := c.RGBA(); a > 0 {
				dst.Set(x+xx, y+yy, c)
			}
		}
	}
}

func label(dst draw.Image, x, y int, s string, c color.Color) {
	d := font.Drawer{Dst: dst, Src: image.NewUniform(c), Face: basicfont.Face7x13, Dot: fixed.P(x, y)}
	d.DrawString(s)
}

// drawChar draws a character whose feet are at (x, y) in output pixels.
func drawChar(dst draw.Image, head, body, leg, frame, x, y int, flip bool) {
	f := anim.Frames[frame]
	for _, layer := range []struct {
		part int
		e    anim.Entry
	}{{leg, f.Leg}, {body, f.Body}, {head, f.Head}} {
		p := parts[layer.part]
		if layer.e.Slot >= len(p) {
			continue
		}
		pi := p[layer.e.Slot]
		im := icon(pi[0])
		if im == nil {
			continue
		}
		ox := (layer.e.Dx + pi[1]) * Z
		oy := (-layer.e.Dy + pi[2]) * Z
		if flip {
			ox = -ox - im.Bounds().Dx()
		}
		blit(dst, im, x+ox, y+oy, flip)
	}
}

func newCanvas(w, h int, bg color.Color) *image.RGBA {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(img, img.Bounds(), image.NewUniform(bg), image.Point{}, draw.Src)
	return img
}

func partSheet(ids []int) image.Image {
	const cell = 90
	img := newCanvas(17*cell, 3*cell+20, color.RGBA{60, 90, 120, 255})
	for row, id := range ids {
		for slot, pi := range parts[id] {
			x, y := slot*cell, row*cell+20
			blit(img, icon(pi[0]), x+20, y+10, false)
			label(img, x+2, y+12, fmt.Sprintf("%d:%d", slot, pi[0]), color.White)
		}
	}
	label(img, 2, 12, fmt.Sprintf("head %d / body %d / leg %d", ids[0], ids[1], ids[2]), color.White)
	return img
}

func charSheet(head, body, leg int) image.Image {
	const cell = 110
	n := len(anim.Frames)
	img := newCanvas(n*cell, 2*cell, color.RGBA{60, 90, 120, 255})
	for i := range n {
		for r, flip := range []bool{false, true} {
			x, y := i*cell+cell/2, r*cell+cell-14
			draw.Draw(img, image.Rect(x-40, y, x+40, y+1), image.NewUniform(color.RGBA{255, 255, 0, 255}), image.Point{}, draw.Src)
			drawChar(img, head, body, leg, i, x, y, flip)
			label(img, i*cell+2, r*cell+12, fmt.Sprintf("%d %s", i, anim.Frames[i].Name), color.White)
		}
	}
	return img
}

type bgTemplate struct{ image, layer, dx, dy int }

func loadBgItems(ctx context.Context, pool *pgxpool.Pool) map[int]bgTemplate {
	out := map[int]bgTemplate{}
	rows, _ := pool.Query(ctx, `SELECT id, image_id, layer, dx, dy FROM bg_item_template`)
	for rows.Next() {
		var id int
		var t bgTemplate
		rows.Scan(&id, &t.image, &t.layer, &t.dx, &t.dy)
		out[id] = t
	}
	return out
}

type npcTemplate struct {
	name            string
	head, body, leg int
}

func loadNpcs(ctx context.Context, pool *pgxpool.Pool) map[int]npcTemplate {
	out := map[int]npcTemplate{}
	rows, _ := pool.Query(ctx, `SELECT id, name, head, body, leg FROM npc_template`)
	for rows.Next() {
		var id int
		var t npcTemplate
		rows.Scan(&id, &t.name, &t.head, &t.body, &t.leg)
		out[id] = t
	}
	return out
}

// bgLayer finds b{bg}{layer}[-type], falling back to the untyped file.
func bgLayer(bg, layer, typ int) image.Image {
	if typ > 0 {
		if im := load(fmt.Sprintf("res/x%d/b%d%d-%d", Z, bg, layer, typ)); im != nil {
			return im
		}
	}
	return load(fmt.Sprintf("res/x%d/b%d%d", Z, bg, layer))
}

func renderMap(m *game.MapTemplate, bg map[int]bgTemplate, npcs map[int]npcTemplate) image.Image {
	w, h := m.Width*game.TileSize*Z, m.Height*game.TileSize*Z
	// Background: layer 3 is farthest (sky), 0 nearest. Sky colour comes from
	// the farthest layer's top row; layers stack upward from the ground line.
	var layers [4]image.Image
	for i := range layers {
		layers[i] = bgLayer(m.BgID, i, m.BgType)
	}
	sky := color.Color(color.RGBA{126, 200, 240, 255})
	for i := 3; i >= 0; i-- {
		if layers[i] != nil {
			b := layers[i].Bounds()
			sky = layers[i].At(b.Min.X+1, b.Min.Y+1)
			break
		}
	}
	img := newCanvas(w, h, sky)
	bottoms := anim.BgLayerBottoms(groundLine(m)*Z, layerHeights(layers))
	for i := 3; i >= 0; i-- {
		if layers[i] == nil {
			continue
		}
		y := bottoms[i]
		b := layers[i].Bounds()
		for x := 0; x < w; x += b.Dx() {
			blit(img, layers[i], x, y-b.Dy(), false)
		}
	}
	for _, it := range m.BgItems {
		t, ok := bg[it.ID]
		if !ok {
			continue
		}
		im := load(fmt.Sprintf("item_bg_temp/x%d/%d.png", Z, t.image))
		blit(img, im, (it.X*game.TileSize+t.dx)*Z, (it.Y*game.TileSize+t.dy)*Z, false)
	}
	for row := 0; row < m.Height; row++ {
		for col := 0; col < m.Width; col++ {
			if v := m.Tiles[row*m.Width+col]; v != 0 {
				blit(img, load(fmt.Sprintf("res/x%d/%d$%d", Z, m.TileID, v)), col*game.TileSize*Z, row*game.TileSize*Z, false)
			}
		}
	}
	for _, n := range m.Npcs {
		t := npcs[n.TemplateID]
		drawChar(img, t.head, t.body, t.leg, 0, n.X*Z, n.Y*Z, false)
		label(img, n.X*Z-20, n.Y*Z+14, t.name, color.White)
	}
	for _, mb := range m.Mobs {
		drawMob(img, mb.TemplateID, mb.X*Z, mb.Y*Z)
	}
	for _, wp := range m.Waypoints {
		r := image.Rect(wp.MinX*Z, wp.MinY*Z, wp.MaxX*Z, wp.MaxY*Z)
		draw.Draw(img, r, image.NewUniform(color.RGBA{255, 255, 0, 120}), image.Point{}, draw.Over)
		label(img, r.Min.X, r.Min.Y-4, wp.Name, color.RGBA{255, 255, 0, 255})
	}
	label(img, 4, h-6, fmt.Sprintf("map %d %s tile=%d bg=%d type=%d", m.ID, m.Name, m.TileID, m.BgID, m.BgType), color.White)
	return img
}

// mobSheet draws frame 0 of every mob id in [from, to] and logs parse failures.
func mobSheet(from, to int) image.Image {
	const cell = 120
	cols := 12
	n := to - from + 1
	img := newCanvas(cols*cell, (n+cols-1)/cols*cell, color.RGBA{60, 90, 120, 255})
	for i := range n {
		x, y := i%cols*cell, i/cols*cell
		draw.Draw(img, image.Rect(x+10, y+cell-20, x+cell-10, y+cell-19), image.NewUniform(color.RGBA{255, 255, 0, 255}), image.Point{}, draw.Src)
		if err := drawMob(img, from+i, x+cell/2, y+cell-20); err != nil {
			slog.Warn("mob", "id", from+i, "err", err)
		}
		label(img, x+2, y+12, strconv.Itoa(from+i), color.White)
	}
	return img
}

func layerHeights(layers [4]image.Image) [4]int {
	var hs [4]int
	for i, l := range layers {
		if l != nil {
			hs[i] = l.Bounds().Dy()
		}
	}
	return hs
}

// groundLine is the most common ground y (x1 px) across map columns.
func groundLine(m *game.MapTemplate) int {
	return anim.GroundLine(m.Width, m.Height, m.Tiles, func(v byte) bool { return tileTop[m.TileID][v] })
}

func drawMob(dst draw.Image, id, x, y int) error {
	b, err := os.ReadFile(filepath.Join(*dataDir, fmt.Sprintf("mob/x%d/%d", Z, id)))
	if err != nil {
		return err
	}
	mob, err := anim.ParseMob(b, id)
	if err != nil {
		label(dst, x-10, y, "?"+strconv.Itoa(id), color.RGBA{255, 0, 0, 255})
		return err
	}
	sz := Z
	if mob.SheetScale > 0 {
		sz = mob.SheetScale
	}
	mob.FillSizes(sz)
	sheet, err := png.Decode(bytes.NewReader(mob.PNG))
	if err != nil {
		return fmt.Errorf("png: %w", err)
	}
	if len(mob.Frames) == 0 {
		return fmt.Errorf("no frames")
	}
	for _, p := range mob.Frames[0] {
		r, ok := mob.Images[p.Img]
		if !ok {
			continue
		}
		src := image.Rect(r.X*sz, r.Y*sz, (r.X+r.W)*sz, (r.Y+r.H)*sz).Add(sheet.Bounds().Min)
		at := image.Pt(x+p.Dx*Z, y+p.Dy*Z)
		xdraw.NearestNeighbor.Scale(dst, image.Rectangle{at, at.Add(image.Pt(r.W*Z, r.H*Z))}, sheet, src, xdraw.Over, nil)
	}
	return nil
}
