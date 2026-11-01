extends Node
## WebSocket connection to the Go server. Messages are JSON {"t": type, "d": payload}.

signal message(t: String, d)
signal connected
signal disconnected(reason: String)

var host := "127.0.0.1:8765"
var ws := WebSocketPeer.new()
var _last_state := WebSocketPeer.STATE_CLOSED
var _ping_timer := 0.0


## host is "127.0.0.1:8765" (plain) or "https://game.example.com" (TLS).
func http_base() -> String:
	if host.begins_with("https://") or host.begins_with("http://"):
		return host.trim_suffix("/")
	return "http://" + host


func ws_url() -> String:
	var base := http_base()
	return ("wss://" + base.substr(8) if base.begins_with("https://") else "ws://" + base.substr(7)) + "/ws"


func connect_to_server() -> void:
	ws = WebSocketPeer.new()
	ws.inbound_buffer_size = 1 << 22
	var err := ws.connect_to_url(ws_url())
	if err != OK:
		disconnected.emit("Không kết nối được máy chủ")


func is_open() -> bool:
	return ws.get_ready_state() == WebSocketPeer.STATE_OPEN


func send(t: String, d = null) -> void:
	if not is_open():
		return
	var msg := {"t": t}
	if d != null:
		msg["d"] = d
	ws.send_text(JSON.stringify(msg))


func _process(delta: float) -> void:
	ws.poll()
	var state := ws.get_ready_state()
	if state != _last_state:
		_last_state = state
		if state == WebSocketPeer.STATE_OPEN:
			connected.emit()
		elif state == WebSocketPeer.STATE_CLOSED:
			var reason := ws.get_close_reason()
			disconnected.emit(reason if reason != "" else "Mất kết nối")
	if state != WebSocketPeer.STATE_OPEN:
		return
	while ws.get_available_packet_count() > 0:
		var m = JSON.parse_string(ws.get_packet().get_string_from_utf8())
		if m is Dictionary and m.has("t"):
			message.emit(m["t"], m.get("d"))
	_ping_timer += delta
	if _ping_timer > 15.0:
		_ping_timer = 0.0
		send("ping")
