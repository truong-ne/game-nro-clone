extends Node
## Minimap in the top-right corner: ground, waypoints, NPCs, mobs, bosses,
## other players and you. Press M to toggle.

var world
var _panel: Control
var _built_for := -1
var _ground: ImageTexture

const W := 220.0
const H := 90.0


class MiniView extends Control:
	var owner_plugin

	func _draw() -> void:
		owner_plugin.draw_to(self)


func _ready() -> void:
	_panel = MiniView.new()
	_panel.owner_plugin = self
	_panel.position = Vector2(1280 - W - 12, 48)
	_panel.size = Vector2(W, H)
	_panel.mouse_filter = Control.MOUSE_FILTER_IGNORE
	world.hud.add_child(_panel)


func handle(_t: String, _d) -> bool:
	return false


func _unhandled_input(e: InputEvent) -> void:
	if e is InputEventKey and e.pressed and not e.echo and e.keycode == KEY_M and not world.hud.typing():
		_panel.visible = not _panel.visible


func _process(_d: float) -> void:
	if not _panel.visible:
		return
	if world.map_id != _built_for and world.map_view.width > 0:
		_build_ground()
	_panel.queue_redraw()


## Bakes the ground layout once per map: one pixel per tile.
func _build_ground() -> void:
	var mv = world.map_view
	_built_for = world.map_id
	var img := Image.create(maxi(mv.width, 1), maxi(mv.height, 1), false, Image.FORMAT_RGBA8)
	for row in mv.height:
		for col in mv.width:
			var v: int = mv.tile_at(col, row)
			if v != 0:
				img.set_pixel(col, row, Color(0.55, 0.45, 0.3) if mv.is_ground(col, row) else Color(0.35, 0.3, 0.25))
	_ground = ImageTexture.create_from_image(img)


func draw_to(c: Control) -> void:
	var mv = world.map_view
	if mv.width == 0 or world.me == null:
		return
	var size: Vector2 = mv.pixel_size()
	var scale := minf(W / size.x, H / size.y)
	var box := Vector2(size.x * scale, size.y * scale)
	c.draw_rect(Rect2(Vector2.ZERO, box), Color(0, 0, 0, 0.45))
	if _ground:
		c.draw_texture_rect(_ground, Rect2(Vector2.ZERO, box), false)
	var to_map := func(p: Vector2) -> Vector2: return p * scale
	for w in Assets.arr(mv.map.get("waypoints")):
		var r := Rect2(Vector2(w["min_x"], w["min_y"]) * scale, Vector2(int(w["max_x"]) - int(w["min_x"]), int(w["max_y"]) - int(w["min_y"])) * scale)
		c.draw_rect(r.grow(1.5), Color(1, 1, 0, 0.8))
	for n in world.entities.get_children():
		if n.has_meta("npc_index"):
			c.draw_circle(to_map.call(n.position), 1.5, Color(0.3, 0.8, 1))
	for m in world.mobs.values():
		if m.alive():
			c.draw_circle(to_map.call(m.position), 1.2, Color(1, 0.3, 0.3))
	for b in world.bosses.values():
		c.draw_circle(to_map.call(b.position), 3.0, Color(1, 0.5, 0))
	for o in world.others.values():
		c.draw_circle(to_map.call(o.position), 1.8, Color.WHITE)
	c.draw_circle(to_map.call(world.me.position), 2.5, Color(1, 1, 0.2))
	c.draw_rect(Rect2(Vector2.ZERO, box), Color(1, 1, 1, 0.6), false, 1.0)
	c.draw_string(ThemeDB.fallback_font, Vector2(2, box.y + 12), world.map_name, HORIZONTAL_ALIGNMENT_LEFT, -1, 11, Color.WHITE)
