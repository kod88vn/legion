package main

// chatjobs.go (redesign package X, 2026-11-14): chat jobs. Wherever the engine speaks for a social event (ask, offer,
// accept, thanks, direction announced, duty warning, kick, leader banter) it can hand the line to a chat job instead
// of saying it at once. A job carries who speaks, on which channel, who hears it, the structured facts and the
// template line(s) the engine would say.
//
//   - chat_jobs_on false, or no chat agent polled GET /api/chat-jobs in the last 2 minutes: the template lines are said
//     at once (the world behaves exactly as without the feature).
//   - chat_jobs_on true and an agent polled recently: the job waits until its deadline (chat_job_wait_min_sec ..
//     chat_job_wait_max_sec, default 45-90 s; a listed job gets at least chat_job_listed_grace_sec more) for lines posted to POST /api/chat-jobs/{id}/lines; the posted lines are
//     said, else the template lines at the deadline.
//
// Nothing ever waits for a job: EnqueueChatJob returns at once (any lock may be held by the caller, it takes only the
// leaf lock chatJobs.mu), the bot carries on, and the line is said later by a goroutine (the resolver loop, or the
// API handler that received the agent's lines). Speakers are looked up by name when the line is said.
//
// Agent lines are checked: the speaker must be the job's speaker or one of its hearers and a character this instance
// runs; channels are the job's channels; text is one short line that never mentions bots / AI / the system; no more
// lines than the template plus one, and never more public (#trade / #global) lines than the template had - the
// public chat rate stays what the engine's rate limits allowed.
//
// Other packages use one helper (see docs/design/exchange.md):
//
//	EnqueueChatJob(ChatJobSpec{Kind: ChatKindDirection, Channel: "guild", Speaker: leader, Hearers: names,
//	    Facts: map[string]any{"direction": "stock", "item": "Red Potion"},
//	    Template: []ChatLine{{Speaker: leaderName, Channel: "guild", Text: "guild, we need red pots this week"}}})
//
// or the one-line form ChatJobLine(kind, speaker, channel, text, facts).

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"
)

// job kinds (free text is allowed; these are the ones the engine uses)
const (
	ChatKindAsk         = "ask"
	ChatKindOffer       = "offer"
	ChatKindAccept      = "accept"
	ChatKindThanks      = "thanks"
	ChatKindDirection   = "direction"
	ChatKindDutyWarning = "duty_warning"
	ChatKindKick        = "kick"
	ChatKindBanter      = "leader_banter"
)

const (
	chatAgentFresh   = 2 * time.Minute
	chatJobsMaxOpen  = 200
	chatJobsRecent   = 60
	chatLineMaxChars = 160
)

// ChatLine: one line of a conversation. DelaySec = pause before it (template sequences); To = whisper target.
type ChatLine struct {
	Speaker  string `json:"speaker"`
	Channel  string `json:"channel"` // party | guild | #trade | #global | local | whisper
	To       string `json:"to,omitempty"`
	Text     string `json:"text"`
	DelaySec int    `json:"delay_sec,omitempty"`
}

// ChatJobSpec: what a caller hands to EnqueueChatJob.
type ChatJobSpec struct {
	Kind     string
	Channel  string // the main channel (the first line's)
	To       string // whisper target of the main line
	Speaker  *Bot   // optional; Template[0].Speaker names the speaker otherwise
	Hearers  []string
	Group    string // the guild or party this ask is for ("guild:<id>" / "party:<id>"), for the dedupe
	Facts    map[string]any
	Template []ChatLine // first line: the speaker's; more lines = a scripted exchange (other speakers allowed)
}

type ChatJob struct {
	ID       string         `json:"id"`
	T        time.Time      `json:"t"`
	Kind     string         `json:"kind"`
	Channel  string         `json:"channel"`
	To       string         `json:"to,omitempty"`
	Speaker  string         `json:"speaker"`
	Hearers  []string       `json:"hearers,omitempty"`
	Group    string         `json:"group,omitempty"`
	Facts    map[string]any `json:"facts,omitempty"`
	Template string         `json:"template_line"`
	Lines    []ChatLine     `json:"template_lines"`
	Deadline time.Time      `json:"deadline"`
	State    string         `json:"state"` // waiting / agent / template / dropped
	Said     []ChatLine     `json:"said,omitempty"`
	Done     time.Time      `json:"done,omitempty"`
}

var chatJobs = struct {
	sync.Mutex
	open      []*ChatJob
	recent    []*ChatJob
	nextID    int64
	agentSeen time.Time
	counts    map[string]int64
	loop      sync.Once
}{counts: map[string]int64{}}

// chatSend says lines (replaced in tests). Runs in its own goroutine, no lock held.
var chatSend = chatSendLines

// chatJobsAutoLoop: EnqueueChatJob starts the resolver goroutine on first use (tests switch it off).
var chatJobsAutoLoop = true

func chatJobsOn() bool { return tune().ChatJobsOn }

func chatJobWait() time.Duration {
	lo, hi := tune().ChatJobWaitMinSec, tune().ChatJobWaitMaxSec
	if lo <= 0 {
		lo = 45
	}
	if hi < lo {
		hi = lo
	}
	return time.Duration(lo+crand(hi-lo+1)) * time.Second
}

// chatAgentPresent: a chat agent polled within the last 2 minutes.
func chatAgentPresent(now time.Time) bool {
	chatJobs.Lock()
	defer chatJobs.Unlock()
	return !chatJobs.agentSeen.IsZero() && now.Sub(chatJobs.agentSeen) <= chatAgentFresh
}

func chatJobPrefix() string {
	if coreInstance() {
		return "c"
	}
	return "m"
}

// ChatJobLine: the one-line form of EnqueueChatJob for a single line by `speaker`.
func ChatJobLine(kind string, speaker *Bot, channel, text string, facts map[string]any) string {
	if speaker == nil {
		return ""
	}
	return EnqueueChatJob(ChatJobSpec{Kind: kind, Channel: channel, Speaker: speaker, Facts: facts,
		Template: []ChatLine{{Speaker: speaker.charName(), Channel: channel, Text: text}}})
}

// EnqueueChatJob: the entry point for social lines. Returns the job id when it waits for a chat agent, "" when the
// template was said at once (no agent, flag off, or the queue is full); the id of the waiting ask when this one is a
// duplicate of it (same group, channel, hearers and item). Never blocks; safe with any lock held.
func EnqueueChatJob(s ChatJobSpec) string {
	return enqueueChatJobAt(s, time.Now())
}

func enqueueChatJobAt(s ChatJobSpec, now time.Time) string {
	if len(s.Template) == 0 {
		return ""
	}
	speaker := s.Template[0].Speaker
	if speaker == "" && s.Speaker != nil {
		speaker = s.Speaker.charName()
		s.Template[0].Speaker = speaker
	}
	if s.Channel == "" {
		s.Channel = s.Template[0].Channel
	}
	if s.To == "" {
		s.To = s.Template[0].To
	}
	for i := range s.Template {
		if s.Template[i].Channel == "" {
			s.Template[i].Channel = s.Channel
		}
	}
	j := &ChatJob{T: now, Kind: s.Kind, Channel: s.Channel, To: s.To, Speaker: speaker, Hearers: s.Hearers, Group: s.Group, Facts: s.Facts,
		Template: s.Template[0].Text, Lines: s.Template}
	chatJobs.Lock()
	wait := chatJobsOn() && !chatJobs.agentSeen.IsZero() && now.Sub(chatJobs.agentSeen) <= chatAgentFresh &&
		len(chatJobs.open) < chatJobsMaxOpen && !chatJobByCode(s.Kind) // chatjobs_wait.go: PR38 kinds the code says itself
	if wait && s.Kind == ChatKindAsk && chatJobDedupeOn() {
		if same := chatAskOpenLocked(j); same != nil {
			// the same ask (same group, channel, hearers, item) is already waiting: one ask is enough (bug queue
			// b1794750152316-420). Returns the waiting job's id, so the caller can tell it from "said at once" ("").
			chatJobs.counts["ask dropped: same ask waiting"]++
			chatJobs.Unlock()
			return same.ID
		}
	}
	chatJobs.nextID++
	j.ID = fmt.Sprintf("%s%d", chatJobPrefix(), chatJobs.nextID)
	if !wait {
		j.State, j.Said, j.Done = "template", j.Lines, now
		chatJobs.counts["template: "+chatWhyTemplate(now, j.Kind)]++
		chatJobs.counts["kind: "+j.Kind]++
		if chatJobsOn() {
			chatRecentLocked(j)
		}
		chatJobs.Unlock()
		go chatSend(j, j.Lines)
		return ""
	}
	j.State, j.Deadline = "waiting", now.Add(chatJobWait())
	chatJobs.open = append(chatJobs.open, j)
	chatJobs.counts["queued"]++
	chatJobs.counts["kind: "+j.Kind]++
	chatJobs.Unlock()
	if chatJobsAutoLoop {
		chatJobs.loop.Do(func() { go chatJobsLoop() })
	}
	return j.ID
}

func chatJobDedupeOn() bool { return tune().ChatJobDedupeOn }

// chatAskKey: two asks are the same ask when they are for the same group (guild or party), channel, whisper target,
// hearers and item (facts item_id, else the #trade line, else the template text). Asks of two groups never share a key.
func chatAskKey(j *ChatJob) string {
	k := strings.ToLower(strings.TrimSpace(j.Template))
	if id, ok := j.Facts["item_id"]; ok {
		k = fmt.Sprint(id)
	} else if l, ok := j.Facts["line"].(string); ok {
		k = strings.ToLower(strings.TrimSpace(l))
	}
	hs := make([]string, 0, len(j.Hearers))
	for _, h := range j.Hearers {
		hs = append(hs, strings.ToLower(h))
	}
	sort.Strings(hs)
	return j.Group + "|" + j.Channel + "|" + j.To + "|" + strings.Join(hs, ",") + "|" + k
}

// chatAskOpenLocked (chatJobs.mu held): the waiting ask with the same key, or nil.
func chatAskOpenLocked(j *ChatJob) *ChatJob {
	key := chatAskKey(j)
	for _, o := range chatJobs.open {
		if o.Kind == ChatKindAsk && chatAskKey(o) == key {
			return o
		}
	}
	return nil
}

// chatWhyTemplate (chatJobs.mu held): why a job went straight to its template (for the counters).
func chatWhyTemplate(now time.Time, kind string) string {
	switch {
	case !chatJobsOn():
		return "flag off"
	case chatJobByCode(kind):
		return "by code"
	case chatJobs.agentSeen.IsZero() || now.Sub(chatJobs.agentSeen) > chatAgentFresh:
		return "no agent"
	}
	return "queue full"
}

func chatRecentLocked(j *ChatJob) {
	chatJobs.recent = append(chatJobs.recent, j)
	if len(chatJobs.recent) > chatJobsRecent {
		chatJobs.recent = chatJobs.recent[len(chatJobs.recent)-chatJobsRecent:]
	}
}

// chatJobsLoop: says the template of every job past its deadline (and of every job when the agent went away).
func chatJobsLoop() {
	t := time.NewTicker(time.Second)
	defer t.Stop()
	for range t.C {
		for _, j := range chatJobsDue(time.Now()) {
			go chatSend(j, j.Lines)
		}
	}
}

// chatJobsDue: removes and returns the jobs whose template must be said now (deadline passed, or no agent polled for
// 2 minutes). Marks them "template".
func chatJobsDue(now time.Time) []*ChatJob {
	chatJobs.Lock()
	defer chatJobs.Unlock()
	gone := chatJobs.agentSeen.IsZero() || now.Sub(chatJobs.agentSeen) > chatAgentFresh
	var due []*ChatJob
	keep := chatJobs.open[:0]
	for _, j := range chatJobs.open {
		if now.After(j.Deadline) || gone {
			j.State, j.Said, j.Done = "template", j.Lines, now
			chatJobs.counts["template: timeout"]++
			chatRecentLocked(j)
			due = append(due, j)
			continue
		}
		keep = append(keep, j)
	}
	chatJobs.open = keep
	return due
}

// chatJobsPolled: the agent looked at the queue (GET /api/chat-jobs).
func chatJobsPolled(now time.Time) {
	chatJobs.Lock()
	chatJobs.agentSeen = now
	chatJobs.counts["polls"]++
	chatJobs.Unlock()
}

var reRevealing = regexp.MustCompile(`(?i)\b(bots?|ai|a\.i\.|llm|gpt|claude|legion|scripts?|scripted|automated|automation|programm?ed|algorithm|template|engine|agents?|simulation|npc controller|persona)\b`)

// chatLineOK: one agent line is short, single-line and never hints at bots / AI / the system.
func chatLineOK(text string) (string, error) {
	t := strings.TrimSpace(text)
	switch {
	case t == "":
		return "", fmt.Errorf("empty line")
	case strings.ContainsAny(t, "\r\n"):
		return "", fmt.Errorf("one line per entry")
	case len(t) > chatLineMaxChars:
		return "", fmt.Errorf("line longer than %d characters", chatLineMaxChars)
	case reRevealing.MatchString(t):
		return "", fmt.Errorf("line mentions something players must never hear (%q)", reRevealing.FindString(t))
	}
	return t, nil
}

func publicChannel(ch string) bool { return strings.HasPrefix(ch, "#") || ch == "local" }

// chatValidate: checks the agent's lines against the job (pure: knownChar tells whether this instance runs a name).
func chatValidate(j *ChatJob, lines []ChatLine, knownChar func(string) bool) ([]ChatLine, error) {
	if len(lines) > len(j.Lines)+1 {
		return nil, fmt.Errorf("at most %d lines for this job", len(j.Lines)+1)
	}
	allowedSpk := map[string]bool{strings.ToLower(j.Speaker): true}
	for _, h := range j.Hearers {
		allowedSpk[strings.ToLower(h)] = true
	}
	allowedCh := map[string]bool{}
	pubTmpl := 0
	for _, l := range j.Lines {
		allowedSpk[strings.ToLower(l.Speaker)] = true
		allowedCh[l.Channel] = true
		if publicChannel(l.Channel) {
			pubTmpl++
		}
	}
	allowedCh[j.Channel] = true
	pub := 0
	out := make([]ChatLine, 0, len(lines))
	for i, l := range lines {
		if l.Channel == "" {
			l.Channel = j.Channel
		}
		if l.Speaker == "" {
			l.Speaker = j.Speaker
		}
		if !allowedSpk[strings.ToLower(l.Speaker)] || !knownChar(l.Speaker) {
			return nil, fmt.Errorf("line %d: %s is not part of this conversation", i+1, l.Speaker)
		}
		if !allowedCh[l.Channel] {
			return nil, fmt.Errorf("line %d: channel %q is not one of this job's channels", i+1, l.Channel)
		}
		if l.Channel == "whisper" {
			if l.To == "" {
				l.To = j.To
				if strings.EqualFold(l.Speaker, j.To) {
					l.To = j.Speaker
				}
			}
			if l.To == "" || strings.EqualFold(l.To, l.Speaker) {
				return nil, fmt.Errorf("line %d: whisper without a partner", i+1)
			}
		}
		if publicChannel(l.Channel) {
			if pub++; pub > pubTmpl {
				return nil, fmt.Errorf("line %d: no more public lines than the template (%d)", i+1, pubTmpl)
			}
		}
		t, err := chatLineOK(l.Text)
		if err != nil {
			return nil, fmt.Errorf("line %d: %v", i+1, err)
		}
		l.Text = t
		if l.DelaySec <= 0 && i > 0 {
			l.DelaySec = 3 + crand(5)
		}
		l.DelaySec = min(l.DelaySec, 20)
		out = append(out, l)
	}
	return out, nil
}

// chatAnswer: the agent's lines for job id. Empty lines = "say the template now". Returns the job (resolved) or an
// error; the caller says the lines.
func chatAnswer(id string, lines []ChatLine, knownChar func(string) bool, now time.Time) (*ChatJob, []ChatLine, error) {
	chatJobs.Lock()
	defer chatJobs.Unlock()
	idx := -1
	for i, j := range chatJobs.open {
		if j.ID == id {
			idx = i
			break
		}
	}
	if idx < 0 {
		if late := chatJobLateLocked(id); late != nil { // chatjobs_wait.go: said by code at its deadline
			return late, nil, errChatLate
		}
		return nil, nil, fmt.Errorf("job %s is not waiting (already said or unknown)", id)
	}
	j := chatJobs.open[idx]
	use := j.Lines
	state := "template"
	if len(lines) > 0 {
		v, err := chatValidate(j, lines, knownChar)
		if err != nil {
			chatJobs.counts["agent lines refused"]++
			return nil, nil, err
		}
		use, state = v, "agent"
	}
	chatJobs.open = append(chatJobs.open[:idx], chatJobs.open[idx+1:]...)
	j.State, j.Said, j.Done = state, use, now
	if state == "agent" {
		chatJobs.counts["agent lines"]++
	} else {
		chatJobs.counts["template: agent passed"]++
	}
	chatRecentLocked(j)
	return j, use, nil
}

// chatSendLines says the lines one after the other (pauses from DelaySec). No lock held while sending.
func chatSendLines(j *ChatJob, lines []ChatLine) {
	for i, l := range lines {
		if i > 0 || l.DelaySec > 0 {
			d := l.DelaySec
			if d <= 0 {
				d = 3 + crand(4)
			}
			time.Sleep(time.Duration(d) * time.Second)
		}
		b := botByCharName(l.Speaker)
		if b == nil {
			continue
		}
		b.mu.Lock()
		ok := b.Online && b.charName() == l.Speaker
		b.mu.Unlock()
		if !ok {
			continue
		}
		switch l.Channel {
		case "party":
			b.partySay(l.Text)
		case "guild":
			b.guildSay(l.Text)
		case "whisper":
			if l.To != "" {
				b.whisper(l.To, l.Text)
			}
		case "local":
			b.say(l.Text)
		default:
			if strings.HasPrefix(l.Channel, "#") {
				b.channelSay(l.Channel, l.Text)
			}
		}
	}
}

// ---------------------------------------------------------------- API

func chatJobsReport(includeRecent bool) map[string]any {
	now := time.Now()
	chatJobs.Lock()
	open := make([]ChatJob, 0, len(chatJobs.open))
	for _, j := range chatJobs.open {
		open = append(open, *j)
	}
	var recent []ChatJob
	if includeRecent {
		for i := len(chatJobs.recent) - 1; i >= 0; i-- {
			recent = append(recent, *chatJobs.recent[i])
		}
	}
	counts := map[string]int64{}
	for k, v := range chatJobs.counts {
		counts[k] = v
	}
	seen := chatJobs.agentSeen
	chatJobs.Unlock()
	lo, hi := tune().ChatJobWaitMinSec, tune().ChatJobWaitMaxSec
	rep := map[string]any{"on": chatJobsOn(), "agent_present": !seen.IsZero() && now.Sub(seen) <= chatAgentFresh,
		"wait_sec": []int{lo, hi}, "jobs": open, "counts": counts,
		"how": "POST /api/chat-jobs/{id}/lines with [{speaker, channel, to?, text}] before the deadline; [] = say the template now"}
	if !seen.IsZero() {
		rep["agent_seen"] = seen.Format("15:04:05")
	}
	if includeRecent {
		rep["recent"] = recent
	}
	return rep
}

func registerChatJobs(mux *http.ServeMux) {
	mux.HandleFunc("/api/chat-jobs", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("peek") != "1" {
			chatJobsPolled(time.Now())
			chatJobsListed(time.Now()) // chatjobs_wait.go: the agent gets time to answer what it just listed
		}
		rep := chatJobsReport(q.Get("recent") == "1")
		if q.Get("local") != "1" {
			if base := coreAPIURL(); base != "" {
				vals := r.URL.Query()
				vals.Set("local", "1")
				if resp, err := coreClient.Get(base + "/api/chat-jobs?" + vals.Encode()); err == nil {
					var core map[string]any
					if json.NewDecoder(resp.Body).Decode(&core) == nil {
						if js, ok := core["jobs"].([]any); ok && len(js) > 0 {
							rep["jobs"] = append(toAnySlice(rep["jobs"]), js...)
						}
						rep["core"] = map[string]any{"counts": core["counts"], "agent_present": core["agent_present"], "recent": core["recent"]}
					}
					resp.Body.Close()
				}
			}
		}
		writeJSON(w, rep)
	})
	// POST /api/chat-jobs/{id}/lines  body: [{speaker, channel, to?, text}] or {"lines": [...]}
	mux.HandleFunc("/api/chat-jobs/", func(w http.ResponseWriter, r *http.Request) {
		rest := strings.TrimPrefix(r.URL.Path, "/api/chat-jobs/")
		parts := strings.Split(strings.Trim(rest, "/"), "/")
		if r.Method != http.MethodPost || len(parts) != 2 || parts[1] != "lines" || parts[0] == "" {
			http.Error(w, "POST /api/chat-jobs/{id}/lines", http.StatusBadRequest)
			return
		}
		id := parts[0]
		raw, err := io.ReadAll(io.LimitReader(r.Body, 64<<10))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if base := coreAPIURL(); base != "" && strings.HasPrefix(id, "c") && !coreInstance() {
			resp, err := coreClient.Post(base+r.URL.Path, "application/json", bytes.NewReader(raw))
			if err != nil {
				http.Error(w, "core instance unreachable: "+err.Error(), http.StatusBadGateway)
				return
			}
			defer resp.Body.Close()
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(resp.StatusCode)
			io.Copy(w, resp.Body)
			return
		}
		lines, err := parseChatLines(raw)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		chatJobsPolled(time.Now()) // answering is polling too
		j, use, err := chatAnswer(id, lines, func(n string) bool { return botByCharName(n) != nil }, time.Now())
		if err == errChatLate {
			writeJSON(w, chatLateReply(j)) // chatjobs_wait.go: too late, the fallback line was said; not an error
			return
		}
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusConflict)
			json.NewEncoder(w).Encode(map[string]any{"ok": false, "error": err.Error()})
			return
		}
		go chatSend(j, use)
		writeJSON(w, map[string]any{"ok": true, "id": j.ID, "state": j.State, "lines": len(use)})
	})
}

func parseChatLines(raw []byte) ([]ChatLine, error) {
	raw = bytes.TrimSpace(raw)
	if len(raw) == 0 {
		return nil, nil
	}
	var lines []ChatLine
	if raw[0] == '[' {
		if err := json.Unmarshal(raw, &lines); err != nil {
			return nil, fmt.Errorf("bad lines: %v", err)
		}
		return lines, nil
	}
	var wrap struct {
		Lines []ChatLine `json:"lines"`
	}
	if err := json.Unmarshal(raw, &wrap); err != nil {
		return nil, fmt.Errorf("bad lines: %v", err)
	}
	return wrap.Lines, nil
}

func toAnySlice(v any) []any {
	switch x := v.(type) {
	case []any:
		return x
	case []ChatJob:
		out := make([]any, 0, len(x))
		for _, j := range x {
			out = append(out, j)
		}
		return out
	}
	return nil
}
