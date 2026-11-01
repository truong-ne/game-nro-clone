extends Node
## Screen flow: login/register -> (create character) -> game.

const GameWorldScript := preload("res://scripts/game_world.gd")
const GENDERS := ["Trái Đất", "Namếc", "Xayda"]

var ui: Control
var world: Node2D
var _status: Label
var _pending_auth := {}
var _heads := []


func _ready() -> void:
	Net.connected.connect(func(): Assets.load_data())
	Net.disconnected.connect(_on_disconnected)
	Net.message.connect(_on_message)
	Assets.data_ready.connect(_send_auth)
	Assets.data_failed.connect(func(e): _set_status(e))
	_show_login()
	var args := OS.get_cmdline_user_args()
	var i := args.find("--smoke")
	if i >= 0 and args.size() > i + 2:
		_start_smoke(args[i + 1], args[i + 2])


# --- headless smoke test: godot --headless -- --smoke host:port username ---
var _smoke_user := ""


func _start_smoke(host: String, user: String) -> void:
	_smoke_user = user
	print("SMOKE start ", host, " ", user)
	Net.host = host
	_pending_auth = {"t": "register", "d": {"username": user, "password": "smoke-pass"}}
	Net.connect_to_server()
	get_tree().create_timer(45.0).timeout.connect(func():
		print("SMOKE timeout")
		get_tree().quit(1))


func _smoke_on_message(t: String, d) -> void:
	match t:
		"error":
			print("SMOKE server error: ", d)
			if str(d.get("text", "")).contains("tồn tại") and world == null:
				Net.send("login", {"username": _smoke_user, "password": "smoke-pass"})
		"need_char":
			Net.send("create_char", {"name": _smoke_user.to_lower().left(10), "gender": 0, "head": 64})
		"map":
			print("SMOKE map ", d["map_id"], " zone ", d["zone"])
			get_tree().create_timer(4.0).timeout.connect(_smoke_finish)


func _smoke_finish() -> void:
	if world == null or world.me == null:
		print("SMOKE no player")
		get_tree().quit(1)
		return
	Input.action_press("move_right")
	await get_tree().create_timer(1.0).timeout
	Input.action_release("move_right")
	Net.send("chat", {"text": "smoke test"})
	Net.send("inventory")
	await get_tree().create_timer(0.5).timeout
	print("SMOKE inventory panel=", world.hud._panel != null)
	Net.send("task")
	await get_tree().create_timer(0.5).timeout
	print("SMOKE skills=", world.hud._skills, " mobs=", world.mobs.size(), " hp=", world.self_info.get("hp"))
	if world.hud._panel == null or world.hud._skills.is_empty():
		print("SMOKE ui missing")
		get_tree().quit(1)
		return
	print("SMOKE player at ", world.me.position, " tiles=", world.map_view.width, "x", world.map_view.height,
		" others=", world.others.size(), " entities=", world.entities.get_child_count())
	print("SMOKE_OK")
	get_tree().quit(0)


func _clear_ui() -> void:
	if ui:
		ui.queue_free()
	ui = Control.new()
	ui.set_anchors_preset(Control.PRESET_FULL_RECT)
	var layer := CanvasLayer.new()
	layer.layer = 10
	add_child(layer)
	layer.add_child(ui)
	ui.tree_exited.connect(layer.queue_free)


func _panel(title: String) -> VBoxContainer:
	var bg := ColorRect.new()
	bg.color = Color("1b2a3a")
	bg.set_anchors_preset(Control.PRESET_FULL_RECT)
	ui.add_child(bg)
	var center := CenterContainer.new()
	center.set_anchors_preset(Control.PRESET_FULL_RECT)
	ui.add_child(center)
	var box := VBoxContainer.new()
	box.custom_minimum_size = Vector2(360, 0)
	box.add_theme_constant_override("separation", 10)
	center.add_child(box)
	var l := Label.new()
	l.text = title
	l.horizontal_alignment = HORIZONTAL_ALIGNMENT_CENTER
	l.add_theme_font_size_override("font_size", 28)
	box.add_child(l)
	return box


func _field(box: Container, placeholder: String, secret := false) -> LineEdit:
	var e := LineEdit.new()
	e.placeholder_text = placeholder
	e.secret = secret
	box.add_child(e)
	return e


func _button(box: Container, text: String, cb: Callable) -> Button:
	var b := Button.new()
	b.text = text
	b.pressed.connect(cb)
	box.add_child(b)
	return b


func _set_status(t: String) -> void:
	if _status:
		_status.text = t


func _show_login() -> void:
	_clear_ui()
	var box := _panel("Ngọc Rồng Online")
	var host := _field(box, "Máy chủ (host:port)")
	host.text = Net.host
	var user := _field(box, "Tài khoản")
	var pw := _field(box, "Mật khẩu", true)
	var row := HBoxContainer.new()
	box.add_child(row)
	var go := func(kind: String):
		Net.host = host.text.strip_edges()
		_pending_auth = {"t": kind, "d": {"username": user.text.strip_edges(), "password": pw.text}}
		_set_status("Đang kết nối...")
		if Net.is_open() and not Assets.data.is_empty():
			_send_auth()
		else:
			Net.connect_to_server()
	_button(row, "Đăng nhập", go.bind("login")).size_flags_horizontal = Control.SIZE_EXPAND_FILL
	_button(row, "Đăng ký", go.bind("register")).size_flags_horizontal = Control.SIZE_EXPAND_FILL
	pw.text_submitted.connect(func(_t): go.call("login"))
	_status = Label.new()
	_status.autowrap_mode = TextServer.AUTOWRAP_WORD
	box.add_child(_status)


func _send_auth() -> void:
	if _pending_auth.is_empty():
		return
	Net.send(_pending_auth["t"], _pending_auth["d"])
	_pending_auth = {}


func _show_create() -> void:
	_clear_ui()
	var box := _panel("Tạo nhân vật")
	var name_e := _field(box, "Tên (5-10 ký tự, chữ thường và số)")
	var gender := OptionButton.new()
	for g in GENDERS:
		gender.add_item(g)
	box.add_child(gender)
	var hair := OptionButton.new()
	box.add_child(hair)
	var fill_hair := func(g: int):
		hair.clear()
		if g < _heads.size():
			for i in _heads[g].size():
				hair.add_item("Kiểu tóc %d" % (i + 1), int(_heads[g][i]))
	gender.item_selected.connect(fill_hair)
	fill_hair.call(0)
	_button(box, "Tạo", func():
		Net.send("create_char", {"name": name_e.text.strip_edges(), "gender": gender.selected,
			"head": hair.get_selected_id()}))
	_status = Label.new()
	_status.autowrap_mode = TextServer.AUTOWRAP_WORD
	box.add_child(_status)


func _on_message(t: String, d) -> void:
	if _smoke_user != "":
		_smoke_on_message(t, d)
	match t:
		"error":
			_set_status(str(d["text"]))
		"need_char":
			_heads = d["heads"]
			_show_create()
		"login_ok":
			if ui:
				ui.queue_free()
				ui = null
				_status = null
			if world == null:
				world = GameWorldScript.new()
				add_child(world)


func _on_disconnected(reason: String) -> void:
	if world:
		world.queue_free()
		world = null
	_show_login()
	_set_status("Mất kết nối: " + reason)
