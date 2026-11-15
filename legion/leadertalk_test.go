package main

import (
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// member lines seen live (2026-11-15 01:58-03:04Z) and what the leader should make of them
func TestLeaderReplyKind(t *testing.T) {
	cases := map[string]string{
		"brb, buying arrows":                                     "errand",
		"getting goggles in prontera, back soon":                 "errand",
		"1 min, going to the refiner":                            "errand",
		"sec, upgrading my Axe":                                  "errand",
		"ok back :)":                                             "back",
		"Back, omw to u":                                         "back",
		"done, coming back":                                      "back",
		"need a tonw run, bag is heavy":                          "town",
		"town run? bag is heavy":                                 "town",
		"bag is heavy but it can wait for our next town run":     "town",
		"ok town run, i got stuff to sell too":                   "",
		"town run, omw":                                          "",
		"coming with u guys":                                     "",
		"sitting for sp, thanks!":                                "rest",
		"ok saved at prontera :D":                                "saved",
		"lost u, im at prt_fild05":                               "lost",
		"cant get to u, can u come to me?":                       "lost",
		"too strong here for me, gonna lvl a bit on my own. ty!": "leave",
		"gotta log soon, ty for the party":                       "leave",
		"omw":                                                    "",
		"ok":                                                     "",
		"nice pull":                                              "",
		"careful, 2 coming":                                      "",
	}
	for in, want := range cases {
		if got := leaderReplyKind(in); got != want {
			t.Errorf("leaderReplyKind(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestLeaderReplyLines(t *testing.T) {
	c := replyCtx{partyMap: "prt_fild05", leaderMap: "prt_fild05", lx: 100, ly: 120}
	for _, k := range []string{"leave", "town", "errand", "back", "rest", "lost", "saved"} {
		for i := 0; i < 20; i++ {
			if leaderReply(k, "Zen", c) == "" {
				t.Fatalf("no reply for %s", k)
			}
		}
	}
	for i := 0; i < 20; i++ {
		if l := leaderReply("lost", "Zen", c); !strings.Contains(l, "100,120") {
			t.Fatalf("lost reply without position: %q", l)
		}
	}
	fresh := c
	fresh.townRunFresh = true
	if l := leaderReply("town", "Zen", fresh); l != "" {
		t.Fatalf("a fresh town run is covered by the plan line, got %q", l)
	}
	if leaderReply("", "Zen", c) != "" {
		t.Fatal("unknown kind answered")
	}
}

func TestLeaderLines(t *testing.T) {
	for i := 0; i < 30; i++ {
		l := townRunPlanLine("Zen", "Lead", "prontera", "prt_fild05")
		if !strings.Contains(l, "prontera") || !strings.Contains(l, mapTalk("prt_fild05")) {
			t.Fatalf("town run plan misses where/back: %q", l)
		}
		l = townRunPlanLine("Lead", "Lead", "payon", "pay_fild01")
		if !strings.Contains(l, "payon") || strings.Contains(l, "Lead") {
			t.Fatalf("own town run plan: %q", l)
		}
		l = rallyWaitLine([]string{"Zen", "Bo"}, "geffen", 3*time.Minute)
		if !strings.Contains(l, "Zen and Bo") || !strings.Contains(l, "min") {
			t.Fatalf("rally wait: %q", l)
		}
		l = rallyMissLine([]string{"Zen"}, "gef_fild07")
		if !strings.Contains(l, "Zen") || !strings.Contains(l, mapTalk("gef_fild07")) {
			t.Fatalf("rally miss: %q", l)
		}
		if l = townRunEndLine("prt_fild05", []string{"Zen"}); !strings.Contains(l, "Zen") {
			t.Fatalf("late line: %q", l)
		}
		if dayEndLine(true, "prt_fild05") == "" || dayEndLine(false, "") == "" {
			t.Fatal("day end")
		}
	}
	if listNames([]string{"a", "b", "c"}) != "a, b and c" {
		t.Fatal(listNames([]string{"a", "b", "c"}))
	}
}

// --- the narrator on a fake bot party ---

type ltCapture struct {
	sync.Mutex
	lines []string
	wg    sync.WaitGroup // the narrator's delayed lines in flight (ltGo)
}

// settle: every line the narrator started so far has been said (no sleeping).
func (c *ltCapture) settle() { c.wg.Wait() }

func (c *ltCapture) said(sub string) bool {
	c.Lock()
	defer c.Unlock()
	for _, l := range c.lines {
		if strings.Contains(strings.ToLower(l), strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

func (c *ltCapture) n() int {
	c.Lock()
	defer c.Unlock()
	return len(c.lines)
}

func ltTestSetup(t *testing.T) (*ltCapture, func()) {
	c := &ltCapture{}
	oldSay, oldRand, oldUnit, oldGo := ltSay, ltRand, ltUnit, ltGo
	ltSay = func(b *Bot, line string) {
		c.Lock()
		c.lines = append(c.lines, b.charName()+" : "+line)
		c.Unlock()
	}
	ltRand = func() float64 { return 0 }
	ltUnit = 0 // no real delay: the delayed lines are tracked and settle() waits for them
	ltGo = func(f func()) {
		c.wg.Add(1)
		go func() {
			defer c.wg.Done()
			f()
		}()
	}
	return c, func() {
		c.settle()
		ltSay, ltRand, ltUnit, ltGo = oldSay, oldRand, oldUnit, oldGo
	}
}

func ltBot(name string) *Bot {
	return &Bot{P: &Persona{Name: "ltt_" + name, Chars: []CharSpec{{Name: name}}}, Online: true}
}

func ltPub(b *Bot, m string, x, y int, trip bool) {
	v := &atomic.Value{}
	v.Store(pubState{Name: b.charName(), Map: m, X: x, Y: y, Online: true, Trip: trip})
	pubs.Store(b.P.Name, v)
}

// waitFor: once everything the narrator started has been said, sub was among the lines.
func waitFor(t *testing.T, c *ltCapture, sub string) {
	c.settle()
	if !c.said(sub) {
		t.Fatalf("never said %q; lines %v", sub, c.lines)
	}
}

func TestLeaderTalkTownRun(t *testing.T) {
	c, done := ltTestSetup(t)
	defer done()
	l, a, b := ltBot("Lead"), ltBot("Ann"), ltBot("Bob")
	p := &Party{Name: "ltt pt", Leader: l, Members: []*Bot{l, a, b}, Map: "prt_fild05"}
	st := &ltState{}
	now := time.Now()
	ltPub(l, "prt_fild05", 100, 100, false)
	ltPub(a, "prt_fild05", 102, 100, false)
	ltPub(b, "prt_fild05", 104, 100, false)
	p.leaderTalkTick(st, now) // first look: nothing said
	p.leaderTalkTick(st, now.Add(4*time.Second))
	c.settle()
	if c.n() != 0 {
		t.Fatalf("said something with nothing going on: %v", c.lines)
	}
	// Ann calls a town run: the leader says where and how
	p.TownRunAt, p.TownRunTown, p.TownRunBy = now.Add(5*time.Second), "prontera", "Ann"
	p.leaderTalkTick(st, now.Add(8*time.Second))
	waitFor(t, c, "prontera")
	n := c.n()
	p.leaderTalkTick(st, now.Add(12*time.Second)) // once per decision
	c.settle()
	if c.n() != n {
		t.Fatalf("plan said twice: %v", c.lines)
	}
	// everyone goes, everyone comes back: "all back"
	ltPub(l, "prontera", 150, 150, true)
	ltPub(a, "prontera", 150, 151, true)
	ltPub(b, "prontera", 150, 152, true)
	p.leaderTalkTick(st, now.Add(60*time.Second))
	ltPub(l, "prt_fild05", 100, 100, false)
	ltPub(a, "prt_fild05", 101, 100, false)
	ltPub(b, "prt_fild05", 102, 100, false)
	p.leaderTalkTick(st, now.Add(4*time.Minute))
	c.settle()
	if c.n() != n+1 || st.trOpen {
		t.Fatalf("town run end not said once: %v", c.lines)
	}
}

func TestLeaderTalkTownRunLate(t *testing.T) {
	c, done := ltTestSetup(t)
	defer done()
	l, a := ltBot("Lead2"), ltBot("Slowpoke")
	p := &Party{Name: "ltt pt2", Leader: l, Members: []*Bot{l, a}, Map: "pay_fild01"}
	st := &ltState{}
	now := time.Now()
	ltPub(l, "pay_fild01", 100, 100, false)
	ltPub(a, "pay_fild01", 100, 101, false)
	p.leaderTalkTick(st, now)
	p.TownRunAt, p.TownRunTown, p.TownRunBy = now, "payon", "Lead2"
	p.leaderTalkTick(st, now.Add(4*time.Second))
	ltPub(a, "payon", 50, 50, true)
	p.leaderTalkTick(st, now.Add(30*time.Second))
	p.leaderTalkTick(st, now.Add(5*time.Minute)) // leader back, Slowpoke still shopping: no "all back"
	c.settle()
	if c.n() != 1 {
		t.Fatalf("all back while a member shops: %v", c.lines)
	}
	p.leaderTalkTick(st, now.Add(11*time.Minute))
	waitFor(t, c, "Slowpoke")
}

func TestLeaderTalkRally(t *testing.T) {
	c, done := ltTestSetup(t)
	defer done()
	l, a, b := ltBot("Lead3"), ltBot("Zed"), ltBot("Late")
	p := &Party{Name: "ltt pt3", Leader: l, Members: []*Bot{l, a, b}, Map: "gef_fild07"}
	st := &ltState{}
	now := time.Now()
	ltPub(l, "geffen", 120, 60, false)
	ltPub(a, "geffen", 121, 60, false)
	ltPub(b, "prt_fild05", 10, 10, false)
	p.leaderTalkTick(st, now)
	p.Rally = rallyState{Town: "geffen", X: 120, Y: 60, Dest: "gef_fild07", At: now.Add(time.Second)}
	p.leaderTalkTick(st, now.Add(30*time.Second))
	c.settle()
	if c.n() != 0 {
		t.Fatalf("wait line too early: %v", c.lines)
	}
	p.leaderTalkTick(st, now.Add(2*time.Minute))
	waitFor(t, c, "Late")
	if c.said("Zed") {
		t.Fatalf("named a member that is at the kafra: %v", c.lines)
	}
	n := c.n()
	p.leaderTalkTick(st, now.Add(2*time.Minute+4*time.Second))
	c.settle()
	if c.n() != n {
		t.Fatalf("wait said twice: %v", c.lines)
	}
	// the rally gives up: the late one is told where to come
	p.Rally = rallyState{Last: "abandoned after 5 min", LastAt: now.Add(5 * time.Minute)}
	p.leaderTalkTick(st, now.Add(5*time.Minute+4*time.Second))
	waitFor(t, c, mapTalk("gef_fild07"))
}

func TestLeaderHeard(t *testing.T) {
	c, done := ltTestSetup(t)
	defer done()
	l, a := ltBot("Lead4"), ltBot("Errandy")
	p := &Party{Name: "ltt pt4", Leader: l, Members: []*Bot{l, a}, Map: "prt_fild02"}
	parties.Lock()
	parties.ofBot[l], parties.ofBot[a] = p, p
	parties.Unlock()
	defer func() {
		parties.Lock()
		delete(parties.ofBot, l)
		delete(parties.ofBot, a)
		parties.Unlock()
	}()
	ltPub(l, "prt_fild02", 100, 100, false)
	leaderHeard(a, "Errandy : brb, buying arrows") // a member hearing it doesn't answer
	if c.n() != 0 {
		t.Fatalf("member answered: %v", c.lines)
	}
	leaderHeard(l, "Errandy : brb, buying arrows")
	if c.n() != 1 || !strings.HasPrefix(c.lines[0], "Lead4 : ") {
		t.Fatalf("leader didn't answer the errand: %v", c.lines)
	}
	leaderHeard(l, "Errandy : brb, buying arrows") // no spam
	leaderHeard(l, "Lead4 : town run guys")        // its own line
	leaderHeard(l, "Stranger : brb")               // not a member
	if c.n() != 1 {
		t.Fatalf("answered too much: %v", c.lines)
	}
	// a human-led party: the human answers, not a bot
	p.Human = "Ayla"
	st := ltStateOf(p)
	st.lastReply = time.Time{}
	leaderHeard(l, "Errandy : ok back")
	if c.n() != 1 {
		t.Fatalf("bot answered in a human-led party: %v", c.lines)
	}
}
