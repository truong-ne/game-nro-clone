extends Node
## Template for feature plugins (copy, rename, implement handle()).
## world: the game_world node (world.hud, world.me, world.self_info, Net.send).

var world


func handle(_t: String, _d) -> bool:
	return false
