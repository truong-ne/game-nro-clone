extends Node2D
## Draws a player or NPC from head/body/leg parts. position is the feet point
## in world (x1) pixels. Frames come from the server bundle (char_frames).

enum State { STAND, RUN, JUMP, FALL, FLY, ATTACK }

var id := 0
var display_name := ""
var head := -1
var body := -1
var leg := -1
var dir := 1
var state := State.STAND
var hp := 0
var max_hp := 0
var is_local := false
var target := Vector2.ZERO # remote players interpolate toward this
var targeted := false # selected as attack target (bosses drawn as characters)
var show_hp_bar := false

var _anim_t := 0.0
var _frame := {}
var _bubble := ""
var _bubble_t := 0.0


func _ready() -> void:
	Assets.texture_loaded.connect(func(_p): queue_redraw())
	target = position


func setup(d: Dictionary) -> void:
	id = int(d.get("id", 0))
	display_name = str(d.get("name", ""))
	head = int(d.get("head", -1))
	body = int(d.get("body", -1))
	leg = int(d.get("leg", -1))
	hp = int(d.get("hp", 0))
	max_hp = int(d.get("max_hp", 0))
	dir = -1 if int(d.get("dir", 1)) < 0 else 1
	state = int(d.get("anim", 0))
	position = Vector2(float(d.get("x", 0)), float(d.get("y", 0)))
	target = position
	queue_redraw()


func alive() -> bool:
	return hp > 0


func set_hp(v: int) -> void:
	hp = v
	visible = hp > 0
	queue_redraw()


func set_state(s: int) -> void:
	if s != state:
		state = s
		_anim_t = 0.0


func say(text: String) -> void:
	_bubble = text
	_bubble_t = 4.0
	queue_redraw()


func _process(delta: float) -> void:
	if not is_local:
		position = position.lerp(target, min(1.0, delta * 12.0))
	_anim_t += delta
	var f := Assets.char_frame(state, _anim_t)
	if f != _frame:
		_frame = f
		queue_redraw()
	if _bubble_t > 0.0:
		_bubble_t -= delta
		if _bubble_t <= 0.0:
			queue_redraw()


func _draw() -> void:
	if not _frame.is_empty():
		draw_set_transform(Vector2.ZERO, 0.0, Vector2(dir, 1))
		_draw_part(leg, _frame["leg"])
		_draw_part(body, _frame["body"])
		_draw_part(head, _frame["head"])
		draw_set_transform(Vector2.ZERO, 0.0, Vector2.ONE)
	var font := ThemeDB.fallback_font
	var col := Color(1, 1, 0.4) if is_local else Color.WHITE
	draw_string_outline(font, Vector2(-40, -62), display_name, HORIZONTAL_ALIGNMENT_CENTER, 80, 7, 2, Color.BLACK)
	draw_string(font, Vector2(-40, -62), display_name, HORIZONTAL_ALIGNMENT_CENTER, 80, 7, col)
	if show_hp_bar and max_hp > 0:
		draw_rect(Rect2(-40, -58, 80, 3), Color(0.2, 0, 0))
		draw_rect(Rect2(-40, -58, 80 * clampf(float(hp) / max_hp, 0, 1), 3), Color(0.9, 0.1, 0.1))
	if targeted:
		draw_arc(Vector2(0, 2), 14, 0, TAU, 20, Color.YELLOW, 1.5)
	if _bubble_t > 0.0:
		var w := minf(160.0, 8.0 + _bubble.length() * 4.0)
		draw_rect(Rect2(-w / 2, -92, w, 18), Color(1, 1, 1, 0.9))
		draw_string(font, Vector2(-w / 2 + 4, -79), _bubble, HORIZONTAL_ALIGNMENT_LEFT, w - 8, 7, Color.BLACK)


func _draw_part(part_id: int, e: Dictionary) -> void:
	if part_id < 0:
		return
	var pi := Assets.part(part_id)
	var slot := int(e["slot"])
	if slot >= pi.size():
		return
	var p: Array = pi[slot] # [iconId, dx, dy]
	var t := Assets.icon(int(p[0]))
	if t == null:
		return
	var pos := Vector2(int(e["dx"]) + int(p[1]), -int(e["dy"]) + int(p[2]))
	draw_texture_rect(t, Rect2(pos, t.get_size() / Assets.ZOOM), false)
