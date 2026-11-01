extends Node
## Skill effects, timed buffs, charms, intrinsics (nội tại) and radar cards.
## Server messages: eff, buff_timers, buff_intrinsic, buff_intrinsic_all, buff_radar.
## Key K casts the selected skill without a target (Thái Dương Hạ San, khiên, biến khỉ...).

var world

const EFF_NAMES := {
	"stun": "Choáng", "sleep": "Ngủ", "hold": "Bị trói", "blind": "Choáng", "binh": "Bị nhốt",
	"transform": "Đang biến hình", "monkey": "Biến khỉ", "shield": "Khiên năng lượng",
	"socola": "Sôcôla", "huyt_sao": "Huýt sáo", "charge": "Tái tạo năng lượng", "egg": "Đẻ trứng",
	"prepare": "Đang tụ lực", "gong": "Đang gồng", "tdhs": "Thái Dương Hạ San", "heal": "Hồi phục",
	"explode": "Tự phát nổ",
}

var _intrinsic := {}
var _timers: Array = []
var _timer_panel_open := false


func _ready() -> void:
	if world == null or world.hud == null:
		return
	world.hud.add_button("Buff", func():
		_timer_panel_open = true
		Net.send("buff_timers"))
	world.hud.add_button("Nội tại", func(): Net.send("buff_intrinsic_menu"))
	world.hud.add_button("Thẻ", func(): Net.send("buff_radar"))


func _unhandled_input(e: InputEvent) -> void:
	if not (e is InputEventKey and e.pressed and not e.echo) or world == null or world.hud.typing():
		return
	if e.keycode == KEY_K and world.hud.selected_skill >= 0:
		Net.send("skill", {"id": world.hud.selected_skill})
		get_viewport().set_input_as_handled()


func handle(t: String, d) -> bool:
	match t:
		"eff":
			_on_eff(d)
			return true
		"buff_timers":
			_timers = Assets.arr(d.get("list"))
			if _timer_panel_open:
				_show_timers()
			return true
		"buff_intrinsic":
			_intrinsic = d
			return true
		"buff_intrinsic_all":
			_show_intrinsic_all(Assets.arr(d.get("list")))
			return true
		"buff_radar":
			_show_radar(Assets.arr(d.get("cards")))
			return true
	return false


func _on_eff(d: Dictionary) -> void:
	var kind := str(d.get("kind", ""))
	var ms := int(d.get("ms", 0))
	var label: String = EFF_NAMES.get(kind, kind)
	var node = null
	if d.has("id"):
		node = world._char(int(d["id"]))
	elif d.has("mob"):
		node = world.mobs.get(int(d["mob"]))
	elif d.has("boss"):
		node = world.bosses.get(str(d["boss"]))
	if node and is_instance_valid(node):
		if ms > 0:
			world._float(node.position, label, Color.SKY_BLUE)
			if kind in ["stun", "sleep", "hold", "blind", "binh"]:
				node.modulate = Color(0.7, 0.7, 1.0)
		elif kind in ["stun", "sleep", "hold", "blind", "binh"]:
			node.modulate = Color.WHITE
	var me_id := int(world.self_info.get("id", -1))
	if d.has("id") and int(d["id"]) == me_id:
		if ms > 0:
			world.log_line("%s (%ds)" % [label, int(ceil(ms / 1000.0))], "skyblue")
		else:
			world.log_line("Hết hiệu lực: " + label, "gray")


func _fmt(ms: int) -> String:
	var s := int(ms / 1000)
	if s >= 3600:
		return "%dh%02dm" % [int(s / 3600), int(s / 60) % 60]
	return "%dm%02ds" % [int(s / 60), s % 60]


func _show_timers() -> void:
	_timer_panel_open = false
	var box: VBoxContainer = world.hud.popup("Hiệu lực đang có")
	if _intrinsic.size() > 0:
		world.hud.row(box, "Nội tại: " + str(_intrinsic.get("name", "")), [["Đổi", func(): Net.send("buff_intrinsic_menu")]])
	if _timers.is_empty():
		world.hud.row(box, "Không có bùa hay vật phẩm hỗ trợ nào", [])
	for r in _timers:
		var kind := str(r.get("kind", ""))
		var name := str(r.get("name", ""))
		if kind == "effect":
			name = EFF_NAMES.get(name, name)
		var tag: String = {"item": "[Vật phẩm]", "charm": "[Bùa]", "effect": "[Hiệu ứng]"}.get(kind, "")
		world.hud.row(box, "%s %s — còn %s" % [tag, name, _fmt(int(r.get("ms", 0)))], [])


func _show_intrinsic_all(list: Array) -> void:
	var box: VBoxContainer = world.hud.popup("Tất cả nội tại")
	for s in list:
		world.hud.row(box, str(s), [])
	world.hud.row(box, "Giá mở: %d vàng  |  VIP: %d ngọc" % [int(_intrinsic.get("cost", 0)), int(_intrinsic.get("vip_gem", 0))], [
		["Mở", func(): Net.send("buff_intrinsic_open", {"vip": false})],
		["Mở VIP", func(): Net.send("buff_intrinsic_open", {"vip": true})]])


func _show_radar(cards: Array) -> void:
	var box: VBoxContainer = world.hud.popup("Sưu tầm thẻ")
	var opt_names: Dictionary = Assets.data.get("options", {})
	for c in cards:
		var lvl := int(c.get("level", 0))
		var text := "%s  (cấp %s, %d/%d)" % [str(c.get("name", "")), "-" if lvl == 0 else str(max(lvl, 0)), int(c.get("amount", 0)), int(c.get("max", 0))]
		var tier: int = max(lvl, 0)
		for o in Assets.arr(c.get("options")):
			if int(o.get("activeCard", 0)) == tier:
				var on := str(opt_names.get(str(int(o.get("id", 0))), "?"))
				text += "\n  " + on.replace("#", str(int(o.get("param", 0))))
		var buttons: Array = []
		if lvl != 0:
			var id := int(c.get("id", 0))
			buttons.append(["Bỏ dùng" if c.get("used", false) else "Dùng", func(): Net.send("buff_radar_use", {"id": id})])
		world.hud.row(box, text, buttons)
