extends Node
## Loads the template bundle and lazily downloads images from the server's
## /assets/ folder, caching them under user://cache.

signal data_ready
signal data_failed(err: String)
signal texture_loaded(path: String)
signal texture_failed(path: String)

const ZOOM := 2 # asset set we download (x2); world coords are x1, so draw at size / ZOOM
const MAX_PARALLEL := 6

var data := {}
var _tex := {} # path -> Texture2D, or null while pending / missing
var _raw := {} # path -> PackedByteArray
var _raw_waiters := {} # path -> Array[Callable]
var _queue: Array[String] = []
var _busy := 0


func load_data() -> void:
	var req := HTTPRequest.new()
	add_child(req)
	req.request_completed.connect(func(result, code, _h, body):
		req.queue_free()
		if result != HTTPRequest.RESULT_SUCCESS or code != 200:
			data_failed.emit("Không tải được dữ liệu game (%d)" % code)
			return
		var d = JSON.parse_string(body.get_string_from_utf8())
		if d is Dictionary:
			data = d
			data_ready.emit()
		else:
			data_failed.emit("Dữ liệu game không hợp lệ"))
	req.request(Net.http_base() + "/api/data")


var _json := {} # url path -> parsed JSON
var _json_waiters := {} # url path -> Array[Callable]


## Fetches immutable JSON (maps by version, mobs) once; memory + disk cached.
func get_json(api_path: String, cb: Callable) -> void:
	if _json.has(api_path):
		cb.call(_json[api_path])
		return
	if _json_waiters.has(api_path):
		_json_waiters[api_path].append(cb)
		return
	_json_waiters[api_path] = [cb]
	var cached := "user://cache/api/" + api_path.replace("?", "_").replace("=", "_")
	if FileAccess.file_exists(cached):
		_on_json(api_path, FileAccess.get_file_as_string(cached), "")
		return
	var req := HTTPRequest.new()
	add_child(req)
	req.request_completed.connect(func(result, code, _h, body):
		req.queue_free()
		var text: String = body.get_string_from_utf8() if result == HTTPRequest.RESULT_SUCCESS and code == 200 else ""
		_on_json(api_path, text, cached))
	req.request(Net.http_base() + "/api/" + api_path)


func _on_json(api_path: String, text: String, store_at: String) -> void:
	var v = JSON.parse_string(text) if text != "" else null
	if v != null and store_at != "":
		DirAccess.make_dir_recursive_absolute(store_at.get_base_dir())
		var f := FileAccess.open(store_at, FileAccess.WRITE)
		if f:
			f.store_string(text)
	_json[api_path] = v
	for cb in _json_waiters.get(api_path, []):
		cb.call(v)
	_json_waiters.erase(api_path)


func char_frame(state: int, t: float) -> Dictionary:
	var anims: Array = data.get("char_anims", [[0]])
	var seq: Array = anims[clampi(state, 0, anims.size() - 1)]
	var frames: Array = data.get("char_frames", [])
	if frames.is_empty() or seq.is_empty():
		return {}
	var fps := 2.0 if state == 0 else 10.0
	return frames[int(seq[int(t * fps) % seq.size()])]


## Tile values of a tileset having the given flag bit (2 ground, 4/8 walls).
func tile_set_with(tileset: int, flag: int) -> Dictionary:
	var out := {}
	var flags: Dictionary = data.get("tile_flags", {}).get(str(tileset), {})
	for k in flags:
		if int(k) & flag:
			for v in flags[k]:
				out[int(v)] = true
	return out


## JSON arrays may arrive as null (empty Go slices); treat those as [].
func arr(v) -> Array:
	return v if v is Array else []


func part(id: int) -> Array:
	var p = data.get("parts", {}).get(str(id))
	return p if p is Array else []


## Returns the texture if ready, else null and starts loading it.
func tex(path: String) -> Texture2D:
	if _tex.has(path):
		return _tex[path]
	_tex[path] = null
	var cached := _cache_path(path)
	if FileAccess.file_exists(cached):
		_on_bytes(path, FileAccess.get_file_as_bytes(cached), false)
	else:
		_enqueue(path)
	return _tex[path]


func icon(id: int) -> Texture2D:
	return tex("icon/x%d/%d.png" % [ZOOM, id])


## Calls cb(bytes) once the raw file is available (used for binary mob data).
func raw(path: String, cb: Callable) -> void:
	if _raw.has(path):
		cb.call(_raw[path])
		return
	if _raw_waiters.has(path):
		_raw_waiters[path].append(cb)
		return
	_raw_waiters[path] = [cb]
	var cached := _cache_path(path)
	if FileAccess.file_exists(cached):
		_on_bytes(path, FileAccess.get_file_as_bytes(cached), false)
	else:
		_enqueue(path)


func _cache_path(path: String) -> String:
	return "user://cache/" + path


func _enqueue(path: String) -> void:
	_queue.append(path)
	_pump()


func _pump() -> void:
	while _busy < MAX_PARALLEL and not _queue.is_empty():
		var path: String = _queue.pop_front()
		_busy += 1
		var req := HTTPRequest.new()
		add_child(req)
		req.request_completed.connect(func(result, code, _h, body):
			req.queue_free()
			_busy -= 1
			if result == HTTPRequest.RESULT_SUCCESS and code == 200:
				_on_bytes(path, body, true)
			else:
				_on_bytes(path, PackedByteArray(), false)
			_pump())
		var url_path := "/".join(Array(path.split("/")).map(func(s): return s.uri_encode()))
		req.request(Net.http_base() + "/assets/" + url_path)


func _on_bytes(path: String, bytes: PackedByteArray, store: bool) -> void:
	if store and bytes.size() > 0:
		var cached := _cache_path(path)
		DirAccess.make_dir_recursive_absolute(cached.get_base_dir())
		var f := FileAccess.open(cached, FileAccess.WRITE)
		if f:
			f.store_buffer(bytes)
	if _raw_waiters.has(path):
		_raw[path] = bytes
		for cb in _raw_waiters[path]:
			cb.call(bytes)
		_raw_waiters.erase(path)
		return
	if bytes.size() == 0:
		texture_failed.emit(path) # stays null: missing asset
		return
	var img := Image.new()
	if img.load_png_from_buffer(bytes) == OK:
		_tex[path] = ImageTexture.create_from_image(img)
		texture_loaded.emit(path)
