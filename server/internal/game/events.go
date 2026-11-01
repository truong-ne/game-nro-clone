package game

// Event bus: gameplay code emits facts (killed a mob, talked to an NPC...) and
// systems such as tasks and achievements subscribe, instead of the legacy
// TaskService.checkDone* calls sprinkled through every service.
// Listeners run synchronously on the emitting zone goroutine.

type EventKind int

const (
	EvKillMob EventKind = iota
	EvKillBoss
	EvTalkNpc
	EvEnterMap
	EvLeaveMap
	EvPickup
	EvAddPoint
	EvMove
	EvUseItem
)

type Event struct {
	Kind    EventKind
	Player  *Player
	Zone    *Zone
	Mob     int // mob template id
	Npc     int // npc template id
	Map     int
	Item    int
	Count   int
	Boss    string
	BossRef *Boss // the killed boss (EvKillBoss)
}

type Bus struct{ listeners map[EventKind][]func(Event) }

func NewBus() *Bus { return &Bus{listeners: map[EventKind][]func(Event){}} }

func (b *Bus) On(k EventKind, f func(Event)) { b.listeners[k] = append(b.listeners[k], f) }

func (b *Bus) Emit(e Event) {
	if b == nil || e.Player == nil {
		return
	}
	for _, f := range b.listeners[e.Kind] {
		f(e)
	}
}
