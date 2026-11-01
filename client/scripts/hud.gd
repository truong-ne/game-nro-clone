extends CanvasLayer
## In-game UI: stats, skill bar, chat, and popup panels (menu, inventory,
## shop, task, points, clan, friends, prompts, confirmations).

signal skill_selected(id: int)

var stats: Label
var chat_log: RichTextLabel
var chat_in: LineEdit
var zone_btn: OptionButton
var skill_bar: HBoxContainer
var target_label: Label
var _panel: PanelContainer
var _skills: Array = []
var selected_skill := -1
var self_info := {}
var shop_open := false
var menu_bar: HBoxContainer


## For feature plugins: add a button to the bottom bar.
func add_button(label: String, cb: Callable) -> void:
	var b := Button.new()
	b.text = label
	b.focus_mode = Control.FOCUS_NONE
	b.pressed.connect(cb)
	menu_bar.add_child(b)


## For feature plugins: open a titled popup and return its content box.
func popup(title: String) -> VBoxContainer:
	return _popup(title)


## For feature plugins: a row with a label and [label, callable] buttons.
func row(box: Container, text: String, buttons: Array) -> void:
	_row(box, text, buttons)


func _ready() -> void:
	stats = Label.new()
	stats.position = Vector2(12, 8)
	_outline(stats)
	add_child(stats)
	target_label = Label.new()
	target_label.position = Vector2(500, 8)
	_outline(target_label)
	add_child(target_label)
	zone_btn = OptionButton.new()
	zone_btn.position = Vector2(1100, 8)
	zone_btn.item_selected.connect(func(i): Net.send("zone", {"zone": i}))
	add_child(zone_btn)
	var bar := HBoxContainer.new()
	bar.position = Vector2(480, 660)
	add_child(bar)
	menu_bar = bar
	for spec in [["Túi (I)", "inventory"], ["Nhiệm vụ (T)", "task"], ["Điểm (P)", "points"], ["Bang (C)", "clan_info"],
			["Bạn (F)", "friends"], ["Đệ tử", "pet"]]:
		var b := Button.new()
		b.text = spec[0]
		b.focus_mode = Control.FOCUS_NONE
		b.pressed.connect(open_panel.bind(spec[1]))
		bar.add_child(b)
	skill_bar = HBoxContainer.new()
	skill_bar.position = Vector2(500, 620)
	add_child(skill_bar)
	chat_log = RichTextLabel.new()
	chat_log.position = Vector2(12, 470)
	chat_log.size = Vector2(460, 200)
	chat_log.bbcode_enabled = true
	chat_log.scroll_following = true
	chat_log.mouse_filter = Control.MOUSE_FILTER_IGNORE
	chat_log.add_theme_color_override("font_outline_color", Color.BLACK)
	chat_log.add_theme_constant_override("outline_size", 4)
	add_child(chat_log)
	chat_in = LineEdit.new()
	chat_in.position = Vector2(12, 676)
	chat_in.size = Vector2(460, 32)
	chat_in.placeholder_text = "Enter: chat khu  |  /w thế giới  |  /c bang"
	chat_in.max_length = 100
	chat_in.text_submitted.connect(_on_chat)
	add_child(chat_in)


func _outline(l: Label) -> void:
	l.add_theme_color_override("font_outline_color", Color.BLACK)
	l.add_theme_constant_override("outline_size", 4)


func _on_chat(t: String) -> void:
	t = t.strip_edges()
	if t.begins_with("/w "):
		Net.send("world_chat", {"text": t.substr(3)})
	elif t.begins_with("/c "):
		Net.send("clan_chat", {"text": t.substr(3)})
	elif t != "":
		Net.send("chat", {"text": t})
	chat_in.text = ""
	chat_in.release_focus()


func log_line(text: String, color := "white") -> void:
	chat_log.append_text("[color=%s]%s[/color]\n" % [color, text.replace("[", "[lb]")])


func typing() -> bool:
	return chat_in.has_focus() or (_panel != null and get_viewport().gui_get_focus_owner() is LineEdit)


func update_stats(s: Dictionary, map_name: String, zone: int) -> void:
	self_info = s
	if s.is_empty():
		return
	stats.text = "%s  |  %s - Khu %d\nHP %d/%d   KI %d/%d\nSức mạnh %d   Tiềm năng %d\nVàng %d   Ngọc %d" % [
		s["name"], map_name, zone, s["hp"], s["max_hp"], s["mp"], s["max_mp"], s["power"], s["potential"], s["gold"], s["gem"]]


func set_skills(skills: Array, gender: int) -> void:
	for c in skill_bar.get_children():
		c.queue_free()
	_skills = []
	for sk in skills:
		if int(sk["level"]) <= 0:
			continue
		_skills.append(int(sk["template_id"]))
	if selected_skill < 0 or not _skills.has(selected_skill):
		selected_skill = _skills[0] if not _skills.is_empty() else -1
	for i in _skills.size():
		var id: int = _skills[i]
		var info: Dictionary = Assets.data.get("skills", {}).get("%d_%d" % [gender, id], {})
		var b := Button.new()
		b.text = "%d %s" % [i + 1, str(info.get("name", "Skill")).left(10)]
		b.toggle_mode = true
		b.button_pressed = id == selected_skill
		b.focus_mode = Control.FOCUS_NONE
		b.pressed.connect(select_skill.bind(id))
		skill_bar.add_child(b)


func select_skill(id: int) -> void:
	selected_skill = id
	for i in skill_bar.get_child_count():
		(skill_bar.get_child(i) as Button).button_pressed = i < _skills.size() and _skills[i] == id
	skill_selected.emit(id)


func select_skill_index(i: int) -> void:
	if i < _skills.size():
		select_skill(_skills[i])


# --- popup panels ---

func close_panel() -> void:
	if _panel:
		_panel.queue_free()
		_panel = null
	if shop_open:
		shop_open = false
		Net.send("shop_close")


func _popup(title: String) -> VBoxContainer:
	close_panel()
	_panel = PanelContainer.new()
	_panel.position = Vector2(380, 90)
	_panel.custom_minimum_size = Vector2(520, 0)
	add_child(_panel)
	var scroll := ScrollContainer.new()
	scroll.custom_minimum_size = Vector2(520, 420)
	_panel.add_child(scroll)
	var box := VBoxContainer.new()
	box.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	scroll.add_child(box)
	var head := HBoxContainer.new()
	box.add_child(head)
	var l := Label.new()
	l.text = title
	l.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	l.add_theme_font_size_override("font_size", 20)
	head.add_child(l)
	var x := Button.new()
	x.text = "Đóng"
	x.pressed.connect(close_panel)
	head.add_child(x)
	return box


func _row(box: Container, text: String, buttons: Array) -> void:
	var row := HBoxContainer.new()
	box.add_child(row)
	var l := Label.new()
	l.text = text
	l.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	l.autowrap_mode = TextServer.AUTOWRAP_WORD
	row.add_child(l)
	for b in buttons: # [label, callable]
		var btn := Button.new()
		btn.text = b[0]
		btn.pressed.connect(b[1])
		row.add_child(btn)


func open_panel(kind: String) -> void:
	match kind:
		"inventory":
			Net.send("inventory")
		"task":
			Net.send("task")
		"points":
			show_points()
		"clan_info":
			Net.send("clan_info")
		"friends":
			Net.send("friends")
		"pet":
			show_pet()


func item_name(it: Dictionary) -> String:
	var t: Dictionary = Assets.data.get("items", {}).get(str(int(it["template_id"] if it.has("template_id") else it["tid"])), {})
	var s := str(t.get("name", "?"))
	var q := int(it.get("quantity", it.get("qty", 1)))
	if q > 1:
		s += " x%d" % q
	for o in Assets.arr(it.get("options")):
		var on := str(Assets.data.get("options", {}).get(str(int(o[0])), ""))
		if on != "":
			s += "\n  " + on.replace("#", str(int(o[1])))
	return s


func show_menu(d: Dictionary) -> void:
	var box := _popup(str(d["name"]))
	var l := Label.new()
	l.text = str(d["text"])
	l.autowrap_mode = TextServer.AUTOWRAP_WORD
	box.add_child(l)
	var opts := Assets.arr(d.get("options"))
	for i in opts.size():
		var b := Button.new()
		b.text = str(opts[i])
		b.pressed.connect(func():
			close_panel()
			Net.send("menu", {"i": i}))
		box.add_child(b)


func show_inventory(d: Dictionary) -> void:
	var box := _popup("Hành trang  —  Vàng %d  Ngọc %d" % [d["gold"], d["gem"]])
	var groups := {0: [], 1: [], 2: []}
	for it in Assets.arr(d.get("items")):
		groups[int(it["container"])].append(it)
	for g in [[0, "Đang mặc"], [1, "Túi"], [2, "Rương"]]:
		var h := Label.new()
		h.text = "— %s —" % g[1]
		box.add_child(h)
		for it in groups[g[0]]:
			var slot := int(it["slot"])
			var btns := []
			match g[0]:
				0:
					btns = [["Tháo", func(): Net.send("unequip", {"slot": slot})]]
				1:
					btns = [["Mặc", func(): Net.send("equip", {"slot": slot})], ["Dùng", func(): Net.send("use", {"slot": slot})],
						["Cất", func(): Net.send("box", {"from": 1, "slot": slot})]]
					if shop_open:
						btns.append(["Bán", func(): Net.send("shop_sell", {"slot": slot})])
					else:
						btns.append(["Vứt", func(): Net.send("discard", {"slot": slot})])
				2:
					btns = [["Lấy", func(): Net.send("box", {"from": 2, "slot": slot})]]
			_row(box, item_name(it), btns)


func show_shop(d: Dictionary) -> void:
	var box := _popup("Cửa hàng")
	shop_open = true
	var cur := {0: "vàng", 1: "ngọc", 3: "hồng ngọc", 4: "coupon"}
	for tab in Assets.arr(d.get("tabs")):
		var h := Label.new()
		h.text = "— %s —" % str(tab["name"]).replace("\n", " ")
		box.add_child(h)
		for it in Assets.arr(tab.get("items")):
			var id := int(it["id"])
			_row(box, "%s\n  Giá: %d %s" % [item_name(it), int(it["cost"]), cur.get(int(it["currency"]), "?")],
				[["Mua", func(): Net.send("shop_buy", {"id": id, "qty": 1})]])
	var inv := Button.new()
	inv.text = "Mở túi để bán"
	inv.pressed.connect(func(): Net.send("inventory"))
	box.add_child(inv)
	shop_open = true


func show_task(d: Dictionary) -> void:
	var box := _popup("Nhiệm vụ")
	var l := Label.new()
	l.autowrap_mode = TextServer.AUTOWRAP_WORD
	l.text = "%s\n\n%s\n\nHiện tại: %s (%d/%d)" % [d.get("name", ""), str(d.get("detail", "")).replace("%1", ""),
		d.get("sub", ""), int(d.get("count", 0)), int(d.get("max", 0))]
	box.add_child(l)


func show_points() -> void:
	var box := _popup("Cộng điểm tiềm năng: %d" % int(self_info.get("potential", 0)))
	var names := ["HP gốc", "KI gốc", "Sức đánh gốc", "Giáp gốc", "Chí mạng gốc"]
	var vals := [self_info.get("max_hp", 0), self_info.get("max_mp", 0), self_info.get("dmg", 0), self_info.get("def", 0), self_info.get("crit", 0)]
	for i in 5:
		var stat := i
		_row(box, "%s: %d" % [names[i], int(vals[i])], [
			["+1", func(): Net.send("add_point", {"stat": stat, "n": 1})],
			["+10", func(): Net.send("add_point", {"stat": stat, "n": 10})],
			["+100", func(): Net.send("add_point", {"stat": stat, "n": 100})]])


func show_pet() -> void:
	var box := _popup("Đệ tử")
	for spec in [["Đi theo", 0], ["Bảo vệ", 1], ["Tấn công", 2], ["Về nhà", 3]]:
		var st: int = spec[1]
		var b := Button.new()
		b.text = spec[0]
		b.pressed.connect(func(): Net.send("pet", {"status": st}))
		box.add_child(b)


func show_clan(d) -> void:
	if d == null:
		var box := _popup("Bang hội")
		var name_e := LineEdit.new()
		name_e.placeholder_text = "Tên bang (3-20 ký tự)"
		box.add_child(name_e)
		var b := Button.new()
		b.text = "Lập bang"
		b.pressed.connect(func(): Net.send("clan_create", {"name": name_e.text}))
		box.add_child(b)
		return
	var box2 := _popup("Bang %s (cấp %d)" % [d["name"], int(d["level"])])
	for m in Assets.arr(d.get("members")):
		var mid := int(m["id"])
		var role: String = ["Bang chủ", "Phó bang", "Thành viên"][clampi(int(m["role"]), 0, 2)]
		var btns := []
		if int(d["leader_id"]) == int(self_info.get("id", 0)) and mid != int(self_info.get("id", 0)):
			btns = [["Đuổi", func(): Net.send("clan_kick", {"id": mid})]]
		_row(box2, "%s — %s — SM %d" % [m["name"], role, int(m["power"])], btns)
	var leave := Button.new()
	leave.text = "Rời bang"
	leave.pressed.connect(func(): Net.send("clan_leave"))
	box2.add_child(leave)


func show_friends(list: Array) -> void:
	var box := _popup("Bạn bè")
	var row := HBoxContainer.new()
	box.add_child(row)
	var name_e := LineEdit.new()
	name_e.placeholder_text = "Tên nhân vật"
	name_e.size_flags_horizontal = Control.SIZE_EXPAND_FILL
	row.add_child(name_e)
	var add := Button.new()
	add.text = "Kết bạn"
	add.pressed.connect(func(): Net.send("friend_add", {"name": name_e.text}))
	row.add_child(add)
	for f in list:
		var fid := int(f["id"])
		_row(box, "%s %s — SM %d" % ["●" if f["online"] else "○", f["name"], int(f["power"])],
			[["Xoá", func(): Net.send("friend_remove", {"id": fid})]])


func prompt(d: Dictionary) -> void:
	var box := _popup(str(d.get("text", "")))
	var e := LineEdit.new()
	box.add_child(e)
	var ok := Button.new()
	ok.text = "Gửi"
	var cmd := str(d.get("cmd", ""))
	ok.pressed.connect(func():
		Net.send(cmd, {"code": e.text})
		close_panel())
	box.add_child(ok)
	e.grab_focus()


func confirm(text: String, yes: Callable) -> void:
	var box := _popup("Xác nhận")
	var l := Label.new()
	l.text = text
	l.autowrap_mode = TextServer.AUTOWRAP_WORD
	box.add_child(l)
	var row := HBoxContainer.new()
	box.add_child(row)
	var y := Button.new()
	y.text = "Đồng ý"
	y.pressed.connect(func():
		close_panel()
		yes.call())
	row.add_child(y)
	var n := Button.new()
	n.text = "Từ chối"
	n.pressed.connect(close_panel)
	row.add_child(n)


func show_revive() -> void:
	var box := _popup("Bạn đã bị hạ gục")
	var a := Button.new()
	a.text = "Hồi sinh tại chỗ (1 ngọc)"
	a.pressed.connect(func():
		close_panel()
		Net.send("revive", {"home": false}))
	box.add_child(a)
	var b := Button.new()
	b.text = "Về nhà"
	b.pressed.connect(func():
		close_panel()
		Net.send("revive", {"home": true}))
	box.add_child(b)


func show_trade(d: Dictionary, my_id: int) -> void:
	var box := _popup("Giao dịch")
	for side in ["a", "b"]:
		var s: Dictionary = d[side]
		var mine := int(s["id"]) == my_id
		var h := Label.new()
		h.text = "— %s %s — Vàng %d" % ["Bạn" if mine else "Đối phương", "(đã khoá)" if s["locked"] else "", int(s["gold"])]
		box.add_child(h)
		for it in Assets.arr(s.get("items")):
			_row(box, item_name(it), [])
	var row := HBoxContainer.new()
	box.add_child(row)
	var lock := Button.new()
	lock.text = "Khoá / Xác nhận"
	lock.pressed.connect(func(): Net.send("trade_lock"))
	row.add_child(lock)
	var cancel := Button.new()
	cancel.text = "Huỷ"
	cancel.pressed.connect(func(): Net.send("trade_cancel"))
	row.add_child(cancel)
