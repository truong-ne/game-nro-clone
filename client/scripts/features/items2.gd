extends Node
## Item economy panels: crafting at Bà Hạt Mít (craft_*), lucky wheel at
## Thượng Đế (lucky_*), consignment shop (consign_*), power limit (limit_*),
## plus bag/mount looks (look2).

var world

var _mode := "" # panel waiting for the next "inventory" message: craft, consign
var _recipes: Array = []
var _inv: Dictionary = {}
var _kind := "upgrade"
var _slot := -1
var _slot2 := -1
var _protect := false
var _info := ""
var _consign: Dictionary = {}
var _lucky: Dictionary = {}
var _fresh := false # a panel of ours was just requested

const KINDS := [["upgrade", "Nâng cấp"], ["crystallize", "Pha lê hoá"], ["socket", "Ép sao"],
	["earring", "Bông tai"], ["earring_opt", "Chỉ số bông tai"], ["disassemble", "Phân rã"], ["recipe", "Chế tạo"]]


func handle(t: String, d) -> bool:
	match t:
		"craft_open":
			_recipes = Assets.arr(d.get("recipes"))
			_mode = "craft"
			_fresh = true
			_info = ""
			return true
		"inventory":
			if _mode == "" or (world.hud._panel == null and not _fresh):
				_mode = ""
				return false
			_fresh = false
			_inv = d
			if _mode == "craft":
				_show_craft()
			elif _mode == "consign_sell":
				_show_consign_sell()
			else:
				return false
			return true
		"craft_info":
			_info = str(d.get("text", ""))
			if not bool(d.get("ok", false)):
				_info += "\n[!] " + str(d.get("error", ""))
			_show_craft()
			return true
		"craft_result":
			_info = str(d.get("text", ""))
			world.log_line(_info, "lightgreen" if d.get("ok") else "tomato")
			return true # an inventory message follows and redraws the panel
		"lucky_open":
			_lucky = d
			_show_lucky()
			return true
		"lucky_result":
			var names := []
			for s in Assets.arr(d.get("items")):
				names.append(_stack_name(s))
			world.log_line("Vòng quay: " + ", ".join(names), "orange")
			_lucky["box"] = int(d.get("box", 0))
			_show_lucky()
			return true
		"lucky_box":
			_show_lucky_box(Assets.arr(d.get("items")))
			return true
		"consign_open":
			_consign = d
			_mode = ""
			Net.send("consign_list", {"tab": 0, "page": 0})
			return true
		"consign_list":
			_show_consign_list(d)
			return true
		"consign_mine":
			_show_consign_mine(Assets.arr(d.get("items")))
			return true
		"limit_info":
			world.log_line("Giới hạn sức mạnh: bậc %d/%d" % [int(d.get("limit", 0)), int(d.get("max", 0))], "yellow")
			return true
		"look2":
			var c = world._char(int(d.get("id", 0)))
			if c:
				c.set_meta("bag", int(d.get("bag", -1)))
				c.set_meta("mount", int(d.get("mount", -1)))
			return true
	return false


func _ready() -> void:
	if world and world.hud:
		world.hud.add_button("Giới hạn SM", func(): Net.send("limit_info"))


func _stack_name(s: Dictionary) -> String:
	var t: Dictionary = Assets.data.get("items", {}).get(str(int(s.get("tid", 0))), {})
	var q := int(s.get("qty", 1))
	return ("%d " % q if q > 1 else "") + str(t.get("name", "?"))


func _bag_items() -> Array:
	var out := []
	for it in Assets.arr(_inv.get("items")):
		if int(it["container"]) == 1:
			out.append(it)
	return out


# --- crafting ---

func _req() -> Dictionary:
	return {"kind": _kind, "slot": _slot, "slot2": _slot2, "protect": _protect, "recipe": _recipe_id(), "n": 1}


var _recipe_idx := 0


func _recipe_id() -> String:
	if _recipes.is_empty():
		return ""
	return str(_recipes[clampi(_recipe_idx, 0, _recipes.size() - 1)]["id"])


func _show_craft() -> void:
	var hud = world.hud
	var box: VBoxContainer = hud.popup("Bà Hạt Mít — Vàng %d  Ngọc %d" % [int(_inv.get("gold", 0)), int(_inv.get("gem", 0))])
	var kinds := HBoxContainer.new()
	box.add_child(kinds)
	for k in KINDS:
		var b := Button.new()
		b.text = k[1]
		b.toggle_mode = true
		b.button_pressed = k[0] == _kind
		var kind: String = k[0]
		b.pressed.connect(func():
			_kind = kind
			_slot = -1
			_slot2 = -1
			_info = ""
			_show_craft())
		kinds.add_child(b)
	if _kind == "recipe":
		for i in _recipes.size():
			var idx := i
			hud.row(box, "%s%s" % ["▶ " if i == _recipe_idx else "", _recipes[i]["name"]], [["Chọn", func():
				_recipe_idx = idx
				Net.send("craft_info", _req())]])
	else:
		if _kind == "upgrade":
			var pr := CheckBox.new()
			pr.text = "Dùng đá bảo vệ"
			pr.button_pressed = _protect
			pr.toggled.connect(func(on): _protect = on)
			box.add_child(pr)
		for it in _bag_items():
			var slot := int(it["slot"])
			var mark := ""
			if slot == _slot:
				mark = "[1] "
			elif slot == _slot2:
				mark = "[2] "
			var btns := [["Chọn", func():
				_slot = slot
				Net.send("craft_info", _req())]]
			if _kind == "socket":
				btns.append(["Đá", func():
					_slot2 = slot
					Net.send("craft_info", _req())])
			hud.row(box, mark + hud.item_name(it), btns)
	var info := Label.new()
	info.autowrap_mode = TextServer.AUTOWRAP_WORD
	info.text = _info
	box.add_child(info)
	var act := HBoxContainer.new()
	box.add_child(act)
	var counts := [1]
	if _kind == "crystallize":
		counts = [1, 10, 100]
	for n in counts:
		var b := Button.new()
		b.text = "Thực hiện" if n == 1 else "x%d" % n
		var nn: int = n
		b.pressed.connect(func():
			var r := _req()
			r["n"] = nn
			_mode = "craft"
			Net.send("craft_do", r))
		act.add_child(b)
	var close := Button.new()
	close.text = "Xong"
	close.pressed.connect(func():
		_mode = ""
		hud.close_panel())
	act.add_child(close)


# --- lucky wheel ---

func _show_lucky() -> void:
	var hud = world.hud
	var box: VBoxContainer = hud.popup("Vòng quay may mắn — rương phụ %d/%d" % [int(_lucky.get("box", 0)), int(_lucky.get("box_max", 100))])
	hud.row(box, "Quay bằng vàng (%d / lượt)" % int(_lucky.get("price_gold", 0)), [
		["x1", func(): Net.send("lucky_spin", {"count": 1, "currency": "gold"})],
		["x10", func(): Net.send("lucky_spin", {"count": 10, "currency": "gold"})]])
	hud.row(box, "Quay bằng ngọc (%d / lượt)" % int(_lucky.get("price_gem", 0)), [
		["x1", func(): Net.send("lucky_spin", {"count": 1, "currency": "gem"})],
		["x10", func(): Net.send("lucky_spin", {"count": 10, "currency": "gem"})]])
	hud.row(box, "Quay bằng vé (có %d)" % int(_lucky.get("tickets", 0)), [
		["x1", func(): Net.send("lucky_spin", {"count": 1, "currency": "ticket"})]])
	hud.row(box, "Rương phụ", [["Xem", func(): Net.send("lucky_box")]])


func _show_lucky_box(items: Array) -> void:
	var hud = world.hud
	var box: VBoxContainer = hud.popup("Rương phụ (%d món)" % items.size())
	hud.row(box, "", [["Nhận hết", func(): Net.send("lucky_claim", {"i": -1})],
		["Xoá hết", func(): hud.confirm("Xoá hết vật phẩm trong rương phụ?", func(): Net.send("lucky_clear"))]])
	for i in items.size():
		var idx := i
		hud.row(box, _stack_name(items[i]), [["Nhận", func(): Net.send("lucky_claim", {"i": idx})]])


# --- consignment ---

func _consign_header(box: VBoxContainer) -> void:
	var tabs := HBoxContainer.new()
	box.add_child(tabs)
	var names := Assets.arr(_consign.get("tabs"))
	for i in names.size():
		var tab := i
		var b := Button.new()
		b.text = str(names[i])
		b.pressed.connect(func(): Net.send("consign_list", {"tab": tab, "page": 0}))
		tabs.add_child(b)
	var mine := Button.new()
	mine.text = "Của tôi"
	mine.pressed.connect(func(): Net.send("consign_mine"))
	tabs.add_child(mine)
	var sell := Button.new()
	sell.text = "Ký gửi"
	sell.pressed.connect(func():
		_mode = "consign_sell"
		_fresh = true
		Net.send("inventory"))
	tabs.add_child(sell)


func _price_text(it: Dictionary) -> String:
	if int(it.get("price_bars", 0)) > 0:
		return "%d thỏi vàng" % int(it["price_bars"])
	return "%d ngọc" % int(it.get("price_gem", 0))


func _show_consign_list(d: Dictionary) -> void:
	var hud = world.hud
	var tab := int(d.get("tab", 0))
	var page := int(d.get("page", 0))
	var pages := int(d.get("pages", 1))
	var box: VBoxContainer = hud.popup("Ký gửi — trang %d/%d" % [page + 1, pages])
	_consign_header(box)
	for it in Assets.arr(d.get("items")):
		var id := int(it["id"])
		var bars := int(it.get("price_bars", 0))
		var price := bars if bars > 0 else int(it.get("price_gem", 0))
		var cur := "bar" if bars > 0 else "gem"
		hud.row(box, "%s\n  Giá: %s%s" % [hud.item_name(it), _price_text(it), " ★" if it.get("up_top") else ""],
			[["Mua", func(): Net.send("consign_buy", {"id": id, "price": price, "currency": cur})]])
	var nav := HBoxContainer.new()
	box.add_child(nav)
	if page > 0:
		var prev := Button.new()
		prev.text = "< Trước"
		prev.pressed.connect(func(): Net.send("consign_list", {"tab": tab, "page": page - 1}))
		nav.add_child(prev)
	if page + 1 < pages:
		var next := Button.new()
		next.text = "Sau >"
		next.pressed.connect(func(): Net.send("consign_list", {"tab": tab, "page": page + 1}))
		nav.add_child(next)


func _show_consign_mine(items: Array) -> void:
	var hud = world.hud
	var box: VBoxContainer = hud.popup("Vật phẩm ký gửi của tôi")
	_consign_header(box)
	for it in items:
		var id := int(it["id"])
		if it.get("sold"):
			hud.row(box, "%s\n  Đã bán: %s" % [hud.item_name(it), _price_text(it)],
				[["Nhận tiền", func(): Net.send("consign_claim", {"id": id})]])
		else:
			hud.row(box, "%s\n  Giá: %s" % [hud.item_name(it), _price_text(it)],
				[["Lên đầu", func(): Net.send("consign_top", {"id": id})], ["Huỷ", func(): Net.send("consign_cancel", {"id": id})]])


func _show_consign_sell() -> void:
	_mode = ""
	var hud = world.hud
	var box: VBoxContainer = hud.popup("Ký gửi (phí %d thỏi vàng, thuế %d%%)" % [int(_consign.get("fee_bars", 1)), int(_consign.get("tax_pct", 10))])
	_consign_header(box)
	var row := HBoxContainer.new()
	box.add_child(row)
	var qty := SpinBox.new()
	qty.min_value = 1
	qty.max_value = int(_consign.get("max_qty", 99))
	qty.prefix = "SL"
	row.add_child(qty)
	var price := SpinBox.new()
	price.min_value = 1
	price.max_value = 1000000
	price.prefix = "Giá"
	row.add_child(price)
	var cur := OptionButton.new()
	cur.add_item("Thỏi vàng")
	cur.add_item("Ngọc")
	row.add_child(cur)
	for it in _bag_items():
		var slot := int(it["slot"])
		hud.row(box, hud.item_name(it), [["Bán", func():
			Net.send("consign_sell", {"slot": slot, "qty": int(qty.value), "price": int(price.value),
				"currency": "bar" if cur.selected == 0 else "gem"})]])
