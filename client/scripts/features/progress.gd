extends Node
## Progression panels (server prog_*.go): daily side tasks, clan tasks,
## achievements, titles (badges) and dragon-ball wishes.

var world
var _buttons_added := false


func _process(_dt: float) -> void:
	if _buttons_added or world == null or world.hud == null or world.hud.menu_bar == null:
		return
	_buttons_added = true
	world.hud.add_button("NV ngày", func(): Net.send("side_info"))
	world.hud.add_button("NV bang", func(): Net.send("ctask_info"))
	world.hud.add_button("Thành tích", func(): Net.send("ach_list"))
	world.hud.add_button("Danh hiệu", func(): Net.send("badge_list"))
	set_process(false)


func handle(t: String, d) -> bool:
	match t:
		"side_task":
			_show_daily(d, "Nhiệm vụ hàng ngày", "side")
		"ctask":
			_show_daily(d, "Nhiệm vụ bang", "ctask")
		"achievements":
			_show_achievements(d)
		"badges":
			_show_badges(d)
		"dragon_summon":
			_show_summon(d)
		"dragon":
			_show_wishes(d)
		"dragon_state":
			world.log_line("Rồng thần: %s" % ("đang xuất hiện" if d.get("active", false) else "có thể gọi"), "yellow")
		"dragon_appear":
			world.log_line("Rồng thần đã xuất hiện!", "gold")
		"dragon_leave":
			world.log_line("Rồng thần đã bay đi", "gold")
		_:
			return false
	return true


func _label(box: Container, text: String) -> void:
	var l := Label.new()
	l.text = text
	l.autowrap_mode = TextServer.AUTOWRAP_WORD
	box.add_child(l)


func _show_daily(d: Dictionary, title: String, prefix: String) -> void:
	var box: VBoxContainer = world.hud.popup(title)
	_label(box, "Lượt còn lại hôm nay: %d/%d" % [int(d.get("left", 0)), int(d.get("per_day", 0))])
	if prefix == "ctask" and d.has("points"):
		_label(box, "Điểm bang: %d" % int(d.get("points", 0)))
	if d.get("active", false):
		var lv := str(d.get("level_name", ""))
		_label(box, "Nhiệm vụ hiện tại: %s%s\nĐã hoàn thành: %d/%d (%d%%)" % [str(d.get("name", "")),
			(" (" + lv + ")") if lv != "" else "", int(d.get("count", 0)), int(d.get("max", 0)), int(d.get("pct", 0))])
		var btns := []
		if d.get("done", false):
			btns.append(["Trả nhiệm vụ", func(): Net.send(prefix + "_pay")])
		else:
			btns.append(["Hủy nhiệm vụ", func(): world.hud.confirm("Bạn có chắc muốn hủy nhiệm vụ này?",
				func(): Net.send(prefix + "_cancel"))])
		world.hud.row(box, "", btns)
		return
	if prefix == "side":
		var names := ["Dễ", "Bình thường", "Khó", "Rất khó", "Địa ngục"]
		var btns2 := []
		for lv2 in Assets.arr(d.get("levels")):
			var level := int(lv2)
			btns2.append([names[clampi(level, 0, 4)], func(): Net.send("side_take", {"level": level})])
		world.hud.row(box, "Chọn cấp độ nhiệm vụ:", btns2)
	else:
		world.hud.row(box, "Chưa có nhiệm vụ bang", [["Nhận nhiệm vụ", func(): Net.send("ctask_take")]])


func _show_achievements(d: Dictionary) -> void:
	var box: VBoxContainer = world.hud.popup("Nhiệm vụ thành tích")
	for a in Assets.arr(d.get("list")):
		var i := int(a["i"])
		var btns := []
		if a.get("claimed", false):
			btns = [["Đã nhận", func(): pass]]
		elif a.get("done", false):
			btns = [["Nhận %d ngọc" % int(a["money"]), func(): Net.send("ach_claim", {"i": i})]]
		world.hud.row(box, "%s\n  %s (%d/%d)" % [a["name"], a["info"], int(a["count"]), int(a["max"])], btns)


func _show_badges(d: Dictionary) -> void:
	var box: VBoxContainer = world.hud.popup("Danh hiệu")
	world.hud.row(box, "", [["Tháo danh hiệu", func(): Net.send("badge_off")]])
	for b in Assets.arr(d.get("list")):
		var id := int(b["id"])
		var text := str(b["name"])
		if b.get("owned", false):
			text += " — còn %d ngày%s" % [int(b.get("days", 0)), " (đang dùng)" if b.get("active", false) else ""]
		if str(b.get("task", "")) != "":
			text += "\n  %s: %d/%d" % [b["task"], int(b.get("progress", 0)), int(b.get("max", 0))]
		var btns := []
		if b.get("owned", false) and not b.get("active", false):
			btns = [["Dùng", func(): Net.send("badge_use", {"id": id})]]
		world.hud.row(box, text, btns)


func _show_summon(d: Dictionary) -> void:
	var star := int(d.get("star", 1))
	var box: VBoxContainer = world.hud.popup(str(d.get("text", "Bạn muốn gọi rồng thần ?")))
	world.hud.row(box, str(d.get("help", "")), [])
	world.hud.row(box, "", [["Gọi Rồng Thần %d Sao" % star, func():
		world.hud.close_panel()
		Net.send("dragon_summon", {"star": star})]])


func _show_wishes(d: Dictionary) -> void:
	var kind := str(d.get("kind", "shenron"))
	var box: VBoxContainer = world.hud.popup("Rồng Thần Namek" if kind == "namek" else "Rồng Thần")
	_label(box, str(d.get("text", "")))
	var wishes := Assets.arr(d.get("wishes"))
	for i in wishes.size():
		var idx := i
		var label := str(wishes[i]).replace("\n", " ")
		world.hud.row(box, label, [["Ước", func():
			if label == "Điều ước khác":
				Net.send("dragon_wish", {"i": idx, "kind": kind})
			else:
				world.hud.confirm("Ngươi có chắc muốn ước? " + label,
					func(): Net.send("dragon_wish", {"i": idx, "kind": kind}))]])
