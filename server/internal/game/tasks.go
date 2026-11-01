package game

import (
	"context"
	"math/rand/v2"
	"regexp"
	"strconv"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"
)

// Task trigger kinds.
const (
	TrigTalk     = "talk"
	TrigKill     = "kill"
	TrigCollect  = "collect"
	TrigMap      = "map"
	TrigMove     = "move"
	TrigAddPoint = "add_point"
	TrigManual   = "manual" // not automatable yet: completed by talking to grandpa
)

type TaskSub struct {
	Name     string `json:"name"`
	MaxCount int    `json:"max"`
	Kind     string `json:"kind"`
	Npc      int    `json:"npc"`  // -2 = grandpa of the player's race
	Maps     []int  `json:"maps"` // per gender when 3 entries
	Mobs     []int  `json:"mobs"`
	Item     int    `json:"item"`
}

type TaskMain struct {
	ID     int        `json:"id"`
	Name   string     `json:"name"`
	Detail string     `json:"detail"`
	Subs   []*TaskSub `json:"subs"`
}

var (
	killRe    = regexp.MustCompile(`(?i)^(?:đánh|hạ|tiêu diệt|giết)\s+(\d+)\s+(?:con\s+)?(.+)$`)
	collectRe = regexp.MustCompile(`(?i)^(?:thu thập|nhặt|lấy)\s+(\d+)\s+(.+)$`)
	reportRe  = regexp.MustCompile(`(?i)^(?:báo cáo|nói chuyện) với`)
	gotoRe    = regexp.MustCompile(`(?i)^(?:đi đến|đến|tới|hãy đi đến)\s+(.+)$`)
)

// loadTasks reads main tasks and infers each subtask's trigger from its text
// ("Đánh 3 con khủng long mẹ" -> kill mob "khủng long mẹ" ×3). content.json
// overrides cover subtasks the text cannot describe.
func loadTasks(ctx context.Context, pool *pgxpool.Pool, d *Data) error {
	byID := map[int]*TaskMain{}
	rows, _ := pool.Query(ctx, `SELECT id, name, detail FROM task_main ORDER BY id`)
	for rows.Next() {
		t := &TaskMain{}
		if err := rows.Scan(&t.ID, &t.Name, &t.Detail); err != nil {
			return err
		}
		byID[t.ID] = t
		d.Tasks = append(d.Tasks, t)
	}
	mobByName := map[string]int{}
	for _, m := range d.Mobs {
		if _, dup := mobByName[strings.ToLower(m.Name)]; !dup {
			mobByName[strings.ToLower(m.Name)] = m.ID
		}
	}
	itemByName := map[string]int{}
	for _, it := range d.ItemsFull {
		if _, dup := itemByName[strings.ToLower(it.Name)]; !dup {
			itemByName[strings.ToLower(it.Name)] = it.ID
		}
	}
	mapByName := map[string]int{}
	for _, m := range d.Maps {
		mapByName[strings.ToLower(m.Name)] = m.ID
	}
	rows, _ = pool.Query(ctx, `SELECT task_main_id, name, max_count, npc_id, map_id FROM task_sub ORDER BY task_main_id, idx`)
	for rows.Next() {
		var tid, npc, mapID int
		s := &TaskSub{}
		if err := rows.Scan(&tid, &s.Name, &s.MaxCount, &npc, &mapID); err != nil {
			return err
		}
		s.Npc, s.Kind = npc, TrigManual
		name := strings.TrimSpace(strings.ReplaceAll(s.Name, "%2", ""))
		switch {
		case killRe.MatchString(name):
			m := killRe.FindStringSubmatch(name)
			if id, ok := mobByName[strings.ToLower(strings.TrimSpace(m[2]))]; ok {
				s.Kind, s.Mobs = TrigKill, []int{id}
			}
		case collectRe.MatchString(name):
			m := collectRe.FindStringSubmatch(name)
			if id, ok := itemByName[strings.ToLower(strings.TrimSpace(m[2]))]; ok {
				s.Kind, s.Item = TrigCollect, id
			}
		case reportRe.MatchString(name):
			s.Kind, s.Npc = TrigTalk, -2
		case gotoRe.MatchString(name):
			m := gotoRe.FindStringSubmatch(name)
			if id, ok := mapByName[strings.ToLower(strings.TrimSpace(m[1]))]; ok {
				s.Kind, s.Maps = TrigMap, []int{id}
			}
		case npc >= 0 || npc == -2:
			s.Kind = TrigTalk
		}
		if t := byID[tid]; t != nil {
			t.Subs = append(t.Subs, s)
		}
	}
	return rows.Err()
}

// applyTaskOverrides is called after content.json is loaded.
func (d *Data) applyTaskOverrides() {
	for key, o := range d.Content.Tasks.Overrides {
		ti, si, ok := strings.Cut(key, ".")
		if !ok {
			continue
		}
		t, _ := strconv.Atoi(ti)
		s, _ := strconv.Atoi(si)
		if t >= len(d.Tasks) || s >= len(d.Tasks[t].Subs) {
			continue
		}
		sub := d.Tasks[t].Subs[s]
		sub.Kind = o.Kind
		if o.Maps != nil {
			sub.Maps = o.Maps
		}
		if o.Npc != nil {
			sub.Npc = *o.Npc
		}
		if o.Mobs != nil {
			sub.Mobs = o.Mobs
		}
		if o.Item != 0 {
			sub.Item = o.Item
		}
	}
}

// taskFor resolves gender variants (tasks 4/5/6 are the same challenge in a
// different mob order per race).
func (w *World) taskFor(p *Player) (*TaskMain, *TaskSub) {
	d := w.Data
	id := p.Char.TaskID
	if v, ok := d.Content.Tasks.GenderVariants[strconv.Itoa(id)]; ok && p.Char.Gender < len(v) {
		id = v[p.Char.Gender]
	}
	if id < 0 || id >= len(d.Tasks) {
		return nil, nil
	}
	t := d.Tasks[id]
	if p.Char.TaskIndex >= len(t.Subs) {
		return t, nil
	}
	return t, t.Subs[p.Char.TaskIndex]
}

// nextTaskID skips the other gender variants after finishing one.
func (w *World) nextTaskID(cur int) int {
	for k, v := range w.Data.Content.Tasks.GenderVariants {
		base, _ := strconv.Atoi(k)
		if cur == base {
			return base + len(v)
		}
	}
	return cur + 1
}

func (w *World) grandpa(p *Player) int {
	g := w.Data.Content.GrandpaNpc
	if p.Char.Gender < len(g) {
		return g[p.Char.Gender]
	}
	return 0
}

type taskView struct {
	ID     int    `json:"id"`
	Name   string `json:"name"`
	Detail string `json:"detail"`
	Sub    string `json:"sub"`
	Count  int    `json:"count"`
	Max    int    `json:"max"`
}

func (w *World) sendTask(p *Player) {
	t, s := w.taskFor(p)
	if t == nil {
		p.Send("task", taskView{ID: -1, Name: "Đã hoàn thành mọi nhiệm vụ"})
		return
	}
	v := taskView{ID: t.ID, Name: t.Name, Detail: t.Detail}
	if s != nil {
		v.Sub, v.Count, v.Max = w.taskText(p, s.Name), p.Char.TaskCount, max(s.MaxCount, 1)
	}
	p.Send("task", v)
}

// taskText fills %2 (the player's grandpa NPC) in task strings.
func (w *World) taskText(p *Player, s string) string {
	return strings.ReplaceAll(s, "%2", npcName(w.Data, w.grandpa(p)))
}

func npcName(d *Data, id int) string {
	if n, ok := d.npcNames[id]; ok {
		return n
	}
	return "NPC"
}

// progress advances the current subtask by n if ev matches it.
func (w *World) progress(p *Player, match func(s *TaskSub) bool, n int) {
	_, s := w.taskFor(p)
	if s == nil || !match(s) {
		return
	}
	p.Char.TaskCount += n
	p.dirty = true
	if p.Char.TaskCount >= max(s.MaxCount, 1) {
		w.advanceTask(p)
		return
	}
	w.sendTask(p)
}

func (w *World) advanceTask(p *Player) {
	t, _ := w.taskFor(p)
	c := p.Char
	c.TaskIndex++
	c.TaskCount = 0
	if t != nil && c.TaskIndex >= len(t.Subs) {
		c.TaskID = w.nextTaskID(c.TaskID)
		c.TaskIndex = 0
		gold := w.Data.Content.Tasks.RewardGold * int64(t.ID+1)
		power := w.Data.Content.Tasks.RewardPower * int64(t.ID+1)
		c.Gold = min(c.Gold+gold, maxGold)
		p.gainPower(power)
		p.Send("notice", map[string]string{"text": "Hoàn thành " + t.Name + "! +" + strconv.FormatInt(gold, 10) + " vàng"})
		p.Send("self", p.Self())
	}
	p.dirty = true
	w.sendTask(p)
}

func containsInt(s []int, v int) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

// installTaskListeners wires tasks and achievements to the event bus.
func (w *World) installTaskListeners() {
	b := w.Events
	b.On(EvKillMob, func(e Event) {
		w.progress(e.Player, func(s *TaskSub) bool { return s.Kind == TrigKill && containsInt(s.Mobs, e.Mob) }, 1)
		w.taskItemDrop(e)
		w.achievement(e.Player, "kill_mob", 1)
	})
	b.On(EvKillBoss, func(e Event) { w.achievement(e.Player, "kill_boss", 1) })
	b.On(EvTalkNpc, func(e Event) {
		w.progress(e.Player, func(s *TaskSub) bool {
			if s.Kind != TrigTalk && s.Kind != TrigManual {
				return false
			}
			npc := s.Npc
			if npc < 0 {
				npc = w.grandpa(e.Player)
			}
			return npc == e.Npc
		}, 1<<20) // talking completes the subtask at once
	})
	b.On(EvEnterMap, func(e Event) {
		w.progress(e.Player, func(s *TaskSub) bool {
			if s.Kind != TrigMap {
				return false
			}
			if len(s.Maps) == 3 {
				return s.Maps[e.Player.Char.Gender] == e.Map
			}
			return containsInt(s.Maps, e.Map)
		}, 1<<20)
	})
	b.On(EvMove, func(e Event) {
		w.progress(e.Player, func(s *TaskSub) bool { return s.Kind == TrigMove }, 1<<20)
	})
	b.On(EvAddPoint, func(e Event) {
		w.progress(e.Player, func(s *TaskSub) bool { return s.Kind == TrigAddPoint }, 1<<20)
	})
	b.On(EvPickup, func(e Event) {
		_, s := w.taskFor(e.Player)
		if s != nil && s.Kind == TrigCollect && s.Item == e.Item {
			e.Player.Char.TaskCount = min(e.Player.CountItem(s.Item), s.MaxCount) - 1
			w.progress(e.Player, func(*TaskSub) bool { return true }, 1)
		}
	})
}

// taskItemDrop drops quest items (e.g. đùi gà) only for players on that task.
func (w *World) taskItemDrop(e Event) {
	_, s := w.taskFor(e.Player)
	if s == nil {
		return
	}
	for _, r := range w.Data.Content.Drops.TaskItems {
		if r.Task == e.Player.Char.TaskID && r.Sub == e.Player.Char.TaskIndex && containsInt(r.Mobs, e.Mob) &&
			rand.Float64()*100 < r.Chance {
			p := e.Player
			e.Zone.dropItem(r.Item, 1, nil, p.Char.X, p.Char.Y, p.ID())
		}
	}
}

type AchievementSpec struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Event     string `json:"event"`
	Count     int    `json:"count"`
	RewardGem int64  `json:"reward_gem"`
}

func (w *World) achievement(p *Player, event string, n int) {
	x := &p.Char.Extra
	for _, a := range w.Data.Content.Achievements {
		if a.Event != event || x.AchDone[a.ID] {
			continue
		}
		if x.Achievements == nil {
			x.Achievements = map[string]int{}
		}
		x.Achievements[a.ID] += n
		if x.Achievements[a.ID] >= a.Count {
			if x.AchDone == nil {
				x.AchDone = map[string]bool{}
			}
			x.AchDone[a.ID] = true
			p.Char.Gem += a.RewardGem
			p.Send("notice", map[string]string{"text": "Thành tựu: " + a.Name + " (+" + strconv.FormatInt(a.RewardGem, 10) + " ngọc)"})
		}
		p.dirty = true
	}
}
