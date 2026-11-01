extends Node
## Arena feature panels: dungeons (dg_*), black ball war, tournament (tour_*),
## super rank ladder (rank_*), leaderboards (top_*) and minigames (mini_*).

var world

const DUNGEONS := [["arena_treasure", "Bản Đồ Kho Báu"], ["arena_snake_way", "Con Đường Rắn Độc"],
	["arena_destron_gas", "Khí Gas Hủy Diệt"], ["arena_red_ribbon", "Doanh Trại Độc Nhãn"]]


func _ready() -> void:
	world.hud.add_button("Đấu trường", show_hub)


func handle(t: String, d) -> bool:
	match t:
		"dg_info":
			show_dungeon(d)
		"dg_bbw":
			show_black_ball(d)
		"tour_info":
			show_tournament(d)
		"rank_info":
			show_rank(d)
		"top_list":
			show_top(d)
		"mini_state":
			show_minigames(d)
		_:
			return false
	return true


func _label(box: Container, text: String) -> void:
	var l := Label.new()
	l.text = text
	l.autowrap_mode = TextServer.AUTOWRAP_WORD
	box.add_child(l)


func show_hub() -> void:
	var box: VBoxContainer = world.hud.popup("Đấu trường")
	for dg in DUNGEONS:
		var id: String = dg[0]
		world.hud.row(box, dg[1], [["Xem", func(): Net.send("dg_info", {"id": id})]])
	world.hud.row(box, "Ngọc rồng sao đen", [["Xem", func(): Net.send("dg_bbw_info")]])
	world.hud.row(box, "Đại hội võ thuật", [["Xem", func(): Net.send("tour_info")]])
	world.hud.row(box, "Giải siêu hạng", [["Xem", func(): Net.send("rank_info")]])
	world.hud.row(box, "Bảng xếp hạng", [["Xem", func(): Net.send("top_get", {"kind": "power"})]])
	world.hud.row(box, "Trò chơi may mắn", [["Xem", func(): Net.send("mini_info")]])


func show_dungeon(d: Dictionary) -> void:
	var box: VBoxContainer = world.hud.popup(str(d.get("name", "Phó bản")))
	var id := str(d.get("id", ""))
	var info := "Thời gian: %d phút" % int(d.get("minutes", 30))
	if d.get("clan_only", false):
		info += "\nChỉ dành cho bang hội"
	if d.get("leader_only", false):
		info += "\nChỉ bang chủ được mở"
	if int(d.get("min_power", 0)) > 0:
		info += "\nYêu cầu sức mạnh %d" % int(d["min_power"])
	if d.has("item"):
		info += "\nCần: %s" % str(d["item"])
	_label(box, info)
	if d.get("running", false):
		_label(box, "Bang đang ở cấp %d, còn %d giây" % [int(d.get("level", 0)), int(d.get("left_s", 0))])
		var go := Button.new()
		go.text = "Vào ngay"
		go.pressed.connect(func():
			world.hud.close_panel()
			Net.send("dg_open", {"id": id, "level": 0}))
		box.add_child(go)
		return
	var row := HBoxContainer.new()
	box.add_child(row)
	var lv := SpinBox.new()
	if d.get("leveled", false):
		lv.min_value = int(d.get("min_level", 1))
		lv.max_value = int(d.get("max_level", 110))
		lv.value = lv.min_value
		row.add_child(lv)
	var open := Button.new()
	open.text = "Mở phó bản"
	open.pressed.connect(func():
		world.hud.close_panel()
		Net.send("dg_open", {"id": id, "level": int(lv.value)}))
	row.add_child(open)


func show_black_ball(d: Dictionary) -> void:
	var box: VBoxContainer = world.hud.popup("Ngọc rồng sao đen")
	var state := "Đang diễn ra" if d.get("open", false) else "Mở lúc %dh-%dh" % [int(d.get("hour_open", 20)), int(d.get("hour_close", 21))]
	if d.get("can_pick", false):
		state += " (đã có thể nhặt ngọc)"
	_label(box, state)
	var planets: Array = d.get("planets", []) if d.get("planets") is Array else []
	for i in planets.size():
		var idx := i
		world.hud.row(box, "%d sao — %s" % [i + 1, str(planets[i])], [["Đến", func(): Net.send("dg_bbw_go", {"i": idx})]])
	var buffs = d.get("buffs")
	if buffs is Array and not buffs.is_empty():
		_label(box, "— Phần thưởng đang có —")
		for b in buffs:
			_label(box, "Ngọc %d sao đen: còn %d phút" % [int(b["star"]), int(b["left_s"]) / 60])


func show_tournament(d: Dictionary) -> void:
	var box: VBoxContainer = world.hud.popup("Đại hội võ thuật")
	var cup := str(d.get("cup", ""))
	if cup == "":
		_label(box, "Hiện không có giải đấu. Giải tiếp theo lúc %dh" % int(d.get("next_hour", 8)))
	else:
		_label(box, "Giải %s — phí %d ngọc, %d vàng — %d người đăng ký\nThi đấu lúc :%d" % [cup, int(d.get("fee_gem", 0)),
			int(d.get("fee_gold", 0)), int(d.get("count", 0)), int(d.get("start_minute", 30))])
	if int(d.get("round", 0)) > 0:
		_label(box, "Đang thi đấu vòng %d%s" % [int(d["round"]), " — bạn đang chờ vòng sau" if d.get("waiting", false) else ""])
	if d.get("can_reg", false):
		if d.get("registered", false):
			world.hud.row(box, "Bạn đã đăng ký", [["Hủy đăng ký", func(): Net.send("tour_unreg")]])
		else:
			world.hud.row(box, "Đăng ký thi đấu", [["Đăng ký", func(): Net.send("tour_reg")]])
	var sched = d.get("schedule")
	if sched is Array:
		_label(box, "Lịch thi đấu:\n" + "\n".join(PackedStringArray(sched)))
	var champs = d.get("champs")
	if champs is Array and not champs.is_empty():
		_label(box, "Vô địch hôm nay: " + ", ".join(PackedStringArray(champs)))


func _rank_line(r: Dictionary) -> String:
	return "#%d %s — HP %d, SĐ %d — %d:%d" % [int(r["rank"]), r["name"], int(r["hp"]), int(r["dmg"]), int(r["win"]), int(r["lose"])]


func show_rank(d: Dictionary) -> void:
	var box: VBoxContainer = world.hud.popup("Giải siêu hạng")
	var me: Dictionary = d.get("me", {})
	_label(box, "Hạng của bạn: %d   Vé miễn phí: %d (sau đó %d ngọc/trận)" % [int(me.get("rank", 0)), int(d.get("tickets", 0)), int(d.get("gem_cost", 1))])
	if int(d.get("daily_gems", 0)) > 0 and not d.get("claimed", false):
		world.hud.row(box, "Thưởng hạng: %d ngọc/ngày" % int(d["daily_gems"]), [["Nhận", func(): Net.send("rank_claim")]])
	for list_name in [["top", "— Top 10 —"], ["near", "— Đối thủ gần bạn —"]]:
		_label(box, list_name[1])
		var rows = d.get(list_name[0])
		if not rows is Array:
			continue
		for r in rows:
			var rid := int(r["id"])
			var btns := []
			if rid != int(me.get("id", 0)) and int(r["rank"]) < int(me.get("rank", 0)):
				btns = [["Thách đấu", func():
					world.hud.close_panel()
					Net.send("rank_challenge", {"id": rid})]]
			world.hud.row(box, _rank_line(r), btns)


func show_top(d: Dictionary) -> void:
	var box: VBoxContainer = world.hud.popup(str(d.get("name", "Bảng xếp hạng")))
	var tabs := HFlowContainer.new()
	box.add_child(tabs)
	var kinds = d.get("kinds")
	if kinds is Array:
		for k in kinds:
			var kid := str(k["id"])
			var b := Button.new()
			b.text = str(k["name"])
			b.pressed.connect(func(): Net.send("top_get", {"kind": kid}))
			tabs.add_child(b)
	var rows = d.get("rows")
	if not rows is Array or rows.is_empty():
		_label(box, "Chưa có dữ liệu")
		return
	for r in rows:
		var extra := str(r.get("extra", ""))
		_label(box, "%d. %s — %d%s" % [int(r["rank"]), r["name"], int(r["value"]), "" if extra == "" else " (" + extra + ")"])


func show_minigames(d: Dictionary) -> void:
	var box: VBoxContainer = world.hud.popup("Trò chơi may mắn")
	var lucky = d.get("lucky")
	if lucky is Array:
		for g in lucky:
			var gid := str(g["id"])
			var phase := "đặt cược còn %ds" % int(g["left"]) if g["betting"] else "quay số sau %ds" % int(g["left"])
			var mine: Array = g["mine"] if g.get("mine") is Array else []
			_label(box, "%s — %s\nGiá %d, thưởng %d. Kết quả trước: %d. Số của bạn: %s" % [g["name"], phase, int(g["cost"]),
				int(g["reward"]), int(g["last"]), ", ".join(PackedStringArray(mine.map(func(x): return str(int(x)))))])
			var row := HBoxContainer.new()
			box.add_child(row)
			var n := SpinBox.new()
			n.min_value = 0
			n.max_value = 99
			row.add_child(n)
			for spec in [["Chọn số", -3], ["Số lẻ", -1], ["Số chẵn", -2]]:
				var code: int = spec[1]
				var b := Button.new()
				b.text = spec[0]
				b.pressed.connect(func(): Net.send("mini_lucky", {"game": gid, "n": int(n.value) if code == -3 else code}))
				row.add_child(b)
	var choose = d.get("choose")
	if choose is Array:
		for g in choose:
			var gid2 := str(g["id"])
			var unit := "ngọc" if g["gem"] else "vàng"
			_label(box, "%s — còn %ds\nThường: %d %s (bạn %d, %d%%)  VIP: %d %s (bạn %d, %d%%)" % [g["name"], int(g["left"]),
				int(g["normal_pool"]), unit, int(g["normal_mine"]), int(g.get("normal_chance", 0)),
				int(g["vip_pool"]), unit, int(g["vip_mine"]), int(g.get("vip_chance", 0))])
			world.hud.row(box, "Đặt cược", [
				["Thường (%d)" % int(g["normal_cost"]), func(): Net.send("mini_choose", {"game": gid2, "vip": false})],
				["VIP (%d)" % int(g["vip_cost"]), func(): Net.send("mini_choose", {"game": gid2, "vip": true})]])
	var refresh := Button.new()
	refresh.text = "Cập nhật"
	refresh.pressed.connect(func(): Net.send("mini_info"))
	box.add_child(refresh)
