package main

// Minds bus: the owner's window into the LLM "minds" (Claude sub-agents) and a way to talk to them.
//
//   POST /api/minds/{mind}/thought         {"text":"...","kind":"thought|decision|action|summary","status":"optional status line"}
//   GET  /api/minds                         every mind: last_seen, status (active = seen in last 3 min), last thought,
//                                           unread owner messages
//   GET  /api/minds/{mind}/thoughts?since=N[&limit=M]   recent thoughts of one mind ({mind}=all for every mind),
//                                           plus the owner's messages to it (with read status); poll with "next"
//   POST /api/minds/{mind}/message          {"text":"..."}  the owner sends an instruction (from the dashboard)
//   GET  /api/minds/{mind}/inbox[?ack=1]    owner messages the mind hasn't read yet; ack=1 marks them read
//
// Minds use it through legionctl (think / orders / minds). Both rings persist to <data>/minds.json every 30 s
// and on shutdown, and are reloaded at start.

import (
	"encoding/json"
	"log"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

const (
	mindThoughtCap = 500
	mindMessageCap = 500
	mindActiveFor  = 3 * time.Minute
	mindTextMax    = 2000
)

// Minds the session normally runs; listed even before they first check in so the owner can leave orders.
var knownMinds = []string{"ayla", "chat", "life", "market", "guild01", "guild02", "guild03", "guild04", "guild05",
	"guild06", "guild07", "guild08", "guild09", "guild10"}

type mindThought struct {
	ID   int64     `json:"id"`
	T    time.Time `json:"t"`
	Mind string    `json:"mind"`
	Kind string    `json:"kind"`
	Text string    `json:"text"`
}

type mindMessage struct {
	ID     int64      `json:"id"`
	T      time.Time  `json:"t"`
	Mind   string     `json:"mind"`
	Text   string     `json:"text"`
	Read   bool       `json:"read"`
	ReadAt *time.Time `json:"read_at,omitempty"`
}

type mindInfo struct {
	LastSeen   time.Time `json:"last_seen"`
	StatusLine string    `json:"status_line"`
}

type mindsBus struct {
	mu       sync.Mutex
	NextID   int64                `json:"next_id"`
	Thoughts []mindThought        `json:"thoughts"`
	Messages []mindMessage        `json:"messages"`
	Minds    map[string]*mindInfo `json:"minds"`
	dirty    bool
	path     string
}

var minds = &mindsBus{NextID: 1, Minds: map[string]*mindInfo{}}

func validMindName(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, c := range s {
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '_' || c == '-') {
			return false
		}
	}
	return true
}

func (m *mindsBus) load() {
	b, err := os.ReadFile(m.path)
	if err != nil {
		return
	}
	var st mindsBus
	if err := json.Unmarshal(b, &st); err != nil {
		log.Printf("minds: %s unreadable: %v", m.path, err)
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Thoughts, m.Messages = st.Thoughts, st.Messages
	if st.Minds != nil {
		m.Minds = st.Minds
	}
	if st.NextID > m.NextID {
		m.NextID = st.NextID
	}
	log.Printf("minds: loaded %d thoughts, %d owner messages", len(m.Thoughts), len(m.Messages))
}

func (m *mindsBus) save() {
	m.mu.Lock()
	if !m.dirty {
		m.mu.Unlock()
		return
	}
	b, err := json.Marshal(m)
	m.dirty = false
	m.mu.Unlock()
	if err != nil {
		return
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0644); err != nil {
		log.Printf("minds: save: %v", err)
		return
	}
	os.Rename(tmp, m.path)
}

func (m *mindsBus) touch(name string) *mindInfo {
	mi := m.Minds[name]
	if mi == nil {
		mi = &mindInfo{}
		m.Minds[name] = mi
	}
	mi.LastSeen = time.Now()
	m.dirty = true
	return mi
}

func (m *mindsBus) unread(name string) int {
	n := 0
	for i := range m.Messages {
		if m.Messages[i].Mind == name && !m.Messages[i].Read {
			n++
		}
	}
	return n
}

// registerMinds is called once from serveAPI.
func registerMinds(mux *http.ServeMux) {
	minds.path = filepath.Join(*dataDir, "minds.json")
	minds.load()
	go func() {
		t := time.NewTicker(30 * time.Second)
		sig := make(chan os.Signal, 1)
		signal.Notify(sig, syscall.SIGINT, syscall.SIGTERM)
		for {
			select {
			case <-t.C:
				minds.save()
			case <-sig:
				minds.save()
				return
			}
		}
	}()

	mux.HandleFunc("/api/minds", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, minds.list())
	})
	mux.HandleFunc("/api/minds/", func(w http.ResponseWriter, r *http.Request) {
		parts := strings.Split(strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/minds/"), "/"), "/")
		if len(parts) != 2 {
			http.Error(w, "use /api/minds/{mind}/thought|thoughts|message|inbox", 404)
			return
		}
		name, action := strings.ToLower(parts[0]), parts[1]
		if !validMindName(name) {
			http.Error(w, "bad mind name (a-z, 0-9, _ -)", 400)
			return
		}
		var body struct {
			Text   string `json:"text"`
			Kind   string `json:"kind"`
			Status string `json:"status"`
		}
		if r.Method == http.MethodPost {
			if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64<<10)).Decode(&body); err != nil {
				http.Error(w, "bad json: "+err.Error(), 400)
				return
			}
			body.Text = strings.TrimSpace(body.Text)
			if len(body.Text) > mindTextMax {
				body.Text = body.Text[:mindTextMax]
			}
		}
		switch {
		case action == "thought" && r.Method == http.MethodPost:
			if name == "all" || body.Text == "" {
				http.Error(w, "need a mind name and text", 400)
				return
			}
			writeJSON(w, minds.think(name, body.Kind, body.Text, body.Status))
		case action == "message" && r.Method == http.MethodPost:
			if name == "all" || body.Text == "" {
				http.Error(w, "need a mind name and text", 400)
				return
			}
			writeJSON(w, minds.message(name, body.Text))
		case action == "thoughts":
			since, _ := strconv.ParseInt(r.URL.Query().Get("since"), 10, 64)
			limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
			writeJSON(w, minds.thoughts(name, since, limit))
		case action == "inbox":
			writeJSON(w, minds.inbox(name, r.URL.Query().Get("ack") == "1"))
		default:
			http.Error(w, "unknown action or wrong method", 405)
		}
	})
}

var mindKinds = map[string]bool{"thought": true, "decision": true, "action": true, "summary": true, "ack": true}

func (m *mindsBus) think(name, kind, text, status string) mindThought {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if !mindKinds[kind] {
		kind = "thought"
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	t := mindThought{ID: m.NextID, T: time.Now(), Mind: name, Kind: kind, Text: text}
	m.NextID++
	m.Thoughts = append(m.Thoughts, t)
	if len(m.Thoughts) > mindThoughtCap {
		m.Thoughts = append([]mindThought(nil), m.Thoughts[len(m.Thoughts)-mindThoughtCap:]...)
	}
	mi := m.touch(name)
	if status = strings.TrimSpace(status); status != "" {
		mi.StatusLine = status
	} else if kind == "summary" || kind == "decision" || mi.StatusLine == "" {
		mi.StatusLine = text
	}
	if len(mi.StatusLine) > 200 {
		mi.StatusLine = mi.StatusLine[:200]
	}
	return t
}

func (m *mindsBus) message(name, text string) mindMessage {
	m.mu.Lock()
	defer m.mu.Unlock()
	msg := mindMessage{ID: m.NextID, T: time.Now(), Mind: name, Text: text}
	m.NextID++
	m.Messages = append(m.Messages, msg)
	if len(m.Messages) > mindMessageCap {
		m.Messages = append([]mindMessage(nil), m.Messages[len(m.Messages)-mindMessageCap:]...)
	}
	m.dirty = true
	return msg
}

func (m *mindsBus) thoughts(name string, since int64, limit int) map[string]interface{} {
	if limit <= 0 || limit > mindThoughtCap {
		limit = 100
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	th := []mindThought{}
	for i := len(m.Thoughts) - 1; i >= 0 && len(th) < limit; i-- {
		t := m.Thoughts[i]
		if t.ID <= since {
			break
		}
		if name == "all" || t.Mind == name {
			th = append(th, t)
		}
	}
	// oldest first, like /api/events
	for i, j := 0, len(th)-1; i < j; i, j = i+1, j-1 {
		th[i], th[j] = th[j], th[i]
	}
	msgs := []mindMessage{}
	for i := len(m.Messages) - 1; i >= 0 && len(msgs) < limit; i-- {
		if name == "all" || m.Messages[i].Mind == name {
			msgs = append(msgs, m.Messages[i])
		}
	}
	for i, j := 0, len(msgs)-1; i < j; i, j = i+1, j-1 {
		msgs[i], msgs[j] = msgs[j], msgs[i]
	}
	return map[string]interface{}{"mind": name, "thoughts": th, "messages": msgs, "next": m.NextID - 1}
}

func (m *mindsBus) inbox(name string, ack bool) map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	now := time.Now()
	out := []mindMessage{}
	for i := range m.Messages {
		msg := &m.Messages[i]
		if msg.Mind != name || msg.Read {
			continue
		}
		if ack {
			msg.Read, msg.ReadAt = true, &now
		}
		out = append(out, *msg)
	}
	m.touch(name) // checking orders counts as being alive
	return map[string]interface{}{"mind": name, "messages": out}
}

func (m *mindsBus) list() []map[string]interface{} {
	m.mu.Lock()
	defer m.mu.Unlock()
	names := map[string]bool{}
	for _, n := range knownMinds {
		names[n] = true
	}
	for n := range m.Minds {
		names[n] = true
	}
	for i := range m.Messages {
		names[m.Messages[i].Mind] = true
	}
	last := map[string]*mindThought{}
	count := map[string]int{}
	for i := range m.Thoughts {
		t := &m.Thoughts[i]
		last[t.Mind] = t
		count[t.Mind]++
	}
	out := []map[string]interface{}{}
	for n := range names {
		row := map[string]interface{}{"name": n, "status": "never", "unread": m.unread(n), "thoughts": count[n]}
		if mi := m.Minds[n]; mi != nil && !mi.LastSeen.IsZero() {
			row["last_seen"] = mi.LastSeen
			row["seen_ago_s"] = int(time.Since(mi.LastSeen).Seconds())
			row["status_line"] = mi.StatusLine
			if time.Since(mi.LastSeen) < mindActiveFor {
				row["status"] = "active"
			} else {
				row["status"] = "idle"
			}
		}
		if t := last[n]; t != nil {
			row["last_thought"] = t
		}
		out = append(out, row)
	}
	rank := map[string]int{"active": 0, "idle": 1, "never": 2}
	sort.Slice(out, func(i, j int) bool {
		a, b := rank[out[i]["status"].(string)], rank[out[j]["status"].(string)]
		if a != b {
			return a < b
		}
		return out[i]["name"].(string) < out[j]["name"].(string)
	})
	return out
}
