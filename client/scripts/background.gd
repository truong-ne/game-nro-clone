extends Node2D
## Parallax background: res/x*/b{bg}{layer}[-{bgType}], layer 3 farthest.
## Layout mirrors server/internal/anim/bg.go (BgLayerBottoms, BgParallax).

var _layers: Array[Texture2D] = [null, null, null, null]
var _paths: Array[String] = ["", "", "", ""]
var _fallback: Array[String] = ["", "", "", ""]
var _ground := 0
var _sky := Color("7ec8f0")
var _last_cam := Vector2.INF


func _ready() -> void:
	Assets.texture_loaded.connect(_on_tex)
	Assets.texture_failed.connect(_on_missing)


## Missing typed variants (b80-3...) fall back to the plain layer file.
func _on_missing(path: String) -> void:
	var i := _paths.find(path)
	if i >= 0 and _paths[i] != _fallback[i]:
		_paths[i] = _fallback[i]
		_layers[i] = Assets.tex(_paths[i])
		queue_redraw()


func setup(m: Dictionary, ground: int) -> void:
	_ground = ground
	var bg := int(m.get("bg_id", 0))
	var typ := int(m.get("bg_type", 0))
	for i in 4:
		_fallback[i] = "res/x%d/b%d%d" % [Assets.ZOOM, bg, i]
		_paths[i] = _fallback[i] + ("-%d" % typ if typ > 0 else "")
		_layers[i] = Assets.tex(_paths[i])
	_update_sky()
	queue_redraw()


func _on_tex(path: String) -> void:
	var i := _paths.find(path)
	if i >= 0:
		_layers[i] = Assets.tex(path)
		_update_sky()
		queue_redraw()


func _process(_d: float) -> void:
	var cam := get_viewport().get_camera_2d()
	if cam and cam.get_screen_center_position() != _last_cam:
		_last_cam = cam.get_screen_center_position()
		queue_redraw()


func _update_sky() -> void:
	for i in [3, 2, 1, 0]:
		if _layers[i]:
			_sky = _layers[i].get_image().get_pixel(1, 1)
			return


func _draw() -> void:
	var cam := get_viewport().get_camera_2d()
	if cam == null:
		return
	var view := get_viewport_rect().size / cam.zoom
	var center := cam.get_screen_center_position()
	var left := center.x - view.x / 2
	draw_rect(Rect2(left, center.y - view.y / 2, view.x, view.y), _sky)
	var parallax: Array = Assets.data.get("bg_parallax", [0.7, 0.5, 0.3, 0.1])
	var heights := []
	for i in 4:
		heights.append(_layers[i].get_size().y / Assets.ZOOM if _layers[i] else 0.0)
	# Bottom of each layer, nearest first (see anim.BgLayerBottoms).
	var bottoms := []
	var y: float = _ground + heights[0] / 4.0
	for i in 4:
		bottoms.append(y)
		y -= heights[i] * 0.55
	for i in [3, 2, 1, 0]:
		var t := _layers[i]
		if t == null:
			continue
		var size := t.get_size() / Assets.ZOOM
		var origin := left * (1.0 - float(parallax[i]))
		var x: float = origin + floor((left - origin) / size.x) * size.x
		while x < left + view.x:
			draw_texture_rect(t, Rect2(Vector2(x, bottoms[i] - size.y), size), false)
			x += size.x
