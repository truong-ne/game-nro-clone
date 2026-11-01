extends Node
## Procedural visual effects: hit sparks, status effects ("eff" messages from
## buffs_*.go: stun, shield, monkey, charge...). Never consumes messages, so
## other plugins still see them (loaded first because of the leading "_").

var world

const COLORS := {
	"tdhs": Color(1, 1, 0.3), "stun": Color(1, 1, 0.3), "sleep": Color(0.6, 0.6, 1), "hold": Color(0.8, 0.5, 0.2),
	"shield": Color(0.4, 0.8, 1), "monkey": Color(1, 0.4, 0.1), "transform": Color(1, 0.6, 0.1),
	"charge": Color(0.5, 0.9, 1), "prepare": Color(1, 1, 1), "gong": Color(0.9, 0.3, 1), "heal": Color(0.3, 1, 0.4),
	"huyt_sao": Color(0.4, 1, 0.6), "socola": Color(0.6, 0.35, 0.2), "egg": Color(1, 0.9, 0.7),
	"explode": Color(1, 0.3, 0.1), "blind": Color(0.2, 0.2, 0.2),
}


class Fx extends Node2D:
	var kind := ""
	var color := Color.WHITE
	var until := 0.0 # seconds left; < 0 = until removed
	var t := 0.0

	func _process(delta: float) -> void:
		t += delta
		if until >= 0.0:
			until -= delta
			if until <= 0.0:
				queue_free()
		queue_redraw()

	func _draw() -> void:
		var pulse := 0.5 + 0.5 * sin(t * 8.0)
		match kind:
			"shield":
				draw_circle(Vector2(0, -24), 26, Color(color, 0.18 + 0.1 * pulse))
				draw_arc(Vector2(0, -24), 26, 0, TAU, 32, Color(color, 0.8), 1.5)
			"monkey", "transform", "huyt_sao", "heal":
				for i in 6:
					var a := t * 3.0 + i * TAU / 6.0
					draw_line(Vector2(cos(a) * 14, 0), Vector2(cos(a) * 18, -50 - 8 * pulse), Color(color, 0.6), 2)
			"charge", "prepare", "gong":
				var r := 6.0 + 18.0 * fmod(t, 0.6) / 0.6
				draw_circle(Vector2(0, -28), r, Color(color, 0.35))
				draw_arc(Vector2(0, -28), r + 4, 0, TAU, 24, color, 1.0)
			"explode":
				draw_circle(Vector2(0, -24), 10 + t * 120, Color(color, maxf(0.0, 0.7 - t)))
			_: # stun / sleep / hold / socola / blind...: orbiting stars over the head
				for i in 3:
					var a := t * 4.0 + i * TAU / 3.0
					var p := Vector2(cos(a) * 12, -64 + sin(a) * 4)
					draw_circle(p, 2.5, color)


class Spark extends Node2D:
	var crit := false
	var t := 0.0

	func _process(delta: float) -> void:
		t += delta
		if t > 0.3:
			queue_free()
		queue_redraw()

	func _draw() -> void:
		var r := (18.0 if crit else 10.0) * (0.4 + t / 0.3)
		var c := Color(1, 0.6, 0.1) if crit else Color(1, 1, 0.8)
		c.a = 1.0 - t / 0.3
		for i in 8:
			var a := i * TAU / 8.0
			draw_line(Vector2(cos(a), sin(a)) * r * 0.4, Vector2(cos(a), sin(a)) * r, c, 2.0 if crit else 1.0)


func _target_node(d):
	if d.has("mob"):
		return world.mobs.get(int(d["mob"]))
	if d.has("boss"):
		return world.bosses.get(str(d["boss"]))
	if d.has("id"):
		return world._char(int(d["id"]))
	return null


func _spark(node, crit: bool) -> void:
	if node == null or not is_instance_valid(node):
		return
	var s := Spark.new()
	s.crit = crit
	s.position = node.position + Vector2(0, -24)
	world.entities.add_child(s)


func handle(t: String, d) -> bool:
	match t:
		"eff":
			var node = _target_node(d)
			if node == null or not is_instance_valid(node):
				return false
			var kind := str(d.get("kind", ""))
			for c in node.get_children():
				if c is Fx and c.kind == kind:
					c.queue_free()
			var ms := int(d.get("ms", 0))
			if ms > 0 or kind == "explode":
				var fx := Fx.new()
				fx.kind = kind
				fx.color = COLORS.get(kind, Color.WHITE)
				fx.until = (ms / 1000.0) if ms > 0 else 0.6
				node.add_child(fx)
		"mob_hit":
			_spark(world.mobs.get(int(d["i"])), bool(d.get("crit", false)))
		"boss_hit":
			_spark(world.bosses.get(str(d["key"])), bool(d.get("crit", false)))
		"player_hit", "mob_atk", "boss_atk":
			_spark(world._char(int(d["id"])), bool(d.get("crit", false)))
	return false
