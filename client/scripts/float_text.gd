extends Node2D
## Rising, fading damage/heal number.

var text := ""
var color := Color.WHITE
var _t := 0.0


func _process(delta: float) -> void:
	_t += delta
	position.y -= 30.0 * delta
	modulate.a = clampf(1.5 - _t, 0.0, 1.0)
	if _t > 1.5:
		queue_free()
	queue_redraw()


func _draw() -> void:
	var font := ThemeDB.fallback_font
	draw_string_outline(font, Vector2(-30, 0), text, HORIZONTAL_ALIGNMENT_CENTER, 60, 9, 3, Color.BLACK)
	draw_string(font, Vector2(-30, 0), text, HORIZONTAL_ALIGNMENT_CENTER, 60, 9, color)
