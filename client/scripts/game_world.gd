extends Node2D
## In-game scene: map, entities, local movement, combat, camera and HUD.

const CharacterScript := preload("res://scripts/character.gd")
const MobScript := preload("res://scripts/mob.gd")
const MapViewScript := preload("res://scripts/map_view.gd")
const HudScript := preload("res://scripts/hud.gd")
const FloatText := preload("res://scripts/float_text.gd")
const S := CharacterScript.State

const WALK := 140.0
const FLY_SPEED := 170.0
const FLY_RISE := -130.0
const FLY_SINK := 35.0
const GRAVITY := 1100.0
const JUMP := -380.0
const MAX_FALL := 500.0
const SEND_EVERY := 0.1
const TILE := 24
const PICKUP_RANGE := 40.0

var map_view: Node2D
var entities: Node2D
var camera: Camera2D
var hud: CanvasLayer
var me: Node2D
var others := {} # id -> character node (players and pets)
var mobs := {} # index -> mob node
var bosses := {} # key -> mob node
var ground := {} # id -> Node2D
var self_info := {}
var map_name := ""
var map_id := -1
var zone := 0
var zones := 1
var target = null # mob/boss node

var _vel := Vector2.ZERO
var _on_ground := false
var _flying := false
var _send_t := 0.0
var _sent := {}
var _waypoint_cd := 0.0
var _attack_t := 0.0
var _loading := false
var _queued: Array = []
var _trading := false
var _trade_slots: Array = []


func _ready() -> void:
	map_view = MapViewScript.new()
	add_child(map_view)
	entities = Node2D.new()
	add_child(entities)
	camera = Camera2D.new()
	camera.zoom = Vector2(2, 2)
	camera.position_smoothing_enabled = true
	add_child(camera)
	hud = HudScript.new()
	add_child(hud)
	Net.message.connect(_on_message)
	_load_plugins()


## Feature plugins: every script in res://scripts/features is a Node with
## `var world` and `func handle(t: String, d) -> bool` (true = consumed).
var plugins: Array = []


func _load_plugins() -> void:
	var dir := DirAccess.open("res://scripts/features")
	if dir == null:
		return
	var files := Array(dir.get_files())
	files.sort()
	for f in files:
		f = str(f).trim_suffix(".remap")
		if not f.ends_with(".gd"):
			continue
		var s = load("res://scripts/features/" + f)
		if s == null:
			continue
		var n: Node = s.new()
		n.set("world", self)
		add_child(n)
		plugins.append(n)


func log_line(text: String, color := "white") -> void:
	hud.log_line(text, color)


func _unhandled_input(e: InputEvent) -> void:
	if not (e is InputEventKey and e.pressed and not e.echo) or hud.typing():
		return
	match e.keycode:
		KEY_ENTER:
			hud.chat_in.grab_focus()
		KEY_ESCAPE:
			hud.close_panel()
		KEY_I:
			hud.open_panel("inventory")
		KEY_T:
			hud.open_panel("task")
		KEY_P:
			hud.open_panel("points")
		KEY_C:
			hud.open_panel("clan_info")
		KEY_F:
			hud.open_panel("friends")
		KEY_TAB:
			_cycle_target()
		KEY_E:
			_interact()
		KEY_Z:
			_pickup_near()
		KEY_1, KEY_2, KEY_3, KEY_4, KEY_5, KEY_6, KEY_7, KEY_8:
			hud.select_skill_index(e.keycode - KEY_1)
		_:
			return
	get_viewport().set_input_as_handled()


func _float(at: Vector2, text: String, color: Color) -> void:
	var f := FloatText.new()
	f.text = text
	f.color = color
	f.position = at + Vector2(0, -50)
	entities.add_child(f)


func _char(id: int):
	if me and id == me.id:
		return me
	return others.get(id)


func _on_message(t: String, d) -> void:
	if _loading and t != "map" and t != "self":
		_queued.append([t, d])
		return
	for pl in plugins:
		if pl.has_method("handle") and pl.handle(t, d):
			return
	match t:
		"map":
			_loading = true
			_queued.clear()
			Assets.get_json("map/%d?v=%s" % [int(d["map_id"]), d["version"]], func(m):
				_loading = false
				if m is Dictionary:
					_enter_map(m, d)
				else:
					log_line("Không tải được bản đồ", "tomato")
				var q := _queued.duplicate()
				_queued.clear()
				for x in q:
					_on_message(x[0], x[1]))
		"self":
			self_info = d
			if me:
				me.hp = int(d["hp"])
				me.max_hp = int(d["max_hp"])
				me.body = int(d["body"])
				me.leg = int(d["leg"])
				me.queue_redraw()
			hud.set_skills(Assets.arr(d.get("skills")), int(d.get("gender", 0)))
			_refresh_stats()
		"mp", "hpmp", "exp":
			for k in ["mp", "hp", "power", "potential"]:
				if d.has(k):
					self_info[k] = int(d[k])
			if int(self_info.get("mp", 1)) <= 0:
				_flying = false
			_refresh_stats()
		"join":
			_add_other(d)
		"leave":
			var id := int(d["id"])
			if others.has(id):
				others[id].queue_free()
				others.erase(id)
		"look":
			var o = _char(int(d["id"]))
			if o:
				o.body = int(d["body"])
				o.leg = int(d["leg"])
				o.queue_redraw()
		"moves":
			for mv in d:
				var o = others.get(int(mv[0]))
				if o:
					o.target = Vector2(float(mv[1]), float(mv[2]))
					o.dir = -1 if int(mv[3]) < 0 else 1
					o.set_state(int(mv[4]))
					o.queue_redraw()
		"pos":
			if me:
				me.position = Vector2(float(d["x"]), float(d["y"]))
				_vel = Vector2.ZERO
		"mob_hit":
			var m = mobs.get(int(d["i"]))
			if m:
				m.set_hp(int(d["hp"]))
				_float(m.position, str(int(d["dmg"])) + ("!" if d.get("crit") else ""), Color.ORANGE if d.get("crit") else Color.WHITE)
		"mob_hp":
			var m2 = mobs.get(int(d["i"]))
			if m2:
				m2.set_hp(int(d["hp"]))
		"mob_die":
			var m3 = mobs.get(int(d["i"]))
			if m3:
				m3.set_hp(0)
				if target == m3:
					_set_target(null)
		"mob_spawn":
			var m4 = mobs.get(int(d["i"]))
			if m4:
				m4.max_hp = int(d["max_hp"])
				m4.set_hp(int(d["hp"]))
		"mob_atk", "boss_atk":
			var c = _char(int(d["id"]))
			if c:
				c.hp = int(d["hp"])
				_float(c.position, "-" + str(int(d["dmg"])), Color.RED)
				if c == me:
					self_info["hp"] = int(d["hp"])
					_refresh_stats()
		"player_hit":
			var c2 = _char(int(d["id"]))
			if c2:
				c2.hp = int(d["hp"])
				_float(c2.position, "-" + str(int(d["dmg"])), Color.RED)
				if c2 == me:
					self_info["hp"] = int(d["hp"])
					_refresh_stats()
		"die":
			var c3 = _char(int(d["id"]))
			if c3:
				c3.set_state(S.FALL)
				c3.modulate = Color(1, 1, 1, 0.5)
			if c3 == me:
				self_info["hp"] = 0
				_refresh_stats()
				hud.show_revive()
		"revive":
			var c4 = _char(int(d["id"]))
			if c4:
				c4.modulate = Color.WHITE
				c4.hp = int(d["hp"])
		"item_drop":
			_add_ground(d)
		"item_gone":
			var g = ground.get(int(d["id"]))
			if g:
				g.queue_free()
				ground.erase(int(d["id"]))
		"boss_spawn":
			_add_boss(d)
		"boss_move":
			var b = bosses.get(str(d["key"]))
			if b:
				var to := Vector2(float(d["x"]), float(d["y"]))
				if b.get("target") is Vector2: # character-style boss interpolates
					b.target = to
				else:
					b.position = to
				b.dir = int(d.get("dir", 1))
		"boss_hit":
			var b2 = bosses.get(str(d["key"]))
			if b2:
				b2.set_hp(int(d["hp"]))
				_float(b2.position, str(int(d["dmg"])), Color.ORANGE)
		"boss_die", "boss_leave":
			var b3 = bosses.get(str(d["key"]))
			if b3:
				if target == b3:
					_set_target(null)
				b3.queue_free()
				bosses.erase(str(d["key"]))
		"boss_chat":
			var b4 = bosses.get(str(d["key"]))
			log_line("%s: %s" % [b4.display_name if b4 else "Boss", d["text"]], "orange")
		"chat":
			log_line("%s: %s" % [d["name"], d["text"]])
			var c5 = _char(int(d["id"]))
			if c5:
				c5.say(str(d["text"]))
		"world_chat":
			log_line("[TG] %s: %s" % [d["name"], d["text"]], "cyan")
		"clan_chat":
			log_line("[Bang] %s: %s" % [d["name"], d["text"]], "lightgreen")
		"pm":
			log_line("[Mật] %s: %s" % [d["name"], d["text"]], "violet")
		"notice":
			log_line(str(d["text"]), "yellow")
		"error":
			log_line(str(d["text"]), "tomato")
		"menu":
			hud.show_menu(d)
		"inventory":
			if _trading:
				_show_trade_picker(d)
			else:
				hud.show_inventory(d)
		"shop":
			hud.show_shop(d)
		"box":
			pass
		"task":
			hud.show_task(d)
		"clan":
			hud.show_clan(d)
		"friends":
			hud.show_friends(Assets.arr(d))
		"prompt":
			hud.prompt(d)
		"clan_invite":
			hud.confirm("%s mời bạn vào bang %s" % [d["from"], d["name"]], func(): Net.send("clan_join"))
		"trade_invite":
			var from := int(d["id"])
			hud.confirm("%s muốn giao dịch" % d["name"], func(): Net.send("trade_accept", {"id": from}))
		"trade_open":
			_trading = true
			_trade_slots = []
			Net.send("inventory")
		"trade":
			hud.show_trade(d, int(self_info.get("id", 0)))
		"trade_close":
			_trading = false
			hud.close_panel()
		"duel_invite":
			var from2 := int(d["id"])
			hud.confirm("%s thách đấu bạn" % d["name"], func(): Net.send("duel_accept", {"id": from2}))
		"duel_start":
			log_line("Bắt đầu thách đấu!", "orange")
		"duel_end":
			log_line("Kết thúc thách đấu", "orange")


func _show_trade_picker(d: Dictionary) -> void:
	hud.show_inventory(d)
	var box: VBoxContainer = hud._panel.get_child(0).get_child(0)
	var gold_e := SpinBox.new()
	gold_e.max_value = int(d["gold"])
	gold_e.prefix = "Vàng"
	box.add_child(gold_e)
	for it in Assets.arr(d.get("items")):
		if int(it["container"]) != 1:
			continue
		var slot := int(it["slot"])
		hud._row(box, "Đưa: " + hud.item_name(it), [["+", func():
			if not _trade_slots.has(slot):
				_trade_slots.append(slot)
			Net.send("trade_offer", {"slots": _trade_slots, "gold": int(gold_e.value)})]])
	var send := Button.new()
	send.text = "Gửi đề nghị"
	send.pressed.connect(func(): Net.send("trade_offer", {"slots": _trade_slots, "gold": int(gold_e.value)}))
	box.add_child(send)


func _refresh_stats() -> void:
	hud.update_stats(self_info, map_name, zone)


func _enter_map(m: Dictionary, d: Dictionary) -> void:
	for c in entities.get_children():
		c.queue_free()
	others.clear()
	mobs.clear()
	bosses.clear()
	ground.clear()
	target = null
	map_name = str(m["name"])
	map_id = int(m["id"])
	zone = int(d["zone"])
	zones = int(d["zones"])
	map_view.setup(m)
	var npcs := Assets.arr(m.get("npcs"))
	for i in npcs.size():
		var n: Dictionary = npcs[i]
		var t = Assets.data.get("npcs", {}).get(str(int(n["template_id"])))
		if t == null:
			continue
		var c := CharacterScript.new()
		c.set_meta("npc_index", i)
		entities.add_child(c)
		c.setup({"name": t["name"], "head": t["head"], "body": t["body"], "leg": t["leg"], "x": n["x"], "y": n["y"]})
	for mv in Assets.arr(d.get("mobs")):
		var t2 = Assets.data.get("mobs", {}).get(str(int(mv["tid"])), {})
		var node := MobScript.new()
		node.position = Vector2(float(mv["x"]), float(mv["y"]))
		node.max_hp = int(mv["max_hp"])
		entities.add_child(node)
		node.setup(int(mv["tid"]), "%s (%d)" % [t2.get("name", ""), int(mv["lv"])])
		node.set_hp(int(mv["hp"]))
		mobs[int(mv["i"])] = node
	for b in Assets.arr(d.get("bosses")):
		_add_boss(b)
	for it in Assets.arr(d.get("items")):
		_add_ground(it)
	for p in Assets.arr(d.get("players")):
		_add_other(p)
	me = CharacterScript.new()
	me.is_local = true
	entities.add_child(me)
	var info := self_info.duplicate()
	info["x"] = d["x"]
	info["y"] = d["y"]
	me.setup(info)
	_vel = Vector2.ZERO
	_flying = false
	_sent = {}
	var size: Vector2 = map_view.pixel_size()
	camera.limit_left = 0
	camera.limit_top = 0
	camera.limit_right = int(size.x)
	camera.limit_bottom = int(size.y)
	camera.position = me.position
	camera.reset_smoothing()
	hud.zone_btn.clear()
	for i in zones:
		hud.zone_btn.add_item("Khu %d" % i)
	hud.zone_btn.select(zone)
	_refresh_stats()
	log_line("Đến %s (khu %d)" % [map_name, zone], "lightgreen")


func _add_other(p: Dictionary) -> void:
	var id := int(p["id"])
	if others.has(id) or (me and id == me.id):
		return
	var c := CharacterScript.new()
	entities.add_child(c)
	c.setup(p)
	others[id] = c


func _add_boss(b: Dictionary) -> void:
	var key := str(b["key"])
	if bosses.has(key):
		return
	var parts := Assets.arr(b.get("parts"))
	var node
	if parts.size() >= 3: # legacy outfit: draw as a character
		node = CharacterScript.new()
		entities.add_child(node)
		node.setup({"name": "BOSS " + str(b["name"]), "head": parts[0], "body": parts[1], "leg": parts[2],
			"x": b["x"], "y": b["y"], "hp": b["hp"], "max_hp": b["max_hp"]})
		node.show_hp_bar = true
	else:
		node = MobScript.new()
		node.is_boss = true
		node.position = Vector2(float(b["x"]), float(b["y"]))
		node.max_hp = int(b["max_hp"])
		entities.add_child(node)
		node.setup(int(b["mob"]), "BOSS " + str(b["name"]))
	node.set_hp(int(b["hp"]))
	node.set_meta("boss_key", key)
	bosses[key] = node


func _add_ground(g: Dictionary) -> void:
	var id := int(g["id"])
	var t: Dictionary = Assets.data.get("items", {}).get(str(int(g["tid"])), {})
	var s := Sprite2D.new()
	s.texture = Assets.icon(int(t.get("icon", 0)))
	s.scale = Vector2.ONE / Assets.ZOOM
	s.position = Vector2(float(g["x"]), float(g["y"]) - 8)
	s.set_meta("tid", int(g["tid"]))
	var icon := int(t.get("icon", 0))
	Assets.texture_loaded.connect(func(p): if is_instance_valid(s) and p.ends_with("/%d.png" % icon): s.texture = Assets.icon(icon))
	entities.add_child(s)
	ground[id] = s


func _set_target(n) -> void:
	if target and is_instance_valid(target):
		target.targeted = false
		target.queue_redraw()
	target = n
	if n:
		n.targeted = true
		n.queue_redraw()
		hud.target_label.text = "Mục tiêu: %s  %d/%d" % [n.display_name, n.hp, n.max_hp]
	else:
		hud.target_label.text = ""


func _cycle_target() -> void:
	var cands := []
	for m in mobs.values():
		if m.alive():
			cands.append(m)
	cands.append_array(bosses.values())
	cands.sort_custom(func(a, b): return a.position.distance_to(me.position) < b.position.distance_to(me.position))
	if cands.is_empty():
		_set_target(null)
		return
	var i := cands.find(target)
	_set_target(cands[(i + 1) % mini(cands.size(), 4)])


func _attack() -> void:
	if target == null or not is_instance_valid(target) or hud.selected_skill < 0 or not target.alive():
		if target and is_instance_valid(target) and not target.alive():
			_set_target(null)
		_cycle_target()
		return
	me.dir = 1 if target.position.x >= me.position.x else -1
	me.set_state(S.ATTACK)
	if target.has_meta("boss_key"):
		Net.send("skill", {"id": hud.selected_skill, "boss": target.get_meta("boss_key")})
	else:
		for i in mobs:
			if mobs[i] == target:
				Net.send("skill", {"id": hud.selected_skill, "mob": i})
	hud.target_label.text = "Mục tiêu: %s  %d/%d" % [target.display_name, target.hp, target.max_hp]


func _interact() -> void:
	var best = null
	var best_d := 120.0
	for c in entities.get_children():
		if c.has_meta("npc_index"):
			var dd: float = c.position.distance_to(me.position)
			if dd < best_d:
				best = c
				best_d = dd
	if best:
		Net.send("npc", {"i": best.get_meta("npc_index")})


func _pickup_near() -> void:
	for id in ground:
		if ground[id].position.distance_to(me.position) < PICKUP_RANGE + 20:
			Net.send("pickup", {"id": id})


func _blocked(x: float, y: float) -> bool:
	var col := int(x / TILE)
	for probe in [2.0, 26.0]:
		if map_view.is_wall(col, int((y - probe) / TILE)):
			return true
	return false


func _physics_process(delta: float) -> void:
	if me == null or map_view.width == 0 or int(self_info.get("hp", 1)) <= 0:
		return
	var typing: bool = hud.typing()
	var ix := 0.0 if typing else Input.get_axis("move_left", "move_right")
	var jump_pressed := not typing and Input.is_action_just_pressed("jump")
	var jump_held := not typing and Input.is_action_pressed("jump")
	var has_ki := int(self_info.get("mp", 0)) > 0

	if jump_pressed:
		if _on_ground:
			_vel.y = JUMP
			_on_ground = false
		elif has_ki:
			_flying = not _flying
	if not has_ki:
		_flying = false
	if _flying:
		_vel.x = ix * FLY_SPEED
		_vel.y = FLY_RISE if jump_held else FLY_SINK
	else:
		_vel.x = ix * WALK
		_vel.y = minf(_vel.y + GRAVITY * delta, MAX_FALL)
	if ix != 0.0:
		me.dir = 1 if ix > 0 else -1

	var size: Vector2 = map_view.pixel_size()
	var p: Vector2 = me.position
	var nx := clampf(p.x + _vel.x * delta, 0.0, size.x - 1.0)
	if nx != p.x and _blocked(nx + signf(_vel.x) * 6.0, p.y):
		nx = p.x
	var ny := maxf(p.y + _vel.y * delta, 30.0)
	_on_ground = false
	if _vel.y >= 0.0:
		var col := int(nx / TILE)
		for row in range(int(floor(p.y / TILE)), int(floor(ny / TILE)) + 1):
			if map_view.is_ground(col, row) and p.y <= row * TILE + 0.01:
				ny = row * TILE
				_vel.y = 0.0
				_on_ground = true
				_flying = false
				break
	if ny >= size.y:
		ny = size.y
		_vel.y = 0.0
		_on_ground = true
		_flying = false
	me.position = Vector2(nx, ny)
	camera.position = me.position

	_attack_t -= delta
	if not typing and Input.is_action_pressed("attack") and _attack_t <= 0.0:
		_attack_t = 0.3
		_attack()
	var st: int = S.FLY if _flying else (S.JUMP if _vel.y < 0 else S.FALL) if not _on_ground \
		else (S.RUN if ix != 0.0 else (S.ATTACK if _attack_t > 0.0 else S.STAND))
	me.set_state(st)
	me.queue_redraw()

	# Auto pickup of own drops while walking over them.
	for id in ground.keys():
		if ground[id].position.distance_to(me.position) < PICKUP_RANGE * 0.5:
			Net.send("pickup", {"id": id})

	_send_t += delta
	var cur := {"x": int(me.position.x), "y": int(me.position.y), "d": me.dir, "a": st}
	if _send_t >= SEND_EVERY and cur != _sent:
		_send_t = 0.0
		_sent = cur
		Net.send("move", cur)

	_waypoint_cd -= delta
	if _waypoint_cd <= 0.0 and ix != 0.0:
		for w in Assets.arr(map_view.map.get("waypoints")):
			var r := Rect2(w["min_x"], w["min_y"], int(w["max_x"]) - int(w["min_x"]), int(w["max_y"]) - int(w["min_y"])).grow(16)
			if r.has_point(me.position):
				Net.send("move", cur)
				Net.send("waypoint")
				_waypoint_cd = 1.5
				break
