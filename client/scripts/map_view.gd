extends Node2D
## Map rendering: parallax background, decorations, tiles (TileMapLayer with
## a runtime atlas, so the engine culls and batches them) and waypoint markers.

const TILE := 24
const ATLAS_COLS := 16
const FLAG_TOP := 2
const FLAG_LEFT := 4
const FLAG_RIGHT := 8
const BackgroundScript := preload("res://scripts/background.gd")

var map := {}
var width := 0
var height := 0
var tiles := PackedByteArray()
var tileset_id := 1
var _top := {}
var _wall := {}

var background: Node2D
var _deco: Node2D
var _layer: TileMapLayer
var _atlas_img: Image
var _atlas_tex: ImageTexture
var _pending_tiles := {} # asset path -> tile value
var _markers: Node2D


func _ready() -> void:
	background = BackgroundScript.new()
	background.z_index = -10
	add_child(background)
	_deco = Node2D.new()
	add_child(_deco)
	_layer = TileMapLayer.new()
	_layer.scale = Vector2.ONE / Assets.ZOOM
	add_child(_layer)
	_markers = Node2D.new()
	_markers.draw.connect(_draw_markers)
	add_child(_markers)
	Assets.texture_loaded.connect(_on_tex)


func setup(m: Dictionary) -> void:
	map = m
	width = int(m.get("width", 0))
	height = int(m.get("height", 0))
	tiles = Marshalls.base64_to_raw(str(m.get("tiles", "")))
	tileset_id = int(m.get("tile_id", 1))
	_top = Assets.tile_set_with(tileset_id, FLAG_TOP)
	_wall = Assets.tile_set_with(tileset_id, FLAG_LEFT | FLAG_RIGHT)
	background.setup(m, ground_line())
	_build_decorations()
	_build_tiles()
	_markers.queue_redraw()


func pixel_size() -> Vector2:
	return Vector2(width * TILE, height * TILE)


func tile_at(col: int, row: int) -> int:
	if col < 0 or row < 0 or col >= width or row >= height:
		return 0
	return tiles[row * width + col]


func is_ground(col: int, row: int) -> bool:
	return _top.has(tile_at(col, row))


func is_wall(col: int, row: int) -> bool:
	return _wall.has(tile_at(col, row))


## Most common topmost ground row across columns, in pixels (see anim.GroundLine).
func ground_line() -> int:
	var count := {}
	for col in width:
		for row in height:
			if _top.has(tiles[row * width + col]):
				count[row] = count.get(row, 0) + 1
				break
	var best := height
	var best_n := 0
	for row in count:
		if count[row] > best_n or (count[row] == best_n and row > best):
			best = row
			best_n = count[row]
	return best * TILE


func _build_decorations() -> void:
	for c in _deco.get_children():
		c.queue_free()
	for b in Assets.arr(map.get("bg_items")):
		var t = Assets.data.get("bg_items", {}).get(str(int(b["id"])))
		if t == null:
			continue
		var s := Sprite2D.new()
		s.centered = false
		s.scale = Vector2.ONE / Assets.ZOOM
		s.position = Vector2(int(b["x"]) * TILE + int(t["dx"]), int(b["y"]) * TILE + int(t["dy"]))
		s.set_meta("path", "item_bg_temp/x%d/%d.png" % [Assets.ZOOM, int(t["image"])])
		s.texture = Assets.tex(s.get_meta("path"))
		_deco.add_child(s)


## One atlas cell per tile value (value v lives at cell v), filled in as each
## tile image arrives; the TileMapLayer cells never need to be reset.
func _build_tiles() -> void:
	_layer.clear()
	_pending_tiles.clear()
	var size := TILE * Assets.ZOOM
	var max_v := 0
	for v in tiles:
		max_v = maxi(max_v, v)
	var rows := max_v / ATLAS_COLS + 1
	_atlas_img = Image.create(ATLAS_COLS * size, rows * size, false, Image.FORMAT_RGBA8)
	_atlas_tex = ImageTexture.create_from_image(_atlas_img)
	var src := TileSetAtlasSource.new()
	src.texture = _atlas_tex
	src.texture_region_size = Vector2i(size, size)
	var ts := TileSet.new()
	ts.tile_size = Vector2i(size, size)
	ts.add_source(src, 0)
	_layer.tile_set = ts
	var used := {}
	for v in tiles:
		if v != 0:
			used[v] = true
	for v in used:
		src.create_tile(Vector2i(v % ATLAS_COLS, v / ATLAS_COLS))
		var path := "res/x%d/%d$%d" % [Assets.ZOOM, tileset_id, v]
		_pending_tiles[path] = v
		var t := Assets.tex(path)
		if t:
			_blit_tile(v, t)
	for row in height:
		for col in width:
			var v := tiles[row * width + col]
			if v != 0:
				_layer.set_cell(Vector2i(col, row), 0, Vector2i(v % ATLAS_COLS, v / ATLAS_COLS))


func _blit_tile(v: int, t: Texture2D) -> void:
	var img := t.get_image()
	var size := TILE * Assets.ZOOM
	if img.get_size() != Vector2i(size, size):
		img.resize(size, size, Image.INTERPOLATE_NEAREST)
	img.convert(Image.FORMAT_RGBA8)
	_atlas_img.blit_rect(img, Rect2i(0, 0, size, size), Vector2i(v % ATLAS_COLS, v / ATLAS_COLS) * size)
	_atlas_tex.update(_atlas_img)


func _on_tex(path: String) -> void:
	if _pending_tiles.has(path):
		_blit_tile(_pending_tiles[path], Assets.tex(path))
		return
	for s in _deco.get_children():
		if s.get_meta("path", "") == path:
			s.texture = Assets.tex(path)


func _draw_markers() -> void:
	var font := ThemeDB.fallback_font
	for w in Assets.arr(map.get("waypoints")):
		var r := Rect2(w["min_x"], w["min_y"], int(w["max_x"]) - int(w["min_x"]), int(w["max_y"]) - int(w["min_y"]))
		_markers.draw_rect(r.grow(2), Color(1, 1, 0, 0.35))
		_markers.draw_string_outline(font, r.position + Vector2(-30, -8), str(w["name"]), HORIZONTAL_ALIGNMENT_CENTER, r.size.x + 60, 7, 2, Color.BLACK)
		_markers.draw_string(font, r.position + Vector2(-30, -8), str(w["name"]), HORIZONTAL_ALIGNMENT_CENTER, r.size.x + 60, 7, Color.YELLOW)
