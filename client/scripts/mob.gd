extends Node2D
## A mob or boss drawn from /api/mob/{id}: server-decoded frames plus inline
## sheet, with HP bar and target marker.

var template_id := 0
var display_name := ""
var hp := 0
var max_hp := 0
var targeted := false
var is_boss := false
var dir := 1

var _tex: Texture2D
var _imgs := {} # id -> Rect2 in rect units
var _frames: Array = []
var _seq: Array = []
var _scale := 1 # sheet pixels per rect unit
var _frame := 0
var _t := 0.0
var _height := 40.0


func setup(tid: int, name_: String) -> void:
	template_id = tid
	display_name = name_
	Assets.get_json("mob/%d?z=%d" % [tid, Assets.ZOOM], _on_mob)


func alive() -> bool:
	return hp > 0


func set_hp(v: int) -> void:
	hp = v
	visible = hp > 0
	queue_redraw()


func _on_mob(m) -> void:
	if not (m is Dictionary) or not is_inside_tree():
		return
	var img := Image.new()
	if img.load_png_from_buffer(Marshalls.base64_to_raw(str(m.get("sheet", "")))) != OK:
		return
	_tex = ImageTexture.create_from_image(img)
	_scale = maxi(int(m.get("sheet_scale", Assets.ZOOM)), 1)
	var images: Dictionary = m.get("images", {})
	for k in images:
		var r: Dictionary = images[k]
		_imgs[int(k)] = Rect2(r["x"], r["y"], r["w"], r["h"])
	_frames = Assets.arr(m.get("frames"))
	_seq = Assets.arr(m.get("sequence"))
	if _seq.is_empty() and not _frames.is_empty():
		_seq = [0]
	var top := 0.0
	if not _frames.is_empty():
		for p in _frames[0]:
			top = minf(top, float(p["dy"]))
	_height = maxf(-top, 20.0)
	queue_redraw()


func _process(delta: float) -> void:
	if _seq.size() < 2:
		return
	_t += delta
	var f := int(_t * 10.0) % _seq.size()
	if f != _frame:
		_frame = f
		queue_redraw()


func _draw() -> void:
	if _tex == null or _frames.is_empty():
		draw_rect(Rect2(-8, -16, 16, 16), Color(0.8, 0.3, 0.3, 0.6))
	else:
		draw_set_transform(Vector2.ZERO, 0.0, Vector2(dir, 1))
		var fi := clampi(int(_seq[_frame]), 0, _frames.size() - 1)
		for p in _frames[fi]:
			var r: Rect2 = _imgs.get(int(p["img"]), Rect2())
			if r.size == Vector2.ZERO:
				continue
			var src := Rect2(r.position * _scale, r.size * _scale)
			draw_texture_rect_region(_tex, Rect2(Vector2(p["dx"], p["dy"]), r.size), src)
		draw_set_transform(Vector2.ZERO, 0.0, Vector2.ONE)
	var font := ThemeDB.fallback_font
	var top := -_height - 10
	var col := Color(1, 0.6, 0.2) if is_boss else Color(1, 0.8, 0.8)
	draw_string_outline(font, Vector2(-50, top - 4), display_name, HORIZONTAL_ALIGNMENT_CENTER, 100, 7, 2, Color.BLACK)
	draw_string(font, Vector2(-50, top - 4), display_name, HORIZONTAL_ALIGNMENT_CENTER, 100, 7, col)
	if max_hp > 0 and (hp < max_hp or targeted or is_boss):
		var w := 40.0 if not is_boss else 80.0
		draw_rect(Rect2(-w / 2, top, w, 3), Color(0.2, 0, 0))
		draw_rect(Rect2(-w / 2, top, w * clampf(float(hp) / max_hp, 0, 1), 3), Color(0.9, 0.1, 0.1))
	if targeted:
		draw_arc(Vector2(0, 2), 14, 0, TAU, 20, Color.YELLOW, 1.5)
