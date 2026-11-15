package main

// Player memory: every persona has a persistent memory (data/memory/<account>.json) so it can behave like a
// person over days: a diary of what happened, the people it knows (party mates, whisperers, guildmates, the
// human players it ran into) with relationship notes, long-term goals, and free-form facts. Legion writes the
// diary automatically from game events; the LLM agents read it before talking and add notes/goals/relations
// through the API (GET/POST /api/memory/{name}).

import (
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

// memoryBlocked: names that never go into a player's memory or the friendship data (2026-11-14: 357 memories listed
// "AIBot", the old seed test bot - a mind mentioning it would give the world away). Test / watcher accounts and NPC
// speakers recorded before bot.go told NPC talk apart ("Resting Adventurer", "Trailmark").
var memoryBlocked = map[string]bool{"aibot": true, "observer": true, "resting adventurer": true, "trailmark": true}

// memoryNameOK: a real player name worth remembering - not a blocked test account / NPC, not a channel-prefixed
// speaker ("[Global] Ayla"), and no non-Legion name containing "bot".
func memoryNameOK(n string) bool {
	n = strings.TrimSpace(n)
	l := strings.ToLower(n)
	if n == "" || strings.HasPrefix(n, "[") || memoryBlocked[l] {
		return false
	}
	return !strings.Contains(l, "bot") || allChars[n]
}

// memoryClean (m.mu held): drop blocked people and the diary lines naming them. True if anything changed.
func (m *Memory) memoryClean() bool {
	changed := false
	var bad []string
	for n := range m.People {
		if !memoryNameOK(n) {
			delete(m.People, n)
			bad = append(bad, n)
			changed = true
		}
	}
	if len(bad) > 0 {
		d := m.Diary[:0]
		for _, e := range m.Diary {
			keep := true
			for _, n := range bad {
				if strings.Contains(e.Text, n) {
					keep = false
					break
				}
			}
			if keep {
				d = append(d, e)
			}
		}
		m.Diary = d
	}
	return changed
}

// memoryCleanSweep (startup): memories of this instance's accounts whose file names a blocked person are loaded, so
// memoryClean drops the entries and the next flush writes the cleaned file.
func memoryCleanSweep(bots []*Bot) {
	marks := []string{`"AIBot"`, `"Observer"`, `"[Global]`, `"[Trade]`, `"Resting Adventurer"`, `"Trailmark"`}
	n := 0
	for _, b := range bots {
		raw, err := os.ReadFile(filepath.Join(memDir, b.P.Account+".json"))
		if err != nil {
			continue
		}
		s := string(raw)
		for _, mk := range marks {
			if strings.Contains(s, mk) {
				memoryOf(b.P)
				n++
				break
			}
		}
	}
	if n > 0 {
		log.Printf("memory: cleaned blocked names (test accounts / NPCs) from %d memories", n)
	}
}

type Person struct {
	Relation string    `json:"relation"` // stranger, acquaintance, friend, partymate, guildmate, rival...
	Notes    string    `json:"notes,omitempty"`
	Met      int       `json:"times_met"`
	LastSeen time.Time `json:"last_seen"`
	LastMap  string    `json:"last_map,omitempty"`
	Human    bool      `json:"human,omitempty"` // a non-Legion player
}

type MemGoal struct {
	Text    string    `json:"text"`
	Status  string    `json:"status"` // active, done, dropped
	Created time.Time `json:"created"`
}

type DiaryEntry struct {
	T    time.Time `json:"t"`
	Text string    `json:"text"`
}

type Memory struct {
	mu       sync.Mutex
	Player   string             `json:"player"`
	Account  string             `json:"account"`
	Facts    []string           `json:"facts"`
	Progress map[string]string  `json:"progress,omitempty"` // character -> build plan status
	Goals    []MemGoal          `json:"goals"`
	People   map[string]*Person `json:"people"`
	Diary    []DiaryEntry       `json:"diary"`
	Visited  map[string]int     `json:"maps_visited"`
	Saved    map[string]string  `json:"saved_town,omitempty"` // character -> its respawn (save) town, as last seen
	Updated  time.Time          `json:"updated"`
	dirty    bool
}

var memories sync.Map // account -> *Memory
var memDir string

func memoryOf(p *Persona) *Memory {
	if v, ok := memories.Load(p.Account); ok {
		return v.(*Memory)
	}
	m := &Memory{Player: p.Name, Account: p.Account, People: map[string]*Person{}, Visited: map[string]int{}}
	if b, err := os.ReadFile(filepath.Join(memDir, p.Account+".json")); err == nil {
		json.Unmarshal(b, m)
		if m.People == nil {
			m.People = map[string]*Person{}
		}
		if m.Visited == nil {
			m.Visited = map[string]int{}
		}
		if m.memoryClean() {
			m.dirty = true
		}
	} else {
		m.Goals = initialGoals(p)
		for _, c := range p.Chars {
			if pl := builds[c.Plan]; pl != nil {
				m.Goals = append(m.Goals, MemGoal{Text: c.Name + ": " + pl.Summary + " (level 99)", Status: "active", Created: time.Now()})
			}
		}
		m.Facts = []string{fmt.Sprintf("I'm %d, from %s. %s.", p.Age, p.Country, p.Bio)}
		m.dirty = true
	}
	v, _ := memories.LoadOrStore(p.Account, m)
	return v.(*Memory)
}

func initialGoals(p *Persona) []MemGoal {
	now := time.Now()
	g := func(t string) MemGoal { return MemGoal{Text: t, Status: "active", Created: now} }
	var out []MemGoal
	switch p.Archetype {
	case "grinder":
		out = append(out, g("reach the next class as fast as possible"), g("find the best exp map for my level"))
	case "socialite":
		out = append(out, g("make friends and find a fun guild"), g("party with people instead of soloing"))
	case "merchant":
		out = append(out, g("save 1,000,000 zeny"), g("run a popular shop in Prontera"))
	case "explorer":
		out = append(out, g("visit every field and dungeon"), g("find a hidden map nobody talks about"))
	case "casual":
		out = append(out, g("level up a bit each evening"), g("get a nice-looking headgear"))
	case "hardcore":
		out = append(out, g("hit max level and third job"), g("be ready for War of Emperium"))
	case "newbie":
		out = append(out, g("figure out how this game works"), g("get my first job change"))
	case "helper":
		out = append(out, g("help new players"), g("level my support character"))
	}
	return out
}

func (m *Memory) setProgress(char, s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.Progress == nil {
		m.Progress = map[string]string{}
	}
	if m.Progress[char] != s {
		m.Progress[char] = s
		m.dirty = true
	}
}

func (m *Memory) add(text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Diary = append(m.Diary, DiaryEntry{T: time.Now(), Text: text})
	if len(m.Diary) > 300 {
		m.Diary = m.Diary[len(m.Diary)-300:]
	}
	m.Updated, m.dirty = time.Now(), true
}

func (m *Memory) meet(name, relation, mapName string, human bool) {
	if !memoryNameOK(name) {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	p := m.People[name]
	if p == nil {
		p = &Person{Relation: "stranger"}
		m.People[name] = p
	}
	if time.Since(p.LastSeen) > 10*time.Minute {
		p.Met++
	}
	p.LastSeen, p.LastMap, p.Human = time.Now(), mapName, human || p.Human
	if relation != "" && rank(relation) > rank(p.Relation) {
		p.Relation = relation
	}
	if p.Met >= 3 && p.Relation == "stranger" {
		p.Relation = "acquaintance"
	}
	m.Updated, m.dirty = time.Now(), true
}

func rank(r string) int {
	switch r {
	case "stranger":
		return 0
	case "acquaintance":
		return 1
	case "partymate":
		return 2
	case "guildmate":
		return 3
	case "friend":
		return 4
	}
	return 1
}

func (m *Memory) visit(mapName string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Visited[mapName]++
	m.dirty = true
}

// snapshot for the API: recent diary, everyone known (most recent first), goals, facts
func (m *Memory) snapshot(diary int) map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.Diary
	if len(d) > diary {
		d = d[len(d)-diary:]
	}
	type pe struct {
		Name string `json:"name"`
		*Person
	}
	var people []pe
	for n, p := range m.People {
		people = append(people, pe{n, p})
	}
	sort.Slice(people, func(i, j int) bool { return people[i].LastSeen.After(people[j].LastSeen) })
	if len(people) > 40 {
		people = people[:40]
	}
	return map[string]interface{}{"player": m.Player, "facts": m.Facts, "goals": m.Goals, "people": people, "progress": m.Progress,
		"diary": d, "maps_visited": len(m.Visited)}
}

func flushMemories() {
	memories.Range(func(k, v interface{}) bool {
		m := v.(*Memory)
		m.mu.Lock()
		if m.dirty {
			if b, err := json.Marshal(m); err == nil {
				os.WriteFile(filepath.Join(memDir, m.Account+".json"), b, 0644)
				m.dirty = false
			}
		}
		m.mu.Unlock()
		return true
	})
}

func memoryFlusher(stop <-chan struct{}) {
	os.MkdirAll(memDir, 0755)
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-stop:
			flushMemories()
			return
		case <-t.C:
			flushMemories()
		}
	}
}

// remember: diary entry for this bot's player, tagged with the character it happened on
func (b *Bot) remember(format string, a ...interface{}) {
	memoryOf(b.P).add(fmt.Sprintf("[%s] ", b.charName()) + fmt.Sprintf(format, a...))
}

// ensureGoal adds an active goal unless an identical one exists.
func (m *Memory) ensureGoal(text string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, g := range m.Goals {
		if g.Text == text {
			return
		}
	}
	m.Goals = append(m.Goals, MemGoal{Text: text, Status: "active", Created: time.Now()})
	m.dirty = true
}
